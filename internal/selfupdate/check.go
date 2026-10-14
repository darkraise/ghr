// Package selfupdate finds ghr's newest release and installs it in place of
// the running binary.
package selfupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBase is ghr's repository on GitHub, where its releases are published.
const DefaultBase = "https://github.com/darkraise/ghr"

// checkTimeout bounds one release check.
const checkTimeout = 30 * time.Second

var tagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// Version is a release tag, vMAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// ParseVersion reads a release tag; ok is false for anything else, such as
// a development build's "dev" or a git describe string.
func ParseVersion(s string) (Version, bool) {
	m := tagPattern.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}
	var v Version
	for i, p := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, false
		}
		*p = n
	}
	return v, true
}

// Less reports whether v is an older release than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

func (v Version) String() string { return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Latest returns the tag of base's newest release. GitHub answers
// releases/latest with a 302 to the release's page; the redirect is read,
// never followed, and only a target that is base's own release page with a
// vMAJOR.MINOR.PATCH tag is accepted, so the tag is safe to put in a URL.
// No credentials are sent. A nil client means a plain http.Client.
func Latest(ctx context.Context, client *http.Client, base string) (string, error) {
	b, err := url.Parse(strings.TrimSuffix(base, "/"))
	if err != nil {
		return "", err
	}
	c := http.Client{}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.String()+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("GET %s: %s, want a redirect to the newest release", req.URL, resp.Status)
	}
	to, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", req.URL, err)
	}
	tag, ok := strings.CutPrefix(to.Path, b.Path+"/releases/tag/")
	if !ok || to.Scheme != b.Scheme || to.Host != b.Host || to.RawQuery != "" || to.Fragment != "" {
		return "", fmt.Errorf("the newest release redirects to %s, which is not a release of %s", to.Redacted(), base)
	}
	if _, ok := ParseVersion(tag); !ok {
		return "", fmt.Errorf("the newest release has tag %q, not vMAJOR.MINOR.PATCH", tag)
	}
	return tag, nil
}
