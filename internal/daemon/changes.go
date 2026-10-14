package daemon

import (
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// setupFlagsTTL bounds how often the fingerprints read the password file and
// stat the setup marker; the watcher checks four times a second.
const setupFlagsTTL = time.Second

type setupFlagCache struct {
	mu          sync.Mutex
	at          time.Time
	webRequired bool
	pending     bool
}

// Subscribe opens a change subscription for GET /changes.
func (b *Backend) Subscribe() (*changes.Sub, error) {
	if b.Changes == nil {
		return nil, changes.ErrNotReady
	}
	return b.Changes.Subscribe()
}

// setupFlags returns WebSetupRequired and SetupPending, read at most once per
// setupFlagsTTL.
func (b *Backend) setupFlags() (webRequired, pending bool) {
	f := &b.flags
	f.mu.Lock()
	defer f.mu.Unlock()
	now := b.now()
	if f.at.IsZero() || now.Sub(f.at) >= setupFlagsTTL {
		f.webRequired = b.WebSetupRequired != nil && b.WebSetupRequired()
		f.pending = b.SetupPending != nil && b.SetupPending()
		f.at = now
	}
	return f.webRequired, f.pending
}

// fingerprints computes every change topic's fingerprint from one read of the
// manager's status and of the token metadata. A topic that fails or panics is
// left out; the hub skips it until it returns. A panic in a shared read drops
// every topic that needs it, because sync.OnceValue repeats the panic.
func (b *Backend) fingerprints() map[string]uint64 {
	status := sync.OnceValue(b.M.Status)
	meta := sync.OnceValue(func() github.TokenMeta {
		if b.GH == nil {
			return github.TokenMeta{}
		}
		return b.GH.TokenMeta()
	})
	token := func() model.TokenStatus { return tokenStatus(meta(), status()) }
	out := make(map[string]uint64, 9)
	hash := func(topic string, v func() any) {
		defer func() { _ = recover() }()
		if fp, err := changes.Fingerprint(v()); err == nil {
			out[topic] = fp
		}
	}
	count := func(topic string, n func() int64) {
		defer func() { _ = recover() }()
		out[topic] = uint64(n())
	}
	hash("status", func() any {
		st := status()
		st.WebSetupRequired, st.SetupPending = b.setupFlags()
		st.Restart, st.WebListenError = b.restartStatus(st), b.WebListenErr
		if b.GHS != nil {
			st.GitHubStorage = b.GHS.Alert()
		}
		st.Now, st.RateRemaining, st.RateLimit, st.DiskUsedBytes, st.DiskTotalBytes = time.Time{}, 0, 0, 0, 0
		return st
	})
	hash("gauges", func() any {
		st, ts := status(), token()
		// CheckedAt is left out: every GitHub response moves it, several times
		// a second while the daemon polls, and the browser's safety poll
		// refreshes it.
		return []any{st.RateRemaining, st.DiskUsedBytes, st.DiskTotalBytes, ts.RateRemaining, ts.RateLimit, ts.RateReset}
	})
	count("events", b.Events.Last)
	hash("config", b.Config)
	hash("token", func() any {
		ts := token()
		ts.RateRemaining, ts.RateLimit, ts.RateReset, ts.CheckedAt = nil, nil, nil, nil
		return ts
	})
	hash("storage", func() any { return b.storageFrom(status()) })
	count("history", b.Hist.Writes)
	count("metrics", func() int64 {
		if b.Sampler == nil {
			return 0
		}
		return b.Sampler.Writes()
	})
	if b.GHS != nil {
		count("github-storage", b.GHS.Version)
	}
	return out
}
