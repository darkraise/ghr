package daemon

import (
	"bufio"
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/changes"
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
		case p == "/repos/darkraise/darkcloud" || p == "/repos/darkraise/darkmem":
			fmt.Fprintf(w, `{"full_name":%q,"private":true}`, strings.TrimPrefix(p, "/repos/"))
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
	for tok, perm := range map[string]string{"no-admin": "Administration: read/write", "no-runs": "Actions: read/write"} {
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
			Pending: filepath.Join(dir, "pending"), Private: filepath.Join(dir, "private"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: filepath.Join(dir, "home")},
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

	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "already running") {
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
			Pending: filepath.Join(dir, "pending"), Private: filepath.Join(dir, "private"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: filepath.Join(dir, "home")},
		GitHubURL: fakeGitHub(t).URL, Systemd: &recSD{}, Docker: nopDocker{}, Disk: nopDisk{}, Host: dirHost{}, Reload: make(chan os.Signal, 1),
		SetupPendingPath: filepath.Join(dir, "setup-pending"), ToolchainsPendingPath: filepath.Join(dir, "toolchains-pending"),
		ArtifactKeysPath: filepath.Join(dir, "artifact-store-keys"), ArtifactCachePath: filepath.Join(dir, "artifact-store-meta.json"),
	}
}

// The startup reconcile lists runners at GitHub, which can be slow; the
// socket must answer before it finishes, because the installer waits for it.
func TestRunServesWhileReconcileLists(t *testing.T) {
	o := testOptions(t, cfgYAML)
	upstream, err := url.Parse(fakeGitHub(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/runners") {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(gh.Close)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	o.GitHubURL = gh.URL
	startRun(t, o)

	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status while runners are being listed", func() bool {
		_, err := c.Status(context.Background())
		return err == nil
	})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the startup reconcile never listed runners")
	}
	releaseOnce.Do(func() { close(release) })
}

func TestListenAllowsOneDaemonPerSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ghr.sock")
	ln, lock, err := listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(socket + ".lock"); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("lock file %v, %v", fi, err)
	}
	// With nothing answering on the socket, as when two daemons start
	// together, only the lock separates them.
	ln.Close()
	if ln2, lock2, err := listen(socket); err == nil {
		ln2.Close()
		lock2.Close()
		t.Fatal("a second daemon took the socket")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second listen: %v", err)
	}
	lock.Close()
	ln, lock, err = listen(socket)
	if err != nil {
		t.Fatalf("listen after the first daemon ended: %v", err)
	}
	ln.Close()
	lock.Close()
}

