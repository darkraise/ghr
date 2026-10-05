package github

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Release is one release of actions/runner.
type Release struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
}

// ListRunnerReleases lists the 30 newest actions/runner releases, one page,
// without drafts and prereleases.
func (c *Client) ListRunnerReleases(ctx context.Context) ([]Release, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.BaseURL+"/repos/actions/runner/releases?per_page=30", nil)
	if err != nil {
		return nil, err
	}
	var all []Release
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	out := []Release{}
	for _, r := range all {
		if !r.Draft && !r.Prerelease {
			out = append(out, r)
		}
	}
	return out, nil
}

var linuxX64SHA = regexp.MustCompile(`<!-- BEGIN SHA linux-x64 -->([0-9a-f]{64})<!-- END SHA linux-x64 -->`)

// LinuxX64SHA256 is the linux-x64 tarball checksum the release notes carry,
// the marker setup.sh reads too.
func (r Release) LinuxX64SHA256() (string, bool) {
	m := linuxX64SHA.FindStringSubmatch(r.Body)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Version is a dotted release version such as 2.338.0.
type Version []int

// ParseVersion reads "2.338.0" or "v2.338.0". Every part must be decimal digits.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil, false
	}
	var v Version
	for _, p := range strings.Split(s, ".") {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		v = append(v, n)
	}
	return v, true
}

// Compare orders versions part by part; a missing part counts as 0.
func (v Version) Compare(o Version) int {
	for i := range max(len(v), len(o)) {
		a, b := 0, 0
		if i < len(v) {
			a = v[i]
		}
		if i < len(o) {
			b = o[i]
		}
		if a != b {
			return cmp.Compare(a, b)
		}
	}
	return 0
}

func (v Version) String() string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// Account is a repository owner.
type Account struct {
	Login string `json:"login"`
}

// UserRepo is one repository the token's user can access.
type UserRepo struct {
	Name    string  `json:"name"`
	Private bool    `json:"private"`
	Owner   Account `json:"owner"`
}

// ListUserRepos lists every repository the token can access, across owners.
func (c *Client) ListUserRepos(ctx context.Context) ([]UserRepo, error) {
	var out []UserRepo
	err := c.getAll(ctx, c.BaseURL+"/user/repos?per_page=100", func(b []byte) error {
		var page []UserRepo
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page...)
		return nil
	})
	return out, err
}
