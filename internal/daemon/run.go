package daemon

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/metrics"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/storage"
	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
	"github.com/darkraise/ghr/internal/webui"
	"github.com/darkraise/ghr/web"
)

type Options struct {
	ConfigPath  string
	TokenPath   string
	Socket      string
	HistoryPath string
	// MetricsPath keeps the metrics rings across restarts; empty keeps them in memory.
	MetricsPath string
	// WebPasswordPath holds the web UI's password hash.
	WebPasswordPath string
	// SetupPendingPath marks an unfinished first-run setup and
	// ToolchainsPendingPath an unmade toolchain choice; setup.sh creates both
	// on a first install.
	SetupPendingPath      string
	ToolchainsPendingPath string
	Paths                 runner.Paths
	// ShutdownWait bounds how long shutdown waits for in-flight cleanups; an
	// unfinished cleanup resumes at the next start.
	ShutdownWait time.Duration

	// Test seams: zero values use GitHub, systemd, Docker, the host, an HTTP
	// download and SIGHUP.
	GitHubURL string
	Systemd   runner.Systemd
	Docker    runner.Docker
	Disk      runner.Disk
	Host      runner.Host
	Fetch     func(ctx context.Context, url, dst string) error
	Reload    <-chan os.Signal
}

// InstallTimeout bounds each command and each download of a toolchain
// install, far above system.CommandTimeout: a .NET SDK, a JDK or Python's
// pip step can take longer than that on a slow link.
const InstallTimeout = time.Hour

