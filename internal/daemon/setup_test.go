package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/model"
)

// fakeSetupGitHub answers the reads first-run setup and a configured start
// make. The bearer token picks the behaviour: "good" sees DarkRaise/Alpha,
// DarkRaise/darkmem and someone-else/x; "other" sees only someone-else/x;
// "bad" is rejected; "noadmin" cannot list runners; "norepo" cannot read the
// repository; "limited" is rate limited; "down" gets a 502.
func fakeSetupGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p := r.URL.Path
		switch tok {
		case "bad":
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		case "limited":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "4102444800")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
			return
		case "down":
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch {
		case p == "/user/repos":
			if tok == "other" {
				fmt.Fprint(w, `[{"name":"x","private":true,"owner":{"login":"someone-else"}}]`)
				return
			}
			fmt.Fprint(w, `[{"name":"darkmem","private":true,"owner":{"login":"DarkRaise"}},`+
				`{"name":"Alpha","private":true,"owner":{"login":"DarkRaise"}},`+
				`{"name":"x","private":true,"owner":{"login":"someone-else"}}]`)
		case strings.HasSuffix(p, "/actions/runners"):
			if tok == "noadmin" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
				return
			}
			fmt.Fprint(w, `{"total_count":0,"runners":[]}`)
		case strings.HasSuffix(p, "/actions/runs"):
			fmt.Fprint(w, `{"total_count":0,"workflow_runs":[]}`)
		case strings.HasPrefix(p, "/repos/") && strings.Count(p, "/") == 3:
			if tok == "norepo" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			fmt.Fprintf(w, `{"name":%q,"private":true}`, p[strings.LastIndex(p, "/")+1:])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestSetup builds a setup over a store whose config names owner (possibly
// empty) and that has no token; readies counts calls to ready.
func newTestSetup(t *testing.T, owner string) (*setup, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	cfg := strings.Replace(cfgYAML, "owner: darkraise", "owner: "+strconv.Quote(owner), 1)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600)
	store, _, err := OpenStore(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	readies := &atomic.Int32{}
	s := &setup{
		store: store, events: events.New(), githubURL: fakeSetupGitHub(t).URL,
		setupPending: filepath.Join(dir, "setup-pending"), toolchainsPending: filepath.Join(dir, "toolchains-pending"),
		webListen: "0.0.0.0:8080", epoch: "e1", ready: func() { readies.Add(1) },
	}
	return s, readies
}

func setupRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestSetupGitHubRefusals(t *testing.T) {
	const ownerMsg = "owner must be a GitHub user or organisation name"
	for _, c := range []struct {
		name, body string
		code       int
		want       string
	}{
		{"owner syntax", `{"owner":"-bad","token":"good"}`, 400, ownerMsg},
		{"owner length", `{"owner":"` + strings.Repeat("a", 40) + `","token":"good"}`, 400, ownerMsg},
		{"no owner anywhere", `{"token":"good"}`, 400, ownerMsg},
		{"empty token", `{"owner":"DarkRaise","token":"  "}`, 400, "token is required"},
		{"trailing data", `{"owner":"DarkRaise","token":"good"} x`, 400, "invalid request body"},
		{"no repo of the owner", `{"owner":"DarkRaise","token":"other"}`, 400,
			"the token cannot see any repository of DarkRaise; grant it access to at least one"},
		{"bad token", `{"owner":"DarkRaise","token":"bad"}`, 400, "token rejected by GitHub: github: 401 Bad credentials"},
		{"no runner access", `{"owner":"DarkRaise","token":"noadmin"}`, 400,
			"token rejected by GitHub: listing runners failed; the token needs Administration: read/write"},
		{"repo not visible", `{"owner":"DarkRaise","token":"norepo"}`, 400, "the token cannot see DarkRaise/Alpha"},
		{"rate limited", `{"owner":"DarkRaise","token":"limited"}`, 429, `"retry_at":"2100-01-01T00:00:00Z"`},
		{"GitHub down", `{"owner":"DarkRaise","token":"down"}`, 502, "cannot reach GitHub: github: 502"},
	} {
		s, readies := newTestSetup(t, "")
		rec := setupRequest(s.routes(s.unconfigured()), http.MethodPost, "/setup/github", c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
		if readies.Load() != 0 || s.store.Configured() {
			t.Errorf("%s: readies %d configured %v", c.name, readies.Load(), s.store.Configured())
		}
		if _, err := os.Stat(s.store.TokenPath); !os.IsNotExist(err) {
			t.Errorf("%s: the token was written", c.name)
		}
	}
}

func TestSetupGitHubConfigures(t *testing.T) {
	s, readies := newTestSetup(t, "")
	h := s.routes(s.unconfigured())
	if rec := setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"darkraise","token":" good "}`); rec.Code != http.StatusNoContent {
		t.Fatalf("configure: %d %s", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(s.store.TokenPath)
	if readies.Load() != 1 || s.store.Config().Owner != "DarkRaise" || string(data) != "good\n" {
		t.Fatalf("readies %d owner %q token file %q", readies.Load(), s.store.Config().Owner, data)
	}
	var st model.SetupState
	json.Unmarshal(setupRequest(h, http.MethodGet, "/setup", "").Body.Bytes(), &st)
	if st != (model.SetupState{Starting: true, Owner: "DarkRaise", WebListen: "0.0.0.0:8080"}) {
		t.Fatalf("state %+v", st)
	}
	rec := setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"DarkRaise","token":"good"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), ErrConfigured.Error()) || readies.Load() != 1 {
		t.Fatalf("second configure: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupGitHubUsesTheConfigOwner(t *testing.T) {
	s, readies := newTestSetup(t, "DarkRaise")
	h := s.routes(s.unconfigured())
	rec := setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"someone-else","token":"good"}`)
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "config.yaml names owner DarkRaise; edit config.yaml to change it") || readies.Load() != 0 {
		t.Fatalf("another owner: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/github", `{"token":"good"}`); rec.Code != http.StatusNoContent || readies.Load() != 1 {
		t.Fatalf("config's owner: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupGitHubOneAtATime(t *testing.T) {
	s, readies := newTestSetup(t, "")
	h := s.routes(s.unconfigured())
	codes := make([]int, 8)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"DarkRaise","token":"good"}`).Code
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusNoContent:
			ok++
		case http.StatusConflict:
		default:
			t.Errorf("code %d", c)
		}
	}
	if ok != 1 || readies.Load() != 1 {
		t.Fatalf("codes %v readies %d", codes, readies.Load())
	}
}

func TestSetupPhase(t *testing.T) {
	s, _ := newTestSetup(t, "")
	s.webSetupRequired = func() bool { return true }
	os.WriteFile(s.setupPending, nil, 0o600)
	s.events.Add("warn", "", "ghr is not configured")
	h := s.routes(s.unconfigured())

	rec := setupRequest(h, http.MethodGet, "/status", "")
	var st model.Status
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &st) != nil || !st.Unconfigured || !st.SetupPending ||
		!st.WebSetupRequired || st.Epoch != "e1" || st.Mode != "queue" || st.GlobalMax != 2 ||
		!strings.Contains(rec.Body.String(), `"repos":[]`) || !strings.Contains(rec.Body.String(), `"instances":[]`) {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodGet, "/events", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "ghr is not configured") {
		t.Fatalf("events: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodGet, "/config", ""); rec.Code != http.StatusServiceUnavailable ||
		!strings.Contains(rec.Body.String(), "ghr is not configured yet; finish setup first") {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"none"}`); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "ghr is not configured yet; finish GitHub setup first") {
		t.Fatalf("finish: %d %s", rec.Code, rec.Body.String())
	}
	var state model.SetupState
	json.Unmarshal(setupRequest(h, http.MethodGet, "/setup", "").Body.Bytes(), &state)
	if state != (model.SetupState{SetupPending: true, WebListen: "0.0.0.0:8080"}) {
		t.Fatalf("state %+v", state)
	}
}

func TestSetupFinish(t *testing.T) {
	s, _ := newTestSetup(t, "DarkRaise")
	space := &fakeStorage{}
	s.backend.Store(&Backend{Space: space, Events: s.events})
	h := s.routes(http.NotFoundHandler())
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"all"}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "toolchains must be popular or none") {
		t.Fatalf("bad value: %d %s", rec.Code, rec.Body.String())
	}
	os.WriteFile(s.setupPending, nil, 0o600)
	os.WriteFile(s.toolchainsPending, nil, 0o600)
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"popular"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("popular: %d %s", rec.Code, rec.Body.String())
	}
	if len(space.calls) != 1 || space.calls[0] != "preset popular" || exists(s.setupPending) || exists(s.toolchainsPending) {
		t.Fatalf("calls %v", space.calls)
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"none"}`); rec.Code != http.StatusNoContent || len(space.calls) != 1 {
		t.Fatalf("none with the markers gone: %d calls %v", rec.Code, space.calls)
	}
	var state model.SetupState
	json.Unmarshal(setupRequest(h, http.MethodGet, "/setup", "").Body.Bytes(), &state)
	if !state.Configured || state.Starting {
		t.Fatalf("state with a backend: %+v", state)
	}
}
