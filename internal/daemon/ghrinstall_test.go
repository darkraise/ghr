package daemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/selfupdate"
)

// busyManager is fakeManager with a prune flag the install goroutine reads
// while the test sets it.
type busyManager struct {
	*fakeManager
	busy atomic.Bool
}

func (m *busyManager) Pruning() bool { return m.busy.Load() }

// installRig is a backend running v0.1.25 as ghr.service, whose last check
// found v0.1.26, with an installer over a temporary root and a fake release.
// Set hold, smoke and restartErr before StartGhrUpdate.
type installRig struct {
	b    *Backend
	m    *busyManager
	root string
	// hold, when set, keeps every download waiting until it is closed.
	hold chan struct{}
	// smoke is what the staged binary prints for ghr version.
	smoke      string
	restartErr error

	mu       sync.Mutex
	restarts int
}

func newInstallRig(t *testing.T) *installRig {
	t.Helper()
	b, fm, _ := newBackend(t)
	m := &busyManager{fakeManager: fm}
	b.M = m
	g := &installRig{b: b, m: m, root: t.TempDir(), smoke: "v0.1.26"}
	opt := filepath.Join(g.root, "opt")
	for _, d := range []string{filepath.Join(opt, "hooks"), filepath.Join(g.root, "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.root, "bin", "ghr"), []byte("old ghr"), 0o755); err != nil {
		t.Fatal(err)
	}
	const archive = "release archive"
	b.Version, b.RestartAvailable = "v0.1.25", true
	b.ghr.latest = "v0.1.26"
	b.UpdateRecordPath = filepath.Join(g.root, "ghr-update.json")
	b.Restart = func(context.Context) error {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.restarts++
		return g.restartErr
	}
	b.Installer = &selfupdate.Installer{
		Base: selfupdate.DefaultBase, Opt: opt, Hooks: filepath.Join(opt, "hooks"), Binary: filepath.Join(g.root, "bin", "ghr"),
		Fetch: func(ctx context.Context, url, dst string) error {
			if g.hold != nil {
				select {
				case <-g.hold:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			body := archive
			if strings.HasSuffix(url, "/checksums.txt") {
				body = fmt.Sprintf("%x  ghr_linux_amd64.tar.gz\n", sha256.Sum256([]byte(archive)))
			}
			return os.WriteFile(dst, []byte(body), 0o644)
		},
		Extract: func(ctx context.Context, tarball, dir string) error {
			members := filepath.Join(dir, "ghr")
			if err := os.MkdirAll(members, 0o755); err != nil {
				return err
			}
			for _, name := range []string{"ghr", "job-started.sh", "job-completed.sh"} {
				if err := os.WriteFile(filepath.Join(members, name), []byte("new "+name), 0o755); err != nil {
					return err
				}
			}
			return nil
		},
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte(g.smoke + "\n"), nil
		},
	}
	saved := ghrWaitPoll
	ghrWaitPoll = time.Millisecond
	t.Cleanup(func() {
		b.StopBackground()
		b.Wait()
		ghrWaitPoll = saved
	})
	return g
}

func (g *installRig) status() model.GhrUpdate { return g.b.Status().GhrUpdate }

func (g *installRig) restartCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.restarts
}

func (g *installRig) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(g.root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (g *installRig) staging(t *testing.T) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(g.root, "opt", ".update-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// installSteps lists the step events of the install of tag, in order.
func installSteps(b *Backend, tag string) []string {
	var out []string
	for _, msg := range eventsWith(b, "ghr "+tag+": ") {
		out = append(out, strings.TrimPrefix(msg, "ghr "+tag+": "))
	}
	return out
}

func TestStartGhrUpdateRefusals(t *testing.T) {
	const notService = "ghr is not running as ghr.service; upgrade with setup.sh"
	for _, c := range []struct {
		name string
		set  func(g *installRig)
		want string
	}{
		{"no installer", func(g *installRig) { g.b.Installer = nil }, notService},
		{"not ghr.service", func(g *installRig) { g.b.RestartAvailable = false }, notService},
		{"a development build", func(g *installRig) { g.b.Version = "dev" }, "this ghr is a development build; install a release with setup.sh"},
		{"no check yet", func(g *installRig) { g.b.ghr.latest = "" }, "no ghr release check has succeeded yet"},
		{"up to date", func(g *installRig) { g.b.ghr.latest = "v0.1.25" }, "ghr v0.1.25 is the newest release"},
	} {
		g := newInstallRig(t)
		c.set(g)
		if err := g.b.StartGhrUpdate(); apiStatus(err) != http.StatusConflict || err.Error() != c.want {
			t.Errorf("%s: %v, want 409 %q", c.name, err, c.want)
		}
		if g.status().Installing {
			t.Errorf("%s: an install started", c.name)
		}
	}

	g := newInstallRig(t)
	g.hold = make(chan struct{})
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := g.b.StartGhrUpdate(); apiStatus(err) != http.StatusConflict || err.Error() != "a ghr update is already running" {
		t.Fatalf("second install: %v", err)
	}
}

// A failed restart leaves the new binary in place; a second install would
// copy it over ghr.previous.
func TestStartGhrUpdateRefusedWhileARestartIsPending(t *testing.T) {
	g := newInstallRig(t)
	g.restartErr = errors.New("systemctl restart: exit status 1")
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return g.status().LastOutcome == "failed" })
	if err := g.b.StartGhrUpdate(); apiStatus(err) != http.StatusConflict || err.Error() != "ghr v0.1.26 is installed; restart ghr to run it" {
		t.Fatalf("second install: %v", err)
	}
	if g.status().Installing || len(eventsWith(g.b, "ghr v0.1.26: download")) != 1 {
		t.Fatal("a second install started")
	}
}

func TestGhrUpdateInstallsAndRestarts(t *testing.T) {
	g := newInstallRig(t)
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the restart", func() bool { return g.restartCount() == 1 })
	waitFor(t, "the install to end", func() bool { return !g.status().Installing })
	if s := g.status(); s.RestartPending != "v0.1.26" || s.Step != "" || s.LastOutcome != "" || s.LastError != "" {
		t.Fatalf("status %+v", s)
	}
	if got := installSteps(g.b, "v0.1.26"); !slices.Equal(got, []string{"download", "verify", "smoke", "waiting", "swap", "restart"}) {
		t.Fatalf("steps %v", got)
	}
	if g.read(t, "bin/ghr") != "new ghr" || g.read(t, "opt/ghr.previous") != "old ghr" || g.read(t, "opt/hooks/job-started.sh") != "new job-started.sh" {
		t.Fatal("the files were not swapped")
	}
	if l := g.staging(t); len(l) != 0 {
		t.Fatalf("staging left behind: %v", l)
	}
	if got := g.read(t, "ghr-update.json"); got != `{"tag":"v0.1.26"}` {
		t.Fatalf("record %s", got)
	}
	bin, prev := filepath.Join(g.root, "bin", "ghr"), filepath.Join(g.root, "opt", "ghr.previous")
	rollback := "install -m 0755 " + prev + " " + bin + " && systemctl restart ghr"
	if got := eventsWith(g.b, "installed ghr v0.1.26 at "+bin); len(got) != 1 || !strings.HasSuffix(got[0], rollback) {
		t.Fatalf("install event %q, want one ending %q", got, rollback)
	}
	if got := eventsWith(g.b, "restarting into ghr v0.1.26"); len(got) != 1 {
		t.Fatalf("restart events %q", got)
	}
}

func TestGhrUpdateWaitsForWork(t *testing.T) {
	g := newInstallRig(t)
	g.m.busy.Store(true)
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the wait", func() bool { return g.status().Step == "waiting" })
	time.Sleep(20 * time.Millisecond)
	if g.read(t, "bin/ghr") != "old ghr" || g.restartCount() != 0 {
		t.Fatal("the install went on while work ran")
	}
	if got := g.b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"pruning"}) {
		t.Fatalf("blocked by %v while waiting", got)
	}
	g.m.busy.Store(false)
	waitFor(t, "the restart", func() bool { return g.restartCount() == 1 })
	if g.read(t, "bin/ghr") != "new ghr" {
		t.Fatal("not swapped")
	}
}

