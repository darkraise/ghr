package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// maxSetupBody caps a setup request body; a token is far below it.
const maxSetupBody = 16 << 10

// githubLogin is a GitHub user or organisation name: letters, digits and
// single inner hyphens; its length (1 to 39) is checked separately.
var githubLogin = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

// setup serves first-run setup in front of the API, before ghr has an owner
// and a token and after, so the wizard and the CLI use one set of routes.
type setup struct {
	store             *Store
	events            *events.Ring
	githubURL         string
	setupPending      string
	toolchainsPending string
	webListen         string
	epoch             string
	// webSetupRequired reports a web listener with no password; nil without one.
	webSetupRequired func() bool
	// ready is called once owner and token are saved; Run then starts the manager.
	ready func()
	// backend is set just before the full API replaces the setup phase's handler.
	backend atomic.Pointer[Backend]
	mu      sync.Mutex
}

func (s *setup) routes(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /setup", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.state()) })
	mux.HandleFunc("POST /setup/github", s.github)
	mux.HandleFunc("POST /setup/finish", s.finish)
	mux.Handle("/", next)
	return mux
}

// unconfigured is the API before ghr has an owner and a token: a reduced
// status, the event feed, and 503 for everything else.
func (s *setup) unconfigured() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.status()) })
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		ev := s.events.After(after)
		if ev == nil {
			ev = []model.Event{}
		}
		writeJSON(w, ev)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeSocketError(w, http.StatusServiceUnavailable, "ghr is not configured yet; finish setup first")
	})
	return mux
}

func (s *setup) state() model.SetupState {
	serving := s.backend.Load() != nil
	return model.SetupState{
		Configured:        serving,
		Starting:          !serving && s.store.Configured(),
		SetupPending:      exists(s.setupPending),
		ToolchainsPending: exists(s.toolchainsPending),
		Owner:             s.store.Config().Owner,
		WebListen:         s.webListen,
	}
}

func (s *setup) status() model.Status {
	cfg := s.store.Config()
	return model.Status{
		Now: time.Now(), Epoch: s.epoch, Mode: cfg.Mode, GlobalMax: cfg.GlobalMax,
		Repos: []model.RepoStatus{}, Instances: []model.InstanceStatus{},
		WebSetupRequired: s.webSetupRequired != nil && s.webSetupRequired(),
		Unconfigured:     true,
		SetupPending:     exists(s.setupPending),
	}
}

func (s *setup) github(w http.ResponseWriter, r *http.Request) {
	var req model.SetupGitHubRequest
	if !decodeSetupBody(w, r, &req) {
		return
	}
	if err := s.configure(r.Context(), req.Owner, req.Token); err != nil {
		writeSetupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// configure validates owner and token against GitHub, saves them and starts
// the manager. One request at a time, so a second one gets the 409.
func (s *setup) configure(ctx context.Context, owner, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.store.Config().Owner
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = current
	}
	if len(owner) > 39 || !githubLogin.MatchString(owner) {
		return api.BadRequest("owner must be a GitHub user or organisation name")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return api.BadRequest("token is required")
	}
	if s.store.Configured() {
		return api.Conflict(ErrConfigured.Error())
	}
	if current != "" && !strings.EqualFold(current, owner) {
		return api.BadRequest((&OwnerMismatchError{Owner: current}).Error())
	}
	login, err := s.check(ctx, owner, token)
	if err != nil {
		return err
	}
	var mismatch *OwnerMismatchError
	switch err := s.store.Configure(login, token); {
	case errors.Is(err, ErrConfigured):
		return api.Conflict(err.Error())
	case errors.As(err, &mismatch):
		return api.BadRequest(err.Error())
	case err != nil:
		return err
	}
	s.ready()
	return nil
}

// check lists the repositories the token can see and runs checkToken on the
// first of owner's, returning owner's login as GitHub spells it.
func (s *setup) check(ctx context.Context, owner, token string) (string, error) {
	c := github.New(owner, func() string { return token })
	if s.githubURL != "" {
		c.BaseURL = s.githubURL
	}
	repos, err := c.ListUserRepos(ctx)
	if err != nil {
		return "", setupGitHubError(err, "")
	}
	var mine []github.UserRepo
	for _, r := range repos {
		if strings.EqualFold(r.Owner.Login, owner) {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return "", api.BadRequest(fmt.Sprintf("the token cannot see any repository of %s; grant it access to at least one", owner))
	}
	sort.Slice(mine, func(i, j int) bool { return strings.ToLower(mine[i].Name) < strings.ToLower(mine[j].Name) })
	first := mine[0]
	if err := checkToken(ctx, c, first.Name); err != nil {
		return "", setupGitHubError(err, owner+"/"+first.Name)
	}
	return first.Owner.Login, nil
}

// setupGitHubError maps a GitHub failure during setup. repo names the
// repository checkToken read, or is empty for the repository listing.
func setupGitHubError(err error, repo string) error {
	var ge *github.APIError
	if errors.As(err, &ge) {
		switch {
		case ge.Kind == github.ErrAuth:
			return api.BadRequest("token rejected by GitHub: " + err.Error())
		case ge.Kind == github.ErrNotFound && repo != "":
			return api.BadRequest("the token cannot see " + repo)
		case ge.Kind == github.ErrRateLimit:
			return &api.Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: ge.RetryAt}
		}
	}
	return &api.Error{Status: http.StatusBadGateway, Msg: "cannot reach GitHub: " + err.Error()}
}

func (s *setup) finish(w http.ResponseWriter, r *http.Request) {
	var req model.SetupFinishRequest
	if !decodeSetupBody(w, r, &req) {
		return
	}
	if req.Toolchains != "popular" && req.Toolchains != "none" {
		writeSocketError(w, http.StatusBadRequest, "toolchains must be popular or none")
		return
	}
	b := s.backend.Load()
	if b == nil {
		writeSocketError(w, http.StatusConflict, "ghr is not configured yet; finish GitHub setup first")
		return
	}
	if req.Toolchains == "popular" {
		if err := b.InstallToolchain(model.InstallRequest{Preset: "popular"}); err != nil {
			writeSetupError(w, err)
			return
		}
	}
	for _, p := range []string{s.toolchainsPending, s.setupPending} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			writeSetupError(w, err)
			return
		}
	}
	s.events.Add("info", "", "first-run setup finished")
	w.WriteHeader(http.StatusNoContent)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// decodeSetupBody reads a capped JSON body; Unmarshal, unlike a Decoder,
// rejects data after the JSON value.
func decodeSetupBody(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSetupBody))
	if err == nil {
		err = json.Unmarshal(data, v)
	}
	if err != nil {
		writeSocketError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeSetupError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	body := map[string]string{"error": err.Error()}
	var ae *api.Error
	if errors.As(err, &ae) {
		status = ae.Status
		if !ae.RetryAt.IsZero() {
			body["retry_at"] = ae.RetryAt.UTC().Format(time.RFC3339)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
