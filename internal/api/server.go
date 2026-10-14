// Package api is the daemon's HTTP+JSON control API, served on a Unix socket.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	// time/tzdata embeds the zone database, so GET /activity can load the
	// browser's zone on a host without /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/darkraise/ghr/internal/activity"
	"github.com/darkraise/ghr/internal/changes"
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
	case github.ErrGone:
		return &Error{Status: http.StatusGone, Msg: err.Error()}
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
	PatchConfig(ctx context.Context, p model.ConfigPatch) error
	AddRepo(ctx context.Context, req model.AddRepoRequest) error
	RemoveRepo(name string) error
	WatchRepo(ctx context.Context, name string) error
	UnwatchRepo(name string) error
	SetPausedAll(paused bool) error
	SetToken(ctx context.Context, token string) error
	KillRunner(ctx context.Context, id string) error
	Reload() ([]string, error)
	// RequestRestart refuses a restart that cannot run now; otherwise run
	// restarts ghr, and the caller answers its request first.
	RequestRestart(ctx context.Context) (run func(), err error)
	Prune() error
	Metrics() model.Metrics
	Activity(ctx context.Context, window, repo string, loc *time.Location) (model.Activity, error)
	Actions(ctx context.Context) (model.Actions, error)
	RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error)
	RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error)
	JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error)
	RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)
	ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error)
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
	Subscribe() (*changes.Sub, error)
	GitHubStorage() model.GitHubStorage
	RefreshGitHubStorage() error
	GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error)
	DeleteGitHubArtifact(ctx context.Context, repo string, id int64) error
	DeleteGitHubCache(ctx context.Context, repo string, id int64) error
	DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error)
	DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error)
	SetGitHubRetention(ctx context.Context, repo string, days int) error
	SetGitHubRetentionAll(days int) (model.OperationStarted, error)
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
	mux.HandleFunc("GET /actions/{repo}/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, attempt, err := runParams(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		d, err := b.RunDetail(r.Context(), r.PathValue("repo"), id, attempt)
		respond(w, d, err)
	})
	mux.HandleFunc("GET /actions/{repo}/runs/{id}/annotations", func(w http.ResponseWriter, r *http.Request) {
		id, attempt, err := runParams(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		a, err := b.RunAnnotations(r.Context(), r.PathValue("repo"), id, attempt)
		respond(w, a, err)
	})
	mux.HandleFunc("GET /actions/{repo}/jobs/{jobId}/log", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("jobId"), "job id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		l, err := b.JobLog(r.Context(), r.PathValue("repo"), id)
		respond(w, l, err)
	})
	mux.HandleFunc("GET /actions/{repo}/jobs/{jobId}/log/raw", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("jobId"), "job id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		deadline := time.Now().Add(rawWriteWindow)
		body, size, err := b.RawJobLog(r.Context(), r.PathValue("repo"), id)
		if err != nil {
			respond(w, nil, err)
			return
		}
		defer body.Close()
		streamLog(w, body, size, deadline)
	})
	mux.HandleFunc("GET /actions/{repo}/artifacts/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("id"), "artifact id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		d, err := b.ArtifactDownload(r.Context(), r.PathValue("repo"), id)
		respond(w, d, err)
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
	mux.HandleFunc("GET /changes", func(w http.ResponseWriter, r *http.Request) {
		sub, err := b.Subscribe()
		if err != nil {
			respond(w, nil, changesErr(err))
			return
		}
		defer sub.Close()
		streamChanges(w, r, sub)
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
		// Only a server that cuts its responses needs the longer deadline. The
		// Unix socket's server sets no WriteTimeout and keeps no deadline.
		if srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server); ok && srv.WriteTimeout > 0 {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(killWriteWindow))
		}
		respond(w, nil, b.KillRunner(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Config()) })
	mux.HandleFunc("PATCH /config", func(w http.ResponseWriter, r *http.Request) {
		var p model.ConfigPatch
		if err := decode(w, r, &p); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.PatchConfig(r.Context(), p))
	})
	mux.HandleFunc("POST /repos", func(w http.ResponseWriter, r *http.Request) {
		var req model.AddRepoRequest
		if err := decode(w, r, &req); err != nil {
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
		if err := decode(w, r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.WatchRepo(r.Context(), req.Name))
	})
	mux.HandleFunc("DELETE /watch/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.UnwatchRepo(r.PathValue("name")))
	})
	mux.HandleFunc("POST /repos/{name}/pause", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(r.Context(), pausePatch(r.PathValue("name"), true)))
	})
	mux.HandleFunc("POST /repos/{name}/resume", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(r.Context(), pausePatch(r.PathValue("name"), false)))
	})
	mux.HandleFunc("POST /pause-all", func(w http.ResponseWriter, r *http.Request) { respond(w, nil, b.SetPausedAll(true)) })
	mux.HandleFunc("POST /resume-all", func(w http.ResponseWriter, r *http.Request) { respond(w, nil, b.SetPausedAll(false)) })
	mux.HandleFunc("PUT /token", func(w http.ResponseWriter, r *http.Request) {
		data, err := readBody(w, r, 64*1024)
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
	mux.HandleFunc("POST /restart", func(w http.ResponseWriter, r *http.Request) {
		run, err := b.RequestRestart(r.Context())
		if err != nil {
			respond(w, nil, err)
			return
		}
		// The answer goes out before systemd is asked: the stop it queues
		// ends this process.
		w.WriteHeader(http.StatusAccepted)
		http.NewResponseController(w).Flush()
		run()
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
	mux.HandleFunc("POST /toolchains", func(w http.ResponseWriter, r *http.Request) {
		var req model.InstallRequest
		if err := decode(w, r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		if err := b.InstallToolchain(req); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("DELETE /toolchains/{tool}/{version}", accepted(func(r *http.Request) error {
		return b.RemoveToolchain(r.PathValue("tool"), r.PathValue("version"))
	}))
	mux.HandleFunc("POST /caches/{name}/clear", accepted(func(r *http.Request) error { return b.ClearCache(r.PathValue("name")) }))
	// A route of its own, so a daemon older than the scopes answers a
	// plain-text 404 instead of running a standard prune.
	mux.HandleFunc("POST /prune/{scope}", accepted(func(r *http.Request) error { return b.PruneScope(r.PathValue("scope")) }))
	mux.HandleFunc("GET /github-storage", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.GitHubStorage()) })
	mux.HandleFunc("POST /github-storage/refresh", accepted(func(*http.Request) error { return b.RefreshGitHubStorage() }))
	mux.HandleFunc("GET /github-storage/preview", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ps, err := b.GitHubStoragePreview(q.Get("cleanup"), q.Get("repo"))
		if ps == nil {
			ps = []model.StoragePreview{}
		}
		respond(w, ps, err)
	})
	mux.HandleFunc("POST /github-storage/retention", func(w http.ResponseWriter, r *http.Request) {
		var req model.RetentionRequest
		if err := decode(w, r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		started(w)(b.SetGitHubRetentionAll(req.Days))
	})
	mux.HandleFunc("POST /github-storage/{repo}/artifacts/delete", func(w http.ResponseWriter, r *http.Request) {
		var req model.DeleteArtifactsRequest
		if err := decode(w, r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		started(w)(b.DeleteGitHubArtifacts(r.PathValue("repo"), req))
	})
	mux.HandleFunc("DELETE /github-storage/{repo}/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("id"), "artifact id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.DeleteGitHubArtifact(r.Context(), r.PathValue("repo"), id))
	})
	mux.HandleFunc("POST /github-storage/{repo}/caches/delete", func(w http.ResponseWriter, r *http.Request) {
		var req model.DeleteCachesRequest
		if err := decode(w, r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		started(w)(b.DeleteGitHubCaches(r.PathValue("repo"), req))
	})
	mux.HandleFunc("DELETE /github-storage/{repo}/caches/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("id"), "cache id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.DeleteGitHubCache(r.Context(), r.PathValue("repo"), id))
	})
	mux.HandleFunc("PUT /github-storage/{repo}/retention", func(w http.ResponseWriter, r *http.Request) {
		var req model.RetentionRequest
		if err := decode(w, r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.SetGitHubRetention(r.Context(), r.PathValue("repo"), req.Days))
	})
	return mux
}

// positive parses a path or query value that must be a positive integer.
func positive(v, name string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, BadRequest(name + " must be a positive integer")
	}
	return n, nil
}

// attemptParam reads ?attempt=; absent means 0, the latest attempt.
func attemptParam(r *http.Request) (int, error) {
	v := r.URL.Query().Get("attempt")
	if v == "" {
		return 0, nil
	}
	n, err := positive(v, "attempt")
	if err != nil || n > math.MaxInt32 {
		return 0, BadRequest("attempt must be a positive integer")
	}
	return int(n), nil
}

func runParams(r *http.Request) (int64, int, error) {
	id, err := positive(r.PathValue("id"), "run id")
	if err != nil {
		return 0, 0, err
	}
	attempt, err := attemptParam(r)
	return id, attempt, err
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

// started answers 202 with the id of the operation a call started.
func started(w http.ResponseWriter) func(op model.OperationStarted, err error) {
	return func(op model.OperationStarted, err error) {
		if err != nil {
			respond(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(op)
	}
}

// readBody reads a request body in full, refusing one over limit. Reading
// it whole lets decode see data after the first JSON value.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return nil, &Error{Status: http.StatusRequestEntityTooLarge, Msg: "request body too large"}
	}
	if err != nil {
		return nil, BadRequest("cannot read the request body: " + err.Error())
	}
	return data, nil
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	data, err := readBody(w, r, 1<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
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

// killWriteWindow bounds the answer to a kill from the route's start, on a
// server that has a WriteTimeout. The daemon waits for the runner's unit to
// stop, which systemd allows 90 seconds by default, longer than the web
// listener's WriteTimeout.
const killWriteWindow = 100 * time.Second

// rawWriteWindow bounds a raw log request from the route's start, the same
// window as the daemon's logRequestWindow, which this package cannot import.
// It also covers the Unix socket listener, which has no WriteTimeout; tests
// shorten it.
var rawWriteWindow = 50 * time.Second

// streamLog copies a raw log of size bytes to the client. Until the first
// byte is written a failure still answers with a status; after it the
// handler aborts, so the connection drops short of Content-Length and the
// browser's download fails instead of saving a truncated log.
func streamLog(w http.ResponseWriter, body io.Reader, size int64, deadline time.Time) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(deadline)
	buf := make([]byte, 32<<10)
	n, err := io.ReadAtLeast(body, buf, 1)
	if err != nil && err != io.EOF {
		respond(w, nil, &Error{Status: http.StatusBadGateway, Msg: "GitHub stopped sending the log"})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf[:n]); err != nil {
		panic(http.ErrAbortHandler)
	}
	if n == 0 {
		return
	}
	// The status line goes out now, so a later abort reaches the client as a
	// broken body rather than as no response at all.
	if err := rc.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
	if _, err := io.CopyBuffer(w, body, buf); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// heartbeat is how often an idle change stream sends a comment, so the
// browser and any proxy see the connection alive; tests shorten it.
var heartbeat = 15 * time.Second

// changesWriteWindow bounds each write to a change stream. Every write
// extends it, so the web listener's WriteTimeout never ends a stream.
var changesWriteWindow = 30 * time.Second

func changesErr(err error) error {
	switch {
	case errors.Is(err, changes.ErrNotReady):
		return &Error{Status: http.StatusServiceUnavailable, Msg: "not ready"}
	case errors.Is(err, changes.ErrFull):
		return &Error{Status: http.StatusServiceUnavailable, Msg: "too many live connections"}
	case errors.Is(err, changes.ErrClosed):
		return &Error{Status: http.StatusServiceUnavailable, Msg: "shutting down"}
	}
	return err
}

// streamChanges sends the subscription's topic sets as server-sent events
// until the client leaves, the hub ends the subscription, or a write fails.
func streamChanges(w http.ResponseWriter, r *http.Request, sub *changes.Sub) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(frame string) bool {
		// The Unix socket and test writers have no write deadline.
		_ = rc.SetWriteDeadline(time.Now().Add(changesWriteWindow))
		if _, err := io.WriteString(w, frame); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("event: hello\ndata: {}\n\n") {
		return
	}
	ping := time.NewTicker(heartbeat)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case topics, ok := <-sub.C:
			if !ok {
				return
			}
			data, _ := json.Marshal(struct {
				Topics []string `json:"topics"`
			}{topics})
			if !send("event: change\ndata: " + string(data) + "\n\n") {
				return
			}
			ping.Reset(heartbeat)
		case <-ping.C:
			if !send(": ping\n\n") {
				return
			}
		}
	}
}
