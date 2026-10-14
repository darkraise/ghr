package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Version
		ok   bool
	}{
		{"v0.1.26", Version{0, 1, 26}, true},
		{"v12.0.3", Version{12, 0, 3}, true},
		{"0.1.26", Version{}, false},
		{"v1.2", Version{}, false},
		{"v1.2.3-rc1", Version{}, false},
		{"v1.2.3\n", Version{}, false},
		{"dev", Version{}, false},
		{"v0.1.26-3-gabc123", Version{}, false},
		{"v99999999999999999999.0.0", Version{}, false},
	} {
		got, ok := ParseVersion(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %+v %v, want %+v %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if s := (Version{0, 1, 26}).String(); s != "v0.1.26" {
		t.Fatalf("String %q", s)
	}
}

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		less bool
	}{
		{"v0.1.9", "v0.1.10", true},
		{"v0.1.10", "v0.1.9", false},
		{"v0.1.99", "v0.2.0", true},
		{"v0.9.9", "v1.0.0", true},
		{"v1.0.0", "v1.0.0", false},
	} {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if a.Less(b) != c.less {
			t.Errorf("%s < %s: %v, want %v", c.a, c.b, a.Less(b), c.less)
		}
	}
}

// releaseServer answers releases/latest of a fake darkraise/ghr with code
// and, when loc is set, the Location loc returns for the request's host. It
// answers every other request, and one carrying credentials, with 418. It
// returns the fake repository's URL.
func releaseServer(t *testing.T, code int, loc func(host string) string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/darkraise/ghr/releases/latest" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		if loc != nil {
			w.Header().Set("Location", loc(r.Host))
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/darkraise/ghr"
}

func TestLatestReadsTheRedirect(t *testing.T) {
	for name, loc := range map[string]func(string) string{
		"relative": func(string) string { return "/darkraise/ghr/releases/tag/v0.1.26" },
		"absolute": func(host string) string { return "http://" + host + "/darkraise/ghr/releases/tag/v0.1.26" },
	} {
		// Following the redirect would reach the 418 and fail the check.
		tag, err := Latest(context.Background(), http.DefaultClient, releaseServer(t, http.StatusFound, loc))
		if err != nil || tag != "v0.1.26" {
			t.Errorf("%s: %q %v", name, tag, err)
		}
	}
	if http.DefaultClient.CheckRedirect != nil {
		t.Fatal("Latest changed the client it was given")
	}
}

func TestLatestRefusals(t *testing.T) {
	tag := func(p string) func(string) string {
		return func(string) string { return "/darkraise/ghr/releases/tag/" + p }
	}
	for _, c := range []struct {
		name string
		code int
		loc  func(host string) string
	}{
		{"no release", http.StatusNotFound, nil},
		{"an answer, not a redirect", http.StatusOK, nil},
		{"a redirect without a target", http.StatusFound, nil},
		{"another host", http.StatusFound, func(string) string { return "http://example.com/darkraise/ghr/releases/tag/v1.0.0" }},
		{"another scheme", http.StatusFound, func(host string) string { return "https://" + host + "/darkraise/ghr/releases/tag/v1.0.0" }},
		{"another repository", http.StatusFound, func(string) string { return "/darkraise/other/releases/tag/v1.0.0" }},
		{"the release list", http.StatusFound, func(string) string { return "/darkraise/ghr/releases" }},
		{"a query", http.StatusFound, tag("v1.0.0?x=1")},
		{"a fragment", http.StatusFound, tag("v1.0.0#x")},
		{"a nested path", http.StatusFound, tag("v1.0.0/x")},
		{"a named tag", http.StatusFound, tag("nightly")},
		{"a short version", http.StatusFound, tag("v1.0")},
		{"an escaped traversal", http.StatusFound, tag("..%2F..%2Fx")},
		{"a permanent redirect", http.StatusMovedPermanently, tag("v1.0.0")},
	} {
		if got, err := Latest(context.Background(), nil, releaseServer(t, c.code, c.loc)); err == nil {
			t.Errorf("%s: accepted %q", c.name, got)
		}
	}
}
