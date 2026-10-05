package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeAPI struct{ paths []string }

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	if r.URL.Path == "/status" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
		return
	}
	http.NotFound(w, r)
}

func newTestHandler(t *testing.T) (http.Handler, *Auth, *fakeAPI) {
	t.Helper()
	a, _ := newTestAuth(t)
	api := &fakeAPI{}
	return Handler(a, api, testFS(), []string{"ghr.lan"}), a, api
}

// do sends a request as the browser app would: Host ghr.lan:8080 and the
// X-GHR header on the routes that need it. mod runs last and may undo either.
func do(h http.Handler, method, target, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Host = "ghr.lan:8080"
	req.RemoteAddr = "192.168.0.10:5555"
	if strings.HasPrefix(req.URL.Path, "/api/") || (method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/auth/")) {
		req.Header.Set("X-GHR", "1")
	}
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func withCookie(id string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookieName, Value: id}) }
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", cookieName, rec.Header())
	return nil
}

func body(pw string) string { return `{"password":"` + pw + `"}` }

func TestSetupThenAPI(t *testing.T) {
	h, _, api := newTestHandler(t)
	rec := do(h, http.MethodGet, "/auth/state", "", nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"authenticated":false,"setup_required":true}` {
		t.Fatalf("state: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(h, http.MethodPost, "/auth/setup", body(pw), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 2592000 || c.Path != "/" || c.Secure {
		t.Fatalf("cookie %+v", c)
	}
	rec = do(h, http.MethodGet, "/auth/state", "", withCookie(c.Value))
	if strings.TrimSpace(rec.Body.String()) != `{"authenticated":true,"setup_required":false}` {
		t.Fatalf("state after setup: %s", rec.Body.String())
	}
	rec = do(h, http.MethodGet, "/api/status", "", withCookie(c.Value))
	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` || len(api.paths) != 1 || api.paths[0] != "GET /status" {
		t.Fatalf("api: %d %q %v", rec.Code, rec.Body.String(), api.paths)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("api cache-control %q", cc)
	}
	if rec := do(h, http.MethodPost, "/auth/setup", body(pw), nil); rec.Code != http.StatusConflict {
		t.Fatalf("second setup: %d", rec.Code)
	}
}

func TestLoginResponses(t *testing.T) {
	h, a, _ := newTestHandler(t)
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), nil); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "no web password is set yet") {
		t.Fatalf("login before setup: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/setup", body("short"), nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "password must be 12 to 1024 bytes") {
		t.Fatalf("short setup: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures {
		rec := do(h, http.MethodPost, "/auth/login", body("wrong password!"), nil)
		if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("wrong password: %d %v", rec.Code, rec.Result().Cookies())
		}
	}
	rec := do(h, http.MethodPost, "/auth/login", body(pw), nil)
	var e struct {
		Error   string `json:"error"`
		RetryAt string `json:"retry_at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &e)
	if _, err := time.Parse(time.RFC3339, e.RetryAt); rec.Code != http.StatusTooManyRequests || err != nil {
		t.Fatalf("throttled: %d %s", rec.Code, rec.Body.String())
	}
	other := do(h, http.MethodPost, "/auth/login", body(pw), func(r *http.Request) { r.RemoteAddr = "192.168.0.11:6000" })
	if other.Code != http.StatusNoContent {
		t.Fatalf("other client: %d", other.Code)
	}
	sessionCookie(t, other)
}

func TestSecureCookie(t *testing.T) {
	h, _, _ := newTestHandler(t)
	rec := do(h, http.MethodPost, "/auth/setup", body(pw), func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") })
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure behind an HTTPS proxy")
	}
	rec = do(h, http.MethodPost, "https://ghr.lan/auth/login", body(pw), nil)
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure over TLS")
	}
}

func TestRequestChecks(t *testing.T) {
	h, a, _ := newTestHandler(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, http.MethodGet, "/auth/state", "", func(r *http.Request) { r.Host = "evil.example:8080" })
	if rec.Code != http.StatusMisdirectedRequest || !strings.Contains(rec.Body.String(), "unknown host; add it to web.hosts") {
		t.Fatalf("unknown host: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Security-Policy") != csp {
		t.Fatal("421 lacks the CSP")
	}
	if rec := do(h, http.MethodGet, "/api/status", "", func(r *http.Request) { r.Host = "evil.example" }); rec.Code != http.StatusMisdirectedRequest ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("421 on /api: %d cache-control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, host := range []string{"192.168.0.99:8080", "[::1]:8080", "localhost:5173", "GHR.LAN", "ghr.lan"} {
		if rec := do(h, http.MethodGet, "/auth/state", "", func(r *http.Request) { r.Host = host }); rec.Code != 200 {
			t.Errorf("host %s: %d", host, rec.Code)
		}
	}
	noHeader := func(r *http.Request) { r.Header.Del("X-GHR") }
	if rec := do(h, http.MethodGet, "/api/status", "", noHeader); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "missing X-GHR header") {
		t.Fatalf("no header, no session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), noHeader); rec.Code != http.StatusForbidden {
		t.Fatalf("login without the header: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", nil); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "not logged in") {
		t.Fatalf("no session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie("made-up")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad session: %d", rec.Code)
	}
	foreign := func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), foreign); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "cross-origin request refused") {
		t.Fatalf("foreign origin: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), func(r *http.Request) { r.Header.Set("Origin", "null") }); rec.Code != http.StatusForbidden {
		t.Fatalf("null origin: %d", rec.Code)
	}
	otherPort := func(r *http.Request) { r.Header.Set("Origin", "https://ghr.lan:8443") }
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), otherPort); rec.Code != http.StatusNoContent {
		t.Fatalf("same host, other port: %d %s", rec.Code, rec.Body.String())
	}
	big := body(strings.Repeat("x", authBodyLimit+1))
	if rec := do(h, http.MethodPost, "/auth/login", big, nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid request body") {
		t.Fatalf("oversized body: %d %s", rec.Code, rec.Body.String())
	}
	padded := body(pw) + strings.Repeat(" ", authBodyLimit)
	if rec := do(h, http.MethodPost, "/auth/login", padded, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("valid JSON padded past the cap: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw)+` {"password":"x"}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/logout", strings.Repeat("x", authBodyLimit+1), withCookie(id)); rec.Code != http.StatusBadRequest || !a.Valid(id) {
		t.Fatalf("oversized logout: %d, session valid %v", rec.Code, a.Valid(id))
	}
	rec = do(h, http.MethodGet, "/", "", withCookie(id))
	if rec.Header().Get("Content-Security-Policy") != csp || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("security headers %v", rec.Header())
	}
}

