package daemon

import (
	"context"
	"net/http"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// ArtifactDownload returns GitHub's short-lived address for an artifact's
// zip; the browser navigates to it, so it is checked first.
func (b *Backend) ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error) {
	name, _, err := b.coveredName(repo)
	if err != nil {
		return model.DownloadURL{}, err
	}
	if err := b.githubErr(); err != nil {
		return model.DownloadURL{}, err
	}
	actx, cancel := context.WithTimeout(ctx, detailDeadline)
	defer cancel()
	u, err := b.GH.ArtifactURL(actx, name, id)
	switch {
	case github.IsKind(err, github.ErrGone):
		return model.DownloadURL{}, &api.Error{Status: http.StatusGone, Msg: "artifact has expired"}
	case err != nil:
		return model.DownloadURL{}, detailErr(err, b.Store.Config().Owner, name)
	case !github.BlobHost(u):
		return model.DownloadURL{}, &api.Error{Status: http.StatusBadGateway, Msg: "GitHub returned an unexpected download address"}
	}
	return model.DownloadURL{URL: u}, nil
}