// A daemon that holds the socket but not the lock, as one from before the
// lock existed, is still refused.
func TestListenRefusesASocketSomeoneAnswers(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ghr.sock")
	old, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	go func() {
		for {
			c, err := old.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if ln, lock, err := listen(socket); err == nil {
		ln.Close()
		lock.Close()
		t.Fatal("took a socket another daemon answers on")
	} else if !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("listen: %v", err)
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
	setupRequired := func() bool {
		t.Helper()
		st, err := c.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return st.WebSetupRequired
	}
	if !setupRequired() {
		t.Fatal("a listener with no password must report web_setup_required")
	}
	waitFor(t, "the no-password warning", func() bool {
		evs, err := c.Events(context.Background(), 0)
		if err != nil {
			return false
		}
		for _, e := range evs {
			if e.Level == "warn" && strings.HasPrefix(e.Msg, "web UI on ") &&
				strings.HasSuffix(e.Msg, " has no password; set one with: ghr web set-password, or open it and enter the code from: ghr code") {
				return true
			}
		}
		return false
	})

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
	if resp.StatusCode != http.StatusUnauthorized || !setupRequired() {
		t.Fatalf("setup without a code: %d %s", resp.StatusCode, body)
	}
	code, err := c.SetupCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(eventMsgs(t, o.Socket), code) {
		t.Fatal("the setup code reached the event ring")
	}
	resp, body = call(http.MethodPost, "/auth/setup", `{"code":"`+code+`","password":"correct horse battery"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	if setupRequired() {
		t.Fatal("after setup: web_setup_required must be false")
	}
	resp, body = call(http.MethodGet, "/api/status", "", session)
	if resp.StatusCode != 200 || !strings.Contains(body, `"epoch"`) {
		t.Fatalf("api with a session: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(http.MethodPost, "/api/web/reset-password", "", session); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("reset over TCP: %d", resp.StatusCode)
	}
	if resp, _ := call(http.MethodGet, "/api/web/setup-code", "", session); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("setup code over TCP: %d", resp.StatusCode)
	}
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !setupRequired() {
		t.Fatal("after a reset: web_setup_required must be true")
	}
	if err := c.SetWebPassword(context.Background(), "set over the socket"); err != nil {
		t.Fatal(err)
	}
	if setupRequired() {
		t.Fatal("after SetWebPassword: web_setup_required must be false")
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

// Run closes what it bound when start fails. A taken web port no longer ends
// Run, so with the port taken and the history unreadable the history is what
// fails, and the control socket is closed. With the port free, or with the
// UI served on the fallback, the port is free again after the failure.
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
	if err := Run(context.Background(), o); err == nil || strings.Contains(err.Error(), "web listener") {
		t.Fatalf("both broken: %v", err)
	}
	if conn, err := net.DialTimeout("unix", o.Socket, time.Second); err == nil {
		conn.Close()
		t.Fatal("the control socket was left serving")
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

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	fallback := freeAddr(t)
	o = testOptions(t, cfgYAML+"web:\n  listen: "+busy.Addr().String()+"\n")
	o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
	if err := os.WriteFile(o.WebListenPath, []byte(fallback+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("history unreadable, UI on the fallback: Run succeeded")
	}
	l, err = net.Listen("tcp", fallback)
	if err != nil {
		t.Fatalf("the fallback listener was left open: %v", err)
	}
	l.Close()
}

// Run serves the web UI through the limited listener: a client that holds
// maxWebConnsPerClient connections has its next one closed at once, and is
// served again once it closes one.
func TestRunBoundsAWebClientsConnections(t *testing.T) {
	addr := freeAddr(t)
	startRun(t, testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n"))
	checkConnBound(t, addr)
}

// checkConnBound checks that the web listener on addr closes one client's
// connection over maxWebConnsPerClient, and serves it again once one closes.
func checkConnBound(t *testing.T, addr string) {
	t.Helper()
	var held []net.Conn
	t.Cleanup(func() {
		for _, c := range held {
			c.Close()
		}
	})
	waitFor(t, "the web listener", func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		held = append(held, c)
		return true
	})
	for len(held) < maxWebConnsPerClient+1 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	// Connections are accepted in the order they arrived, so the last one
	// dialled is the one over the bound. The deadline only ends a failing
	// run, and it is shorter than the 10 s after which the server closes a
	// connection that sent no request.
	over := held[maxWebConnsPerClient]
	over.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := over.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connection %d of one client was not closed: %v", maxWebConnsPerClient+1, err)
	}
	held[0].Close()
	waitFor(t, "an answer once a connection closed", func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		defer c.Close()
		fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", addr)
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, err = c.Read(make([]byte, 1))
		return err == nil
	})
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
	if st, err := c.Status(context.Background()); err != nil || st.WebSetupRequired {
		t.Fatalf("without a listener: web_setup_required %v err %v", st.WebSetupRequired, err)
	}
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.WebPasswordPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("password file after reset: %v", err)
	}
}

// statusOver waits for the socket to serve the status and returns it.
func statusOver(t *testing.T, socket string) model.Status {
	t.Helper()
	c := api.NewUnixClient(socket)
	var st model.Status
	waitFor(t, "the socket to serve status", func() bool {
		var err error
		st, err = c.Status(context.Background())
		return err == nil
	})
	return st
}

// With the web port taken and no address to fall back to, the daemon runs
// without the web UI, and the status and an event say why.
func TestRunWithoutTheWebUIWhenItsPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr := taken.Addr().String()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	startRun(t, o)
	st := statusOver(t, o.Socket)
	want := model.WebListenError{Address: addr, Reason: "address already in use"}
	if st.WebListenError == nil || *st.WebListenError != want {
		t.Fatalf("listen error %+v, want %+v", st.WebListenError, want)
	}
	if st.Restart.WebListen != "" || !slices.Equal(st.Restart.Pending, []string{"web.listen"}) || st.WebSetupRequired {
		t.Fatalf("restart %+v, web_setup_required %v", st.Restart, st.WebSetupRequired)
	}
	msgs := eventMsgs(t, o.Socket)
	if !strings.Contains(msgs, "web.listen "+addr+": address already in use; the web UI is off until it is fixed") ||
		strings.Contains(msgs, "web UI listening") {
		t.Fatalf("events %q", msgs)
	}
	if ss, ok := setupStateOver(o.Socket); !ok || ss.WebListen != "" {
		t.Fatalf("setup state %+v", ss)
	}
}

// A web.listen that fails to bind falls back to the address a browser last
// used, behind the same connection bound.
func TestRunFallsBackToTheLastWebAddress(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr, fallback := taken.Addr().String(), freeAddr(t)
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
	if err := os.WriteFile(o.WebListenPath, []byte(fallback+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	startRun(t, o)
	st := statusOver(t, o.Socket)
	want := model.WebListenError{Address: addr, Reason: "address already in use", ServingOn: fallback}
	if st.WebListenError == nil || *st.WebListenError != want {
		t.Fatalf("listen error %+v, want %+v", st.WebListenError, want)
	}
	if st.Restart.WebListen != fallback || !slices.Equal(st.Restart.Pending, []string{"web.listen"}) {
		t.Fatalf("restart %+v", st.Restart)
	}
	if msgs := eventMsgs(t, o.Socket); !strings.Contains(msgs, "web.listen "+addr+": address already in use; serving the web UI on "+fallback) {
		t.Fatalf("events %q", msgs)
	}
	if ss, ok := setupStateOver(o.Socket); !ok || ss.WebListen != fallback {
		t.Fatalf("setup state %+v", ss)
	}
	checkConnBound(t, fallback)
}

// A web address file that cannot be read, or holds no address, offers no
// fallback. The names are short: the subtest's temporary directory holds the
// control socket, whose path may not exceed 108 bytes.
func TestRunIgnoresABadWebAddressFile(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr := taken.Addr().String()
	for name, prepare := range map[string]func(path string) error{
		"dir":     func(p string) error { return os.Mkdir(p, 0o755) },
		"garbage": func(p string) error { return os.WriteFile(p, []byte("garbage\n"), 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
			o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
			if err := prepare(o.WebListenPath); err != nil {
				t.Fatal(err)
			}
			startRun(t, o)
			st := statusOver(t, o.Socket)
			want := model.WebListenError{Address: addr, Reason: "address already in use"}
			if st.WebListenError == nil || *st.WebListenError != want {
				t.Fatalf("listen error %+v, want %+v", st.WebListenError, want)
			}
		})
	}
}

func TestRunRestartsThroughTheSeam(t *testing.T) {
	o := testOptions(t, cfgYAML)
	restarted := make(chan error, 1)
	o.SelfUnit = func(context.Context) bool { return true }
	o.Restart = func(ctx context.Context) error {
		restarted <- ctx.Err()
		return nil
	}
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "a status that allows a restart", func() bool {
		st, err := c.Status(context.Background())
		return err == nil && st.Restart.Available && len(st.Restart.BlockedBy) == 0
	})
	code, body, err := socketCall(o.Socket, http.MethodPost, "/restart", "")
	if err != nil || code != http.StatusAccepted {
		t.Fatalf("restart: %d %s %v", code, body, err)
	}
	select {
	case err := <-restarted:
		if err != nil {
			t.Fatalf("the restart ran with an ended context: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the restart seam never ran")
	}
	if msgs := eventMsgs(t, o.Socket); !strings.Contains(msgs, "restart requested") {
		t.Fatalf("no restart event in %q", msgs)
	}
}

// The web address is recorded once a browser has used it with a session,
// not when it is bound or at a login.
func TestRunRecordsTheWebAddressABrowserUsed(t *testing.T) {
	addr := freeAddr(t)
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
	startRun(t, o)
	statusOver(t, o.Socket)
	absent := func(when string) {
		t.Helper()
		if _, err := os.Stat(o.WebListenPath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("recorded %s: %v", when, err)
		}
	}
	if resp, _ := webCall(t, addr, http.MethodGet, "/auth/state", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("state: %d", resp.StatusCode)
	}
	absent("after the bind alone")
	resp, body := webCall(t, addr, http.MethodPost, "/auth/setup", claimBody(t, o.Socket), "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	absent("at the login")
	if resp, body := webCall(t, addr, http.MethodGet, "/api/status", "", session); resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, body)
	}
	if data, err := os.ReadFile(o.WebListenPath); err != nil || string(data) != addr+"\n" {
		t.Fatalf("recorded %q %v, want %q", data, err, addr+"\n")
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

func TestRunMeasuresAfterAPrune(t *testing.T) {
	o := testOptions(t, cfgYAML)
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
	var first time.Time
	waitFor(t, "the first measurement", func() bool {
		st, err := c.Storage(context.Background())
		if err != nil || st.MeasuredAt == nil || st.Measuring {
			return false
		}
		first = *st.MeasuredAt
		return true
	})
	time.Sleep(10 * time.Millisecond) // so the next measurement's time differs on a coarse clock
	if err := c.PruneScope(context.Background(), runner.ScopeBuildCacheAll); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a measurement after the prune", func() bool {
		st, err := c.Storage(context.Background())
		return err == nil && st.MeasuredAt != nil && st.MeasuredAt.After(first)
	})
}

// startRun runs the daemon until the test ends; done receives Run's result.
func startRun(t *testing.T, o Options) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { done <- Run(ctx, o); close(finished) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return")
		}
	})
	return cancel, done
}

// unconfiguredOptions is testOptions with no owner and no token file, a
// GitHub that answers first-run setup, and a reload channel the test sends on.
func unconfiguredOptions(t *testing.T, extra string) (Options, chan os.Signal) {
	t.Helper()
	o := testOptions(t, strings.Replace(cfgYAML, "owner: darkraise", `owner: ""`, 1)+extra)
	os.Remove(o.TokenPath)
	o.GitHubURL = fakeSetupGitHub(t).URL
	reload := make(chan os.Signal, 1)
	o.Reload = reload
	return o, reload
}

// socketCall is a raw request over the Unix socket. The client's HTTP client
// has no timeout, so the request carries its own.
func socketCall(socket, method, path, body string) (int, string, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, method, "http://ghr"+path, rd)
	resp, err := api.NewUnixClient(socket).HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func setupStateOver(socket string) (model.SetupState, bool) {
	code, body, err := socketCall(socket, http.MethodGet, "/setup", "")
	var st model.SetupState
	if err != nil || code != http.StatusOK || json.Unmarshal([]byte(body), &st) != nil {
		return st, false
	}
	return st, true
}

// claimBody is a browser's setup request, carrying the code the socket gives.
func claimBody(t *testing.T, socket string) string {
	t.Helper()
	code, err := api.NewUnixClient(socket).SetupCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return `{"code":"` + code + `","password":"correct horse battery"}`
}

// webCall calls the web listener with the X-GHR header and an optional session.
func webCall(t *testing.T, addr, method, path, body, session string) (*http.Response, string) {
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
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func eventMsgs(t *testing.T, socket string) string {
	t.Helper()
	evs, err := api.NewUnixClient(socket).Events(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, e := range evs {
		msgs = append(msgs, e.Msg)
	}
	return strings.Join(msgs, "\n")
}

func TestRunConfiguresFromTheWebUI(t *testing.T) {
	addr := freeAddr(t)
	_, port, _ := net.SplitHostPort(addr)
	o, _ := unconfiguredOptions(t, "web:\n  listen: "+addr+"\n")
	cancel, done := startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if st, _ := setupStateOver(o.Socket); st.Configured || st.Starting || st.Owner != "" || st.WebListen != addr {
		t.Fatalf("before configuring: %+v", st)
	}
	if resp, _ := webCall(t, addr, http.MethodGet, "/api/setup", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("setup state without a session: %d", resp.StatusCode)
	}
	if resp, _ := webCall(t, addr, http.MethodPost, "/api/setup/github", `{"owner":"darkraise","token":"good"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("configure without a session: %d", resp.StatusCode)
	}
	resp, body := webCall(t, addr, http.MethodPost, "/auth/setup", claimBody(t, o.Socket), "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("claim: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	resp, body = webCall(t, addr, http.MethodGet, "/api/status", "", session)
	var before model.Status
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &before) != nil || !before.Unconfigured || before.Epoch == "" {
		t.Fatalf("status before: %d %s", resp.StatusCode, body)
	}
	if resp, body := webCall(t, addr, http.MethodGet, "/api/config", "", session); resp.StatusCode != http.StatusServiceUnavailable ||
		!strings.Contains(body, "ghr is not configured yet; finish setup first") {
		t.Fatalf("config before: %d %s", resp.StatusCode, body)
	}
	if resp, body := webCall(t, addr, http.MethodPost, "/api/setup/github", `{"owner":"darkraise","token":"good"}`, session); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("configure: %d %s", resp.StatusCode, body)
	}
	waitFor(t, "the full API", func() bool { st, _ := setupStateOver(o.Socket); return st.Configured })
	resp, body = webCall(t, addr, http.MethodGet, "/api/status", "", session)
	var after model.Status
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &after) != nil || after.Unconfigured || after.Epoch != before.Epoch {
		t.Fatalf("status after, same session: %d %s (epoch before %q)", resp.StatusCode, body, before.Epoch)
	}
	waitFor(t, "the start event", func() bool { return strings.Contains(eventMsgs(t, o.Socket), "ghr daemon started") })
	msgs := eventMsgs(t, o.Socket)
	for _, want := range []string{
		"ghr is not configured; finish setup at http://<this host>:" + port + "/setup or run: ghr setup github --owner <owner>",
		"ghr configured for owner DarkRaise; starting",
		"ghr daemon started (owner DarkRaise, mode queue)",
	} {
		if !strings.Contains(msgs, want) {
			t.Errorf("events lack %q:\n%s", want, msgs)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener outlived Run: %v", err)
	}
	l.Close()
}

func TestRunConfiguresOnReload(t *testing.T) {
	o, reload := unconfiguredOptions(t, "")
	startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if code, body, _ := socketCall(o.Socket, http.MethodGet, "/config", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("config before: %d %s", code, body)
	}
	if !strings.Contains(eventMsgs(t, o.Socket), "ghr is not configured; run: ghr setup github --owner <owner>") {
		t.Fatal("no start-up warning without the web UI")
	}
	os.WriteFile(o.ConfigPath, []byte(cfgYAML), 0o600)
	reload <- os.Interrupt
	waitFor(t, "the owner-only reload", func() bool { return strings.Contains(eventMsgs(t, o.Socket), "config and token reloaded") })
	if st, _ := setupStateOver(o.Socket); st.Configured || st.Starting || st.Owner != "darkraise" {
		t.Fatalf("owner without a token: %+v", st)
	}
	os.WriteFile(o.TokenPath, []byte("good\n"), 0o600)
	reload <- os.Interrupt
	waitFor(t, "the full API", func() bool { st, _ := setupStateOver(o.Socket); return st.Configured })
	if st, err := api.NewUnixClient(o.Socket).Status(context.Background()); err != nil || st.Unconfigured {
		t.Fatalf("status after: %+v %v", st, err)
	}
}

// Before ghr is configured a reload is handled by Run itself. It too writes a
// warning only when the load before did not have it; the start-up load did.
func TestRunUnconfiguredReloadRepeatsNoWarning(t *testing.T) {
	o, reload := unconfiguredOptions(t, "")
	cfg, err := os.ReadFile(o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(o.ConfigPath, []byte(strings.Replace(string(cfg), "global_max: 2", "global_max: 500", 1)), 0o600)
	startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	reload <- os.Interrupt
	waitFor(t, "the reload", func() bool { return strings.Contains(eventMsgs(t, o.Socket), "config and token reloaded") })
	if n := strings.Count(eventMsgs(t, o.Socket), "global_max 500 is above 99; using 99"); n != 1 {
		t.Fatalf("the warning was written %d times:\n%s", n, eventMsgs(t, o.Socket))
	}
}

func TestRunConfiguresOnceWhenReloadAndSetupRace(t *testing.T) {
	o, reload := unconfiguredOptions(t, "")
	startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	os.WriteFile(o.ConfigPath, []byte(cfgYAML), 0o600)
	os.WriteFile(o.TokenPath, []byte("good\n"), 0o600)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		socketCall(o.Socket, http.MethodPost, "/setup/github", `{"owner":"darkraise","token":"good"}`)
	}()
	reload <- os.Interrupt
	wg.Wait()
	waitFor(t, "the full API", func() bool { st, _ := setupStateOver(o.Socket); return st.Configured })
	waitFor(t, "the start event", func() bool { return strings.Contains(eventMsgs(t, o.Socket), "ghr daemon started") })
	if n := strings.Count(eventMsgs(t, o.Socket), "ghr daemon started"); n != 1 {
		t.Fatalf("the manager started %d times", n)
	}
}

func TestRunStopsWhileUnconfigured(t *testing.T) {
	addr := freeAddr(t)
	o, _ := unconfiguredOptions(t, "web:\n  listen: "+addr+"\n")
	cancel, done := startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if err := api.NewUnixClient(o.Socket).SetWebPassword(context.Background(), "set over the socket"); err != nil {
		t.Fatalf("set-password while unconfigured: %v", err)
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
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener outlived Run: %v", err)
	}
	l.Close()
	if _, err := api.NewUnixClient(o.Socket).Status(context.Background()); err == nil {
		t.Fatal("the socket still answers")
	}
}

func TestRunClosesListenersWhenAConfiguredStartFails(t *testing.T) {
	addr := freeAddr(t)
	o, _ := unconfiguredOptions(t, "web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, done := startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if code, body, err := socketCall(o.Socket, http.MethodPost, "/setup/github", `{"owner":"darkraise","token":"good"}`); err != nil || code != http.StatusNoContent {
		t.Fatalf("configure: %d %s %v", code, body, err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a start with an unreadable history returned nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the failed start")
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener was left open: %v", err)
	}
	l.Close()
}

func TestRunReportsAPendingSetup(t *testing.T) {
	o := testOptions(t, cfgYAML)
	os.WriteFile(o.SetupPendingPath, nil, 0o600)
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket", func() bool { _, err := c.Status(context.Background()); return err == nil })
	if st, _ := c.Status(context.Background()); !st.SetupPending || st.Unconfigured {
		t.Fatalf("status %+v", st)
	}
	if st, _ := setupStateOver(o.Socket); !st.Configured || !st.SetupPending || st.Owner != "darkraise" {
		t.Fatalf("setup state %+v", st)
	}
}

// readFrames sends each server-sent event of body, without its closing blank
// line, and closes the channel when body ends.
func readFrames(body io.Reader) <-chan string {
	frames := make(chan string, 16)
	go func() {
		defer close(frames)
		rd := bufio.NewReader(body)
		var sb strings.Builder
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" {
				frames <- sb.String()
				sb.Reset()
				continue
			}
			sb.WriteString(line)
		}
	}()
	return frames
}

func nextFrame(t *testing.T, frames <-chan string, what string) string {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatalf("the stream ended while waiting for %s", what)
		}
		return f
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
	return ""
}

