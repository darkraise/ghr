package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
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
	return Handler(a, api, testFS(), []string{"ghr.lan"}, nil), a, api
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

// setupBody is a setup request carrying a's current setup code.
func setupBody(t *testing.T, a *Auth, pw string) string {
	t.Helper()
	code, err := a.SetupCode()
	if err != nil {
		t.Fatal(err)
	}
	return `{"code":"` + code + `","password":"` + pw + `"}`
}

// local sends the request from loopback, then applies mods.
func local(mods ...func(*http.Request)) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = "127.0.0.1:5555"
		for _, m := range mods {
			m(r)
		}
	}
}

func TestSetupThenAPI(t *testing.T) {
	h, a, api := newTestHandler(t)
	rec := do(h, http.MethodGet, "/auth/state", "", nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"authenticated":false,"setup_required":true}` {
		t.Fatalf("state: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(h, http.MethodPost, "/auth/setup", body(pw), local())
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "wrong setup code; run on the host: ghr code") ||
		len(rec.Result().Cookies()) != 0 {
		t.Fatalf("setup without a code: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(h, http.MethodGet, "/auth/state", "", nil)
	if strings.TrimSpace(rec.Body.String()) != `{"authenticated":false,"setup_required":true}` {
		t.Fatalf("state after a refused setup: %s", rec.Body.String())
	}
	claimBody := setupBody(t, a, pw)
	rec = do(h, http.MethodPost, "/auth/setup", claimBody, local())
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
	if rec := do(h, http.MethodPost, "/auth/setup", claimBody, local()); rec.Code != http.StatusConflict {
		t.Fatalf("second setup: %d", rec.Code)
	}
}

func TestLoginResponses(t *testing.T) {
	h, a, _ := newTestHandler(t)
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), nil); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "no web password is set yet") {
		t.Fatalf("login before setup: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/setup", setupBody(t, a, "short"), local()); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "password must be 12 to 1024 characters") {
		t.Fatalf("short setup: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := claim(a, pw); err != nil {
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
	h, a, _ := newTestHandler(t)
	rec := do(h, http.MethodPost, "/auth/setup", setupBody(t, a, pw), local(func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }))
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure behind an HTTPS proxy")
	}
	rec = do(h, http.MethodPost, "https://ghr.lan/auth/login", body(pw), nil)
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure over TLS")
	}
	for proto, secure := range map[string]bool{"https, http": true, " HTTPS ,http": true, "http, https": false} {
		rec = do(h, http.MethodPost, "/auth/login", body(pw), func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", proto) })
		if c := sessionCookie(t, rec); c.Secure != secure {
			t.Fatalf("X-Forwarded-Proto %q: Secure %v", proto, c.Secure)
		}
	}
}

func TestLoginEndsWithTheRequest(t *testing.T) {
	h, a, _ := newTestHandler(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	defer func() { <-a.sem; <-a.sem }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- do(h, http.MethodPost, "/auth/login", body(pw), func(r *http.Request) { *r = *r.WithContext(ctx) })
	}()
	select {
	case rec := <-done:
		if len(rec.Result().Cookies()) != 0 {
			t.Fatalf("a login whose client left got a session: %v", rec.Result().Cookies())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the login handler ignored the request context")
	}
}

// Every /api/ request a session lets through reaches the API marked as the
// browser's, with the Host it sent, past the /api prefix strip. A request
// the session check refuses never reaches it.
func TestAPIRequestsCarryTheBrowserMark(t *testing.T) {
	a, _ := newTestAuth(t)
	var seen []string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, ok := api.Browser(r.Context())
		if !ok {
			host = "(no mark)"
		}
		seen = append(seen, r.Method+" "+r.URL.Path+" "+host)
		w.WriteHeader(http.StatusNoContent)
	})
	h := Handler(a, backend, testFS(), []string{"ghr.lan"}, nil)
	id, err := claim(a, pw)
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(id)); rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	byIP := func(r *http.Request) {
		withCookie(id)(r)
		r.Host = "192.168.0.99:8080"
	}
	if rec := do(h, http.MethodPatch, "/api/config", `{}`, byIP); rec.Code != http.StatusNoContent {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPatch, "/api/config", `{}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", rec.Code)
	}
	want := []string{"GET /status ghr.lan:8080", "PATCH /config 192.168.0.99:8080"}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the API saw %q, want %q", seen, want)
	}
}

