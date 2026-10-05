package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"theme-init.js":      {Data: []byte("init()")},
		"assets/app-1234.js": {Data: []byte("app()")},
	}
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestStaticServesFiles(t *testing.T) {
	h := staticHandler(testFS())
	rec := get(h, http.MethodGet, "/assets/app-1234.js")
	if rec.Code != 200 || rec.Body.String() != "app()" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset content-type %q", ct)
	}
	rec = get(h, http.MethodGet, "/theme-init.js")
	if rec.Code != 200 || rec.Body.String() != "init()" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("root file: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}

func TestStaticFallsBackToIndex(t *testing.T) {
	h := staticHandler(testFS())
	for _, p := range []string{"/", "/runners/abc123", "/assets", "/assets/", "/index.html", "/../index.html", "/missing.js"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: cache-control %q", p, cc)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%s: redirected to %q", p, loc)
		}
	}
}

func TestStaticMethods(t *testing.T) {
	h := staticHandler(testFS())
	if rec := get(h, http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := get(h, http.MethodHead, "/"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d body %d", rec.Code, rec.Body.Len())
	}
}

func TestStaticWithoutABuild(t *testing.T) {
	h := staticHandler(fstest.MapFS{
		".gitkeep":           {Data: []byte{}},
		"assets/app-1234.js": {Data: []byte("app()")},
	})
	for _, p := range []string{"/runners", "/", "/.gitkeep", "/assets/app-1234.js"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "ghr web UI was not built into this binary") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
}
