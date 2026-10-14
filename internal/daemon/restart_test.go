package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
)

// restartManager is fakeManager with a runner update that can be running or
// preparing; the prune flag is fakeManager's own pruning field.
type restartManager struct {
	*fakeManager
	update, preparing bool
}

func (r *restartManager) Status() model.Status {
	st := r.fakeManager.Status()
	st.RunnerUpdate.Running = r.update
	st.RunnerUpdate.Preparing = r.preparing
	return st
}

func setWeb(t *testing.T, b *Backend, w config.Web) {
	t.Helper()
	if err := b.update(func(c *config.Config) error { c.Web = w; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestRestartPendingComparesTheSavedWebBlock(t *testing.T) {
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "GHR.example"}}
	for _, tc := range []struct {
		name string
		web  config.Web
		want []string
	}{
		{"the applied block", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "GHR.example"}}, nil},
		{"hosts reordered, recased and repeated", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.example", "GHR.LAN", "ghr.lan"}}, nil},
		{"a new listen", config.Web{Listen: "0.0.0.0:9090", Hosts: []string{"ghr.lan", "ghr.example"}}, []string{"web.listen"}},
		{"a host removed", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}}, []string{"web.hosts"}},
		{"both", config.Web{Listen: "127.0.0.1:8080"}, []string{"web.listen", "web.hosts"}},
		{"a trusted proxy added", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "ghr.example"}, TrustedProxies: []string{"10.0.0.2"}}, []string{"web.trusted_proxies"}},
	} {
		setWeb(t, b, tc.web)
		st := b.Status()
		if !slices.Equal(st.Restart.Pending, tc.want) {
			t.Errorf("%s: pending %v, want %v", tc.name, st.Restart.Pending, tc.want)
		}
		if st.Restart.WebListen != "0.0.0.0:8080" {
			t.Errorf("%s: web_listen %q", tc.name, st.Restart.WebListen)
		}
	}

	setWeb(t, b, b.WebApplied)
	if err := os.WriteFile(b.Store.ConfigPath, []byte(cfgYAML+"web:\n  listen: 0.0.0.0:9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := b.Status().Restart.Pending; !slices.Equal(got, []string{"web.listen", "web.hosts"}) {
		t.Fatalf("after a hand edit and a reload: %v", got)
	}
}

func TestRestartPendingComparesTrustedProxiesAsPrefixes(t *testing.T) {
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:8080", TrustedProxies: []string{"10.0.0.2", "10.1.0.0/16", "fd00::1"}}
	for _, tc := range []struct {
		name    string
		proxies []string
		want    []string
	}{
		{"the same prefixes spelled differently", []string{"10.0.0.2/32", "10.1.2.3/16", "::ffff:10.0.0.2", "FD00::1/128"}, nil},
		{"a prefix widened", []string{"10.0.0.2", "10.1.0.0/15", "fd00::1"}, []string{"web.trusted_proxies"}},
		{"a proxy removed", []string{"10.0.0.2/32", "fd00::1"}, []string{"web.trusted_proxies"}},
	} {
		setWeb(t, b, config.Web{Listen: "0.0.0.0:8080", TrustedProxies: tc.proxies})
		if got := b.Status().Restart.Pending; !slices.Equal(got, tc.want) {
			t.Errorf("%s: pending %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRestartBlockedBy(t *testing.T) {
	b, m, _ := newBackend(t)
	rm := &restartManager{fakeManager: m}
	space := &fakeStorage{}
	b.M, b.Space = rm, space
	if got := b.Status().Restart.BlockedBy; len(got) != 0 {
		t.Fatalf("an idle daemon is blocked by %v", got)
	}
	space.snap.Operations = model.Operations{Current: &model.Operation{Kind: "install", Target: "node 24"}, Queued: 1}
	m.pruning, rm.update = true, true
	want := []string{"installing node 24", "1 queued", "pruning", "updating the runner software"}
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, want) {
		t.Fatalf("everything at once: %v, want %v", got, want)
	}
	m.pruning, rm.update = false, false
	for kind, text := range map[string]string{"remove": "removing node 22", "clear": "clearing node 22", "measure": "measure node 22"} {
		space.snap.Operations = model.Operations{Current: &model.Operation{Kind: kind, Target: "node 22"}}
		if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{text}) {
			t.Errorf("%s: %v, want %q", kind, got, text)
		}
	}
	space.snap.Operations = model.Operations{Queued: 2}
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"2 queued"}) {
		t.Fatalf("queued only: %v", got)
	}
	space.snap.Operations = model.Operations{}
	m.pruning = true
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"pruning"}) {
		t.Fatalf("a prune: %v", got)
	}
	m.pruning, rm.preparing = false, true
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"preparing the runner update"}) {
		t.Fatalf("a runner update preparing: %v", got)
	}
}

