package toolchain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/darkraise/ghr/internal/system"
)

func DefaultSources() Sources {
	return Sources{
		NodeManifest:   "https://raw.githubusercontent.com/actions/node-versions/main/versions-manifest.json",
		GoManifest:     "https://raw.githubusercontent.com/actions/go-versions/main/versions-manifest.json",
		PythonManifest: "https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json",
		GoReleases:     "https://go.dev/dl/?mode=json&include=all",
		GoDownload:     "https://go.dev/dl/",
		Adoptium:       "https://api.adoptium.net",
		DotnetIndex:    "https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json",
		DotnetScript:   "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh",
	}
}

// NewEnv is the daemon's Env for the tool cache at root. run should kill a
// command's whole process group on cancel (system.ExecGroup), because the
// install scripts start children of their own.
func NewEnv(root, home, user string, run system.Runner) *Env {
	return &Env{
		Root:    root,
		Home:    home,
		User:    user,
		Sources: DefaultSources(),
		Get:     httpGet,
		Fetch:   system.Download,
		Run:     run,
		Extract: func(ctx context.Context, archive, dir string) error {
			_, err := run(ctx, "tar", "-xzf", archive, "-C", dir)
			return err
		},
		OpID: newOpID,
	}
}

// GetTimeout bounds one read of a version list.
var GetTimeout = time.Minute

// httpGet reads a public version list; go.dev's full list is several MB,
// hence the generous cap.
func httpGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, GetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func newOpID() string {
	var b [6]byte
	rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}
