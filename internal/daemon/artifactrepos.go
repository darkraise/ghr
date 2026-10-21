package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/artifactstore"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// SetArtifactRepo switches self-hosted storage on or off for one covered
// repository, or changes its cleanup age, and applies the repository's
// variable and secret before it answers. A public repository cannot be
// switched on.
func (b *Backend) SetArtifactRepo(ctx context.Context, repo string, req model.ArtifactRepoRequest) (model.ArtifactStoreRepo, error) {
	if req.Enabled == nil && req.Cleanup == nil {
		return model.ArtifactStoreRepo{}, api.BadRequest("give enabled or cleanup")
	}
	cfg := b.Store.Config()
	if !cfg.ArtifactStore.Configured() {
		return model.ArtifactStoreRepo{}, asErr(artifactstore.ErrNotConfigured)
	}
	name, ok := artifactCovered(cfg, repo)
	if !ok {
		return model.ArtifactStoreRepo{}, uncoveredErr(repo)
	}
	if req.Cleanup != nil {
		if err := checkArtifactCleanup(*req.Cleanup); err != nil {
			return model.ArtifactStoreRepo{}, err
		}
	}
	if err := b.githubErr(); err != nil {
		return model.ArtifactStoreRepo{}, err
	}
	if req.Enabled != nil && *req.Enabled {
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
		if _, ok := artifactCovered(c, name); !ok {
			return uncoveredErr(name)
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
	// The saved switch stands when GitHub refuses the sync: each pass retries it.
	row, err := b.AS.SyncRepo(ctx, name)
	if err != nil {
		return model.ArtifactStoreRepo{}, b.asWriteErr(err, name)
	}
	return row, nil
}

// EnablePrivateArtifactRepos switches self-hosted storage on for every
// covered private repository the token can see, except those whose variable
// another daemon wrote, then applies each one's variable and secret. When a
// sync fails the switches stay saved and a pass is asked for, which syncs
// the rest.
func (b *Backend) EnablePrivateArtifactRepos(ctx context.Context) ([]model.ArtifactStoreRepo, error) {
	cfg := b.Store.Config()
	if !cfg.ArtifactStore.Configured() {
		return nil, asErr(artifactstore.ErrNotConfigured)
	}
	if err := b.githubErr(); err != nil {
		return nil, err
	}
	snap := b.AS.Snapshot()
	if snap.JobKey == nil {
		return nil, asErr(artifactstore.ErrNoJobKey)
	}
	// The Artifacts page's bulk button leaves these out and names them.
	foreign := map[string]bool{}
	for _, r := range snap.Repos {
		if r.State == "foreign" {
			foreign[strings.ToLower(r.Name)] = true
		}
	}
	vis, err := b.ownerVisibility(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, name := range artifactCoveredRepos(cfg) {
		if vis[strings.ToLower(name)] && !foreign[strings.ToLower(name)] {
			names = append(names, name)
		}
	}
	rows := []model.ArtifactStoreRepo{}
	if len(names) == 0 {
		return rows, nil
	}
	if err := b.update(func(c *config.Config) error {
		for _, name := range names {
			if _, ok := artifactCovered(c, name); !ok {
				continue
			}
			r := dropArtifactRepo(&c.ArtifactStore, name)
			r.Enabled = true
			setArtifactRepo(&c.ArtifactStore, name, r)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	b.AS.PolicyChanged()
	for _, name := range names {
		row, err := b.AS.SyncRepo(ctx, name)
		if err != nil {
			b.AS.Kick()
			return nil, b.asWriteErr(err, name)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// artifactSwitchOffTimeout bounds the GitHub calls that switch a repository's
// self-hosted storage off before the repository stops being covered.
const artifactSwitchOffTimeout = 30 * time.Second

// switchOffArtifacts deletes ghr's variable and secret of a repository whose
// self-hosted storage is on, before the repository stops being covered: once
// it is not covered, no pass would ever delete them. The repository's
// artifact_store entry is saved away first, so a pass running meanwhile
// cannot write them back; when the switch-off fails, the entry is restored.
// refused finishes the error's sentence, saying what did not happen.
func (b *Backend) switchOffArtifacts(name, refused string) error {
	if b.AS == nil {
		return nil
	}
	if !b.Store.Config().ArtifactStore.Enabled(name) {
		return nil
	}
	if err := b.githubErr(); err != nil {
		return switchOffRefusal(b.asWriteErr(err, name), name, refused)
	}
	var prev config.ArtifactRepo
	if err := b.update(func(c *config.Config) error {
		prev = dropArtifactRepo(&c.ArtifactStore, name)
		return nil
	}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), artifactSwitchOffTimeout)
	err := b.AS.SwitchOff(ctx, name)
	cancel()
	if err == nil {
		return nil
	}
	if rerr := b.update(func(c *config.Config) error {
		if spelled, ok := artifactCovered(c, name); ok {
			if _, set := c.ArtifactStore.Repo(name); !set {
				setArtifactRepo(&c.ArtifactStore, spelled, prev)
			}
		}
		return nil
	}); rerr != nil {
		b.Events.Add("warn", name, "self-hosted storage stays off in config.yaml after a failed switch-off: %v", rerr)
	}
	return switchOffRefusal(b.asWriteErr(err, name), name, refused)
}

// switchOffRefusal words a failed switch-off, keeping GitHub's status; any
// other error is a 502.
func switchOffRefusal(err error, name, refused string) error {
	err = api.FromGitHub(err)
	status, retry := http.StatusBadGateway, time.Time{}
	var ae *api.Error
	if errors.As(err, &ae) {
		status, retry = ae.Status, ae.RetryAt
	}
	return &api.Error{Status: status, Msg: fmt.Sprintf("could not switch off self-hosted storage for %s, so %s: %s", name, refused, err), RetryAt: retry}
}

// asWriteErr is asErr for a GitHub write: a refused token gets the
// repository rows' wording, which names the missing PAT permission.
func (b *Backend) asWriteErr(err error, repo string) error {
	if !github.IsKind(err, github.ErrAuth) {
		return asErr(err)
	}
	msg, _ := actionsErr(err, b.Store.Config().Owner, repo)
	return &api.Error{Status: http.StatusForbidden, Msg: msg}
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
// repository being removed is not covered: it could not be switched off
// before it goes.
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

// artifactCoveredRepos lists the repositories self-hosted storage can serve:
// runner repositories that are not being removed, then watched ones.
func artifactCoveredRepos(c *config.Config) []string {
	var out []string
	for _, r := range c.Repos {
		if !r.Removing {
			out = append(out, r.Name)
		}
	}
	return append(out, c.WatchRepos...)
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