func TestRestartAvailabilityAndListenError(t *testing.T) {
	b, _, _ := newBackend(t)
	if st := b.Status(); st.Restart.Available || st.WebListenError != nil {
		t.Fatalf("by default: %+v %+v", st.Restart, st.WebListenError)
	}
	b.RestartAvailable = true
	b.WebListenErr = &model.WebListenError{Address: "10.0.0.5:8080", Reason: "cannot assign requested address", ServingOn: "0.0.0.0:8080"}
	st := b.Status()
	if !st.Restart.Available || st.WebListenError == nil || *st.WebListenError != *b.WebListenErr {
		t.Fatalf("set at start: %+v %+v", st.Restart, st.WebListenError)
	}
}

func TestRestartChangesPublishStatus(t *testing.T) {
	b, m, _ := newBackend(t)
	prev := b.fingerprints()
	m.pruning = true
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a prune changed %v", got)
	}
	b.WebListenErr = &model.WebListenError{Address: "10.0.0.5:8080", Reason: "cannot assign requested address"}
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a listen error changed %v", got)
	}
	b.RestartAvailable = true
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("availability changed %v", got)
	}
	setWeb(t, b, config.Web{Listen: "0.0.0.0:9090"})
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"config", "status"}) {
		t.Fatalf("a pending listen changed %v", got)
	}
}

// restartWarnings counts the restart warning's events.
func restartWarnings(b *Backend) int {
	n := 0
	for _, e := range b.Events.After(0) {
		if e.Level == "warn" && e.Msg == "config: "+restartWarning {
			n++
		}
	}
	return n
}

func TestPatchingWebSettingsWarnsOncePerChange(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Bind = func(string) error { return nil }
	setWeb(t, b, config.Web{Listen: "0.0.0.0:8080"})
	b.WebApplied = b.Store.Config().Web
	patch := func(what string, w model.WebPatch, wantEvents int) {
		t.Helper()
		if err := b.PatchConfig(context.Background(), model.ConfigPatch{Web: &w}); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n := restartWarnings(b); n != wantEvents {
			t.Fatalf("%s: %d warning events, want %d", what, n, wantEvents)
		}
	}
	moved, back := "0.0.0.0:9090", "0.0.0.0:8080"
	patch("a new listen", model.WebPatch{Listen: &moved}, 1)
	patch("the same listen again", model.WebPatch{Listen: &moved}, 1)
	patch("a host as well", model.WebPatch{Hosts: &[]string{"ghr.lan"}}, 2)
	patch("both reverted", model.WebPatch{Listen: &back, Hosts: &[]string{}}, 2)
	patch("the listen moved again", model.WebPatch{Listen: &moved}, 3)
}

// A web patch is checked for the restart warning before another web patch
// can commit. The first patch is held in Wake, after its commit and before
// its check; a second patch that reverts it must wait, or the first patch's
// transition is never written.
func TestConcurrentWebPatchesWarnInCommitOrder(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Bind = func(string) error { return nil }
	setWeb(t, b, config.Web{Listen: "0.0.0.0:8080"})
	b.WebApplied = b.Store.Config().Web
	held, release := make(chan struct{}), make(chan struct{})
	var wakes atomic.Int32
	b.Wake = func() {
		if wakes.Add(1) == 1 {
			close(held)
			<-release
		}
	}
	moved, back := "0.0.0.0:9090", "0.0.0.0:8080"
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		first <- b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &moved}})
	}()
	<-held
	go func() {
		second <- b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &back}})
	}()
	select {
	case err := <-second:
		t.Fatalf("the second web patch finished while the first was unchecked: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for _, done := range []chan error{first, second} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := restartWarnings(b); n != 1 {
		t.Fatalf("%d warning events, want 1 for the first patch's change", n)
	}
	if got := b.Status().Restart.Pending; len(got) != 0 {
		t.Fatalf("pending after the revert: %v", got)
	}
}

