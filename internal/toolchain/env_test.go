package toolchain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/system"
)

func TestDefaultSources(t *testing.T) {
	want := Sources{
		NodeManifest:   "https://raw.githubusercontent.com/actions/node-versions/main/versions-manifest.json",
		GoManifest:     "https://raw.githubusercontent.com/actions/go-versions/main/versions-manifest.json",
		PythonManifest: "https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json",
		GoReleases:     "https://go.dev/dl/?mode=json&include=all",
		GoDownload:     "https://go.dev/dl/",
		Adoptium:       "https://api.adoptium.net",
		DotnetIndex:    "https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json",
		DotnetScript:   "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh",
	}
	if got := DefaultSources(); got != want {
		t.Fatalf("sources %+v", got)
	}
}

func TestHTTPGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("body"))
	}))
	defer srv.Close()
	if b, err := httpGet(context.Background(), srv.URL+"/ok"); err != nil || string(b) != "body" {
		t.Fatalf("got %q, %v", b, err)
	}
	if _, err := httpGet(context.Background(), srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}

func TestNewEnvWiresTheRunnerAndFetch(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	fetch := func(_ context.Context, url, dst string) error {
		calls = append(calls, "fetch "+url+" "+dst)
		return nil
	}
	e := NewEnv("/var/lib/ghr/toolcache", "/home/ghrunner", "ghrunner", run, fetch)
	if e.Root != "/var/lib/ghr/toolcache" || e.Home != "/home/ghrunner" || e.User != "ghrunner" || e.Sources != DefaultSources() {
		t.Fatalf("env %+v", e)
	}
	if err := e.Extract(context.Background(), "a.tar.gz", "dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), "chown", "x"); err != nil {
		t.Fatal(err)
	}
	if err := e.Fetch(context.Background(), "https://example.test/a.tar.gz", "dst"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tar -xzf a.tar.gz -C dir", "chown x", "fetch https://example.test/a.tar.gz dst"}; !slices.Equal(calls, want) {
		t.Fatalf("calls %q", calls)
	}
	if a, b := e.OpID(), e.OpID(); a == b || a == "" {
		t.Fatalf("op ids %q, %q", a, b)
	}
}

func TestNewEnvExtractsWithRealTar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("GNU tar on Linux is the target")
	}
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not on PATH")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(archive, tarGz(map[string]string{"node-22/bin/node": "n"}), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "x")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewEnv(dir, "", "", system.Exec, system.Download).Extract(context.Background(), archive, out); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(out, "node-22", "bin", "node")) {
		t.Fatal("not extracted")
	}
}
