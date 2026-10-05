package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestListRunnerReleasesSkipsDraftsAndPrereleases(t *testing.T) {
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name":"v2.339.0","draft":true,"published_at":"2026-10-04T10:00:00Z"},
			{"tag_name":"v2.339.0-rc1","prerelease":true,"published_at":"2026-10-03T10:00:00Z"},
			{"tag_name":"v2.338.0","published_at":"2026-10-02T10:00:00Z","body":"notes"}]`)
	})
	rels, err := c.ListRunnerReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].TagName != "v2.338.0" || rels[0].Body != "notes" || rels[0].PublishedAt.Day() != 2 {
		t.Fatalf("releases %+v", rels)
	}
	if got := f.requests[0]; got != "GET /repos/actions/runner/releases?per_page=30" {
		t.Fatalf("request %q", got)
	}
}

func TestParseVersionAndCompare(t *testing.T) {
	for _, s := range []string{"", "v", "2..1", "2.x.0", "2.-1.0", "2.+1.0", " "} {
		if _, ok := ParseVersion(s); ok {
			t.Errorf("%q parsed", s)
		}
	}
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2.338.0", "v2.338.0", 0},
		{"2.338.0", "2.337.9", 1},
		{"2.9.0", "2.10.0", -1},
		{"2.338", "2.338.0", 0},
		{"2.338.1", "2.338", 1},
	} {
		a, okA := ParseVersion(c.a)
		b, okB := ParseVersion(c.b)
		if !okA || !okB {
			t.Fatalf("%q or %q did not parse", c.a, c.b)
		}
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if v, _ := ParseVersion("v2.338.0"); v.String() != "2.338.0" {
		t.Fatalf("String %q", v.String())
	}
}

func TestLinuxX64SHA256(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	r := Release{Body: "x\n<!-- BEGIN SHA linux-x64 -->" + sum + "<!-- END SHA linux-x64 -->\ny"}
	if got, ok := r.LinuxX64SHA256(); !ok || got != sum {
		t.Fatalf("got %q %v", got, ok)
	}
	for _, body := range []string{
		"",
		"<!-- BEGIN SHA linux-arm64 -->" + sum + "<!-- END SHA linux-arm64 -->",
		"<!-- BEGIN SHA linux-x64 -->" + strings.ToUpper(sum) + "<!-- END SHA linux-x64 -->",
		"<!-- BEGIN SHA linux-x64 -->abc<!-- END SHA linux-x64 -->",
	} {
		if got, ok := (Release{Body: body}).LinuxX64SHA256(); ok {
			t.Errorf("body %q gave %q", body, got)
		}
	}
}

func TestListUserReposPaginates(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?per_page=100&page=2>; rel="next"`, base))
			fmt.Fprint(w, `[{"name":"darkcloud","private":true,"owner":{"login":"darkraise"}}]`)
			return
		}
		fmt.Fprint(w, `[{"name":"other","private":false,"owner":{"login":"someone"}}]`)
	})
	base = c.BaseURL
	rs, err := c.ListUserRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0] != (UserRepo{Name: "darkcloud", Private: true, Owner: Account{Login: "darkraise"}}) ||
		rs[1].Owner.Login != "someone" || rs[1].Private {
		t.Fatalf("repos %+v", rs)
	}
	if got := strings.Join(f.requests, "|"); got != "GET /user/repos?per_page=100|GET /user/repos?per_page=100&page=2" {
		t.Fatalf("requests %q", got)
	}
}
