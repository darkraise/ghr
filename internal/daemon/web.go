package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/webui"
)

// maxPasswordBody caps the set-password body. JSON escapes <, > and & as six
// bytes each, so a 1024-byte password can encode to over 6 KiB.
const maxPasswordBody = 16 << 10

// socketHandler serves the Unix socket: the control API plus routes that must
// stay unreachable from the web listener, which mounts the API handler alone.
func socketHandler(api http.Handler, auth *webui.Auth, ev *events.Ring) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /web/reset-password", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Reset(); err != nil {
			writeSocketError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ev.Add("warn", "", "web password reset; the next visitor to the web UI sets a new one")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /web/set-password", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		// Unmarshal, unlike a Decoder, rejects data after the JSON value.
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPasswordBody))
		if err == nil {
			err = json.Unmarshal(data, &body)
		}
		if err != nil {
			writeSocketError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := auth.Set(body.Password); err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, webui.ErrPasswordLength) {
				code = http.StatusBadRequest
			}
			writeSocketError(w, code, err.Error())
			return
		}
		ev.Add("info", "", "web password set from the command line; every browser was logged out")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", api)
	return mux
}

func writeSocketError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
