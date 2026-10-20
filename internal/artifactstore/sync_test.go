package artifactstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
)

var forbidden = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"}

// storeJSON is the variable this environment's store writes with key id.
func (e *testEnv) storeJSON(keyID string) string {
	return artifact.Store{V: 1, Endpoint: e.srv.URL(), Bucket: "gha", Region: config.DefaultArtifactRegion, KeyID: keyID}.JSON()
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
	setSecret = "SetSecret darkmem GHR_ARTIFACT_KEY"
	setVar    = "SetVariable darkmem GHR_ARTIFACT_STORE"
	delSecret = "DeleteSecret darkmem GHR_ARTIFACT_KEY"
	delVar    = "DeleteVariable darkmem GHR_ARTIFACT_STORE"
)

func current(e *testEnv) string { return e.storeJSON(jobID) }

func fixed(v string) func(*testEnv) string { return func(*testEnv) string { return v } }

func TestSyncStates(t *testing.T) {
	jobKey := artifact.FormatKey(jobID, jobSecret)
	foreign := artifact.Store{V: 1, Endpoint: "https://other.example", Bucket: "theirs", Region: "us-east-1", KeyID: "X"}.JSON()
	for _, tc := range []struct {
		name       string
		set        func(e *testEnv)
		state      string
		stateErr   string // a prefix of the row's error text
		writes     []string
		variable   func(e *testEnv) string // nil when absent
		secret     string                  // "" when absent
		eventsLike string                  // an event the pass writes, if any
	}{
		{"switched on, nothing written yet", func(e *testEnv) { e.enable("darkmem") },
			"set", "", []string{setSecret, setVar}, current, jobKey, "artifact store: wrote the GHR_ARTIFACT_KEY secret and the GHR_ARTIFACT_STORE variable"},
		{"switched on and set", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), jobKey
		}, "set", "", []string{}, current, jobKey, ""},
		{"switched on, secret deleted by hand", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.vars["darkmem"] = e.storeJSON(jobID)
		}, "set", "", []string{setSecret}, current, jobKey, "artifact store: wrote the GHR_ARTIFACT_KEY secret"},
		{"switched on, variable deleted by hand", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.secrets["darkmem"] = "OLDKEY:old"
		}, "set", "", []string{setSecret, setVar}, current, jobKey, ""},
		{"switched on, job key replaced", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON("OLDKEYID0001"), "OLDKEYID0001:old"
		}, "set", "", []string{setSecret, setVar}, current, jobKey, ""},
		{"switched on, region changed", func(e *testEnv) {
			e.enable("darkmem")
			e.cfg.ArtifactStore.Region = "ap-southeast-1"
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), jobKey
		}, "set", "", []string{setVar}, func(e *testEnv) string {
			return artifact.Store{V: 1, Endpoint: e.srv.URL(), Bucket: "gha", Region: "ap-southeast-1", KeyID: jobID}.JSON()
		}, jobKey, "artifact store: wrote the GHR_ARTIFACT_STORE variable"},
		{"another daemon's variable", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = foreign, "THEIRS:x"
		}, "foreign", "GHR_ARTIFACT_STORE points to https://other.example, bucket theirs", []string{}, fixed(foreign), "THEIRS:x", ""},
		{"a variable ghr did not write, switched off", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = "hello", "THEIRS:x"
		}, "foreign", "GHR_ARTIFACT_STORE holds a value ghr did not write", []string{}, fixed("hello"), "THEIRS:x", ""},
		{"switched on but public", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.repos[0].Private = false
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), jobKey
		}, "public", "", []string{delSecret, delVar}, nil, "", "artifact store: darkmem is public, so self-hosted storage is off for it"},
		{"switched off with ghr's variable and secret", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), jobKey
		}, "off", "", []string{delSecret, delVar}, nil, "", "artifact store: removed the GHR_ARTIFACT_KEY secret and the GHR_ARTIFACT_STORE variable"},
		{"switched off, nothing written", func(e *testEnv) {}, "off", "", []string{}, nil, "", ""},
		{"switched off, the token cannot read variables", func(e *testEnv) {
			e.gh.readErr["darkmem"] = forbidden
		}, "off", "", []string{}, nil, "", ""},
		{"switched on without a job key", func(e *testEnv) {
			e.enable("darkmem")
			e.setKeys(Keys{Admin: &Key{ID: adminID, Secret: adminSecret}})
		}, "error", "set the job key first", []string{}, nil, "", ""},
		{"the variable cannot be read", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.readErr["darkmem"] = forbidden
		}, "error", "described: ", []string{}, nil, "", ""},
		{"the token cannot see the repository", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.repos = nil
		}, "error", "the token cannot see darkmem: give it access to the repository", []string{}, nil, "", ""},
		{"the repository list fails", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.reposErr = errors.New("boom")
		}, "error", "described: boom", []string{}, nil, "", ""},
		{"a write fails", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.writeErr["darkmem"] = forbidden
		}, "error", "described: ", []string{setSecret}, nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.s.Describe = func(err error, repo string) string { return "described: " + err.Error() }
			tc.set(e)
			e.s.pass()
			r := row(t, e.s.Snapshot(), "darkmem")
			if r.State != tc.state || !strings.HasPrefix(r.StateError, tc.stateErr) || (tc.stateErr == "" && r.StateError != "") {
				t.Fatalf("state %q, error %q; want %q, %q", r.State, r.StateError, tc.state, tc.stateErr)
			}
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
			if tc.eventsLike != "" && len(eventsLike(e.s.Events, tc.eventsLike)) != 1 {
				t.Fatalf("events %q, want one like %q", eventsLike(e.s.Events, ""), tc.eventsLike)
			}
			if tc.state == "error" {
				return // an error is retried on every pass
			}
			e.s.pass()
			if got := e.writes(); !reflect.DeepEqual(got, tc.writes) {
				t.Fatalf("writes after a second pass %v, want none added", got)
			}
			if tc.eventsLike != "" && len(eventsLike(e.s.Events, tc.eventsLike)) != 1 {
				t.Fatalf("events after a second pass %q, want still one", eventsLike(e.s.Events, ""))
			}
		})
	}
}

