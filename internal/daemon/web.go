package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/webui"
)

// socketHandler serves the Unix socket: the control API plus routes that must
// stay unreachable from the web listener, which mounts the API handler alone.
func socketHandler(api http.Handler, auth *webui.Auth, ev *events.Ring) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /web/reset-password", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Reset(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		ev.Add("warn", "", "web password reset; the next visitor to the web UI sets a new one")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", api)
	return mux
}