func TestRequestChecks(t *testing.T) {
	h, a, _ := newTestHandler(t)
	id, err := claim(a, pw)
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
	big := body(strings.Repeat("x", authBodyLimit+1<<10))
	if rec := do(h, http.MethodPost, "/auth/login", big, nil); rec.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("oversized body: %d %s", rec.Code, rec.Body.String())
	}
	atCap := body(pw) + strings.Repeat(" ", authBodyLimit-len(body(pw)))
	if rec := do(h, http.MethodPost, "/auth/login", atCap, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("valid JSON padded to the cap: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/login", atCap+" ", nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("valid JSON padded past the cap: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw)+` {"password":"x"}`, nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid request body") {
		t.Fatalf("trailing JSON: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/logout", strings.Repeat("x", authBodyLimit+1), withCookie(id)); rec.Code != http.StatusRequestEntityTooLarge || !a.Valid(id) {
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
	id, err := claim(a, pw)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		rec := do(h, m, "/auth/nope", "", nil)
		if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s /auth/nope: %d %q", m, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	for _, tc := range []struct{ method, path, allow string }{
		{http.MethodPost, "/auth/state", "GET, HEAD"},
		{http.MethodGet, "/auth/setup", "POST"},
		{http.MethodGet, "/auth/login", "POST"},
		{http.MethodPut, "/auth/logout", "POST"},
		{http.MethodDelete, "/auth/password", "POST"},
	} {
		rec := do(h, tc.method, tc.path, "", withCookie(id))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != tc.allow ||
			!strings.Contains(rec.Body.String(), `"error":"method not allowed"`) {
			t.Errorf("%s %s: %d Allow %q %s", tc.method, tc.path, rec.Code, rec.Header().Get("Allow"), rec.Body.String())
		}
	}
	if rec := do(h, http.MethodHead, "/auth/state", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("HEAD /auth/state: %d", rec.Code)
	}
	if !a.Valid(id) {
		t.Fatal("a refused method ended the session")
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

// A JSON encoder may write a character outside the BMP as a twelve-byte
// surrogate-pair escape, so the body limit has to hold two passwords of the
// maximum length written that way.
func TestLongestPasswordsFitTheBodyLimit(t *testing.T) {
	h, a, _ := newTestHandler(t)
	current, next := strings.Repeat("\U0001F600", MaxPasswordLen), strings.Repeat("\U0001F601", MaxPasswordLen)
	id, err := claim(a, current)
	if err != nil {
		t.Fatal(err)
	}
	// U+1F600 and U+1F601 as JSON escapes. The backslash stands apart so that
	// no editor or tool turns the escape back into the character.
	const bs = `\`
	req := `{"current":"` + strings.Repeat(bs+"ud83d"+bs+"ude00", MaxPasswordLen) + `","new":"` + strings.Repeat(bs+"ud83d"+bs+"ude01", MaxPasswordLen) + `"}`
	if len(req) <= 24*MaxPasswordLen {
		t.Fatalf("the request is %d bytes; its passwords are not escaped", len(req))
	}
	if rec := do(h, http.MethodPost, "/auth/password", req, withCookie(id)); rec.Code != http.StatusNoContent {
		t.Fatalf("a %d-byte password change: %d %s", len(req), rec.Code, rec.Body.String())
	}
	if _, err := a.Login("10.0.0.1", next); err != nil {
		t.Fatalf("the new password: %v", err)
	}
}

func TestLogoutAndPasswordChange(t *testing.T) {
	h, a, _ := newTestHandler(t)
	keep, err := claim(a, pw)
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
	if rec := change(keep, "wrong password!", "another long secret"); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "wrong password") {
		t.Fatalf("wrong current: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(keep)); rec.Code != 200 {
		t.Fatalf("a wrong current password ended the session: %d", rec.Code)
	}
	rec := change(keep, pw, "another long secret")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	c := sessionCookie(t, rec)
	if c.Value == keep || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 2592000 || c.Path != "/" {
		t.Fatalf("cookie after the change %+v", c)
	}
	for name, id := range map[string]string{"the changing": keep, "another": other} {
		if rec := do(h, http.MethodGet, "/api/status", "", withCookie(id)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s session survived the change: %d", name, rec.Code)
		}
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(c.Value)); rec.Code != 200 {
		t.Fatalf("the new session: %d", rec.Code)
	}
	rec = do(h, http.MethodPost, "/auth/logout", "", withCookie(c.Value))
	if rec.Code != http.StatusNoContent || sessionCookie(t, rec).MaxAge >= 0 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(c.Value)); rec.Code != http.StatusUnauthorized {
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
		do(h, http.MethodPost, "/auth/setup", body(pw), local()),
	} {
		if rec.Code != http.StatusInternalServerError ||
			!strings.Contains(rec.Body.String(), "web password file is unreadable; run ghr web set-password") {
			t.Errorf("%d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestSetupFromAnyAddress(t *testing.T) {
	for name, mod := range map[string]func(*http.Request){
		"LAN address":     nil,
		"X-Forwarded-For": func(r *http.Request) { r.Header.Set("X-Forwarded-For", "192.168.0.10") },
		"loopback":        local(),
	} {
		h, a, _ := newTestHandler(t)
		rec := do(h, http.MethodPost, "/auth/setup", setupBody(t, a, pw), mod)
		if rec.Code != http.StatusNoContent || len(rec.Result().Cookies()) == 0 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if req, err := a.SetupRequired(); req || err != nil {
			t.Errorf("%s: setup required %v err %v", name, req, err)
		}
	}
}

// waitAPI holds every request open until its context ends.
type waitAPI struct {
	started chan struct{}
	ended   chan struct{}
}

func (a *waitAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	close(a.started)
	<-r.Context().Done()
	close(a.ended)
}

func setSessionRecheck(t *testing.T, d time.Duration) {
	t.Helper()
	old := sessionRecheck
	sessionRecheck = d
	t.Cleanup(func() { sessionRecheck = old })
}

func TestLongRequestEndsWithItsSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(a *Auth, c *clock, id string)
	}{
		{"logout", func(a *Auth, _ *clock, id string) { a.Logout(id) }},
		{"expiry", func(_ *Auth, c *clock, _ string) { c.add(sessionTTL) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSessionRecheck(t, 20*time.Millisecond)
			a, c := newTestAuth(t)
			id, err := claim(a, pw)
			if err != nil {
				t.Fatal(err)
			}
			api := &waitAPI{started: make(chan struct{}), ended: make(chan struct{})}
			h := Handler(a, api, testFS(), []string{"ghr.lan"}, nil)
			returned := make(chan struct{})
			go func() {
				do(h, http.MethodGet, "/api/changes", "", withCookie(id))
				close(returned)
			}()
			select {
			case <-api.started:
			case <-time.After(2 * time.Second):
				t.Fatal("the request never reached the API")
			}
			select {
			case <-api.ended:
				t.Fatal("a live session's request was cancelled")
			case <-time.After(100 * time.Millisecond):
			}
			tc.end(a, c, id)
			select {
			case <-api.ended:
			case <-time.After(2 * time.Second):
				t.Fatal("the request outlived its session")
			}
			<-returned
		})
	}
}

func TestRequestStopsItsSessionTimer(t *testing.T) {
	var timers []*time.Timer
	afterFunc = func(d time.Duration, f func()) *time.Timer {
		tm := time.AfterFunc(d, f)
		timers = append(timers, tm)
		return tm
	}
	t.Cleanup(func() { afterFunc = time.AfterFunc })
	h, a, _ := newTestHandler(t)
	id, err := claim(a, pw)
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(id)); rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if len(timers) != 1 {
		t.Fatalf("%d timers for one request, want 1", len(timers))
	}
	if timers[0].Stop() {
		t.Fatal("the request returned with its re-check timer still armed")
	}
}

func TestOnSessionRunsForLoggedInAPIRequests(t *testing.T) {
	a, _ := newTestAuth(t)
	id, err := claim(a, pw)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := Handler(a, &fakeAPI{}, testFS(), []string{"ghr.lan"}, func() { calls++ })
	for _, tc := range []struct {
		target string
		mod    func(*http.Request)
		want   int
	}{
		{"/api/status", nil, 0},
		{"/auth/state", withCookie(id), 0},
		{"/", withCookie(id), 0},
		{"/api/status", withCookie(id), 1},
		{"/api/status", withCookie(id), 2},
	} {
		do(h, http.MethodGet, tc.target, "", tc.mod)
		if calls != tc.want {
			t.Fatalf("GET %s: %d calls, want %d", tc.target, calls, tc.want)
		}
	}
}
