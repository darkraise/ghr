package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
)

// maxLogChunk bounds the log bytes of one RunnerLog response (file headers come
// on top); the cursor resumes where it stopped.
const maxLogChunk = 256 * 1024

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// errListProjects marks a cleanup that could not even list Docker containers,
// usually because the daemon is down; it must not count toward the give-up.
var errListProjects = errors.New("list compose containers")

// projects returns the compose projects of an instance, sorted: ghr-<id>,
// every project whose working_dir label lies inside the instance dir, and
// those an earlier cleanup recorded.
//
// The recorded list is read from the private directory, where the job cannot
// write, so a job cannot forge it. The labels remain the job's word: a job
// with Docker access can label a container with another project's name and a
// working_dir inside its instance dir, and cleanup, as root, then removes
// that project's containers, networks and volumes. That gives the job nothing
// new: the runner user is in the docker group and can remove any project
// itself.
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
	// Cleanup remembers the projects it has seen: custom projects are found
	// only through their containers, so a retry after those are removed must
	// still reach the project's networks and volumes.
	if b, err := os.ReadFile(m.privateProjects(id)); err == nil {
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

// dockerInstanceLabel returns the label the actions runner puts on the job and
// service containers it creates and on the job network: a bare key of the
// first six hex digits of the SHA-256 of its lower-cased .runner file. It
// returns "" when the runner never wrote the file, and then created nothing.
// Containers a job starts itself with docker run do not carry it.
func (m *Manager) dockerInstanceLabel(id string) (string, error) {
	data, err := readJobFile(filepath.Join(m.instanceDir(id), ".runner"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read .runner: %w", err)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(string(data))))
	return fmt.Sprintf("%x", sum[:3]), nil
}

// cleanupDocker removes the compose projects started from an instance's work dir,
// the containers and network the runner itself created, and the repo's
// prefix-matched containers. It returns how many containers it removed and
// every failure joined; the caller retries the whole cleanup on error.
func (m *Manager) cleanupDocker(ctx context.Context, cfg *config.Config, id, repo string) (int, error) {
	names, err := m.projects(ctx, id)
	if err != nil {
		return 0, err
	}
	record := func() error {
		err := os.MkdirAll(m.Paths.Private, 0o700)
		if err == nil {
			err = writeFile(m.privateProjects(id), []byte(strings.Join(names, "\n")+"\n"), 0o600)
		}
		if err != nil {
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
	if label, err := m.dockerInstanceLabel(id); err != nil {
		errs = append(errs, err)
	} else if label != "" {
		ids, err := m.Docker.ContainerIDsByLabel(ctx, label)
		if err != nil {
			errs = append(errs, fmt.Errorf("list containers of the runner: %w", err))
		} else if err := m.Docker.RemoveContainers(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("remove containers of the runner: %w", err))
		} else {
			removed += len(ids)
		}
		if err := m.Docker.RemoveNetworksByLabel(ctx, label); err != nil {
			errs = append(errs, fmt.Errorf("remove networks of the runner: %w", err))
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

// diskMeasureTimeout bounds the tick's own disk reading (docker info and df),
// so a Docker daemon that does not answer costs a tick seconds, not the
// command timeout.
var diskMeasureTimeout = 15 * time.Second

// measureDisk reads the Docker data root's usage and reports whether it is
// above disk_high_water. A failed read keeps the last good measurement.
func (m *Manager) measureDisk(ctx context.Context, cfg *config.Config) (pct int, over bool) {
	ctx, cancel := context.WithTimeout(ctx, diskMeasureTimeout)
	defer cancel()
	u, err := m.dataRoot(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return 0, false
	}
	m.setDisk(u)
	return u.Pct, u.Pct > cfg.DiskHighWater
}

// pruneDisk prunes Docker build cache and dangling images after measureDisk
// found the disk at pct, above disk_high_water. Every failure is reported.
func (m *Manager) pruneDisk(ctx context.Context, cfg *config.Config, pct int) {
	started := m.Now()
	m.mu.Lock()
	m.lastPruneRec = &model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: started, Steps: []model.PruneStep{}}
	m.mu.Unlock()
	defer m.pruneDone()
	var notes []string
	freedOld, err := m.Docker.PruneBuildCacheOlderThan(ctx, 72)
	m.recordStep("build cache older than 72h", freedOld, err)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune build cache older than 72h failed: %v", err))
		freedOld = "0B"
	}
	freedKeep := "0B"
	if mid, err := m.Docker.DataRootUsage(ctx); err != nil {
		m.recordStep("disk usage", "", err)
		notes = append(notes, fmt.Sprintf("disk usage after the 72h prune failed: %v", err))
	} else if mid > cfg.DiskHighWater {
		freedKeep, err = m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep)
		m.recordStep("build cache to "+cfg.BuildCacheKeep, freedKeep, err)
		if err != nil {
			notes = append(notes, fmt.Sprintf("prune build cache to %s failed: %v", cfg.BuildCacheKeep, err))
			freedKeep = "0B"
		}
	}
	freedImages, err := m.Docker.PruneDanglingImages(ctx)
	m.recordStep("dangling images", freedImages, err)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune dangling images failed: %v", err))
		freedImages = "0B"
	}
	nowPct := "unknown"
	if after, err := m.dataRoot(ctx); err != nil {
		m.recordStep("disk usage", "", err)
		notes = append(notes, fmt.Sprintf("disk usage after pruning failed: %v", err))
	} else {
		m.setDisk(after)
		nowPct = strconv.Itoa(after.Pct) + "%"
	}
	outcome := "ok"
	switch {
	case ctx.Err() != nil:
		outcome = "interrupted"
	case len(notes) > 0:
		outcome = "errors"
	}
	m.finishPruneRecord(outcome)
	msg := fmt.Sprintf("disk %d%% > high-water %d%% — pruned build cache (%s older than 72h, %s to %s) and dangling images (%s); now %s",
		pct, cfg.DiskHighWater, freedOld, freedKeep, cfg.BuildCacheKeep, freedImages, nowPct)
	if len(notes) > 0 {
		msg += "; " + strings.Join(notes, "; ")
	}
	m.Events.Add("warn", "", "%s", msg)
}

// recordStep appends one step to the prune record in progress; freed is
// Docker's "Total reclaimed space", 0 when absent or unreadable.
func (m *Manager) recordStep(name, freed string, err error) {
	st := model.PruneStep{Name: name}
	if err != nil {
		st.Error = err.Error()
	} else if n, perr := system.ParseSize(freed); perr == nil {
		st.Freed = n
	}
	m.mu.Lock()
	if m.lastPruneRec != nil {
		m.lastPruneRec.Steps = append(m.lastPruneRec.Steps, st)
	}
	m.mu.Unlock()
}

func (m *Manager) finishPruneRecord(outcome string) {
	now := m.Now()
	m.mu.Lock()
	if m.lastPruneRec != nil {
		m.lastPruneRec.FinishedAt = &now
		m.lastPruneRec.Outcome = outcome
	}
	m.mu.Unlock()
}

func (m *Manager) pruneDone() {
	if m.PruneDone != nil {
		m.PruneDone()
	}
}

func (m *Manager) setDisk(u system.DiskUsage) {
	m.mu.Lock()
	m.disk = u
	m.mu.Unlock()
}

// diskBytes is a Docker that can also report the data root in bytes. The
// check is optional so the Docker interface and its fakes stay as they are.
type diskBytes interface {
	DataRootBytes(ctx context.Context) (system.DiskUsage, error)
}

// dataRoot measures the data-root filesystem, with bytes when the Docker
// can report them.
func (m *Manager) dataRoot(ctx context.Context) (system.DiskUsage, error) {
	if d, ok := m.Docker.(diskBytes); ok {
		return d.DataRootBytes(ctx)
	}
	pct, err := m.Docker.DataRootUsage(ctx)
	return system.DiskUsage{Pct: pct}, err
}

// prune drops history lines and archived logs older than history_retention.
// It reports whether it removed anything and whether every step succeeded.
func (m *Manager) prune(cfg *config.Config, now time.Time) (removed, ok bool) {
	ok = true
	cutoff := now.Add(-cfg.HistoryRetention.D())
	// The store counts a prune only when it rewrote the file, which it does
	// only when a line went. An entry appended meanwhile is counted as well,
	// so removed can be true when nothing went; it is never false when
	// something did.
	writes := m.History.Writes()
	if err := m.History.Prune(cutoff); err != nil {
		ok = false
		m.Events.Add("warn", "", "history prune: %v", err)
	}
	removed = m.History.Writes() != writes
	entries, err := os.ReadDir(m.Paths.Logs)
	if err != nil && !os.IsNotExist(err) {
		ok = false
		m.Events.Add("warn", "", "log archive prune: %v", err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(m.Paths.Logs, e.Name())); err != nil {
				ok = false
				m.Events.Add("warn", "", "log archive prune: %v", err)
			} else {
				removed = true
			}
		}
	}
	m.mu.Lock()
	m.lastPrune = now
	m.mu.Unlock()
	return removed, ok
}

// forcedPrune is a manual prune of one scope. standard prunes build cache
// down to build_cache_keep and dangling images, then history and logs past
// retention; every scope ends with a fresh disk reading. Unlike checkDisk it
// ignores disk_high_water.
func (m *Manager) forcedPrune(ctx context.Context, scope string) {
	cfg := m.Config()
	failed := false
	step := func(name string, fn func() (string, error)) {
		if ctx.Err() != nil {
			return
		}
		freed, err := fn()
		m.recordStep(name, freed, err)
		if err != nil {
			failed = true
			m.Events.Add("warn", "", "prune: %s failed: %v", name, err)
			return
		}
		m.Events.Add("info", "", "prune: %s freed %s", name, freed)
	}
	keep := func() (string, error) { return m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep) }
	images := func() (string, error) { return m.Docker.PruneDanglingImages(ctx) }
	switch scope {
	case ScopeStandard:
		step("build cache to "+cfg.BuildCacheKeep, keep)
		step("dangling images", images)
	case ScopeBuildCacheKeep:
		step("build cache to "+cfg.BuildCacheKeep, keep)
	case ScopeBuildCacheAll:
		step("all build cache", func() (string, error) { return m.Disk.PruneAllBuildCache(ctx) })
	case ScopeDanglingImages:
		step("dangling images", images)
	case ScopeUnusedVolumes:
		step("unused volumes", func() (string, error) { return m.Disk.PruneUnusedVolumes(ctx) })
	}
	if scope == ScopeStandard && ctx.Err() == nil {
		if removed, ok := m.prune(cfg, m.Now()); ok {
			m.recordStep("history and logs past retention", "0B", nil)
			if removed {
				m.Events.Add("info", "", "prune: history and logs past retention removed")
			}
		} else {
			failed = true
			m.recordStep("history and logs past retention", "", errors.New("failed; see the warnings before this event"))
		}
	}
	if ctx.Err() == nil {
		if u, err := m.dataRoot(ctx); err != nil {
			failed = true
			m.recordStep("disk usage", "", err)
			m.Events.Add("warn", "", "prune: disk usage: %v", err)
		} else {
			m.setDisk(u)
			m.Events.Add("info", "", "prune: disk %d%% used", u.Pct)
		}
	}
	outcome, level, msg := "ok", "ok", "prune finished"
	switch {
	case ctx.Err() != nil:
		outcome, level, msg = "interrupted", "warn", "prune interrupted by shutdown"
	case failed:
		outcome, level, msg = "errors", "warn", "prune finished with errors"
	}
	now := m.Now()
	m.mu.Lock()
	m.pruning = false
	m.maint.Running = false
	m.maint.LastFinished = &now
	m.maint.LastOutcome = outcome
	// Finished under the same lock that releases the reservation: once it is
	// released, the next prune replaces lastPruneRec.
	if m.lastPruneRec != nil {
		m.lastPruneRec.FinishedAt = &now
		m.lastPruneRec.Outcome = outcome
	}
	m.mu.Unlock()
	m.Events.Add(level, "", "%s", msg)
	m.pruneDone()
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

// logPatterns match the runner's diagnostic logs, the only files of _diag
// that RunnerLog reads and an archive keeps.
var logPatterns = []string{"Runner_*.log", "Worker_*.log"}

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
	root, err := m.openLogs(id)
	if err != nil {
		return model.LogChunk{}, ErrUnknownRunner(id)
	}
	defer root.Close()
	var files []string
	for _, pat := range logPatterns {
		matches, _ := fs.Glob(root.FS(), pat)
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
		name := f
		off := offsets[name]
		if budget := maxLogChunk - len(data); budget > 0 {
			if b, err := readFrom(root, name, off, budget); err == nil && len(b) > 0 {
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

// openLogs opens the directory that holds a runner's diagnostic logs: the
// live instance's _diag, or its archive. The job wrote these files and
// RunnerLog reads them as root, so the log directory itself is opened as an
// os.Root and every read goes through it: the root keeps the directory it
// opened even if its name is swapped afterwards, and refuses any path that
// leaves it through a symlink. A _diag that is a symlink is refused.
func (m *Manager) openLogs(id string) (*os.Root, error) {
	if inst, err := os.OpenRoot(m.instanceDir(id)); err == nil {
		defer inst.Close()
		if fi, err := inst.Lstat("_diag"); err == nil {
			if !fi.IsDir() {
				return nil, fs.ErrInvalid
			}
			logs, err := inst.OpenRoot("_diag")
			if err == nil || !errors.Is(err, fs.ErrNotExist) {
				return logs, err
			}
		}
	}
	dir := filepath.Join(m.Paths.Logs, id)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil, fs.ErrNotExist
	}
	return os.OpenRoot(dir)
}

// trimArchive removes from an archived _diag everything but its regular
// files named by logPatterns, and cuts each of those to its first
// maxArchivedLog bytes. The job filled the directory, so it works through an
// os.Root, which follows no symlink out of it.
func trimArchive(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !isLogName(name) {
			if err := root.RemoveAll(name); err != nil {
				return err
			}
			continue
		}
		if err := truncateLog(root, name); err != nil {
			return err
		}
	}
	return nil
}

func isLogName(name string) bool {
	for _, pat := range logPatterns {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

func truncateLog(root *os.Root, name string) error {
	fi, err := root.Lstat(name)
	if err != nil || fi.Size() <= maxArchivedLog {
		return err
	}
	f, err := root.OpenFile(name, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", name)
	}
	return f.Truncate(maxArchivedLog)
}

// readFrom reads up to limit bytes of a log from off. Only a regular file is
// read: a symlink is skipped, and a FIFO or a device would block the reader
// or feed it for ever. The check is repeated on the open file, because the
// job can swap the name between the two; whatever it swaps in, the root
// lets the open reach only a file inside the log directory.
func readFrom(root *os.Root, name string, off int64, limit int) ([]byte, error) {
	if fi, err := root.Lstat(name); err != nil || !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
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