// A configured daemon streams changes on its socket, and shutting it down
// ends an open stream instead of waiting on it.
func TestRunStreamsChangesAndEndsThemOnShutdown(t *testing.T) {
	// With an hour-long tick, only the event ring's wake can publish the change.
	old := hubOptions
	hubOptions = changes.Options{Tick: time.Hour}
	t.Cleanup(func() { hubOptions = old })
	o := testOptions(t, cfgYAML)
	reload := make(chan os.Signal, 1)
	o.Reload = reload
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	returned := false
	defer func() {
		cancel()
		if !returned {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
			}
		}
	}()
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })

	stream := &http.Client{Transport: c.HTTP.Transport}
	req, err := http.NewRequest(http.MethodGet, "http://ghr/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stream.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /changes: %d", resp.StatusCode)
	}
	frames := readFrames(resp.Body)
	next := func(what string) string {
		t.Helper()
		return nextFrame(t, frames, what)
	}
	if f := next("hello"); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	reload <- os.Interrupt
	for !strings.Contains(next(`a change naming "events"`), `"events"`) {
	}

	cancel()
	// Shutdown gives up on open connections after 5 s; the stream must end
	// well before that, through the hub closing it.
	deadline := time.After(3 * time.Second)
	for closed := false; !closed; {
		select {
		case _, ok := <-frames:
			closed = !ok
		case <-deadline:
			t.Fatal("the change stream was still open 3 s into shutdown")
		}
	}
	select {
	case err := <-done:
		returned = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within 10 s")
	}
}

