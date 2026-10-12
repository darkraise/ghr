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

// visibility is what GitHub last said about whether a repository is public.
// public and readAt are GetRepo's answer and its time; a zero readAt makes
// the next poll ask GetRepo again whatever the answer's age. listedAt is the
// time of the tick in which a runs listing last said public, zero when none
// has since GetRepo cleared it. The two are kept apart so that a listing
// blocks without making GetRepo's answer look fresh.
type visibility struct {
	public   bool
	readAt   time.Time
	listedAt time.Time
	// rev counts the answers handed over through RecordVisibility. A GetRepo
	// read applies its answer only when rev is what it was as the read began.
	rev uint64
	// handed marks an answer handed over for a repository that is being
	// added: it is kept until that repository shows up in the config, or for
	// at most visibilityTTL.
	handed bool
}

// visibilityLocked reports whether GitHub last said repo is public, through
// GetRepo or through a runs listing, and whether it has said anything for
// repo at all; m.mu must be held.
func (m *Manager) visibilityLocked(repo string) (public, known bool) {
	v, known := m.vis[strings.ToLower(repo)]
	return v.public || !v.listedAt.IsZero(), known
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
	defer m.mu.Unlock()
	nv := m.vis[key]
	if nv.rev != v.rev {
		return nil // an answer was handed over during the read; it is the newer one
	}
	nv.public, nv.readAt = !r.Private, now
	if m.visGen != gen {
		nv.readAt = time.Time{} // ClearDegraded ran during the read; the next poll reads again
	}
	// A listing of this same poll said public: the block holds for this tick
	// whatever GetRepo says. An older one is what this answer replaces.
	if !nv.listedAt.Equal(now) {
		nv.listedAt = time.Time{}
	}
	m.vis[key] = nv
	return nil
}

// RecordVisibility takes an answer the caller has just read from GitHub for
// repo, as a read made now. Adding a repository reads it first; handing that
// answer over replaces whatever is left from before the repository was
// removed, and a read of the manager's own that is still in flight does not
// replace it. The caller hands over only for a repository it is adding: the
// answer is kept until that repository shows up in the config, so it must be
// handed over before the config with the repository can be seen.
func (m *Manager) RecordVisibility(repo string, public bool) {
	now := m.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(repo)
	m.vis[key] = visibility{public: public, readAt: now, rev: m.vis[key].rev + 1, handed: true}
}

// sawPublic records that a run GitHub just listed for repo says its
// repository is public: the repository is blocked from this tick on, without
// waiting for readVisibility's next turn. GetRepo's answer and its time are
// left as they are, so that read still comes when it is due. A listing only
// ever blocks. One that says private is no answer, because a listing can come
// from the client's cache; lifting a block stays with readVisibility.
func (m *Manager) sawPublic(repo string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(repo)
	v := m.vis[key]
	v.listedAt = now
	m.vis[key] = v
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
	return repo + " is public; ghr serves only private repositories. Turn on Serve when public on its repository page to serve it"
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
// and its removal is not reported as a block being lifted. An answer handed
// over for a repo that is being added is not in the config yet; it is kept
// until the repo has been seen there once, or for at most visibilityTTL. It
// needs no API call, so it runs on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, v := range m.vis {
		switch listed := cfg.Repo(key) != nil; {
		case listed && v.handed:
			v.handed = false
			m.vis[key] = v
		case !listed && (!v.handed || now.Sub(v.readAt) >= visibilityTTL):
			delete(m.vis, key)
		}
	}
	for key := range m.wasBlocked {
		if cfg.Repo(key) == nil {
			delete(m.wasBlocked, key)
		}
	}
}
