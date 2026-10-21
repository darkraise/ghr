package artifactstore

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
)

var forbidden = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"}

var badCreds = &github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"}

// ours is the GHR_ARTIFACT_STORE value an older ghr on this environment's
// store wrote.
func (e *testEnv) ours() string {
	return artifact.Store{V: 1, Endpoint: e.srv.URL(), Bucket: "gha", Region: config.DefaultArtifactRegion, KeyID: jobID}.JSON()
}

func (e *testEnv) enable(repo string) {
	if e.cfg.ArtifactStore.Repos == nil {
		e.cfg.ArtifactStore.Repos = map[string]config.ArtifactRepo{}
	}
	e.cfg.ArtifactStore.Repos[repo] = config.ArtifactRepo{Enabled: true}
}

// writes returns the variable and secret writes made, in order.
func (e *testEnv) writes() []string {
	out := []string{}
	for _, c := range e.gh.callsLike("") {
		if strings.HasPrefix(c, "Set") || strings.HasPrefix(c, "Delete") {
			out = append(out, c)
		}
	}
	return out
}

const (
	delSecret  = "DeleteSecret darkmem GHR_ARTIFACT_KEY"
	delVar     = "DeleteVariable darkmem GHR_ARTIFACT_STORE"
	retireMsg  = "artifact store: cannot remove ghr's old GHR_ARTIFACT_STORE variable or GHR_ARTIFACT_KEY secret from darkmem: "
	retireHint = "; delete them with gh variable delete GHR_ARTIFACT_STORE -R darkraise/darkmem and gh secret delete GHR_ARTIFACT_KEY -R darkraise/darkmem"
)

func TestRetire(t *testing.T) {
	foreign := artifact.Store{V: 1, Endpoint: "https://other.example", Bucket: "theirs", Region: "us-east-1", KeyID: "X"}.JSON()
	for _, tc := range []struct {
		name     string
		set      func(e *testEnv)
		writes   []string
		variable func(e *testEnv) string // nil when absent
		secret   string                  // "" when absent
		info     string                  // the info event the pass writes, if any
	}{
		{"ghr's variable and secret", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.ours(), "JOBKEYID0001:job-secret-value"
		}, []string{delSecret, delVar}, nil, "",
			"artifact store: removed ghr's old GHR_ARTIFACT_KEY secret and GHR_ARTIFACT_STORE variable from darkmem"},
		{"ghr's variable alone", func(e *testEnv) {
			e.gh.vars["darkmem"] = e.ours()
		}, []string{delSecret, delVar}, nil, "", "artifact store: removed ghr's old GHR_ARTIFACT_STORE variable from darkmem"},
		{"ghr's variable on a repository switched on", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.ours(), "k"
		}, []string{delSecret, delVar}, nil, "",
			"artifact store: removed ghr's old GHR_ARTIFACT_KEY secret and GHR_ARTIFACT_STORE variable from darkmem"},
		{"another ghr's variable", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = foreign, "THEIRS:x"
		}, []string{}, func(*testEnv) string { return foreign }, "THEIRS:x", ""},
		{"a variable ghr did not write", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = "hello", "THEIRS:x"
		}, []string{}, func(*testEnv) string { return "hello" }, "THEIRS:x", ""},
		{"a secret without ghr's variable", func(e *testEnv) {
			e.gh.secrets["darkmem"] = "JOBKEYID0001:job-secret-value"
		}, []string{}, nil, "JOBKEYID0001:job-secret-value", ""},
		{"nothing there", func(e *testEnv) {}, []string{}, nil, "", ""},
		{"the variable read is refused", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.ours(), "k"
			e.gh.readErr["darkmem"] = forbidden
		}, []string{}, (*testEnv).ours, "k", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			tc.set(e)
			e.s.pass()
			if got := e.writes(); !reflect.DeepEqual(got, tc.writes) {
				t.Fatalf("writes %v, want %v", got, tc.writes)
			}
			if got, ok := e.gh.vars["darkmem"]; tc.variable == nil && ok {
				t.Fatalf("variable %q, want none", got)
			} else if tc.variable != nil && got != tc.variable(e) {
				t.Fatalf("variable %q, want %q", got, tc.variable(e))
			}
			if got, ok := e.gh.secrets["darkmem"]; got != tc.secret || ok != (tc.secret != "") {
				t.Fatalf("secret %q, want %q", got, tc.secret)
			}
			if got := eventsLike(e.s.Events, "artifact store: removed"); tc.info == "" && len(got) != 0 || tc.info != "" && !reflect.DeepEqual(got, []string{tc.info}) {
				t.Fatalf("events %q, want %q", got, tc.info)
			}
			if got := eventsLike(e.s.Events, "cannot remove"); len(got) != 0 {
				t.Fatalf("warnings %q, want none", got)
			}
			if !e.s.retired["darkmem"] {
				t.Fatal("darkmem not retired")
			}
			e.s.pass()
			if n := len(e.gh.callsLike("GetVariable darkmem")); n != 1 {
				t.Fatalf("GetVariable called %d times over two passes, want once", n)
			}
			if got := e.writes(); !reflect.DeepEqual(got, tc.writes) {
				t.Fatalf("writes after a second pass %v, want none added", got)
			}
		})
	}
}

