// Package api is the daemon's HTTP+JSON control API, served on a Unix socket.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	// time/tzdata embeds the zone database, so GET /activity can load the
	// browser's zone on a host without /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/darkraise/ghr/internal/activity"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// Error carries an HTTP status to the client; RetryAt is set on 429.
type Error struct {
	Status  int
	Msg     string
	RetryAt time.Time
}

func (e *Error) Error() string { return e.Msg }

// FromGitHub gives a GitHub API error the status the daemon answers with.
// Other errors are returned unchanged.
func FromGitHub(err error) error {
	var ae *Error
	if errors.As(err, &ae) {
		return err
	}
	var ge *github.APIError
	if !errors.As(err, &ge) {
		return err
	}
	switch ge.Kind {
	case github.ErrRateLimit:
		return &Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: ge.RetryAt}
	case github.ErrAuth:
		return &Error{Status: http.StatusForbidden, Msg: err.Error()}
	case github.ErrNotFound:
		return &Error{Status: http.StatusNotFound, Msg: err.Error()}
	case github.ErrUnprocessable:
		return &Error{Status: http.StatusConflict, Msg: err.Error()}
	}
	return &Error{Status: http.StatusBadGateway, Msg: err.Error()}
}

func BadRequest(msg string) error { return &Error{Status: http.StatusBadRequest, Msg: msg} }
func NotFound(msg string) error   { return &Error{Status: http.StatusNotFound, Msg: msg} }
func Conflict(msg string) error   { return &Error{Status: http.StatusConflict, Msg: msg} }

// Backend is implemented by the daemon.
type Backend interface {
	Status() model.Status
	EventsAfter(seq int64) []model.Event
	History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error)
	RunnerLog(id, cursor string) (model.LogChunk, error)
	RunnerSteps(ctx context.Context, id string) ([]model.Step, error)
	RunnerContainers(ctx context.Context, id string) ([]model.Container, error)
	Config() any
	PatchConfig(p model.ConfigPatch) error
	AddRepo(ctx context.Context, req model.AddRepoRequest) error
	RemoveRepo(name string) error
	WatchRepo(ctx context.Context, name string) error
	UnwatchRepo(name string) error
	SetPausedAll(paused bool) error
	SetToken(ctx context.Context, token string) error
	KillRunner(ctx context.Context, id string) error
	Reload() ([]string, error)
	Prune() error
	Metrics() model.Metrics
	Activity(ctx context.Context, window, repo string, loc *time.Location) (model.Activity, error)
	Actions(ctx context.Context) (model.Actions, error)
	Token() model.TokenStatus
	StartLabelCheck(repo string) error
	LabelCheck(repo string) (model.LabelCheck, error)
	Registrations(ctx context.Context, repo string) ([]model.Registration, error)
	DeleteRegistration(ctx context.Context, repo string, id int64) error
	QueueRunnerUpdate(ctx context.Context) error
	CancelRunnerUpdate() error
	AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)
	Storage() model.Storage
	RefreshStorage() error
	AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error)
	InstallToolchain(req model.InstallRequest) error
	RemoveToolchain(tool, version string) error
	ClearCache(name string) error
	PruneScope(scope string) error
}