func TestSyncReadsAnOffRepositoryOnce(t *testing.T) {
	e := newTestEnv(t)
	e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), artifact.FormatKey(jobID, jobSecret)
	e.s.pass()
	e.s.pass()
	if n := len(e.gh.callsLike("GetVariable darkmem")); n != 1 {
		t.Fatalf("GetVariable called %d times over two passes, want once", n)
	}
	if r := row(t, e.s.Snapshot(), "darkmem"); r.State != "off" {
		t.Fatalf("state %q, want off", r.State)
	}
	e.enable("darkmem")
	e.s.pass()
	if n := len(e.gh.callsLike("GetVariable darkmem")); n != 2 {
		t.Fatalf("GetVariable called %d times after switching on, want twice", n)
	}
	if r := row(t, e.s.Snapshot(), "darkmem"); r.State != "set" {
		t.Fatalf("state %q after switching on, want set", r.State)
	}
}

func TestSyncWaitsForTheRateReserve(t *testing.T) {
	e := newTestEnv(t)
	e.enable("darkmem")
	e.gh.remaining = ghstorage.MinRateLeft - 1
	e.s.pass()
	if calls := e.gh.callsLike(""); len(calls) != 0 {
		t.Fatalf("GitHub calls %v below the rate reserve, want none", calls)
	}
	if r := row(t, e.s.Snapshot(), "darkmem"); r.State != "missing" {
		t.Fatalf("state %q, want missing until a pass writes it", r.State)
	}
}

