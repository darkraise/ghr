package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
)

// GitHubStorageService is the part of *ghstorage.Service the backend uses.
type GitHubStorageService interface {
	Snapshot() model.GitHubStorage
	Alert() *model.StorageAlert
	Version() int64
	Refresh() error
	Kick()
	TokenChanged()
	PolicyChanged()
	Preview(cleanup, repo string) ([]model.StoragePreview, error)
	DeleteArtifact(ctx context.Context, repo string, id int64) error
	DeleteCache(ctx context.Context, repo string, id int64) error
	DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error)
	DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error)
	SetRetention(ctx context.Context, repo string, days int) error
	SetRetentionAll(days int) (string, error)
}

func (b *Backend) GitHubStorage() model.GitHubStorage { return b.GHS.Snapshot() }

// GitHubStoragePreview counts what a cleanup age would delete; it reads the
// snapshot only, so it does not wait for GitHub.
func (b *Backend) GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error) {
	ps, err := b.GHS.Preview(cleanup, repo)
	return ps, ghsErr(err)
}

func (b *Backend) RefreshGitHubStorage() error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.Refresh())
}

func (b *Backend) DeleteGitHubArtifact(ctx context.Context, repo string, id int64) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.DeleteArtifact(ctx, repo, id))
}

func (b *Backend) DeleteGitHubCache(ctx context.Context, repo string, id int64) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.DeleteCache(ctx, repo, id))
}

func (b *Backend) DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error) {
	if err := b.githubErr(); err != nil {
		return model.OperationStarted{}, err
	}
	id, err := b.GHS.DeleteArtifacts(repo, req)
	return model.OperationStarted{ID: id}, ghsErr(err)
}

func (b *Backend) DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error) {
	if err := b.githubErr(); err != nil {
		return model.OperationStarted{}, err
	}
	id, err := b.GHS.DeleteCaches(repo, req)
	return model.OperationStarted{ID: id}, ghsErr(err)
}

func (b *Backend) SetGitHubRetention(ctx context.Context, repo string, days int) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.SetRetention(ctx, repo, days))
}

func (b *Backend) SetGitHubRetentionAll(days int) (model.OperationStarted, error) {
	if err := b.githubErr(); err != nil {
		return model.OperationStarted{}, err
	}
	id, err := b.GHS.SetRetentionAll(days)
	return model.OperationStarted{ID: id}, ghsErr(err)
}

// ghsErr gives the storage service's refusals their HTTP statuses. Other
// errors, GitHub's among them, are returned unchanged.
func ghsErr(err error) error {
	var bad ghstorage.BadInput
	var budget *ghstorage.BudgetError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &bad):
		return api.BadRequest(err.Error())
	case errors.Is(err, ghstorage.ErrUnknownRepo):
		return api.NotFound(err.Error())
	case errors.Is(err, ghstorage.ErrBusy), errors.Is(err, ghstorage.ErrOperationRunning):
		return api.Conflict(err.Error())
	case errors.As(err, &budget):
		return &api.Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: budget.RetryAt}
	case errors.Is(err, ghstorage.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}
