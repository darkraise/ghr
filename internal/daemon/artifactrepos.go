package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/artifactstore"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// SetArtifactRepo switches self-hosted storage on or off for one repository,
// or changes its cleanup age, and answers its row. The repository is looked
// up among the covered ones and then the artifact_store.repos entries, so a
// leftover entry can still be switched off. Only a private repository that
// ghr serves and is not removing can be switched on, and only that check
// reads GitHub; nothing is written there.
func (b *Backend) SetArtifactRepo(ctx context.Context, repo string, req model.ArtifactRepoRequest) (model.ArtifactStoreRepo, error) {
	if req.Enabled == nil && req.Cleanup == nil {
		return model.ArtifactStoreRepo{}, api.BadRequest("give enabled or cleanup")
	}
	cfg := b.Store.Config()
	if !cfg.ArtifactStore.Configured() {
		return model.ArtifactStoreRepo{}, asErr(artifactstore.ErrNotConfigured)
	}
	name, ok := artifactSwitchable(cfg, repo)
	if !ok {
		return model.ArtifactStoreRepo{}, uncoveredErr(repo)
	}
	if req.Cleanup != nil {
		if err := checkArtifactCleanup(*req.Cleanup); err != nil {
			return model.ArtifactStoreRepo{}, err
		}
	}
	on := req.Enabled != nil && *req.Enabled
	if on {
		if r := cfg.Repo(name); r == nil || r.Removing {
			return model.ArtifactStoreRepo{}, notServedErr(name)
		}
		if err := b.githubErr(); err != nil {
			return model.ArtifactStoreRepo{}, err
		}
		if b.AS.Snapshot().JobKey == nil {
			return model.ArtifactStoreRepo{}, asErr(artifactstore.ErrNoJobKey)
		}
		private, err := b.artifactRepoPrivate(ctx, name)
		if err != nil {
			return model.ArtifactStoreRepo{}, err
		}
		if !private {
			return model.ArtifactStoreRepo{}, api.Conflict(name + " is public; self-hosted storage serves only private repositories")
		}
	}
	if err := b.update(func(c *config.Config) error {
		if _, ok := artifactSwitchable(c, name); !ok {
			return uncoveredErr(name)
		}
		if r := c.Repo(name); on && (r == nil || r.Removing) {
			return notServedErr(name)
		}
		r := dropArtifactRepo(&c.ArtifactStore, name)
		if req.Enabled != nil {
			r.Enabled = *req.Enabled
		}
		if req.Cleanup != nil {
			r.Cleanup = strings.TrimSpace(*req.Cleanup)
		}
		setArtifactRepo(&c.ArtifactStore, name, r)
		return nil
	}); err != nil {
		return model.ArtifactStoreRepo{}, err
	}
	b.AS.PolicyChanged()
	if row, ok := b.AS.Row(name); ok {
		return row, nil
	}
	// Switching off a leftover entry that set nothing else drops it, and a
	// repository ghr does not cover has no row without an entry or objects.
	return model.ArtifactStoreRepo{Name: name, Private: true, State: "not_served", Items: []model.ArtifactStoreItem{}}, nil
}