func TestSyncWritesCountAgainstTheSharedBudget(t *testing.T) {
	e := newTestEnv(t)
	e.enable("darkmem")
	e.s.Budget = &ghstorage.Budget{Now: e.clock.Now}
	for range ghstorage.WriteBudget {
		if err := e.s.Budget.Do(func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	e.s.pass()
	r := row(t, e.s.Snapshot(), "darkmem")
	if r.State != "error" || !strings.HasPrefix(r.StateError, "GitHub write budget used up") || len(e.writes()) != 0 {
		t.Fatalf("state %q, error %q, writes %v; want the budget's refusal and no write", r.State, r.StateError, e.writes())
	}
}

func TestAJobKeyReplacementRewritesEverySwitchedOnRepository(t *testing.T) {
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
	for _, repo := range []string{"darkmem", "homelab"} {
		if n := len(e.gh.callsLike("SetSecret " + repo)); n != 2 {
			t.Errorf("%s secret written %d times, want twice", repo, n)
		}
		if got := e.gh.secrets[repo]; got != "NEWJOBKEY0002:new-job-secret" {
			t.Errorf("%s secret %q, want the new key", repo, got)
		}
		if got := e.gh.vars[repo]; got != e.storeJSON("NEWJOBKEY0002") {
			t.Errorf("%s variable %q, want the new key id", repo, got)
		}
		if r := row(t, e.s.Snapshot(), repo); r.State != "set" {
			t.Errorf("%s state %q", repo, r.State)
		}
	}
}

func TestSyncRewritesTheSecretWhenTheJobKeyIsReplacedUnderTheSameID(t *testing.T) {
	e := newTestEnv(t)
	e.enable("darkmem")
	e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), artifact.FormatKey(jobID, jobSecret)
	e.s.pass()
	if got := e.writes(); len(got) != 0 {
		t.Fatalf("writes %v with the secret current, want none", got)
	}
	e.clock.Add(time.Minute)
	if err := e.s.SetKey("job", jobID, "rotated-secret"); err != nil {
		t.Fatal(err)
	}
	e.s.pass()
	want := []string{setSecret}
	if got := e.writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes %v after the job key was replaced, want %v", got, want)
	}
	if got := e.gh.secrets["darkmem"]; got != artifact.FormatKey(jobID, "rotated-secret") {
		t.Fatalf("secret %q, want the replaced key", got)
	}
	if r := row(t, e.s.Snapshot(), "darkmem"); r.State != "set" || r.StateError != "" {
		t.Fatalf("state %q, error %q; want set", r.State, r.StateError)
	}
	if n := len(eventsLike(e.s.Events, "artifact store: wrote the GHR_ARTIFACT_KEY secret")); n != 1 {
		t.Fatalf("events %q, want one secret write", eventsLike(e.s.Events, ""))
	}
	e.s.pass()
	if got := e.writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes after a third pass %v, want none added", got)
	}
}

// A token without Variables access reads the variable as missing, so only
// ghr's record tells that the secret it wrote is still current.
func TestSyncWritesTheSecretOnceWhileTheVariableWriteFails(t *testing.T) {
	e := newTestEnv(t)
	e.enable("darkmem")
	e.gh.setVarErr["darkmem"] = forbidden
	e.s.pass()
	e.s.pass()
	if got, want := e.writes(), []string{setSecret, setVar, setVar}; !reflect.DeepEqual(got, want) {
		t.Fatalf("writes %v over two passes, want %v", got, want)
	}
	if r := row(t, e.s.Snapshot(), "darkmem"); r.State != "error" {
		t.Fatalf("state %q, want error", r.State)
	}
}

func TestSyncShowsNoSecret(t *testing.T) {
	e := newTestEnv(t)
	e.enable("darkmem")
	e.s.pass()
	if got := e.gh.secrets["darkmem"]; got != artifact.FormatKey(jobID, jobSecret) {
		t.Fatalf("secret %q, want the job key", got)
	}
	e.gh.writeErr["darkmem"] = forbidden
	delete(e.gh.secrets, "darkmem")
	e.s.pass()
	r, err := e.s.SyncRepo(context.Background(), "darkmem")
	if err == nil && r.StateError == "" {
		t.Fatal("a failing write left no row error")
	}
	b, jerr := json.Marshal(e.s.Snapshot())
	if jerr != nil {
		t.Fatal(jerr)
	}
	for _, text := range append(eventsLike(e.s.Events, ""), string(b), r.StateError) {
		if strings.Contains(text, jobSecret) {
			t.Fatalf("%q shows the job secret", text)
		}
	}
}