func TestRoutes(t *testing.T) {
	h, a, api := newTestHandler(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		rec := do(h, m, "/auth/nope", "", nil)
		if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s /auth/nope: %d %q", m, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := do(h, http.MethodPost, "/", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/runners/abc123", "", nil); rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
		t.Fatalf("client route: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/nope", "", withCookie(id)); rec.Code != http.StatusNotFound || rec.Body.String() == "<html>app</html>" {
		t.Fatalf("unknown api path: %d %q", rec.Code, rec.Body.String())
	}
	if api.paths[len(api.paths)-1] != "GET /nope" {
		t.Fatalf("api saw %v", api.paths)
	}
}

func TestLogoutAndPasswordChange(t *testing.T) {
	h, a, _ := newTestHandler(t)
	keep, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	change := func(id, current, next string) *httptest.ResponseRecorder {
		return do(h, http.MethodPost, "/auth/password", `{"current":"`+current+`","new":"`+next+`"}`, withCookie(id))
	}
	if rec := do(h, http.MethodPost, "/auth/password", `{"current":"x","new":"y"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("change without a session: %d", rec.Code)
	}
	if rec := change(keep, "wrong password!", "another long secret"); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "wrong password") {
		t.Fatalf("wrong current: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(keep)); rec.Code != 200 {
		t.Fatalf("a wrong current password ended the session: %d", rec.Code)
	}
	if rec := change(keep, pw, "another long secret"); rec.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(other)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session survived the change: %d", rec.Code)
	}
	rec := do(h, http.MethodPost, "/auth/logout", "", withCookie(keep))
	if rec.Code != http.StatusNoContent || sessionCookie(t, rec).MaxAge >= 0 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(keep)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("session survived logout: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/logout", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("logout without a session: %d", rec.Code)
	}
}

func TestUnreadablePasswordFile(t *testing.T) {
	h, a, _ := newTestHandler(t)
	os.WriteFile(a.path, []byte("garbage\n"), 0o600)
	for _, rec := range []*httptest.ResponseRecorder{
		do(h, http.MethodGet, "/auth/state", "", nil),
		do(h, http.MethodPost, "/auth/login", body(pw), nil),
		do(h, http.MethodPost, "/auth/setup", body(pw), nil),
	} {
		if rec.Code != http.StatusInternalServerError ||
			!strings.Contains(rec.Body.String(), "web password file is unreadable; run ghr web reset-password") {
			t.Errorf("%d %s", rec.Code, rec.Body.String())
		}
	}
}
