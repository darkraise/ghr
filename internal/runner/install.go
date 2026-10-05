package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// stepError names the update step that failed.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

const tarballURL = "https://github.com/actions/runner/releases/download/v%[1]s/actions-runner-linux-x64-%[1]s.tar.gz"

// complete reports whether dir is a finished install: setup.sh and the update
// publish a dist dir only after its dependency script succeeded, so an
// executable run.sh means the version is complete.
func complete(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "run.sh"))
	// Windows has no execute bit; ghr runs on Linux, its tests on Windows too.
	return err == nil && fi.Mode().IsRegular() && (fi.Mode().Perm()&0o111 != 0 || runtime.GOOS == "windows")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// installLatest installs the latest runner release and points dist/current
// at it. It returns the version now current and "ok", or "current" when the
// installed runner was already the latest. A failure is a *stepError and
// leaves dist/current untouched.
func (m *Manager) installLatest(ctx context.Context) (string, string, error) {
	rels, err := m.checkRelease(ctx)
	if err != nil {
		return "", "", &stepError{"check", err}
	}
	m.mu.Lock()
	installed, newer := m.upd.installed, !m.upd.deadline.IsZero()
	m.mu.Unlock()
	switch {
	case installed == "":
		return "", "", &stepError{"check", ErrNoDist}
	case !newer:
		return installed, "current", nil
	}
	f, _ := factsFor(installed, rels)
	ver := f.version.String()
	sum, ok := f.latest.LinuxX64SHA256()
	if !ok {
		return "", "", &stepError{"checksum", fmt.Errorf("no linux-x64 checksum in the v%s release notes", ver)}
	}
	distDir := filepath.Dir(m.Paths.Dist)
	dir := filepath.Join(distDir, ver)
	if !complete(dir) {
		if err := m.stage(ctx, distDir, ver, sum); err != nil {
			return "", "", err
		}
	}
	// Switching current commits the update; one cancelled before it leaves the
	// installed runner as it was.
	if err := ctx.Err(); err != nil {
		return "", "", &stepError{"switch", err}
	}
	if err := m.Host.SwitchLink(dir, m.Paths.Dist); err != nil {
		return "", "", &stepError{"switch", err}
	}
	nf, _ := factsFor(ver, rels)
	m.mu.Lock()
	m.upd.installed, m.upd.deadline, m.upd.gen = ver, nf.deadline, m.upd.gen+1
	m.mu.Unlock()
	m.removeOldVersions(distDir, ver)
	return ver, "ok", nil
}

// stage downloads, verifies, unpacks and prepares version ver in
// dist/<ver>.tmp, then renames it to dist/<ver>. Every failure removes the
// staging dir.
func (m *Manager) stage(ctx context.Context, distDir, ver, sum string) error {
	leftovers, _ := filepath.Glob(filepath.Join(distDir, "*.tmp"))
	for _, l := range leftovers {
		if err := removeAll(l); err != nil {
			return &stepError{"download", fmt.Errorf("remove leftover %s: %w", filepath.Base(l), err)}
		}
	}
	staging := filepath.Join(distDir, ver+".tmp")
	fail := func(step string, err error) error {
		os.RemoveAll(staging)
		return &stepError{step, err}
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fail("download", err)
	}
	tarball := filepath.Join(staging, "runner.tar.gz")
	if err := m.Fetch(ctx, fmt.Sprintf(tarballURL, ver), tarball); err != nil {
		return fail("download", err)
	}
	got, err := fileSHA256(tarball)
	if err != nil {
		return fail("checksum", err)
	}
	if got != sum {
		return fail("checksum", fmt.Errorf("SHA-256 mismatch: the release notes say %s, the download is %s", sum, got))
	}
	if err := m.Host.Extract(ctx, tarball, staging); err != nil {
		return fail("extract", err)
	}
	if err := os.Remove(tarball); err != nil {
		return fail("extract", err)
	}
	if err := m.Host.RunScript(ctx, staging, "bin/installdependencies.sh"); err != nil {
		return fail("dependencies", err)
	}
	final := filepath.Join(distDir, ver)
	if err := os.RemoveAll(final); err != nil {
		return fail("install", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fail("install", err)
	}
	return nil
}

// removeAll is os.RemoveAll; tests replace it to make a removal fail.
var removeAll = os.RemoveAll

// removeOldVersions deletes every dist dir but ver. Live runners keep
// working: each instance copied the runner files when it was spawned. It runs
// after current switched, so a failure is a warning, not a failed update.
func (m *Manager) removeOldVersions(distDir, ver string) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		m.Events.Add("warn", "", "runner update: list %s: %v", distDir, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ver {
			continue
		}
		if err := removeAll(filepath.Join(distDir, e.Name())); err != nil {
			m.Events.Add("warn", "", "runner update: remove old runner %s: %v", e.Name(), err)
		}
	}
}

// clearQueue drops the queued update. When the state cannot be rewritten the
// file is removed instead, so neither this daemon nor the next one repeats
// the update on its own; only the warned version is forgotten.
func (m *Manager) clearQueue() {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	f := m.upd.file
	m.mu.Unlock()
	f.QueuedAt = nil
	if err := m.storeUpdateFile(f); err != nil {
		m.Events.Add("warn", "", "%v", err)
		if rerr := os.Remove(m.Paths.UpdateState); rerr != nil && !os.IsNotExist(rerr) {
			m.Events.Add("warn", "", "remove runner update state: %v", rerr)
		}
		m.mu.Lock()
		m.upd.file = updateFile{}
		m.mu.Unlock()
	}
}

// runUpdate installs the latest runner and records the outcome. A failure
// clears the queue, so only the owner re-queues; a shutdown keeps it, so the
// update runs again after the restart.
func (m *Manager) runUpdate(ctx context.Context) {
	ver, outcome, err := m.installLatest(ctx)
	if err != nil && m.maintCtx.Err() != nil {
		m.Events.Add("warn", "", "runner update interrupted by shutdown")
		return
	}
	if err != nil {
		outcome = "failed"
	}
	m.clearQueue()
	now := m.Now()
	m.mu.Lock()
	m.upd.lastOutcome, m.upd.lastFinished, m.upd.lastErr = outcome, now, ""
	if err != nil {
		m.upd.lastErr = err.Error()
	}
	m.mu.Unlock()
	switch outcome {
	case "failed":
		m.Events.Add("error", "", "runner update failed: %v", err)
	case "current":
		m.Events.Add("info", "", "runner already up to date (%s)", ver)
	default:
		m.Events.Add("ok", "", "runner updated to %s", ver)
	}
}