// The web listener's WriteTimeout counts from the moment a request is read.
// A change stream outlives it only while the handler can still move the
// connection's write deadline through every wrapper in front of it.
func TestRunStreamsChangesPastTheWebWriteTimeout(t *testing.T) {
	oldHub, oldTimeout := hubOptions, webWriteTimeout
	hubOptions = changes.Options{Tick: time.Hour}
	webWriteTimeout = 100 * time.Millisecond
	t.Cleanup(func() { hubOptions, webWriteTimeout = oldHub, oldTimeout })
	addr := freeAddr(t)
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	// A login at the default iteration count would outlast the short timeout.
	const password = "correct horse battery"
	salt := make([]byte, 16)
	key, err := pbkdf2.Key(sha256.New, password, salt, 1000, 32)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawStdEncoding
	hash := "pbkdf2-sha256$1000$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key) + "\n"
	if err := os.WriteFile(o.WebPasswordPath, []byte(hash), 0o600); err != nil {
		t.Fatal(err)
	}
	reload := make(chan os.Signal, 1)
	o.Reload = reload
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

	hc := &http.Client{}
	login, err := http.NewRequest(http.MethodPost, "http://"+addr+"/auth/login", strings.NewReader(`{"password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("X-GHR", "1")
	resp, err := hc.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	var session *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck
		}
	}
	if session == nil {
		t.Fatal("login set no session cookie")
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-GHR", "1")
	req.AddCookie(session)
	stream, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/changes: %d", stream.StatusCode)
	}
	frames := readFrames(stream.Body)
	if f := nextFrame(t, frames, "hello"); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	time.Sleep(3 * webWriteTimeout)
	reload <- os.Interrupt
	for !strings.Contains(nextFrame(t, frames, `a change naming "events" after the write timeout`), `"events"`) {
	}
}

func TestDefaultOptionsHaveARootOnlyRecordDir(t *testing.T) {
	if got := DefaultOptions().Paths.Private; got != "/var/lib/ghr/private" {
		t.Fatalf("private dir %q", got)
	}
}

// With no runner repository configured, the running daemon checks a
// replacement token by listing the owner's repositories with it.
func TestRunChecksANewTokenWithoutRunnerRepos(t *testing.T) {
	o := testOptions(t, "owner: DarkRaise\nlabels: [homelab]\n")
	o.GitHubURL = fakeSetupGitHub(t).URL
	startRun(t, o)
	waitFor(t, "the full API", func() bool { st, ok := setupStateOver(o.Socket); return ok && st.Configured })
	code, body, err := socketCall(o.Socket, http.MethodPut, "/token", "other")
	if err != nil || code != http.StatusBadRequest ||
		!strings.Contains(body, "the token cannot see any repository of DarkRaise; grant it access to at least one") {
		t.Fatalf("a token that sees no repository of the owner: %d %s %v", code, body, err)
	}
	if data, _ := os.ReadFile(o.TokenPath); string(data) != "tok\n" {
		t.Fatalf("the refused token was saved: %q", data)
	}
	if code, body, err := socketCall(o.Socket, http.MethodPut, "/token", "good"); err != nil || code != http.StatusNoContent {
		t.Fatalf("a token that sees one: %d %s %v", code, body, err)
	}
	if data, _ := os.ReadFile(o.TokenPath); string(data) != "good\n" {
		t.Fatalf("token file %q", data)
	}
}
