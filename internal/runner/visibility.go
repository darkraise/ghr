package runner

import (
	"context"
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
	m.mu.Unlock()
	if known && now.Sub(v.readAt) < visibilityTTL {
		return nil
	}
	r, err := m.GH.GetRepo(ctx, repo)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.vis[key] = visibility{public: !r.Private, readAt: now}
	m.mu.Unlock()
	return nil
}

// forgetRemoved drops the visibility kept for repos that are no longer in the
// config, so one that is removed and added again starts unread. It needs no
// API call, so it runs on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.vis {
		if cfg.Repo(key) == nil {
			delete(m.vis, key)
		}
	}
}
