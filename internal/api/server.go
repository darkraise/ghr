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

	"github.com/darkraise/ghr/internal/model"
)

// Error carries an HTTP status to the client.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func BadRequest(msg string) error { return &Error{Status: http.StatusBadRequest, Msg: msg} }
func NotFound(msg string) error   { return &Error{Status: http.StatusNotFound, Msg: msg} }
func Conflict(msg string) error   { return &Error{Status: http.StatusConflict, Msg: msg} }

// Backend is implemented by the daemon.
type Backend interface {
	Status() model.Status
	EventsAfter(seq int64) []model.Event
	History(repo, conclusion string, limit int) ([]model.HistoryEntry, error)
	RunnerLog(id, cursor string) (model.LogChunk, error)
	RunnerSteps(ctx context.Context, id string) ([]model.Step, error)
	RunnerContainers(ctx context.Context, id string) ([]model.Container, error)
	Config() any
	PatchConfig(p model.ConfigPatch) error
	AddRepo(ctx context.Context, req model.AddRepoRequest) error
	RemoveRepo(name string) error
	SetPausedAll(paused bool) error
	SetToken(ctx context.Context, token string) error
	KillRunner(ctx context.Context, id string) error
}

func NewServer(b Backend) http.Handler {
	mux := http.NewServeMux()
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
		h, err := b.History(q.Get("repo"), q.Get("conclusion"), limit)
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
	mux.HandleFunc("DELETE /repos/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.RemoveRepo(r.PathValue("name")))
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
	return mux
}

func pausePatch(name string, paused bool) model.ConfigPatch {
	return model.ConfigPatch{Repos: map[string]model.RepoPatch{name: {Paused: &paused}}}
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
		status := http.StatusInternalServerError
		var ae *Error
		if errors.As(err, &ae) {
			status = ae.Status
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, v)
}
