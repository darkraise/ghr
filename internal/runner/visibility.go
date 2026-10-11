package runner

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
)

// visibilityTTL is how long GitHub's answer on whether a repository is public
// is used before it is read again.
const visibilityTTL = 6 * time.Hour

// visibility is GitHub's last answer on whether a repository is public. A
// zero readAt makes the next poll read it again whatever its age.
type visibility struct {
	public bool
	readAt time.Time
}

// visibilityLocked reports whether GitHub last said repo is public, and
// whether it has answered for repo at all; m.mu must be held.
func (m *Manager) visibilityLocked(repo string) (public, known bool) {
	v, known := m.vis[strings.ToLower(repo)]
	return v.public, known
}

// readVisibility asks GitHub whether repo is public when the last answer is
// missing or visibilityTTL old. A failed read leaves the last answer as it is.
func (m *Manager) readVisibility(ctx context.Context, repo string, now time.Time) error {
	key := strings.ToLower(repo)
	m.mu.Lock()
	v, known := m.vis[key]
	gen := m.visGen
	m.mu.Unlock()
	if known && now.Sub(v.readAt) < visibilityTTL {
		return nil
	}
	r, err := m.GH.GetRepo(ctx, repo)
	if err != nil {
		return err
	}
	m.mu.Lock()
	nv := visibility{public: !r.Private, readAt: now}
	if m.visGen != gen {
		nv.readAt = time.Time{} // ClearDegraded ran during the read; the next poll reads again
	}
	m.vis[key] = nv
	m.mu.Unlock()
	return nil
}

// blockedLocked is blocked with m.mu held.
func (m *Manager) blockedLocked(cfg *config.Config, repo config.Repo) bool {
	r := cfg.Repo(repo.Name)
	if r == nil || r.AllowPublic {
		return false
	}
	public, _ := m.visibilityLocked(repo.Name)
	return public
}

// blocked reports whether repo must get no runner: GitHub last reported it
// public and cfg does not set allow_public for it. A job in a public
// repository can come from anyone's pull request.
func (m *Manager) blocked(cfg *config.Config, repo config.Repo) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blockedLocked(cfg, repo)
}

// unread reports whether GitHub has not yet said whether repo is public.
// Until it has, repo gets no new runner.
func (m *Manager) unread(repo config.Repo) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, known := m.visibilityLocked(repo.Name)
	return !known
}

// planConfig returns a copy of cfg in which every blocked repo is paused, for
// the scheduler to plan from: a blocked repo then gets no new runner and its
// idle ones stop, while its running jobs finish. Unlike a paused repo it is
// still polled and keeps its error, so the copy is never saved or shown.
func (m *Manager) planConfig(cfg *config.Config) *config.Config {
	plan := *cfg
	plan.Repos = slices.Clone(cfg.Repos)
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range plan.Repos {
		if m.blockedLocked(cfg, r) {
			plan.Repos[i].Paused = true
		}
	}
	return &plan
}

// blockedText tells the owner why a blocked repo gets no runner and what
// lifts the block.
func blockedText(repo string) string {
	return repo + " is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it"
}

// noteBlocked writes one event when a repo becomes blocked and one when it
// stops being blocked, whether GitHub's answer or the config changed. It
// needs no API call, so it runs on every tick.
func (m *Manager) noteBlocked(cfg *config.Config) {
	var became, lifted []string
	m.mu.Lock()
	for _, r := range cfg.Repos {
		key := strings.ToLower(r.Name)
		is := m.blockedLocked(cfg, r)
		switch {
		case is && !m.wasBlocked[key]:
			m.wasBlocked[key] = true
			became = append(became, r.Name)
		case !is && m.wasBlocked[key]:
			delete(m.wasBlocked, key)
			lifted = append(lifted, r.Name)
		}
	}
	m.mu.Unlock()
	for _, name := range became {
		m.Events.Add("warn", name, "%s", blockedText(name))
	}
	for _, name := range lifted {
		m.Events.Add("ok", name, "no longer blocked")
	}
}

// forgetRemoved drops what is kept about visibility for repos that are no
// longer in the config, so one that is removed and added again starts unread
// and its removal is not reported as a block being lifted. It needs no API
// call, so it runs on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.vis {
		if cfg.Repo(key) == nil {
			delete(m.vis, key)
		}
	}
	for key := range m.wasBlocked {
		if cfg.Repo(key) == nil {
			delete(m.wasBlocked, key)
		}
	}
}
