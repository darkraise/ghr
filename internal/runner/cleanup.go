package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// maxLogChunk bounds the log bytes of one RunnerLog response (file headers come
// on top); the cursor resumes where it stopped.
const maxLogChunk = 256 * 1024

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// projectsFile remembers the compose projects cleanup has seen: custom projects
// are found only through their containers, so a retry after those are removed
// must still reach the project's networks and volumes.
const projectsFile = "ghr-projects"

// errListProjects marks a cleanup that could not even list Docker containers,
// usually because the daemon is down; it must not count toward the give-up.
var errListProjects = errors.New("list compose containers")

// projects returns the compose projects of an instance: ghr-<id> plus every
// project whose working_dir label lies inside the instance dir, sorted.
func (m *Manager) projects(ctx context.Context, id string) ([]string, error) {
	dir := m.instanceDir(id)
	set := map[string]bool{"ghr-" + id: true}
	cc, err := m.Docker.ComposeContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errListProjects, err)
	}
	for _, c := range cc {
		if c.Project != "" && within(filepath.FromSlash(c.WorkingDir), dir) {
			set[c.Project] = true
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, projectsFile)); err == nil {
		for _, p := range strings.Fields(string(b)) {
			set[p] = true
		}
	}
	names := make([]string, 0, len(set))
	for p := range set {
		names = append(names, p)
	}
	sort.Strings(names)
	return names, nil
}

// cleanupDocker removes the compose projects started from an instance's work dir
// and the repo's prefix-matched containers. It returns how many containers it
// removed and every failure joined; the caller retries the whole cleanup on error.
func (m *Manager) cleanupDocker(ctx context.Context, cfg *config.Config, id, repo string) (int, error) {
	names, err := m.projects(ctx, id)
	if err != nil {
		return 0, err
	}
	known := filepath.Join(m.instanceDir(id), projectsFile)
	record := func() error {
		if err := writeFile(known, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("record compose projects: %w", err)
		}
		return nil
	}
	recordAgain := false
	if err := record(); err != nil {
		if !errors.Is(err, syscall.ENOSPC) {
			return 0, err
		}
		// Removing the projects is what frees space; record them once it has run.
		m.Events.Add("warn", repo, "%s: %v; removing them first", id, err)
		recordAgain = true
	}
	var errs []error
	removed := 0
	for _, p := range names {
		label := "com.docker.compose.project=" + p
		ids, err := m.Docker.ContainerIDsByLabel(ctx, label)
		if err != nil {
			errs = append(errs, fmt.Errorf("list containers of %s: %w", p, err))
		} else if err := m.Docker.RemoveContainers(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("remove containers of %s: %w", p, err))
		} else {
			removed += len(ids)
		}
		if err := m.Docker.RemoveNetworksByLabel(ctx, label); err != nil {
			errs = append(errs, fmt.Errorf("remove networks of %s: %w", p, err))
		}
		if err := m.Docker.RemoveVolumesByLabel(ctx, label); err != nil {
			errs = append(errs, fmt.Errorf("remove volumes of %s: %w", p, err))
		}
	}
	if recordAgain {
		if err := record(); err != nil {
			errs = append(errs, err)
		}
	}
	r := cfg.Repo(repo)
	if r != nil && len(r.CleanupNamePrefixes) > 0 {
		all, err := m.Docker.Containers(ctx)
		if err != nil {
			return removed, errors.Join(append(errs, fmt.Errorf("list containers: %w", err))...)
		}
		var ids []string
		for _, c := range all {
			for _, p := range r.CleanupNamePrefixes {
				// Config validation rejects blank prefixes; an empty one would match every container.
				if strings.TrimSpace(p) != "" && strings.HasPrefix(c.Name, p) {
					ids = append(ids, c.ID)
					break
				}
			}
		}
		if err := m.Docker.RemoveContainers(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("remove prefixed containers: %w", err))
		} else {
			removed += len(ids)
		}
	}
	return removed, errors.Join(errs...)
}

// RunnerContainers lists the containers of a live instance's compose projects.
func (m *Manager) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	m.mu.Lock()
	_, ok := m.insts[id]
	m.mu.Unlock()
	if !ok {
		return nil, ErrUnknownRunner(id)
	}
	names, err := m.projects(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []model.Container{}
	for _, p := range names {
		cs, err := m.Docker.ProjectContainers(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			out = append(out, model.Container{ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Project: p})
		}
	}
	return out, nil
}

