package toolchain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
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
// command's whole process group on cancel (system.ExecGroupFor), because the
// install scripts start children of their own; run and fetch carry the
// install-length timeouts.
func NewEnv(root, home, user string, run system.Runner, fetch func(ctx context.Context, url, dst string) error) *Env {
	return &Env{
		Root:    root,
		Home:    home,
		User:    user,
		Sources: DefaultSources(),
		Get:     httpGet,
		Fetch:   fetch,
		Allow:   httpsFrom,
		Run:     run,
		Extract: func(ctx context.Context, archive, dir string) error {
			// As root, tar would otherwise give each file the owner the
			// archive names.
			_, err := run(ctx, "tar", "--no-same-owner", "-xzf", archive, "-C", dir)
			return err
		},
		OpID: newOpID,
	}
}

// httpsFrom is the download check the daemon uses. It passes rawURL only when
// it is an https URL with no user, port, query or fragment, whose path holds
// nothing a server could resolve to another place (dot segments, doubled or
// encoded slashes, encoded dots, backslashes), and which is from's one file
// or lies below from's directory on the same host. The host must match byte
// for byte, because case folding maps some non-ASCII letters onto ASCII ones.
func httpsFrom(rawURL string, from Origin) error {
	refused := fmt.Errorf("refusing to download %s", rawURL)
	u, err := url.Parse(rawURL)
	// A URL that does not print back as written carries something the parsed
	// fields do not show, such as an empty fragment.
	if err != nil || u.String() != rawURL {
		return refused
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.Port() != "" {
		return refused
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return refused
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%2e") || strings.Contains(u.Path, `\`) {
		return refused
	}
	if path.Clean(u.Path) != u.Path {
		return refused
	}
	switch {
	case from.File != "" && from.Dir == "":
		if rawURL != from.File {
			return refused
		}
	case from.Dir != "" && from.File == "":
		// Without its closing slash a directory would also cover its
		// siblings: "/dl" is a prefix of "/dlx/".
		d, err := url.Parse(from.Dir)
		if err != nil || d.Scheme != "https" || d.Host != u.Host || !strings.HasSuffix(d.Path, "/") || !strings.HasPrefix(u.Path, d.Path) {
			return refused
		}
	default:
		return refused
	}
	return nil
}

// GetTimeout bounds one read of a version list.
var GetTimeout = time.Minute

var getClient = &http.Client{CheckRedirect: system.HTTPSRedirect}

// httpGet reads a public version list; go.dev's full list is several MB,
// hence the generous cap.
func httpGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, GetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := getClient.Do(req)
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
