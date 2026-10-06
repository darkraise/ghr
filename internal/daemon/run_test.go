package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/system"
)

type recSD struct {
	mu      sync.Mutex
	started []string
	stopped []string
}

func (s *recSD) Start(_ context.Context, u system.UnitSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, u.Unit)
	return nil
}
func (s *recSD) Stop(_ context.Context, unit string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = append(s.stopped, unit)
	return nil
}
func (s *recSD) Active(context.Context, string) (bool, error) { return true, nil }
func (s *recSD) List(context.Context, string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.started...), nil
}
func (s *recSD) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.started), len(s.stopped)
}

type nopDocker struct{}

func (nopDocker) ComposeContainers(context.Context) ([]system.ComposeContainer, error) {
	return nil, nil
}
func (nopDocker) Containers(context.Context) ([]system.NamedContainer, error) { return nil, nil }
func (nopDocker) ProjectContainers(context.Context, string) ([]system.ProjectContainer, error) {
	return nil, nil
}
func (nopDocker) ContainerIDsByLabel(context.Context, string) ([]string, error) { return nil, nil }
func (nopDocker) RemoveContainers(context.Context, []string) error              { return nil }
func (nopDocker) RemoveNetworksByLabel(context.Context, string) error           { return nil }
func (nopDocker) RemoveVolumesByLabel(context.Context, string) error            { return nil }
func (nopDocker) DataRootUsage(context.Context) (int, error)                    { return 10, nil }
func (nopDocker) PruneBuildCacheOlderThan(context.Context, int) (string, error) { return "0B", nil }
func (nopDocker) PruneBuildCacheTo(context.Context, string) (string, error)     { return "0B", nil }
func (nopDocker) PruneDanglingImages(context.Context) (string, error)           { return "0B", nil }

type nopDisk struct{}

func (nopDisk) DiskUsage(context.Context) ([]system.DiskRow, error) {
	return []system.DiskRow{{Type: "Images", Count: 1}}, nil
}
func (nopDisk) BuildCacheUsage(context.Context) ([]system.CacheTypeUsage, error) { return nil, nil }
func (nopDisk) PruneAllBuildCache(context.Context) (string, error)               { return "0B", nil }
func (nopDisk) PruneUnusedVolumes(context.Context) (string, error)               { return "0B", nil }

// dirHost creates instance dirs without cp; links, extraction and scripts
// are the real host's.
type dirHost struct{ system.Host }

func (dirHost) CopyTree(_ context.Context, _, dst string) error { return os.MkdirAll(dst, 0o755) }
func (dirHost) ChownR(context.Context, string, string) error    { return nil }

