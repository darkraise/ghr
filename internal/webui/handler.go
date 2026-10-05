package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	cookieName    = "ghr_session"
	authBodyLimit = 4 << 10
	csp           = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"
)

type handler struct {
	auth  *Auth
	hosts []string
}

// Handler serves the web UI's TCP listener: /auth/* for login, the control
// API under /api/ for logged-in sessions, and the app from static.
func Handler(a *Auth, api http.Handler, static fs.FS, hosts []string) http.Handler {
	h := &handler{auth: a, hosts: hosts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/state", h.state)
	mux.HandleFunc("POST /auth/setup", h.setup)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("POST /auth/password", h.password)
	mux.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/api/", h.requireSession(http.StripPrefix("/api", api)))
	mux.Handle("/", staticHandler(static))
	return h.checks(mux)
}

// checks runs before routing. The Host allowlist stops DNS rebinding; the
// X-GHR header forces a CORS preflight on any cross-origin script, which ghr
// never approves; the Origin check refuses cross-site form posts. Every /auth
// POST body is read in full here, so the 4 KB cap holds for routes that
// ignore their body.
func (h *handler) checks(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", csp)
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		api := strings.HasPrefix(r.URL.Path, "/api/")
		auth := strings.HasPrefix(r.URL.Path, "/auth/")
		if api || auth {
			hd.Set("Cache-Control", "no-store")
		}
		if !h.hostAllowed(r.Host) {
			hd.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMisdirectedRequest)
			io.WriteString(w, "unknown host; add it to web.hosts\n")
			return
		}
		if (api || (auth && r.Method == http.MethodPost)) && r.Header.Get("X-GHR") != "1" {
			writeError(w, http.StatusForbidden, "missing X-GHR header")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && !sameHost(o, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		if auth && r.Method == http.MethodPost {
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authBodyLimit))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		next.ServeHTTP(w, r)
	})
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

func (h *handler) hostAllowed(host string) bool {
	name := hostname(host)
	if _, err := netip.ParseAddr(name); err == nil {
		return true
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	for _, a := range h.hosts {
		if strings.EqualFold(name, a) {
			return true
		}
	}
	return false
}

// sameHost compares hostnames only: nginx-proxy-manager forwards Host without
// the port the browser used.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Hostname() != "" && strings.EqualFold(u.Hostname(), hostname(host))
}

func (h *handler) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || !h.auth.Valid(c.Value) {
		return "", false
	}
	return c.Value, true
}

func (h *handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.session(r); !ok {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) state(w http.ResponseWriter, r *http.Request) {
	setup, err := h.auth.SetupRequired()
	if err != nil {
		h.fail(w, err)
		return
	}
	_, ok := h.session(r)
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": setup, "authenticated": !setup && ok})
}

type passwordBody struct {
	Password string `json:"password"`
	Current  string `json:"current"`
	New      string `json:"new"`
}

// decodeBody parses the whole body, which checks has already capped;
// json.Unmarshal, unlike a Decoder, rejects data after the JSON value.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(r.Body)
	if err == nil {
		err = json.Unmarshal(data, v)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	id, err := h.auth.Setup(b.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	setSession(w, r, id, int(sessionTTL/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	id, err := h.auth.login(r.Context(), ClientKey(r.RemoteAddr), b.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	setSession(w, r, id, int(sessionTTL/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	id, ok := h.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	h.auth.Logout(id)
	setSession(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) password(w http.ResponseWriter, r *http.Request) {
	id, ok := h.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	if err := h.auth.ChangePassword(id, b.Current, b.New); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setSession sets the session cookie; maxAge -1 deletes it. X-Forwarded-Proto
// is trusted here because it can only make the cookie stricter.
func setSession(w http.ResponseWriter, r *http.Request, id string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: r.TLS != nil || forwardedHTTPS(r),
	})
}

// forwardedHTTPS reads the first X-Forwarded-Proto entry: chained proxies
// append theirs, and the first is the scheme the browser used.
func forwardedHTTPS(r *http.Request) bool {
	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

func (h *handler) fail(w http.ResponseWriter, err error) {
	var te *ThrottledError
	switch {
	case errors.As(err, &te):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": err.Error(), "retry_at": te.RetryAt.UTC().Format(time.RFC3339),
		})
	case errors.Is(err, ErrWrongPassword):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrPasswordLength):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrPasswordSet), errors.Is(err, ErrSetupRequired):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrUnreadable):
		writeError(w, http.StatusInternalServerError, ErrUnreadable.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
