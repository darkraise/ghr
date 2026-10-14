package daemon

import (
	"context"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
)

// restartStatus is the status's restart block; st is the manager's status
// it is served with.
func (b *Backend) restartStatus(st model.Status) model.RestartStatus {
	return model.RestartStatus{
		Pending:   webPending(b.Store.Config().Web, b.WebApplied),
		BlockedBy: b.blockedBy(st),
		Available: b.RestartAvailable,
		WebListen: b.WebApplied.Listen,
		Failed:    b.lastRestartFailure(),
	}
}

func (b *Backend) lastRestartFailure() string {
	b.restartMu.Lock()
	defer b.restartMu.Unlock()
	return b.restartFailed
}

func (b *Backend) setRestartFailure(why string) {
	b.restartMu.Lock()
	b.restartFailed = why
	b.restartMu.Unlock()
}

// webPending names the web settings whose saved value differs from the one
// the running listener applied. Hosts compare as sets ignoring case, as the
// listener matches them, and trusted proxies as sets of the prefixes the
// listener parses them to.
func webPending(saved, applied config.Web) []string {
	var out []string
	if saved.Listen != applied.Listen {
		out = append(out, "web.listen")
	}
	if !slices.Equal(hostSet(saved.Hosts), hostSet(applied.Hosts)) {
		out = append(out, "web.hosts")
	}
	if !slices.Equal(prefixSet(saved), prefixSet(applied)) {
		out = append(out, "web.trusted_proxies")
	}
	return out
}

func hostSet(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, strings.ToLower(h))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func prefixSet(w config.Web) []netip.Prefix {
	out := w.TrustedProxyPrefixes()
	slices.SortFunc(out, netip.Prefix.Compare)
	return slices.Compact(out)
}

// opVerbs words a storage operation as the UI's queue line does.
var opVerbs = map[string]string{"install": "installing", "remove": "removing", "clear": "clearing"}

// blockedBy names the daemon work a restart would cut, in the order the UI
// shows it. Running jobs are not on it: their units outlive the daemon, and
// the next start adopts them.
func (b *Backend) blockedBy(st model.Status) []string {
	var out []string
	ops := b.storageOps()
	if c := ops.Current; c != nil {
		verb, ok := opVerbs[c.Kind]
		if !ok {
			verb = c.Kind
		}
		out = append(out, verb+" "+c.Target)
	}
	if ops.Queued > 0 {
		out = append(out, strconv.Itoa(ops.Queued)+" queued")
	}
	if b.M.Pruning() {
		out = append(out, "pruning")
	}
	if st.RunnerUpdate.Running {
		out = append(out, "updating the runner software")
	}
	// Preparing runs the runner's dependency script, which installs packages;
	// a restart kills its process group mid-install.
	if st.RunnerUpdate.Preparing {
		out = append(out, "preparing the runner update")
	}
	return out
}

// storageOps reads the storage queue. A snapshot that panics reads as an
// empty queue, so it cannot take the status, or its change topic, with it.
func (b *Backend) storageOps() (ops model.Operations) {
	defer func() { _ = recover() }()
	return b.Space.Snapshot().Operations
}

const restartWarning = "web settings changed; restart ghr to apply"

// warnPending writes the restart warning when the pending web settings
// become non-empty or change, and returns them. An empty list resets the
// record, so the warning returns with the next change. The caller holds
// b.pendingMu across the change it checks.
func (b *Backend) warnPending() []string {
	pending := webPending(b.Store.Config().Web, b.WebApplied)
	if len(pending) > 0 && !slices.Equal(pending, b.pendingWarned) {
		b.Events.Add("warn", "", "config: %s", restartWarning)
	}
	b.pendingWarned = pending
	return pending
}

// selfUnit is the systemd unit a restart from the UI restarts.
const selfUnit = "ghr.service"

// selfUnitCheck is the default Options.SelfUnit: whether this process is
// selfUnit's main process, asked through run. INVOCATION_ID alone is not
// enough: every process a unit starts inherits it, a job's processes on a
// runner unit among them.
func selfUnitCheck(run system.Runner) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		if os.Getenv("INVOCATION_ID") == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := run(ctx, "systemctl", "show", "-p", "MainPID", "--value", selfUnit)
		return err == nil && strings.TrimSpace(string(out)) == strconv.Itoa(os.Getpid())
	}
}

// restartUnit is the default Options.Restart: it queues a restart of
// selfUnit through run and returns without waiting for it.
func restartUnit(run system.Runner) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		_, err := run(ctx, "systemctl", "restart", "--no-block", selfUnit)
		return err
	}
}

// RequestRestart refuses a restart that cannot run now. Otherwise it writes
// the request's event and returns run, which asks systemd for the restart;
// the caller answers its request before calling run. Work that starts in
// between cancels the restart: nothing reserves the daemon for it.
func (b *Backend) RequestRestart(ctx context.Context) (run func(), err error) {
	if !b.RestartAvailable {
		return nil, api.Conflict("ghr is not running as ghr.service; restart it by hand")
	}
	if blocked := b.blockedBy(b.M.Status()); len(blocked) > 0 {
		return nil, api.Conflict("waits for: " + strings.Join(blocked, ", "))
	}
	b.setRestartFailure("")
	if _, ok := api.Browser(ctx); ok {
		b.Events.Add("info", "", "restart requested from the web UI")
	} else {
		b.Events.Add("info", "", "restart requested")
	}
	// The request ends once it is answered; the restart must not end with it.
	detached := context.WithoutCancel(ctx)
	return func() {
		if blocked := b.blockedBy(b.M.Status()); len(blocked) > 0 {
			why := strings.Join(blocked, ", ") + " started"
			b.Events.Add("warn", "", "restart cancelled: %s", why)
			b.setRestartFailure(why)
			return
		}
		if err := b.Restart(detached); err != nil {
			b.Events.Add("error", "", "restart failed: %v", err)
			b.setRestartFailure(err.Error())
		}
	}, nil
}