func TestRetireRetriesAFailedDelete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   func(e *testEnv)
		heal   func(e *testEnv)
		writes []string // made by the failing pass
		warn   string   // the warning, after retireMsg and before retireHint
	}{
		{"a refused secret delete", func(e *testEnv) { e.gh.writeErr["darkmem"] = forbidden },
			func(e *testEnv) { delete(e.gh.writeErr, "darkmem") }, []string{delSecret},
			"described: github: 403 Resource not accessible by personal access token"},
		{"a write error", func(e *testEnv) { e.gh.writeErr["darkmem"] = errors.New("connection reset") },
			func(e *testEnv) { delete(e.gh.writeErr, "darkmem") }, []string{delSecret}, "described: connection reset"},
		{"the write budget used up", func(e *testEnv) {
			e.s.Budget = &ghstorage.Budget{Now: e.clock.Now}
			for range ghstorage.WriteBudget {
				if err := e.s.Budget.Do(func() error { return nil }); err != nil {
					e.t.Fatal(err)
				}
			}
		}, func(e *testEnv) { e.s.Budget = &ghstorage.Budget{Now: e.clock.Now} }, []string{}, "GitHub write budget used up"},
		{"the rate reserve reached after the read", func(e *testEnv) {
			e.gh.onGetVar = func(string) { e.gh.remaining = ghstorage.MinRateLeft - 1 }
		}, func(e *testEnv) { e.gh.onGetVar, e.gh.remaining = nil, -1 }, []string{}, errRateLow.Error()},
		{"a variable read with bad credentials", func(e *testEnv) { e.gh.readErr["darkmem"] = badCreds },
			func(e *testEnv) { delete(e.gh.readErr, "darkmem") }, []string{},
			"described: github: 401 Bad credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.s.Describe = func(err error, repo string) string { return "described: " + err.Error() }
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.ours(), "k"
			tc.fail(e)
			e.s.pass()
			e.s.pass()
			if got := e.writes(); !reflect.DeepEqual(got, slices.Repeat(tc.writes, 2)) {
				t.Fatalf("writes %v over two failing passes, want %v twice", got, tc.writes)
			}
			warns := eventsLike(e.s.Events, "cannot remove")
			if len(warns) != 1 || !strings.HasPrefix(warns[0], retireMsg+tc.warn) || !strings.HasSuffix(warns[0], retireHint) {
				t.Fatalf("warnings %q, want one starting %q", warns, retireMsg+tc.warn)
			}
			if e.s.retired["darkmem"] {
				t.Fatal("darkmem retired after a failed delete")
			}
			tc.heal(e)
			e.s.pass()
			if _, ok := e.gh.vars["darkmem"]; ok {
				t.Fatal("the variable is still there after the retry")
			}
			if _, ok := e.gh.secrets["darkmem"]; ok {
				t.Fatal("the secret is still there after the retry")
			}
			if !e.s.retired["darkmem"] || e.s.retireErr["darkmem"] != "" {
				t.Fatalf("retired %v, failure %q after the retry", e.s.retired["darkmem"], e.s.retireErr["darkmem"])
			}
			reads := len(e.gh.callsLike("GetVariable darkmem"))
			e.s.pass()
			if n := len(e.gh.callsLike("GetVariable darkmem")); n != reads {
				t.Fatalf("GetVariable called again after the retry succeeded")
			}
		})
	}
}

func TestRetireStopsWhenStorageIsUnconfigured(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.Repos = append(e.cfg.Repos, config.Repo{Name: "darkdocs"})
	e.gh.addRepo("darkdocs", true)
	e.gh.vars["darkmem"], e.gh.vars["darkdocs"] = e.ours(), e.ours()
	unconf := *e.cfg
	unconf.ArtifactStore = config.ArtifactStore{}
	e.gh.onGetVar = func(repo string) {
		if repo == "darkmem" {
			e.s.Config = func() *config.Config { return &unconf }
		}
	}
	e.s.pass()
	if got := e.gh.callsLike("GetVariable darkdocs"); len(got) != 0 {
		t.Fatalf("calls %v after storage became unconfigured, want none", got)
	}
	if e.s.retired["darkdocs"] {
		t.Fatal("darkdocs retired after storage became unconfigured")
	}
	if _, ok := e.gh.vars["darkdocs"]; !ok {
		t.Fatal("darkdocs's variable is gone")
	}
	e.s.Config = func() *config.Config { return e.cfg }
	e.gh.onGetVar = nil
	e.s.pass()
	if _, ok := e.gh.vars["darkdocs"]; ok {
		t.Fatal("darkdocs's variable is still there once storage is configured again")
	}
	if !e.s.retired["darkdocs"] {
		t.Fatal("darkdocs not retired once storage is configured again")
	}
}

