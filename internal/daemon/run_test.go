package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
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
			Pending: filepath.Join(dir, "pending"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner"},
		GitHubURL: fakeGitHub(t).URL, Systemd: sd, Docker: nopDocker{}, Host: dirHost{}, Reload: reload,
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
