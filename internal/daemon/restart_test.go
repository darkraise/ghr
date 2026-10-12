package daemon

import (
	"os"
	"slices"
	"testing"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// restartManager is fakeManager with a runner update that can be running;
// the prune flag is fakeManager's own pruning field.
type restartManager struct {
	*fakeManager
	update bool
}

func (r *restartManager) Status() model.Status {
	st := r.fakeManager.Status()
	st.RunnerUpdate.Running = r.update
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
