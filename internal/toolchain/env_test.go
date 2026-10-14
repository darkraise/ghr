package toolchain

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestHTTPGetFollowsARedirectOnlyToHTTPS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("sent over plain http"))
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			w.Write([]byte("body"))
		case "/to-https":
			http.Redirect(w, r, "/list", http.StatusFound)
		default:
			http.Redirect(w, r, plain.URL+"/list", http.StatusFound)
		}
	}))
	defer secure.Close()
	// The version-list client has no transport of its own, so it trusts the
	// test server's certificate through the default one.
	old := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	defer func() { http.DefaultTransport = old }()

	if b, err := httpGet(context.Background(), secure.URL+"/to-https"); err != nil || string(b) != "body" {
		t.Fatalf("a redirect to https: %q, %v", b, err)
	}
	b, err := httpGet(context.Background(), secure.URL+"/to-http")
	if err == nil || !strings.Contains(err.Error(), "refusing a redirect to "+plain.URL) {
		t.Fatalf("a redirect to plain http: %q, %v", b, err)
	}
}

func TestNewEnvWiresTheRunnerAndFetch(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	fetch := func(_ context.Context, url string, _ io.Writer) error {
		calls = append(calls, "fetch "+url)
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
	if err := e.Fetch(context.Background(), "https://example.test/a.tar.gz", io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tar --no-same-owner -xzf a.tar.gz -C dir", "chown x", "fetch https://example.test/a.tar.gz"}; !slices.Equal(calls, want) {
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
	if err := NewEnv(dir, "", "", system.Exec, system.DownloadFor(time.Minute)).Extract(context.Background(), archive, out); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(out, "node-22", "bin", "node")) {
		t.Fatal("not extracted")
	}
}

func TestNewEnvChownsToTheRunnerUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix owners")
	}
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "f"))
	r := openRoot(t, dir)
	if err := NewEnv(dir, "", me.Username, nil, nil).Chown(r, "f"); err != nil {
		t.Fatal(err)
	}
	if err := NewEnv(dir, "", "ghr-no-such-user", nil, nil).Chown(r, "f"); err == nil {
		t.Fatal("chowned to a user that does not exist")
	}
}