func DefaultOptions() Options {
	return Options{
		ConfigPath:            "/etc/ghr/config.yaml",
		TokenPath:             "/etc/ghr/token",
		Socket:                api.DefaultSocket,
		HistoryPath:           "/var/lib/ghr/history.jsonl",
		MetricsPath:           "/var/lib/ghr/metrics.json",
		WebPasswordPath:       "/etc/ghr/web-password",
		SetupPendingPath:      "/var/lib/ghr/setup-pending",
		ToolchainsPendingPath: "/var/lib/ghr/toolchains-pending",
		ShutdownWait:          30 * time.Second,
		Paths: runner.Paths{
			Dist:        "/opt/ghr/dist/current",
			Instances:   "/var/lib/ghr/instances",
			Logs:        "/var/lib/ghr/logs",
			Pending:     "/var/lib/ghr/pending",
			ToolCache:   "/var/lib/ghr/toolcache",
			Hooks:       "/opt/ghr/hooks",
			Home:        "/home/" + runner.RunnerUser,
			UpdateState: "/var/lib/ghr/runner-update.json",
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

// swapHandler serves the handler stored last, so the setup phase hands over
// to the full API without rebinding a listener.
type swapHandler struct{ h atomic.Pointer[http.Handler] }

func (s *swapHandler) Store(h http.Handler) { s.h.Store(&h) }

func (s *swapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.h.Load()).ServeHTTP(w, r)
}

// running is what a configured start builds.
type running struct {
	m       *runner.Manager
	space   *storage.Service
	b       *Backend
	sampled chan struct{}
	wake    chan struct{}
}

// start reads the owner once, then adopts and reconciles the runners already
// on the host before anything serves the full API.
func start(ctx context.Context, o Options, store *Store, ev *events.Ring, epoch string, webCfg config.Web, webSetupRequired func() bool) (*running, error) {
	owner := store.Config().Owner
	gh := github.New(owner, store.Token)
	if o.GitHubURL != "" {
		gh.BaseURL = o.GitHubURL
	}
	hist := &history.Store{Path: o.HistoryPath}
	m := &runner.Manager{
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Disk: o.Disk, Host: o.Host, Fetch: o.Fetch,
		Paths: o.Paths, Events: ev, History: hist, Now: time.Now, NewID: runner.RandomID, Epoch: epoch,
	}
	if m.SD == nil {
		m.SD = system.Systemd{Run: system.Exec}
	}
	if m.Docker == nil {
		m.Docker = system.Docker{Run: system.Exec}
	}
	if m.Disk == nil {
		m.Disk = system.Docker{Run: system.Exec}
	}
	if m.Host == nil {
		m.Host = system.Host{Run: system.Exec, Script: system.ExecGroup}
	}
	if m.Fetch == nil {
		m.Fetch = system.Download
	}
	if err := m.Init(); err != nil {
		return nil, err
	}
	if err := m.Adopt(ctx); err != nil {
		return nil, err
	}
	m.Reconcile(ctx, store.Config())

	tools := toolchain.New(toolchain.NewEnv(o.Paths.ToolCache, o.Paths.Home, runner.RunnerUser,
		system.ExecGroupFor(InstallTimeout), system.DownloadFor(InstallTimeout)))
	space := &storage.Service{Tools: tools, Docker: m.Disk, Home: o.Paths.Home, User: runner.RunnerUser,
		Busy: m.BusyCount, Events: ev}
	space.Start()
	m.PruneDone = space.Trigger

	sampler := metrics.NewSampler(func() metrics.Snapshot {
		st := m.Status()
		var s metrics.Snapshot
		for _, i := range st.Instances {
			if i.State != "cleaning" {
				s.Live++
			}
		}
		for _, r := range st.Repos {
			s.Queued += r.Queued
		}
		return s
	}, func() int { return m.Status().DiskPct })
	sampler.Path = o.MetricsPath
	loadMetrics(sampler, ev)
	sampled := make(chan struct{})
	go func() { sampler.Run(ctx, time.Minute); close(sampled) }()

	wake := make(chan struct{}, 1)
	b := &Backend{
		Store: store, M: m, GH: gh, Events: ev, Hist: hist, Space: space,
		CheckToken: func(ctx context.Context, token, repo string) error {
			c := github.New(owner, func() string { return token })
			c.BaseURL = gh.BaseURL
			return checkToken(ctx, c, repo)
		},
		Sampler:          sampler,
		WebApplied:       webCfg,
		WebSetupRequired: webSetupRequired,
		SetupPending:     func() bool { return exists(o.SetupPendingPath) },
		Wake: func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		},
	}
	return &running{m: m, space: space, b: b, sampled: sampled, wake: wake}, nil
}

// loadMetrics restores the metrics rings; an unreadable file starts them
// empty with a warning, because losing graphs must not stop the daemon.
func loadMetrics(s *metrics.Sampler, ev *events.Ring) {
	if err := s.Load(); err != nil {
		ev.Add("warn", "", "metrics history unreadable, starting empty: %v", err)
	}
}

// notConfigured is the start-up warning while ghr has no owner or token.
func notConfigured(webLn net.Listener) string {
	const cli = "run: ghr setup github --owner <owner>"
	if webLn == nil {
		return "ghr is not configured; " + cli
	}
	_, port, _ := net.SplitHostPort(webLn.Addr().String())
	return "ghr is not configured; finish setup at http://<this host>:" + port + "/setup or " + cli
}

// Run starts the daemon and blocks until ctx is cancelled. Runner units keep running after it exits.
// Without an owner or a token it serves first-run setup until both are set,
// then starts the manager in the same process.
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
	webSrv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	var webLn net.Listener
	served := false
	defer func() {
		if !served {
			ln.Close()
			if webLn != nil {
				webLn.Close()
			}
		}
	}()
	webCfg := store.Config().Web
	if webCfg.Listen != "" {
		if webLn, err = net.Listen("tcp", webCfg.Listen); err != nil {
			return fmt.Errorf("web listener: %w", err)
		}
	}
	auth := webui.NewAuth(o.WebPasswordPath, time.Now, rand.Reader)

	ev := events.New()
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
	if err := auth.Check(); err != nil {
		ev.Add("warn", "", "%v", err)
	}
	epoch := strconv.FormatInt(time.Now().UnixNano(), 36)

	reload := o.Reload
	if reload == nil {
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		defer signal.Stop(hup)
		reload = hup
	}

	var webSetupRequired func() bool
	webListen := ""
	if webLn != nil {
		// A wildcard listener reports [::] on a dual-stack host; setup output uses the configured address.
		webListen = webCfg.Listen
		webSetupRequired = func() bool {
			// An unreadable file is reported by the start-up warning instead.
			req, err := auth.SetupRequired()
			return err == nil && req
		}
	}
	ready := make(chan struct{})
	var readyOnce sync.Once
	su := &setup{
		store: store, events: ev, githubURL: o.GitHubURL,
		setupPending: o.SetupPendingPath, toolchainsPending: o.ToolchainsPendingPath,
		webListen: webListen, epoch: epoch, webSetupRequired: webSetupRequired,
		ready: func() { readyOnce.Do(func() { close(ready) }) },
	}
	handler := &swapHandler{}
	srv.Handler = socketHandler(handler, auth, ev)
	if webLn != nil {
		static, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			return err
		}
		webSrv.Handler = webui.Handler(auth, handler, static, webCfg.Hosts)
	}
	serve := func() {
		served = true
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("api server: %v", err)
			}
		}()
		if webLn == nil {
			return
		}
		go func() {
			if err := webSrv.Serve(webLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("web server: %v", err)
			}
		}()
		ev.Add("info", "", "web UI listening on %s", webLn.Addr())
		if webSetupRequired() {
			ev.Add("warn", "", "web UI on %s has no password; run: ghr web set-password", webLn.Addr())
		}
	}
	// Every return after serve goes through teardown, so no path leaves the
	// socket or the web port bound.
	teardown := func() {
		var wg sync.WaitGroup
		for _, s := range []*http.Server{srv, webSrv} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.Shutdown(shutdownCtx); err != nil {
					// Shutdown leaves connections open when its deadline passes.
					s.Close()
				}
			}()
		}
		wg.Wait()
	}

	unconfigured := !store.Configured()
	if unconfigured {
		handler.Store(su.routes(su.unconfigured()))
		serve()
		ev.Add("warn", "", "%s", notConfigured(webLn))
	wait:
		for {
			select {
			case <-ctx.Done():
				teardown()
				return nil
			case <-ready:
				break wait
			case <-reload:
				ws, err := store.Reload()
				if err != nil {
					ev.Add("error", "", "reload rejected, keeping previous config: %v", err)
					continue
				}
				for _, w := range ws {
					ev.Add("warn", "", "config: %s", w)
				}
				ev.Add("info", "", "config and token reloaded")
				if store.Configured() {
					su.ready()
				}
			}
		}
	}

	rt, err := start(ctx, o, store, ev, epoch, webCfg, webSetupRequired)
	if err != nil {
		if served {
			teardown()
		}
		return err
	}
	defer rt.space.Close()
	// Handler first: GET /setup reports configured only once the full API serves.
	handler.Store(su.routes(api.NewServer(rt.b)))
	su.backend.Store(rt.b)
	if !served {
		serve()
	}
	owner := store.Config().Owner
	if unconfigured {
		ev.Add("info", "", "ghr configured for owner %s; starting", owner)
	}
	ev.Add("info", "", "ghr daemon started (owner %s, mode %s)", owner, store.Config().Mode)

	m := rt.m
	timer := time.NewTimer(0)
	defer timer.Stop()
	tick := func() {
		m.Tick(ctx)
		rt.b.FinalizeRemovals()
		timer.Reset(store.Config().PollInterval.D())
	}
	for {
		select {
		case <-ctx.Done():
			teardown()
			m.Close()
			rt.space.Close()
			rt.b.Close()
			<-rt.sampled
			done := make(chan struct{})
			go func() {
				m.Wait()
				rt.space.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(o.ShutdownWait):
				log.Printf("shutdown: cleanups still running after %s; they resume at the next start", o.ShutdownWait)
			}
			return nil
		case <-reload:
			rt.b.Reload()
		case <-rt.wake:
			tick()
		case <-timer.C:
			tick()
		}
	}
}
