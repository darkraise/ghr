package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if len(*calls) != 1 || (*calls)[0].String() != "tar -xzf /d/2.338.0.tmp/runner.tar.gz -C /d/2.338.0.tmp" {
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
	if err := DownloadFor(100*time.Millisecond)(context.Background(), srv.URL, filepath.Join(t.TempDir(), "f")); err == nil {
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
	dst := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := DownloadFor(time.Minute)(context.Background(), srv.URL, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "archive" {
		t.Fatalf("saved %q", b)
	}
}