func TestGhrUpdateCancelBeforeTheSwap(t *testing.T) {
	g := newInstallRig(t)
	if err := g.b.CancelGhrUpdate(); apiStatus(err) != http.StatusConflict || err.Error() != "no ghr update is running" {
		t.Fatalf("cancel with none running: %v", err)
	}
	g.hold = make(chan struct{})
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the download", func() bool { return g.status().Step == "download" })
	if got := g.b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"installing ghr"}) {
		t.Fatalf("blocked by %v while downloading", got)
	}
	if _, err := g.b.RequestRestart(context.Background()); apiStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "installing ghr") {
		t.Fatalf("a restart during the download: %v", err)
	}
	if err := g.b.CancelGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the install to end", func() bool { return !g.status().Installing })
	if s := g.status(); s.LastOutcome != "cancelled" || s.LastError != "" || s.RestartPending != "" {
		t.Fatalf("status %+v", s)
	}
	if l := g.staging(t); len(l) != 0 || g.read(t, "bin/ghr") != "old ghr" || g.restartCount() != 0 {
		t.Fatalf("a cancelled install left %v, or changed the binary or restarted", l)
	}
	if got := eventsWith(g.b, "ghr v0.1.26 update cancelled"); len(got) != 1 {
		t.Fatalf("events %q", got)
	}
}