func TestSyncRepo(t *testing.T) {
	e := newTestEnv(t)
	e.gh.addRepo("homelab", false)
	e.cfg.WatchRepos = []string{"homelab"}
	if _, err := e.s.SyncRepo(context.Background(), "elsewhere"); !errors.Is(err, ErrUnknownRepo) {
		t.Errorf("an uncovered repository: %v, want ErrUnknownRepo", err)
	}
	e.enable("darkmem")
	e.setKeys(Keys{Admin: &Key{ID: adminID, Secret: adminSecret}})
	if _, err := e.s.SyncRepo(context.Background(), "darkmem"); !errors.Is(err, ErrNoJobKey) {
		t.Errorf("without a job key: %v, want ErrNoJobKey", err)
	}
	if len(e.writes()) != 0 {
		t.Fatalf("writes %v before the job key was set", e.writes())
	}
	e.setKeys(Keys{Admin: &Key{ID: adminID, Secret: adminSecret}, Job: &Key{ID: jobID, Secret: jobSecret}})
	r, err := e.s.SyncRepo(context.Background(), "DarkMem")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "darkmem" || r.State != "set" || !r.Private || !r.Enabled || e.gh.vars["darkmem"] != e.storeJSON(jobID) {
		t.Fatalf("row %+v, variable %q", r, e.gh.vars["darkmem"])
	}
	e.enable("homelab")
	if r, err := e.s.SyncRepo(context.Background(), "homelab"); err != nil || r.State != "public" || r.Private {
		t.Fatalf("a public repository: %+v, %v; want state public", r, err)
	}
	e.cfg.ArtifactStore = config.ArtifactStore{}
	if _, err := e.s.SyncRepo(context.Background(), "darkmem"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("not set up: %v, want ErrNotConfigured", err)
	}
}

func TestSyncRepoWritesBelowTheRateReserve(t *testing.T) {
	e := newTestEnv(t)
	e.enable("darkmem")
	e.gh.remaining = ghstorage.MinRateLeft - 1
	if r, err := e.s.SyncRepo(context.Background(), "darkmem"); err != nil || r.State != "set" {
		t.Fatalf("row %+v, %v; a switch the owner made is applied at once", r, err)
	}
}

func TestSwitchOff(t *testing.T) {
	foreign := artifact.Store{V: 1, Endpoint: "https://other.example", Bucket: "theirs", Region: "us-east-1", KeyID: "X"}.JSON()
	for _, tc := range []struct {
		name     string
		set      func(e *testEnv)
		err      error
		writes   []string
		variable string
	}{
		{"ghr's variable and secret", func(e *testEnv) {
			e.enable("darkmem")
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), "k"
		}, nil, []string{delSecret, delVar}, ""},
		{"another daemon's variable", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = foreign, "k"
		}, nil, []string{}, foreign},
		{"a failed delete", func(e *testEnv) {
			e.gh.vars["darkmem"], e.gh.secrets["darkmem"] = e.storeJSON(jobID), "k"
			e.gh.writeErr["darkmem"] = forbidden
		}, forbidden, []string{delSecret}, "want"},
		{"not set up", func(e *testEnv) {
			e.gh.vars["darkmem"] = e.storeJSON(jobID)
			e.cfg.ArtifactStore = config.ArtifactStore{}
		}, nil, []string{}, "want"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			want := tc.variable
			if want == "want" {
				want = e.storeJSON(jobID)
			}
			tc.set(e)
			if err := e.s.SwitchOff(context.Background(), "darkmem"); !errors.Is(err, tc.err) {
				t.Fatalf("SwitchOff = %v, want %v", err, tc.err)
			}
			if got := e.writes(); !reflect.DeepEqual(got, tc.writes) {
				t.Fatalf("writes %v, want %v", got, tc.writes)
			}
			if got := e.gh.vars["darkmem"]; got != want {
				t.Fatalf("variable %q, want %q", got, want)
			}
		})
	}
}