func TestRetireWarnsAgainWhenTheFailureChanges(t *testing.T) {
	e := newTestEnv(t)
	e.gh.vars["darkmem"] = e.ours()
	e.gh.writeErr["darkmem"] = errors.New("connection reset")
	e.s.pass()
	e.s.pass()
	e.gh.writeErr["darkmem"] = errors.New("bad gateway")
	e.s.pass()
	e.s.pass()
	want := []string{retireMsg + "connection reset" + retireHint, retireMsg + "bad gateway" + retireHint}
	if got := eventsLike(e.s.Events, "cannot remove"); !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings %q, want %q", got, want)
	}
}

func TestRetireWaitsForTheRateReserve(t *testing.T) {
	e := newTestEnv(t)
	e.gh.vars["darkmem"] = e.ours()
	e.gh.remaining = ghstorage.MinRateLeft - 1
	e.s.pass()
	if calls := e.gh.callsLike(""); len(calls) != 0 {
		t.Fatalf("GitHub calls %v below the rate reserve, want none", calls)
	}
	if got := eventsLike(e.s.Events, "artifact store"); len(got) != 0 {
		t.Fatalf("events %q below the rate reserve, want none", got)
	}
}

func TestSwitchingOnWritesNothingToGitHub(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.WatchRepos = []string{"homelab"}
	e.gh.addRepo("homelab", true)
	e.enable("darkmem")
	e.enable("homelab")
	e.s.pass()
	if err := e.s.SetKey("job", "NEWJOBKEY0002", "new-job-secret"); err != nil {
		t.Fatal(err)
	}
	e.s.pass()
	if got := e.writes(); len(got) != 0 {
		t.Fatalf("writes %v for switched-on repositories, want none", got)
	}
	if len(e.gh.vars) != 0 || len(e.gh.secrets) != 0 {
		t.Fatalf("variables %v and secrets %v at GitHub, want none", e.gh.vars, e.gh.secrets)
	}
}

func TestRetireOneRepository(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.ours(), "k"
	e.gh.remaining = ghstorage.MinRateLeft - 1
	if err := e.s.Retire(ctx, "DarkMem"); err != nil {
		t.Fatal(err)
	}
	if got, want := e.writes(), []string{"DeleteSecret DarkMem GHR_ARTIFACT_KEY", "DeleteVariable DarkMem GHR_ARTIFACT_STORE"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("writes %v below the rate reserve, want %v: a removal retires at once", got, want)
	}
	if err := e.s.Retire(ctx, "darkmem"); err != nil {
		t.Fatal(err)
	}
	if n := len(e.gh.callsLike("GetVariable")); n != 1 {
		t.Fatalf("GetVariable called %d times, want once: a retired repository is not read again", n)
	}

	e = newTestEnv(t)
	e.gh.vars["darkmem"] = e.ours()
	e.gh.writeErr["darkmem"] = forbidden
	if err := e.s.Retire(ctx, "darkmem"); !errors.Is(err, forbidden) {
		t.Fatalf("Retire = %v, want the refusal", err)
	}
	if got := eventsLike(e.s.Events, "cannot remove"); len(got) != 1 {
		t.Fatalf("warnings %q, want one", got)
	}

	e = newTestEnv(t)
	e.gh.vars["darkmem"] = e.ours()
	e.cfg.ArtifactStore = config.ArtifactStore{}
	if err := e.s.Retire(ctx, "darkmem"); err != nil || len(e.gh.callsLike("")) != 0 {
		t.Fatalf("Retire = %v with calls %v while not set up, want nil and none", err, e.gh.callsLike(""))
	}
}

func TestRetirePrunesRepositoriesNoLongerCovered(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.WatchRepos = []string{"homelab"}
	e.gh.writeErr["homelab"] = errors.New("connection reset")
	e.gh.vars["homelab"] = e.ours()
	e.s.pass()
	if !e.s.retired["darkmem"] || e.s.retireErr["homelab"] == "" {
		t.Fatalf("retired %v, failures %v", e.s.retired, e.s.retireErr)
	}
	e.cfg.Repos, e.cfg.WatchRepos = nil, []string{"homelab"}
	e.s.pass()
	if _, ok := e.s.retired["darkmem"]; ok {
		t.Fatal("darkmem still retired after it left the config")
	}
	e.cfg.WatchRepos = nil
	e.s.pass()
	if _, ok := e.s.retireErr["homelab"]; ok {
		t.Fatal("homelab's failure kept after it left the config")
	}
	e.cfg.Repos = []config.Repo{{Name: "darkmem"}}
	e.s.pass()
	if n := len(e.gh.callsLike("GetVariable darkmem")); n != 2 {
		t.Fatalf("GetVariable darkmem called %d times, want twice: a repository covered again is read again", n)
	}
}