func TestRequestRestart(t *testing.T) {
	b, m, _ := newBackend(t)
	rm := &restartManager{fakeManager: m}
	b.M = rm
	var got []context.Context
	b.Restart = func(ctx context.Context) error { got = append(got, ctx); return nil }
	restartEvents := func() []string {
		var out []string
		for _, e := range b.Events.After(0) {
			if strings.HasPrefix(e.Msg, "restart ") {
				out = append(out, e.Level+" "+e.Msg)
			}
		}
		return out
	}

	if _, err := b.RequestRestart(context.Background()); apiStatus(err) != http.StatusConflict ||
		apiMsg(err) != "ghr is not running as ghr.service; restart it by hand" {
		t.Fatalf("not the unit's main process: %v", err)
	}
	b.RestartAvailable = true
	m.pruning, rm.update = true, true
	if _, err := b.RequestRestart(context.Background()); apiStatus(err) != http.StatusConflict ||
		apiMsg(err) != "waits for: pruning, updating the runner software" {
		t.Fatalf("blocked: %v", err)
	}
	if evs := restartEvents(); len(evs) != 0 || len(got) != 0 {
		t.Fatalf("a refused restart wrote %v and ran %d times", evs, len(got))
	}
	m.pruning, rm.update = false, false

	ctx, cancel := context.WithCancel(api.WithBrowser(context.Background(), "ghr.lan:8080"))
	run, err := b.RequestRestart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("the restart ran before run was called")
	}
	cancel()
	run()
	if len(got) != 1 {
		t.Fatalf("run restarted %d times", len(got))
	}
	if err := got[0].Err(); err != nil {
		t.Fatalf("the restart's context ended with the request: %v", err)
	}

	run, err = b.RequestRestart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run()
	b.Restart = func(context.Context) error {
		return errors.New("systemctl restart: exit status 5: Unit ghr.service not loaded.")
	}
	run, err = b.RequestRestart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run()
	if got := b.Status().Restart.Failed; got != "systemctl restart: exit status 5: Unit ghr.service not loaded." {
		t.Fatalf("failed after a failed restart: %q", got)
	}
	if _, err := b.RequestRestart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := b.Status().Restart.Failed; got != "" {
		t.Fatalf("an accepted request kept the last failure: %q", got)
	}
	want := []string{
		"info restart requested from the web UI",
		"info restart requested",
		"info restart requested",
		"error restart failed: systemctl restart: exit status 5: Unit ghr.service not loaded.",
		"info restart requested",
	}
	if evs := restartEvents(); !slices.Equal(evs, want) {
		t.Fatalf("events %q, want %q", evs, want)
	}
}

// Work that starts after the request was accepted cancels the restart: run
// checks again and does not call the seam.
func TestRestartCancelledWhenWorkStarts(t *testing.T) {
	b, m, _ := newBackend(t)
	b.RestartAvailable = true
	restarts := 0
	b.Restart = func(context.Context) error { restarts++; return nil }
	run, err := b.RequestRestart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.pruning = true
	b.Space = &fakeStorage{snap: model.Storage{Operations: model.Operations{Current: &model.Operation{Kind: "install", Target: "node 24"}}}}
	run()
	if restarts != 0 {
		t.Fatal("the restart ran while work had started")
	}
	evs := b.Events.After(0)
	last := evs[len(evs)-1]
	if last.Level != "warn" || last.Msg != "restart cancelled: installing node 24, pruning started" {
		t.Fatalf("last event %s %q", last.Level, last.Msg)
	}
	if got := b.Status().Restart.Failed; got != "installing node 24, pruning started" {
		t.Fatalf("failed after a cancelled restart: %q", got)
	}
}

// recordRunner is a system.Runner that records each command and answers
// with out and err.
func recordRunner(calls *[]string, out string, err error) system.Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return []byte(out), err
	}
}

func TestSelfUnitCheck(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	var calls []string
	t.Setenv("INVOCATION_ID", "")
	if selfUnitCheck(recordRunner(&calls, pid+"\n", nil))(context.Background()) || len(calls) != 0 {
		t.Fatalf("without INVOCATION_ID: ran %q", calls)
	}
	t.Setenv("INVOCATION_ID", "0123abcd")
	for _, tc := range []struct {
		name, out string
		err       error
		want      bool
	}{
		{"this process", pid + "\n", nil, true},
		{"another process", "1\n", nil, false},
		{"no main process", "0\n", nil, false},
		{"a failed command", pid + "\n", errors.New("systemctl show: exit status 1: Unit ghr.service not loaded."), false},
	} {
		calls = nil
		got := selfUnitCheck(recordRunner(&calls, tc.out, tc.err))(context.Background())
		if got != tc.want || !slices.Equal(calls, []string{"systemctl show -p MainPID --value ghr.service"}) {
			t.Errorf("%s: %v after %q, want %v", tc.name, got, calls, tc.want)
		}
	}
}

func TestRestartUnit(t *testing.T) {
	var calls []string
	if err := restartUnit(recordRunner(&calls, "", nil))(context.Background()); err != nil ||
		!slices.Equal(calls, []string{"systemctl restart --no-block ghr.service"}) {
		t.Fatalf("ran %q: %v", calls, err)
	}
	failure := errors.New("systemctl restart: exit status 5: Unit ghr.service not loaded.")
	if err := restartUnit(recordRunner(&calls, "", failure))(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("the command's error came back as %v", err)
	}
	if o := DefaultOptions(); o.SelfUnit == nil || o.Restart == nil {
		t.Fatal("the default options lack the restart seams")
	}
}
