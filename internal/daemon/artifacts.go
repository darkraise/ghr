package daemon

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// downloadHost reports whether u is an address GitHub sends artifact and log
// downloads to: https on a host under githubusercontent.com or Azure blob
// storage.
func downloadHost(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" {
		return false
	}
	host := strings.ToLower(p.Hostname())
	return strings.HasSuffix(host, ".githubusercontent.com") || strings.HasSuffix(host, ".blob.core.windows.net")
}

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
	case !downloadHost(u):
		return model.DownloadURL{}, &api.Error{Status: http.StatusBadGateway, Msg: "GitHub returned an unexpected download address"}
	}
	return model.DownloadURL{URL: u}, nil
}
