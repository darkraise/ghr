package daemon

import (
	"slices"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// restartStatus is the status's restart block; st is the manager's status
// it is served with.
func (b *Backend) restartStatus(st model.Status) model.RestartStatus {
	return model.RestartStatus{
		Pending:   webPending(b.Store.Config().Web, b.WebApplied),
		BlockedBy: b.blockedBy(st),
		Available: b.RestartAvailable,
		WebListen: b.WebApplied.Listen,
	}
}

// webPending names the web settings whose saved value differs from the one
// the running listener applied. Hosts compare as sets ignoring case, as the
// listener matches them.
func webPending(saved, applied config.Web) []string {
	var out []string
	if saved.Listen != applied.Listen {
		out = append(out, "web.listen")
	}
	if !slices.Equal(hostSet(saved.Hosts), hostSet(applied.Hosts)) {
		out = append(out, "web.hosts")
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
	return out
}

// storageOps reads the storage queue. A snapshot that panics reads as an
// empty queue, so it cannot take the status, or its change topic, with it.
func (b *Backend) storageOps() (ops model.Operations) {
	defer func() { _ = recover() }()
	return b.Space.Snapshot().Operations
}