// fakeGitHub serves one queued job for darkmem and accepts JIT registrations.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/generate-jitconfig"):
			w.WriteHeader(201)
			fmt.Fprint(w, `{"runner":{"id":5,"name":"x"},"encoded_jit_config":"E"}`)
		case strings.HasSuffix(p, "/darkmem/actions/runs") && r.URL.Query().Get("status") == "queued":
			fmt.Fprint(w, `{"workflow_runs":[{"id":1,"status":"queued"}]}`)
		case strings.HasSuffix(p, "/actions/runs"):
			fmt.Fprint(w, `{"workflow_runs":[]}`)
		case strings.HasSuffix(p, "/actions/runs/1/jobs"):
			fmt.Fprint(w, `{"jobs":[{"id":11,"status":"queued","labels":["self-hosted","homelab"],"created_at":"2026-10-03T12:00:00Z"}]}`)
		case strings.HasSuffix(p, "/actions/runners/5"):
			fmt.Fprint(w, `{"id":5,"status":"offline","busy":false}`)
		case strings.HasSuffix(p, "/actions/runners"):
			fmt.Fprint(w, `{"runners":[]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A token that can read the repo but not its runners or runs must be rejected,
// naming the permission it lacks.
func TestCheckTokenNeedsRunnerAndRunAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/actions/runners") && tok == "no-admin",
			strings.HasSuffix(p, "/actions/runs") && tok == "no-runs":
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
		case strings.HasSuffix(p, "/actions/runners"):
			fmt.Fprint(w, `{"runners":[]}`)
		case strings.HasSuffix(p, "/actions/runs"):
			fmt.Fprint(w, `{"workflow_runs":[]}`)
		case p == "/repos/darkraise/darkmem":
			fmt.Fprint(w, `{"full_name":"darkraise/darkmem","private":true}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	check := func(tok string) error {
		c := github.New("darkraise", func() string { return tok })
		c.BaseURL = srv.URL
		return checkToken(context.Background(), c, "darkmem")
	}
	if err := check("good"); err != nil {
		t.Fatalf("a token with every permission was rejected: %v", err)
	}
	for tok, perm := range map[string]string{"no-admin": "Administration", "no-runs": "Actions"} {
		if err := check(tok); err == nil || !strings.Contains(err.Error(), perm) {
			t.Errorf("%s: got %v, want a rejection naming %s", tok, err, perm)
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunServesTicksReloadsAndKeepsRunners(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := strings.Replace(cfgYAML, "global_max: 2", "global_max: 2\npoll_interval: 5s", 1)
	os.WriteFile(cfgPath, []byte(cfg), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok\n"), 0o600)
	dist := filepath.Join(dir, "dist", "2.330.0")
	os.MkdirAll(dist, 0o755)
	sd := &recSD{}
	reload := make(chan os.Signal, 1)
	o := Options{
		ConfigPath: cfgPath, TokenPath: filepath.Join(dir, "token"),
		Socket: filepath.Join(dir, "ghr.sock"), HistoryPath: filepath.Join(dir, "history.jsonl"),
		ShutdownWait: 5 * time.Second,
		Paths: runner.Paths{Dist: dist, Instances: filepath.Join(dir, "instances"), Logs: filepath.Join(dir, "logs"),
			Pending: filepath.Join(dir, "pending"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: filepath.Join(dir, "home")},
		GitHubURL: fakeGitHub(t).URL, Systemd: sd, Docker: nopDocker{}, Disk: nopDisk{}, Host: dirHost{}, Reload: reload,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
	c := api.NewUnixClient(o.Socket)

	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
	waitFor(t, "a tick to spawn darkmem's runner", func() bool { n, _ := sd.counts(); return n == 1 })
	st, _ := c.Status(context.Background())
	if len(st.Instances) != 1 || st.Instances[0].Repo != "darkmem" || st.Epoch == "" {
		t.Fatalf("status %+v", st)
	}

	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("second daemon: %v", err)
	}
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatalf("second daemon disturbed the first: %v", err)
	}

	os.WriteFile(cfgPath, []byte(strings.Replace(cfg, "global_max: 2", "global_max: 3", 1)), 0o600)
	reload <- os.Interrupt
	waitFor(t, "the reload", func() bool { st, err := c.Status(context.Background()); return err == nil && st.GlobalMax == 3 })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	done <- nil // for the deferred receive
	if _, stopped := sd.counts(); stopped != 0 {
		t.Fatalf("shutdown stopped %d runner units; they must keep running", stopped)
	}
}

// freeAddr returns a loopback address nothing listens on; validation rejects
// port 0, so the daemon cannot pick its own.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func testOptions(t *testing.T, cfg string) Options {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(cfg), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok\n"), 0o600)
	dist := filepath.Join(dir, "dist", "2.330.0")
	os.MkdirAll(dist, 0o755)
	return Options{
		ConfigPath: cfgPath, TokenPath: filepath.Join(dir, "token"), WebPasswordPath: filepath.Join(dir, "web-password"),
		Socket: filepath.Join(dir, "ghr.sock"), HistoryPath: filepath.Join(dir, "history.jsonl"),
		ShutdownWait: 5 * time.Second,
		Paths: runner.Paths{Dist: dist, Instances: filepath.Join(dir, "instances"), Logs: filepath.Join(dir, "logs"),
			Pending: filepath.Join(dir, "pending"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: filepath.Join(dir, "home")},
		GitHubURL: fakeGitHub(t).URL, Systemd: &recSD{}, Docker: nopDocker{}, Disk: nopDisk{}, Host: dirHost{}, Reload: make(chan os.Signal, 1),
	}
}

func TestRunServesTheWebUI(t *testing.T) {
	addr := freeAddr(t)
	webBlock := "web:\n  listen: " + addr + "\n"
	o := testOptions(t, cfgYAML+webBlock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })

	hc := &http.Client{Timeout: 10 * time.Second}
	call := func(method, path, body, session string) (*http.Response, string) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, "http://"+addr+path, rd)
		req.Header.Set("X-GHR", "1")
		if session != "" {
			req.AddCookie(&http.Cookie{Name: "ghr_session", Value: session})
		}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	resp, body := call(http.MethodGet, "/auth/state", "", "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"setup_required":true`) {
		t.Fatalf("state: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(http.MethodGet, "/", "", ""); resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("the app response lacks the CSP")
	}
	if resp, _ := call(http.MethodGet, "/api/status", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api without a session: %d", resp.StatusCode)
	}
	resp, body = call(http.MethodPost, "/auth/setup", `{"password":"correct horse battery"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	resp, body = call(http.MethodGet, "/api/status", "", session)
	if resp.StatusCode != 200 || !strings.Contains(body, `"epoch"`) {
		t.Fatalf("api with a session: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(http.MethodPost, "/api/web/reset-password", "", session); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("reset over TCP: %d", resp.StatusCode)
	}
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resp, _ := call(http.MethodGet, "/api/status", "", session); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after reset: %d", resp.StatusCode)
	}

	os.WriteFile(o.ConfigPath, []byte(cfgYAML+"web:\n  listen: "+freeAddr(t)+"\n"), 0o600)
	if ws, err := c.Reload(context.Background()); err != nil || !slices.Contains(ws, "web settings changed; restart ghr to apply") {
		t.Fatalf("reload with a new listen: %v %v", ws, err)
	}
	if resp, _ := call(http.MethodGet, "/auth/state", "", ""); resp.StatusCode != 200 {
		t.Fatalf("the listener moved on reload: %d", resp.StatusCode)
	}
	os.WriteFile(o.ConfigPath, []byte(cfgYAML+webBlock), 0o600)
	if ws, err := c.Reload(context.Background()); err != nil || len(ws) != 0 {
		t.Fatalf("reload back to the bound listen: %v %v", ws, err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	done <- nil
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener outlived Run: %v", err)
	}
	l.Close()
}

// Two runs establish the cleanup. With the web port taken and the history
// file unreadable, the port error wins, so the listener is bound before the
// manager starts. With the port free, start fails in the manager, and the
// port is free again afterwards, so that listener was closed.
func TestRunClosesTheWebListenerWhenStartFails(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := taken.Addr().String()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "web listener") {
		t.Fatalf("both broken: %v", err)
	}
	taken.Close()

	o = testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), o)
	if err == nil || strings.Contains(err.Error(), "web listener") {
		t.Fatalf("history unreadable: %v", err)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener was left open: %v", err)
	}
	l.Close()
}

func TestRunWithoutTheWebListener(t *testing.T) {
	o := testOptions(t, cfgYAML)
	if err := os.WriteFile(o.WebPasswordPath, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
	evs, err := c.Events(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	warned, listening := false, false
	for _, e := range evs {
		if e.Level == "warn" && strings.HasPrefix(e.Msg, "web password file is unreadable") {
			warned = true
		}
		if strings.Contains(e.Msg, "web UI listening") {
			listening = true
		}
	}
	if !warned || listening {
		t.Fatalf("warned %v listening %v in %+v", warned, listening, evs)
	}
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.WebPasswordPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("password file after reset: %v", err)
	}
}

func TestRunFailsWhenTheWebPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+taken.Addr().String()+"\n")
	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "web listener") {
		t.Fatalf("Run with the web port taken: %v", err)
	}
	if conn, err := net.DialTimeout("unix", o.Socket, time.Second); err == nil {
		conn.Close()
		t.Fatal("the control socket was left serving")
	}
}

func TestRunServesStorageAndSweepsTheToolCache(t *testing.T) {
	o := testOptions(t, cfgYAML)
	left := filepath.Join(o.Paths.ToolCache, ".tmp", "20261006T140000-abcdef", "partial")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return after cancel")
		}
	}()
	c := api.NewUnixClient(o.Socket)
	var st model.Storage
	waitFor(t, "the first measurement", func() bool {
		var err error
		st, err = c.Storage(context.Background())
		return err == nil && st.MeasuredAt != nil
	})
	if len(st.PackageCaches) != 10 || len(st.Docker.Rows) != 1 || st.Docker.Rows[0].Type != "Images" || st.Toolchains == nil {
		t.Fatalf("storage %+v", st)
	}
	if _, err := os.Stat(filepath.Join(o.Paths.ToolCache, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf(".tmp survived the start: %v", err)
	}
	var ae *api.Error
	if err := c.PruneScope(context.Background(), "everything"); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("unknown scope: %v", err)
	}
	if err := c.InstallToolchain(context.Background(), "ruby", "3.3"); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("unknown tool: %v", err)
	}
}
