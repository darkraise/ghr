package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
)

const (
	cookieName = "ghr_session"
	// authBodyLimit holds two passwords of MaxPasswordLen characters with
	// every character outside the BMP written as a twelve-byte surrogate-pair
	// JSON escape.
	authBodyLimit = 32 << 10
	csp           = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"
)

// sessionRecheck is how often an /api request still running checks that its
// session exists, so a logout ends an open change stream; tests shorten it.
var sessionRecheck = 5 * time.Second

// afterFunc is time.AfterFunc; tests replace it to see the timers.
var afterFunc = time.AfterFunc

type handler struct {
	auth    *Auth
	hosts   []string
	trusted []netip.Prefix
	// onSession runs for every request a valid session lets through; nil
	// runs nothing.
	onSession func()
}

// Handler serves the web UI's TCP listener: /auth/* for login, the control
// API under /api/ for logged-in sessions, and the app from static. A request
// from a trusted proxy is throttled by the client its X-Forwarded-For names.
// onSession, when set, runs for every /api/ request a valid session lets
// through, before the API handles it.
func Handler(a *Auth, apiHandler http.Handler, static fs.FS, hosts []string, trusted []netip.Prefix, onSession func()) http.Handler {
	h := &handler{auth: a, hosts: hosts, trusted: trusted, onSession: onSession}
	mux := http.NewServeMux()
	allow := map[string]string{}
	route := func(method, path string, fn http.HandlerFunc) {
		mux.HandleFunc(method+" "+path, fn)
		allow[path] = method
		if method == http.MethodGet {
			// The mux serves HEAD from a GET pattern.
			allow[path] = "GET, HEAD"
		}
	}
	route(http.MethodGet, "/auth/state", h.state)
	route(http.MethodPost, "/auth/setup", h.setup)
	route(http.MethodPost, "/auth/login", h.login)
	route(http.MethodPost, "/auth/logout", h.logout)
	route(http.MethodPost, "/auth/password", h.password)
	// Without this catch-all the mux would hand a wrong method or an unknown
	// /auth path to the static handler, which answers with the app's page.
	mux.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
		if methods, ok := allow[r.URL.Path]; ok {
			w.Header().Set("Allow", methods)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/api/", h.requireSession(http.StripPrefix("/api", apiHandler)))
	mux.Handle("/", staticHandler(static))
	return h.checks(mux)
}

// checks runs before routing. The Host allowlist stops DNS rebinding; the
// X-GHR header forces a CORS preflight on any cross-origin script, which ghr
// never approves; the Origin check refuses cross-site form posts. Every /auth
// POST body is read in full here, so authBodyLimit holds for routes that
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
			var tooLarge *http.MaxBytesError
			switch {
			case errors.As(err, &tooLarge):
				writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			case err != nil:
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

// requireSession serves next only for a logged-in session, and cancels the
// request's context once that session ends: logout, a password change or
// reset, or expiry. The context it hands on carries the browser mark.
func (h *handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := h.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if h.onSession != nil {
			h.onSession()
		}
		every, after := sessionRecheck, afterFunc
		ctx, cancel := context.WithCancel(api.WithBrowser(r.Context(), r.Host))
		defer cancel()
		var mu sync.Mutex
		done := false
		var timer *time.Timer
		var check func()
		check = func() {
			mu.Lock()
			defer mu.Unlock()
			if done {
				return
			}
			if !h.auth.Valid(id) {
				cancel()
				return
			}
			timer = after(every, check)
		}
		mu.Lock()
		timer = after(every, check)
		mu.Unlock()
		defer func() {
			mu.Lock()
			done = true
			timer.Stop()
			mu.Unlock()
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
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
	Code     string `json:"code"`
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
	id, err := h.auth.Setup(b.Code, b.Password)
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
	id, err := h.auth.login(r.Context(), h.clientKey(r), b.Password)
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
	if _, ok := h.session(r); !ok {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	id, err := h.auth.ChangePassword(h.clientKey(r), b.Current, b.New)
	if err != nil {
		// 401 on this route means only "not logged in", which the app answers
		// by going to the login page.
		if errors.Is(err, ErrWrongPassword) {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		h.fail(w, err)
		return
	}
	setSession(w, r, id, int(sessionTTL/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

// clientKey is the throttle key of the client behind r. Each proxy appends
// the address it took the request from to X-Forwarded-For, so walking it from
// the right while the hop is trusted ends at the nearest address no trusted
// proxy is: anything left of that the client could have written. An entry
// that does not parse ends the walk at the trusted hop that wrote it, and a
// chain trusted throughout ends at its leftmost address.
func (h *handler) clientKey(r *http.Request) string {
	hop, ok := remoteAddr(r.RemoteAddr)
	if !ok || !h.isTrusted(hop) {
		return ClientKey(r.RemoteAddr)
	}
	lines := r.Header.Values("X-Forwarded-For")
	for i := len(lines) - 1; i >= 0; i-- {
		entries := strings.Split(lines[i], ",")
		for j := len(entries) - 1; j >= 0; j-- {
			ip, ok := forwardedAddr(entries[j])
			if !ok {
				return hop.String()
			}
			hop = ip
			if !h.isTrusted(hop) {
				return hop.String()
			}
		}
	}
	return hop.String()
}

func (h *handler) isTrusted(ip netip.Addr) bool {
	return slices.ContainsFunc(h.trusted, func(p netip.Prefix) bool { return p.Contains(ip) })
}

// forwardedAddr parses an X-Forwarded-For entry, which some proxies write
// with a port or an IPv6 address in brackets.
func forwardedAddr(entry string) (netip.Addr, bool) {
	entry = strings.TrimSpace(entry)
	if ip, ok := remoteAddr(entry); ok {
		return ip, true
	}
	return remoteAddr(strings.TrimSuffix(strings.TrimPrefix(entry, "["), "]"))
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
	case errors.Is(err, ErrWrongPassword), errors.Is(err, ErrSetupCode):
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