// checkDisk prunes Docker build cache and dangling images above disk_high_water.
// A failed usage read keeps the last good measurement; every failure is reported.
func (m *Manager) checkDisk(ctx context.Context, cfg *config.Config) {
	pct, err := m.Docker.DataRootUsage(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return
	}
	m.setDisk(pct)
	if pct <= cfg.DiskHighWater {
		return
	}
	var notes []string
	freedOld, err := m.Docker.PruneBuildCacheOlderThan(ctx, 72)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune build cache older than 72h failed: %v", err))
		freedOld = "0B"
	}
	freedKeep := "0B"
	if mid, err := m.Docker.DataRootUsage(ctx); err != nil {
		notes = append(notes, fmt.Sprintf("disk usage after the 72h prune failed: %v", err))
	} else if mid > cfg.DiskHighWater {
		if freedKeep, err = m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep); err != nil {
			notes = append(notes, fmt.Sprintf("prune build cache to %s failed: %v", cfg.BuildCacheKeep, err))
			freedKeep = "0B"
		}
	}
	freedImages, err := m.Docker.PruneDanglingImages(ctx)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune dangling images failed: %v", err))
		freedImages = "0B"
	}
	now := "unknown"
	if after, err := m.Docker.DataRootUsage(ctx); err != nil {
		notes = append(notes, fmt.Sprintf("disk usage after pruning failed: %v", err))
	} else {
		m.setDisk(after)
		now = strconv.Itoa(after) + "%"
	}
	msg := fmt.Sprintf("disk %d%% > high-water %d%% — pruned build cache (%s older than 72h, %s to %s) and dangling images (%s); now %s",
		pct, cfg.DiskHighWater, freedOld, freedKeep, cfg.BuildCacheKeep, freedImages, now)
	if len(notes) > 0 {
		msg += "; " + strings.Join(notes, "; ")
	}
	m.Events.Add("warn", "", "%s", msg)
}

func (m *Manager) setDisk(pct int) {
	m.mu.Lock()
	m.diskPct = pct
	m.mu.Unlock()
}

// prune drops history lines and archived logs older than history_retention.
func (m *Manager) prune(cfg *config.Config, now time.Time) {
	cutoff := now.Add(-cfg.HistoryRetention.D())
	if err := m.History.Prune(cutoff); err != nil {
		m.Events.Add("warn", "", "history prune: %v", err)
	}
	entries, err := os.ReadDir(m.Paths.Logs)
	if err != nil && !os.IsNotExist(err) {
		m.Events.Add("warn", "", "log archive prune: %v", err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(m.Paths.Logs, e.Name())); err != nil {
				m.Events.Add("warn", "", "log archive prune: %v", err)
			}
		}
	}
	m.lastPrune = now
}

// parseCursor reads "file=offset&file=offset" (URL query encoding).
func parseCursor(cursor string) map[string]int64 {
	out := map[string]int64{}
	q, err := url.ParseQuery(cursor)
	if err != nil {
		return out
	}
	for name, vs := range q {
		if n, err := strconv.ParseInt(vs[0], 10, 64); err == nil && n >= 0 {
			out[name] = n
		}
	}
	return out
}

// lastLogKey is the cursor key naming the file the previous chunk ended in; log
// file names always match Runner_*.log or Worker_*.log, so it never collides.
const lastLogKey = "last"

// RunnerLog returns the runner's diagnostic logs (Runner_* then Worker_*) after
// cursor, read from the live instance or its archive. The Runner and Worker logs
// grow concurrently, so the cursor tracks one offset per file rather than one
// offset into their concatenation; archiving keeps file names, so it stays valid.
// A "==> file <==" header marks every switch to another file, including one
// across chunks, so a partial last line never runs into the next file's text.
func (m *Manager) RunnerLog(id, cursor string) (model.LogChunk, error) {
	if !idRe.MatchString(id) {
		return model.LogChunk{}, ErrUnknownRunner(id)
	}
	dir := filepath.Join(m.instanceDir(id), "_diag")
	if _, err := os.Stat(dir); err != nil {
		dir = filepath.Join(m.Paths.Logs, id)
		if _, err := os.Stat(dir); err != nil {
			return model.LogChunk{}, ErrUnknownRunner(id)
		}
	}
	var files []string
	for _, pat := range []string{"Runner_*.log", "Worker_*.log"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pat))
		sort.Strings(matches)
		files = append(files, matches...)
	}
	offsets := parseCursor(cursor)
	last := ""
	if q, err := url.ParseQuery(cursor); err == nil {
		last = q.Get(lastLogKey)
	}
	// Keep offsets of files this listing missed (the logs can move to the archive
	// mid-listing) so a later call does not replay them.
	next := url.Values{}
	for name, off := range offsets {
		next.Set(name, strconv.FormatInt(off, 10))
	}
	var data []byte
	for _, f := range files {
		name := filepath.Base(f)
		off := offsets[name]
		if budget := maxLogChunk - len(data); budget > 0 {
			if b, err := readFrom(f, off, budget); err == nil && len(b) > 0 {
				if name != last {
					if last != "" {
						data = append(data, '\n')
					}
					data = append(data, "==> "+name+" <==\n"...)
					last = name
				}
				data = append(data, b...)
				off += int64(len(b))
			}
		}
		next.Set(name, strconv.FormatInt(off, 10))
	}
	if last != "" {
		next.Set(lastLogKey, last)
	}
	return model.LogChunk{Data: string(data), Next: next.Encode()}, nil
}

func readFrom(path string, off int64, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	if err != nil {
		return nil, err
	}
	return trimPartialRune(b), nil
}

// trimPartialRune drops a trailing incomplete UTF-8 sequence (a chunk boundary or
// a half-flushed write) so the next call re-reads it whole; JSON would otherwise
// replace it with U+FFFD while the cursor skipped its bytes.
func trimPartialRune(b []byte) []byte {
	for i := 1; i < utf8.UTFMax && i <= len(b); i++ {
		if utf8.RuneStart(b[len(b)-i]) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			return b
		}
	}
	return b
}