// Work that starts during the swap cancels the restart: the install goes
// back to waiting with the files already swapped, and can no longer be
// cancelled.
func TestGhrUpdateRestartCancelledByNewWork(t *testing.T) {
	g := newInstallRig(t)
	var started atomic.Bool
	g.b.Events.SetOnChange(func() {
		evs := g.b.Events.After(0)
		if !started.Load() && evs[len(evs)-1].Msg == "ghr v0.1.26: swap" {
			started.Store(true)
			g.m.busy.Store(true)
		}
	})
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second wait", func() bool {
		s := g.status()
		return s.Step == "waiting" && s.RestartPending == "v0.1.26"
	})
	if err := g.b.CancelGhrUpdate(); apiStatus(err) != http.StatusConflict || err.Error() != "already installing; it cannot be cancelled now" {
		t.Fatalf("cancel after the swap: %v", err)
	}
	if g.read(t, "bin/ghr") != "new ghr" || g.restartCount() != 0 {
		t.Fatal("expected the files swapped and no restart yet")
	}
	if got := eventsWith(g.b, "restart cancelled: pruning started"); len(got) != 1 {
		t.Fatalf("events %q", got)
	}
	g.m.busy.Store(false)
	waitFor(t, "the restart", func() bool { return g.restartCount() == 1 })
	if got := installSteps(g.b, "v0.1.26"); !slices.Equal(got, []string{"download", "verify", "smoke", "waiting", "swap", "restart", "waiting", "restart"}) {
		t.Fatalf("steps %v", got)
	}
}

func TestGhrUpdateFailures(t *testing.T) {
	for _, c := range []struct {
		name    string
		set     func(t *testing.T, g *installRig)
		prefix  string
		swapped bool
	}{
		{"smoke", func(t *testing.T, g *installRig) { g.smoke = "v0.1.25" }, "smoke: ", false},
		// The hooks folder turns into a file as the swap begins, after the
		// stage's leftover cleanup has looked inside it.
		{"swap", func(t *testing.T, g *installRig) {
			hooks := filepath.Join(g.root, "opt", "hooks")
			g.b.Events.SetOnChange(func() {
				evs := g.b.Events.After(0)
				if evs[len(evs)-1].Msg != "ghr v0.1.26: swap" {
					return
				}
				if err := os.RemoveAll(hooks); err != nil {
					t.Error(err)
				}
				if err := os.WriteFile(hooks, []byte("not a folder"), 0o644); err != nil {
					t.Error(err)
				}
			})
		}, "swap: ", false},
		{"restart", func(t *testing.T, g *installRig) { g.restartErr = errors.New("systemctl restart: exit status 1") }, "restart: systemctl restart: exit status 1", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newInstallRig(t)
			c.set(t, g)
			if err := g.b.StartGhrUpdate(); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the failure", func() bool { return g.status().LastOutcome == "failed" })
			s := g.status()
			if s.Installing || !strings.HasPrefix(s.LastError, c.prefix) {
				t.Fatalf("status %+v, want an error starting %q", s, c.prefix)
			}
			if l := g.staging(t); len(l) != 0 {
				t.Fatalf("staging left behind: %v", l)
			}
			if c.swapped {
				if s.RestartPending != "v0.1.26" || g.read(t, "bin/ghr") != "new ghr" || g.b.Status().Restart.Failed != "systemctl restart: exit status 1" {
					t.Fatalf("after a failed restart %+v", s)
				}
				return
			}
			if s.RestartPending != "" || g.read(t, "bin/ghr") != "old ghr" || g.restartCount() != 0 {
				t.Fatalf("a failure before the swap changed the install: %+v", s)
			}
			if _, err := os.Stat(filepath.Join(g.root, "opt", "ghr.previous")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("ghr.previous written: %v", err)
			}
			if c.name == "swap" && !strings.Contains(s.LastError, "run the install again or upgrade with setup.sh") {
				t.Fatalf("swap error %q", s.LastError)
			}
		})
	}
}

func TestShutdownDuringTheWaitCancelsTheInstall(t *testing.T) {
	g := newInstallRig(t)
	g.m.busy.Store(true)
	if err := g.b.StartGhrUpdate(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the wait", func() bool { return g.status().Step == "waiting" })
	g.b.StopBackground()
	g.b.Wait()
	if s := g.status(); s.Installing || s.LastOutcome != "cancelled" {
		t.Fatalf("status %+v", s)
	}
	if l := g.staging(t); len(l) != 0 || g.read(t, "bin/ghr") != "old ghr" || g.restartCount() != 0 {
		t.Fatalf("a shutdown during the wait left %v, or changed the binary or restarted", l)
	}
}

func TestLoadGhrUpdateRecord(t *testing.T) {
	for _, c := range []struct {
		version, outcome string
	}{
		{"v0.1.26", "ok"},
		{"v0.1.25", ""},
	} {
		g := newInstallRig(t)
		g.b.Version = c.version
		if err := os.WriteFile(g.b.UpdateRecordPath, []byte(`{"tag":"v0.1.26"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		g.b.LoadGhrUpdateRecord()
		if got := g.status().LastOutcome; got != c.outcome {
			t.Errorf("%s: outcome %q, want %q", c.version, got, c.outcome)
		}
		if _, err := os.Stat(g.b.UpdateRecordPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: the record was kept: %v", c.version, err)
		}
		if got := len(eventsWith(g.b, "ghr updated to v0.1.26")); (got == 1) != (c.outcome == "ok") {
			t.Errorf("%s: %d update events", c.version, got)
		}
	}
}
