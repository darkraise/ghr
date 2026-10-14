package system

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHostExtractAndRunScriptArgs(t *testing.T) {
	run, calls := fake(nil)
	script, scripts := fake(nil)
	h := Host{Run: run, Script: script}
	if err := h.Extract(context.Background(), "/d/2.338.0.tmp/runner.tar.gz", "/d/2.338.0.tmp"); err != nil {
		t.Fatal(err)
	}
	if err := h.RunScript(context.Background(), "/d/2.338.0.tmp", "bin/installdependencies.sh"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].String() != "tar --no-same-owner -xzf /d/2.338.0.tmp/runner.tar.gz -C /d/2.338.0.tmp" {
		t.Fatalf("Run calls %v", *calls)
	}
	want := filepath.Join("/d/2.338.0.tmp", "bin/installdependencies.sh") + " "
	if len(*scripts) != 1 || (*scripts)[0].String() != want {
		t.Fatalf("Script calls %v, want %q", *scripts, want)
	}
	// Without a Script runner the script goes to Run.
	h.Script = nil
	if err := h.RunScript(context.Background(), "/d", "bin/x.sh"); err != nil || len(*calls) != 2 {
		t.Fatalf("fallback: %v %v", err, *calls)
	}
}

func TestHostChownRActsOnLinksThemselves(t *testing.T) {
	run, calls := fake(nil)
	if err := (Host{Run: run}).ChownR(context.Background(), "/var/lib/ghr/logs/abc123", "root"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].String() != "chown -R -h root:root /var/lib/ghr/logs/abc123" {
		t.Fatalf("calls %v", *calls)
	}
}

func TestHostRunScriptReportsFailure(t *testing.T) {
	run, _ := fake(map[string]string{filepath.Join("/d", "bin/installdependencies.sh"): "ERR:exit status 1"})
	if err := (Host{Run: run}).RunScript(context.Background(), "/d", "bin/installdependencies.sh"); err == nil {
		t.Fatal("a failed script must be an error")
	}
}

// SwitchLink needs symlinks, which Windows grants only to privileged users;
// CI runs it on Linux.
func TestSwitchLinkReplacesTheLink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(dir, "probe")); err != nil {
		t.Skip("symlinks unavailable here:", err)
	}
	old, next := filepath.Join(dir, "2.337.0"), filepath.Join(dir, "2.338.0")
	for _, d := range []string{old, next} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cur := filepath.Join(dir, "current")
	var h Host
	if err := h.SwitchLink(old, cur); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cur+".tmp", []byte("leftover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.SwitchLink(next, cur); err != nil {
		t.Fatal(err)
	}
	got, err := h.ReadLink(cur)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(next)
	if got != want {
		t.Fatalf("current resolves to %s, want %s", got, want)
	}
	if _, err := os.Lstat(cur + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("current.tmp left behind: %v", err)
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("download sent credentials")
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("tarball bytes"))
	}))
	t.Cleanup(srv.Close)
	dst := filepath.Join(t.TempDir(), "runner.tar.gz")
	if err := Download(context.Background(), srv.URL+"/ok", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "tarball bytes" {
		t.Fatalf("saved %q", b)
	}
	err := Download(context.Background(), srv.URL+"/missing", dst)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}

func TestDownloadRefusesMoreThanTheCap(t *testing.T) {
	old := maxDownload
	maxDownload = 64
	defer func() { maxDownload = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if r.URL.Query().Get("chunked") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(n))
		}
		for i := 0; i < n; i += 16 {
			w.Write(bytes.Repeat([]byte("x"), min(16, n-i)))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		name    string
		query   string
		refused bool
		created bool
	}{
		{name: "declared at the cap", query: "n=64"},
		{name: "declared over the cap", query: "n=65", refused: true},
		{name: "chunked at the cap", query: "n=64&chunked=1"},
		{name: "chunked over the cap", query: "n=65&chunked=1", refused: true, created: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "f")
			err := Download(context.Background(), srv.URL+"/?"+tc.query, dst)
			if tc.refused != (err != nil) || (err != nil && !strings.Contains(err.Error(), "larger than")) {
				t.Fatalf("err %v, refused want %v", err, tc.refused)
			}
			_, statErr := os.Stat(dst)
			if !tc.refused && statErr != nil {
				t.Fatalf("a download within the cap was not saved: %v", statErr)
			}
			if tc.refused && !tc.created && statErr == nil {
				t.Fatal("a download declared over the cap created its file")
			}
		})
	}
}

func TestDownloadFollowsARedirectOnlyToHTTPS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("sent over plain http"))
	}))
	t.Cleanup(plain.Close)
	// Another host, as GitHub serves a release download from its asset host.
	assets := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tarball bytes"))
	}))
	t.Cleanup(assets.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/to-https" {
			http.Redirect(w, r, assets.URL+"/asset", http.StatusFound)
			return
		}
		http.Redirect(w, r, plain.URL+"/asset", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	// The download client has no transport of its own, so it trusts the test
	// servers' certificate through the default one.
	old := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	defer func() { http.DefaultTransport = old }()

	dst := filepath.Join(t.TempDir(), "runner.tar.gz")
	if err := Download(context.Background(), secure.URL+"/to-https", dst); err != nil {
		t.Fatalf("a redirect to https: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "tarball bytes" {
		t.Fatalf("saved %q", b)
	}
	dst = filepath.Join(t.TempDir(), "runner.tar.gz")
	err := Download(context.Background(), secure.URL+"/to-http", dst)
	if err == nil || !strings.Contains(err.Error(), "refusing a redirect to "+plain.URL) {
		t.Fatalf("a redirect to plain http: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("what plain http sent was saved: %v", err)
	}
}

// hangingServer never answers; a request ends only when the client gives up.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadFollowsDownloadTimeout(t *testing.T) {
	srv := hangingServer(t)
	old := DownloadTimeout
	DownloadTimeout = 100 * time.Millisecond
	defer func() { DownloadTimeout = old }()
	start := time.Now()
	if err := Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "f")); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Download ignored DownloadTimeout: %v", time.Since(start))
	}
}

func TestDownloadForTimesOutAtItsOwnLimit(t *testing.T) {
	srv := hangingServer(t)
	old := DownloadTimeout
	DownloadTimeout = time.Hour
	defer func() { DownloadTimeout = old }()
	start := time.Now()
	if err := DownloadFor(100*time.Millisecond)(context.Background(), srv.URL, io.Discard); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("DownloadFor ignored its own timeout: %v", time.Since(start))
	}
}

func TestDownloadForSavesTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("archive"))
	}))
	t.Cleanup(srv.Close)
	var got bytes.Buffer
	if err := DownloadFor(time.Minute)(context.Background(), srv.URL, &got); err != nil {
		t.Fatal(err)
	}
	if got.String() != "archive" {
		t.Fatalf("wrote %q", got.String())
	}
}
