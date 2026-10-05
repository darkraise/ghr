package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/system"
)

type Options struct {
	ConfigPath  string
	TokenPath   string
	Socket      string
	HistoryPath string
	Paths       runner.Paths
	// ShutdownWait bounds how long shutdown waits for in-flight cleanups; an
	// unfinished cleanup resumes at the next start.
	ShutdownWait time.Duration

	// Test seams: zero values use GitHub, systemd, Docker, the host and SIGHUP.
	GitHubURL string
	Systemd   runner.Systemd
	Docker    runner.Docker
	Host      runner.Host
	Reload    <-chan os.Signal
}

func DefaultOptions() Options {
	return Options{
		ConfigPath:   "/etc/ghr/config.yaml",
		TokenPath:    "/etc/ghr/token",
		Socket:       api.DefaultSocket,
		HistoryPath:  "/var/lib/ghr/history.jsonl",
		ShutdownWait: 30 * time.Second,
		Paths: runner.Paths{
			Dist:      "/opt/ghr/dist/current",
			Instances: "/var/lib/ghr/instances",
			Logs:      "/var/lib/ghr/logs",
			Pending:   "/var/lib/ghr/pending",
			ToolCache: "/var/lib/ghr/toolcache",
			Hooks:     "/opt/ghr/hooks",
			Home:      "/home/" + runner.RunnerUser,
		},
	}
}

// listen binds the control socket before any runner is touched. The bound
// socket is the single-daemon lock: when another daemon answers on it, Run
// refuses to start instead of removing the live socket; a socket nobody
// answers is a crashed daemon's leftover and is replaced.
func listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return nil, err
	}
	if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		conn.Close()
		return nil, fmt.Errorf("another ghr daemon is already serving %s", socket)
	}
	os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// checkToken validates a candidate token with the reads the manager depends on,
// since a token that can read the repo may still lack the runner or run scopes.
func checkToken(ctx context.Context, c *github.Client, repo string) error {
	if _, err := c.GetRepo(ctx, repo); err != nil {
		return err
	}
	if _, err := c.ListRunners(ctx, repo); err != nil {
		return fmt.Errorf("listing runners failed; the token needs Administration: read/write: %w", err)
	}
	if _, err := c.ListRuns(ctx, repo, "queued"); err != nil {
		return fmt.Errorf("listing workflow runs failed; the token needs Actions: read: %w", err)
	}
	return nil
}

// Run starts the daemon and blocks until ctx is cancelled. Runner units keep running after it exits.
func Run(ctx context.Context, o Options) error {
	store, warnings, err := OpenStore(o.ConfigPath, o.TokenPath)
	if err != nil {
		return err
	}
	ln, err := listen(o.Socket)
	if err != nil {
		return err
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	served := false
	defer func() {
		if !served {
			ln.Close()
		}
	}()

	ev := events.New()
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
	owner := store.Config().Owner
	gh := github.New(owner, store.Token)
	if o.GitHubURL != "" {
		gh.BaseURL = o.GitHubURL
	}
	hist := &history.Store{Path: o.HistoryPath}
	m := &runner.Manager{
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Host: o.Host,
		Paths: o.Paths, Events: ev, History: hist, Now: time.Now, NewID: runner.RandomID,
	}
	if m.SD == nil {
		m.SD = system.Systemd{Run: system.Exec}
	}
	if m.Docker == nil {
		m.Docker = system.Docker{Run: system.Exec}
	}
	if m.Host == nil {
		m.Host = system.Host{Run: system.Exec}
	}
	if err := m.Init(); err != nil {
		return err
	}
	if err := m.Adopt(ctx); err != nil {
		return err
	}
	m.Reconcile(ctx, store.Config())

	wake := make(chan struct{}, 1)
	b := &Backend{
		Store: store, M: m, GH: gh, Events: ev, Hist: hist,
		CheckToken: func(ctx context.Context, token, repo string) error {
			c := github.New(owner, func() string { return token })
			c.BaseURL = gh.BaseURL
			return checkToken(ctx, c, repo)
		},
		Wake: func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		},
	}
	srv.Handler = api.NewServer(b)
	served = true
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("api server: %v", err)
		}
	}()
	ev.Add("info", "", "ghr daemon started (owner %s, mode %s)", owner, store.Config().Mode)

	reload := o.Reload
	if reload == nil {
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		defer signal.Stop(hup)
		reload = hup
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	tick := func() {
		m.Tick(ctx)
		b.FinalizeRemovals()
		timer.Reset(store.Config().PollInterval.D())
	}
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			srv.Shutdown(shutdownCtx)
			cancel()
			m.Close()
			done := make(chan struct{})
			go func() { m.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(o.ShutdownWait):
				log.Printf("shutdown: cleanups still running after %s; they resume at the next start", o.ShutdownWait)
			}
			return nil
		case <-reload:
			b.Reload()
		case <-wake:
			tick()
		case <-timer.C:
			tick()
		}
	}
}