func NewServer(b Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Metrics()) })
	mux.HandleFunc("GET /activity", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		window := q.Get("window")
		if !activity.Valid(window) {
			respond(w, nil, BadRequest("window must be one of 1h, 3h, 24h, 7d, 30d"))
			return
		}
		loc := time.Local
		if tz := q.Get("tz"); tz != "" {
			l, err := time.LoadLocation(tz)
			if err != nil {
				respond(w, nil, BadRequest("unknown time zone "+tz))
				return
			}
			loc = l
		}
		a, err := b.Activity(r.Context(), window, q.Get("repo"), loc)
		respond(w, a, err)
	})
	mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		a, err := b.Actions(r.Context())
		respond(w, a, err)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Status()) })
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		ev := b.EventsAfter(after)
		if ev == nil {
			ev = []model.Event{}
		}
		writeJSON(w, ev)
	})
	mux.HandleFunc("GET /history", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		var since time.Time
		if s := q.Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				respond(w, nil, BadRequest("since must be an RFC 3339 time"))
				return
			}
			since = t
		}
		h, err := b.History(q.Get("repo"), q.Get("conclusion"), since, limit)
		if h == nil {
			h = []model.HistoryEntry{}
		}
		respond(w, h, err)
	})
	mux.HandleFunc("GET /runners/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		c, err := b.RunnerLog(r.PathValue("id"), r.URL.Query().Get("cursor"))
		respond(w, c, err)
	})
	mux.HandleFunc("GET /runners/{id}/containers", func(w http.ResponseWriter, r *http.Request) {
		cs, err := b.RunnerContainers(r.Context(), r.PathValue("id"))
		if cs == nil {
			cs = []model.Container{}
		}
		respond(w, cs, err)
	})
	mux.HandleFunc("GET /runners/{id}/steps", func(w http.ResponseWriter, r *http.Request) {
		s, err := b.RunnerSteps(r.Context(), r.PathValue("id"))
		if s == nil {
			s = []model.Step{}
		}
		respond(w, s, err)
	})
	mux.HandleFunc("DELETE /runners/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.KillRunner(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Config()) })
	mux.HandleFunc("PATCH /config", func(w http.ResponseWriter, r *http.Request) {
		var p model.ConfigPatch
		if err := decode(r, &p); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.PatchConfig(p))
	})
	mux.HandleFunc("POST /repos", func(w http.ResponseWriter, r *http.Request) {
		var req model.AddRepoRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.AddRepo(r.Context(), req))
	})
	mux.HandleFunc("GET /repos/available", func(w http.ResponseWriter, r *http.Request) {
		rs, err := b.AvailableRepos(r.Context())
		if rs == nil {
			rs = []model.AvailableRepo{}
		}
		respond(w, rs, err)
	})
	mux.HandleFunc("DELETE /repos/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.RemoveRepo(r.PathValue("name")))
	})
	mux.HandleFunc("POST /watch", func(w http.ResponseWriter, r *http.Request) {
		var req model.WatchRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.WatchRepo(r.Context(), req.Name))
	})
	mux.HandleFunc("DELETE /watch/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.UnwatchRepo(r.PathValue("name")))
	})
	mux.HandleFunc("POST /repos/{name}/pause", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(pausePatch(r.PathValue("name"), true)))
	})
	mux.HandleFunc("POST /repos/{name}/resume", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(pausePatch(r.PathValue("name"), false)))
	})
	mux.HandleFunc("POST /pause-all", func(w http.ResponseWriter, r *http.Request) { respond(w, nil, b.SetPausedAll(true)) })
	mux.HandleFunc("POST /resume-all", func(w http.ResponseWriter, r *http.Request) { respond(w, nil, b.SetPausedAll(false)) })
	mux.HandleFunc("PUT /token", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			respond(w, nil, err)
			return
		}
		tok := strings.TrimSpace(string(data))
		if tok == "" {
			respond(w, nil, BadRequest("empty token"))
			return
		}
		respond(w, nil, b.SetToken(r.Context(), tok))
	})
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Token()) })
	mux.HandleFunc("POST /repos/{name}/label-check", func(w http.ResponseWriter, r *http.Request) {
		if err := b.StartLabelCheck(r.PathValue("name")); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /repos/{name}/label-check", func(w http.ResponseWriter, r *http.Request) {
		lc, err := b.LabelCheck(r.PathValue("name"))
		respond(w, lc, err)
	})
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		ws, err := b.Reload()
		respond(w, ws, err)
	})
	mux.HandleFunc("GET /repos/{name}/registrations", func(w http.ResponseWriter, r *http.Request) {
		rs, err := b.Registrations(r.Context(), r.PathValue("name"))
		respond(w, rs, err)
	})
	mux.HandleFunc("DELETE /repos/{name}/registrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			respond(w, nil, BadRequest("runner id must be a positive integer"))
			return
		}
		respond(w, nil, b.DeleteRegistration(r.Context(), r.PathValue("name"), id))
	})
	mux.HandleFunc("POST /prune", func(w http.ResponseWriter, r *http.Request) {
		if err := b.Prune(); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /runner-update", func(w http.ResponseWriter, r *http.Request) {
		if err := b.QueueRunnerUpdate(r.Context()); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("DELETE /runner-update", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.CancelRunnerUpdate())
	})
	mux.HandleFunc("GET /storage", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Storage()) })
	mux.HandleFunc("POST /storage/refresh", accepted(func(*http.Request) error { return b.RefreshStorage() }))
	mux.HandleFunc("GET /toolchains/available", func(w http.ResponseWriter, r *http.Request) {
		cs, err := b.AvailableToolchains(r.Context(), r.URL.Query().Get("tool"))
		if cs == nil {
			cs = []model.ToolchainChoice{}
		}
		respond(w, cs, err)
	})
	mux.HandleFunc("POST /toolchains", accepted(func(r *http.Request) error {
		var req model.InstallRequest
		if err := decode(r, &req); err != nil {
			return err
		}
		return b.InstallToolchain(req)
	}))
	mux.HandleFunc("DELETE /toolchains/{tool}/{version}", accepted(func(r *http.Request) error {
		return b.RemoveToolchain(r.PathValue("tool"), r.PathValue("version"))
	}))
	mux.HandleFunc("POST /caches/{name}/clear", accepted(func(r *http.Request) error { return b.ClearCache(r.PathValue("name")) }))
	// A route of its own, so a daemon older than the scopes answers a
	// plain-text 404 instead of running a standard prune.
	mux.HandleFunc("POST /prune/{scope}", accepted(func(r *http.Request) error { return b.PruneScope(r.PathValue("scope")) }))
	return mux
}

func pausePatch(name string, paused bool) model.ConfigPatch {
	return model.ConfigPatch{Repos: map[string]model.RepoPatch{name: {Paused: &paused}}}
}

// accepted answers 202 once fn has queued or started its work.
func accepted(fn func(r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(r); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return BadRequest("invalid JSON: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		err = FromGitHub(err)
		status := http.StatusInternalServerError
		body := map[string]string{"error": err.Error()}
		var ae *Error
		if errors.As(err, &ae) {
			status = ae.Status
			if !ae.RetryAt.IsZero() {
				body["retry_at"] = ae.RetryAt.UTC().Format(time.RFC3339)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, v)
}
