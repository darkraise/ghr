package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/artifactstore"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

// ArtifactStoreService is the part of *artifactstore.Service the backend uses.
type ArtifactStoreService interface {
	Kick()
	Refresh() error
	Version() int64
	Snapshot() model.ArtifactStore
	Summary() *model.ArtifactStoreSummary
	SetKey(which, id, secret string) error
	KeysChanged()
	PolicyChanged()
	Row(repo string) (model.ArtifactStoreRepo, bool)
	Retire(ctx context.Context, repo string) error
	JobKey() *artifactstore.Key
	Test(ctx context.Context) (model.ArtifactStoreTest, error)
	RunObjects(ctx context.Context, repo string, runID int64) ([]model.ArtifactStoreItem, error)
	Delete(repo string, keys []string) (string, error)
	Link(key, ttl string) (model.ArtifactLink, error)
}

func (b *Backend) ArtifactStore() model.ArtifactStore { return b.AS.Snapshot() }

// RefreshArtifactStore does not wait for GitHub: the pass lists RustFS, and
// it skips the variable and secret writes itself while GitHub cannot take them.
func (b *Backend) RefreshArtifactStore() error { return asErr(b.AS.Refresh()) }

func (b *Backend) TestArtifactStore(ctx context.Context) (model.ArtifactStoreTest, error) {
	t, err := b.AS.Test(ctx)
	return t, asErr(err)
}

// SetArtifactKey replaces the admin or the job key. The service writes the
// event, which carries the shortened key ID only.
func (b *Backend) SetArtifactKey(req model.ArtifactKeyRequest) error {
	return asErr(b.AS.SetKey(req.Which, req.ID, req.Secret))
}

func (b *Backend) ArtifactRunObjects(ctx context.Context, repo string, runID int64) ([]model.ArtifactStoreItem, error) {
	items, err := b.AS.RunObjects(ctx, repo, runID)
	return items, asErr(err)
}

// DeleteArtifactObjects starts deleting objects of one repository. Every key
// must lie under that repository's prefix, so a request cannot reach another
// repository's artifacts.
func (b *Backend) DeleteArtifactObjects(repo string, req model.ArtifactDeleteRequest) (model.OperationStarted, error) {
	if !config.ValidRepoName(repo) {
		return model.OperationStarted{}, api.BadRequest("repo name must be a repository name under owner, " + config.RepoNameRule)
	}
	prefix := artifact.RepoPrefix(b.Store.Config().Owner, repo)
	for _, k := range req.Keys {
		if err := artifact.ScopeKey(k, prefix); err != nil {
			return model.OperationStarted{}, api.BadRequest(err.Error())
		}
	}
	id, err := b.AS.Delete(repo, req.Keys)
	return model.OperationStarted{ID: id}, asErr(err)
}

// ArtifactLink signs a download link for one object under the owner's prefix.
func (b *Backend) ArtifactLink(req model.ArtifactLinkRequest) (model.ArtifactLink, error) {
	if err := artifact.ScopeKey(req.Key, strings.ToLower(b.Store.Config().Owner)+"/"); err != nil {
		return model.ArtifactLink{}, api.BadRequest(err.Error())
	}
	l, err := b.AS.Link(req.Key, req.TTL)
	return l, asErr(err)
}

// asErr gives the artifact store's refusals their HTTP statuses. An error
// RustFS answered with is a 502; other errors, GitHub's among them, are
// returned unchanged.
func asErr(err error) error {
	var bad artifactstore.BadInput
	var budget *ghstorage.BudgetError
	var s3err *s3.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &bad):
		return api.BadRequest(err.Error())
	case errors.Is(err, artifactstore.ErrUnknownRepo):
		return api.NotFound(err.Error())
	case errors.Is(err, artifactstore.ErrBusy), errors.Is(err, artifactstore.ErrOperationRunning),
		errors.Is(err, artifactstore.ErrNotConfigured), errors.Is(err, artifactstore.ErrNoAdminKey),
		errors.Is(err, artifactstore.ErrNoJobKey):
		return api.Conflict(err.Error())
	case errors.As(err, &budget):
		return &api.Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: budget.RetryAt}
	case errors.Is(err, artifactstore.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	case errors.As(err, &s3err):
		return &api.Error{Status: http.StatusBadGateway, Msg: err.Error()}
	}
	return err
}