// EnablePrivateArtifactRepos switches self-hosted storage on for every
// repository ghr serves that the token sees as private, and answers their
// rows. Watched-only, public and departing repositories are left as they
// are, and nothing is written to GitHub.
func (b *Backend) EnablePrivateArtifactRepos(ctx context.Context) ([]model.ArtifactStoreRepo, error) {
	cfg := b.Store.Config()
	if !cfg.ArtifactStore.Configured() {
		return nil, asErr(artifactstore.ErrNotConfigured)
	}
	if err := b.githubErr(); err != nil {
		return nil, err
	}
	if b.AS.Snapshot().JobKey == nil {
		return nil, asErr(artifactstore.ErrNoJobKey)
	}
	vis, err := b.ownerVisibility(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range cfg.Repos {
		if !r.Removing && vis[strings.ToLower(r.Name)] {
			names = append(names, r.Name)
		}
	}
	rows := []model.ArtifactStoreRepo{}
	if len(names) == 0 {
		return rows, nil
	}
	if err := b.update(func(c *config.Config) error {
		for _, name := range names {
			if r := c.Repo(name); r == nil || r.Removing {
				continue
			}
			e := dropArtifactRepo(&c.ArtifactStore, name)
			e.Enabled = true
			setArtifactRepo(&c.ArtifactStore, name, e)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	b.AS.PolicyChanged()
	for _, name := range names {
		if row, ok := b.AS.Row(name); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// artifactRetireTimeout bounds the GitHub calls that remove ghr's old
// variable and secret from a repository leaving coverage.
const artifactRetireTimeout = 30 * time.Second

// retireArtifacts removes ghr's old GHR_ARTIFACT_STORE variable and
// GHR_ARTIFACT_KEY secret from a repository about to leave coverage, while
// it is still covered: no pass reads it once it is not. Its error is
// ignored because the service has already written the warning, which names
// the gh commands that delete them by hand; a failure never stops or undoes
// leaving coverage.
func (b *Backend) retireArtifacts(name string) {
	if b.AS == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), artifactRetireTimeout)
	defer cancel()
	_ = b.AS.Retire(ctx, name)
}

// artifactRepoPrivate reports whether name is private, read fresh from
// GitHub: a snapshot row also reads private while its visibility is unknown.
func (b *Backend) artifactRepoPrivate(ctx context.Context, name string) (bool, error) {
	vis, err := b.ownerVisibility(ctx)
	if err != nil {
		return false, err
	}
	private, ok := vis[strings.ToLower(name)]
	if !ok {
		return false, api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", b.Store.Config().Owner, name))
	}
	return private, nil
}

// ownerVisibility maps each of the owner's repositories the token can see,
// by lower-case name, to whether it is private.
func (b *Backend) ownerVisibility(ctx context.Context) (map[string]bool, error) {
	rs, err := b.GH.ListUserRepos(ctx)
	if err != nil {
		return nil, err
	}
	owner := b.Store.Config().Owner
	out := map[string]bool{}
	for _, r := range rs {
		if strings.EqualFold(r.Owner.Login, owner) {
			out[strings.ToLower(r.Name)] = r.Private
		}
	}
	return out, nil
}

// artifactCovered resolves name to its spelling in repos or watch_repos. A
// repository being removed is not covered: its artifact_store entry went
// with the removal.
func artifactCovered(c *config.Config, name string) (string, bool) {
	if r := c.Repo(name); r != nil {
		return r.Name, !r.Removing
	}
	for _, w := range c.WatchRepos {
		if strings.EqualFold(w, name) {
			return w, true
		}
	}
	return "", false
}

// artifactSwitchable resolves name to its spelling among the covered
// repositories, then among the artifact_store.repos entries.
func artifactSwitchable(c *config.Config, name string) (string, bool) {
	if spelled, ok := artifactCovered(c, name); ok {
		return spelled, true
	}
	for k := range c.ArtifactStore.Repos {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

func notServedErr(name string) error {
	return api.Conflict(name + " is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for")
}

func uncoveredErr(name string) error {
	return api.NotFound(name + " is not a covered repository; add it as a runner or watched repository first")
}

// checkArtifactCleanup accepts a repository's cleanup override: empty for the
// global age, "off", or a duration of at least config.MinArtifactOverride.
func checkArtifactCleanup(s string) error {
	s = strings.TrimSpace(s)
	if s == "" || s == "off" {
		return nil
	}
	d, err := config.ParseDuration(s)
	if err != nil {
		return api.BadRequest("cleanup: " + err.Error())
	}
	if d.D() < config.MinArtifactOverride {
		return api.BadRequest("cleanup must be at least 1h, or off")
	}
	return nil
}

// dropArtifactRepo removes name's entry from the artifact_store repos,
// whatever its case, and returns it.
func dropArtifactRepo(a *config.ArtifactStore, name string) config.ArtifactRepo {
	var prev config.ArtifactRepo
	for k, v := range a.Repos {
		if strings.EqualFold(k, name) {
			prev = v
			delete(a.Repos, k)
		}
	}
	if len(a.Repos) == 0 {
		a.Repos = nil
	}
	return prev
}

// setArtifactRepo stores r under name; an entry that says nothing is left out.
func setArtifactRepo(a *config.ArtifactStore, name string, r config.ArtifactRepo) {
	if r == (config.ArtifactRepo{}) {
		return
	}
	if a.Repos == nil {
		a.Repos = map[string]config.ArtifactRepo{}
	}
	a.Repos[name] = r
}
