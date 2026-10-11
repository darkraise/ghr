package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRunDetailFieldsDecode(t *testing.T) {
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":102,"name":"ci","run_attempt":2,"head_sha":"a1f9c3e","path":".github/workflows/ci.yml",
			"pull_requests":[{"number":87,"head":{"ref":"x"}}]}`)
	})
	r, err := c.GetRun(context.Background(), "darkmem", 102)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != 102 || r.RunAttempt != 2 || r.HeadSHA != "a1f9c3e" || r.Path != ".github/workflows/ci.yml" ||
		len(r.PullRequests) != 1 || r.PullRequests[0].Number != 87 {
		t.Fatalf("run %+v", r)
	}
	if f.requests[0] != "GET /repos/darkraise/darkmem/actions/runs/102" {
		t.Fatalf("request %s", f.requests[0])
	}
}

func TestRunReadsAskTheRightPaths(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(c *Client) error
		want string
	}{
		{"attempt", func(c *Client) error { _, err := c.GetRunAttempt(ctx, "darkmem", 102, 1); return err },
			"GET /repos/darkraise/darkmem/actions/runs/102/attempts/1"},
		{"attempt jobs", func(c *Client) error { _, err := c.ListAttemptJobs(ctx, "darkmem", 102, 1); return err },
			"GET /repos/darkraise/darkmem/actions/runs/102/attempts/1/jobs?per_page=100"},
		{"job", func(c *Client) error { _, err := c.GetJob(ctx, "darkmem", 1002); return err },
			"GET /repos/darkraise/darkmem/actions/jobs/1002"},
		{"artifacts", func(c *Client) error { _, err := c.ListArtifacts(ctx, "darkmem", 102); return err },
			"GET /repos/darkraise/darkmem/actions/runs/102/artifacts?per_page=100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if len(f.requests) != 1 || f.requests[0] != tc.want {
				t.Fatalf("requests %v", f.requests)
			}
		})
	}
}

func TestAttemptJobsAndArtifactsPaginate(t *testing.T) {
	var base string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		next := ""
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
		} else {
			next = "2"
		}
		if strings.Contains(r.URL.Path, "/artifacts") {
			fmt.Fprintf(w, `{"artifacts":[{"id":1%s,"name":"dist","size_in_bytes":2400000,"expired":true,"expires_at":"2026-10-17T13:59:00Z"}]}`, next)
			return
		}
		fmt.Fprintf(w, `{"jobs":[{"id":1%s,"name":"build"}]}`, next)
	})
	base = c.BaseURL
	jobs, err := c.ListAttemptJobs(context.Background(), "darkmem", 102, 1)
	if err != nil || len(jobs) != 2 || jobs[1].ID != 12 {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
	arts, err := c.ListArtifacts(context.Background(), "darkmem", 102)
	if err != nil || len(arts) != 2 || arts[1].ID != 12 {
		t.Fatalf("artifacts %+v err %v", arts, err)
	}
	a := arts[0]
	if a.Name != "dist" || a.SizeInBytes != 2400000 || !a.Expired || a.ExpiresAt == nil ||
		!a.ExpiresAt.Equal(time.Date(2026, 10, 17, 13, 59, 0, 0, time.UTC)) {
		t.Fatalf("artifact %+v", a)
	}
}

func TestGetContent(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, want, wantURI, wantErr string
	}{
		{name: "base64 with line breaks", path: ".github/workflows/ci.yml",
			body:    `{"content":"am9iczoK\nICBi\nOiB7fQo=\n","encoding":"base64"}`,
			want:    "jobs:\n  b: {}\n",
			wantURI: "GET /repos/darkraise/darkmem/contents/.github/workflows/ci.yml?ref=a1f9c3e"},
		{name: "segments escaped, slashes kept", path: ".github/workflows/a b#1.yml",
			body:    `{"content":"","encoding":"base64"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/.github/workflows/a%20b%231.yml?ref=a1f9c3e"},
		{name: "too large for the API", path: "big.yml",
			body:    `{"content":"","encoding":"none"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/big.yml?ref=a1f9c3e",
			wantErr: `github: file content cannot be decoded: big.yml has encoding "none", not base64`},
		{name: "base64 that does not decode", path: "bad.yml",
			body:    `{"content":"am9i!","encoding":"base64"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/bad.yml?ref=a1f9c3e",
			wantErr: "github: file content cannot be decoded: bad.yml: illegal base64 data at input byte 4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			got, err := c.GetContent(context.Background(), "darkmem", tc.path, "a1f9c3e")
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr || !errors.Is(err, ErrBadContent) || got != nil {
					t.Fatalf("got %q err %v", got, err)
				}
			} else if err != nil || string(got) != tc.want {
				t.Fatalf("got %q err %v", got, err)
			}
			if f.requests[0] != tc.wantURI {
				t.Fatalf("request %s", f.requests[0])
			}
		})
	}
}

func TestMissingContentsPermissionIsNotATokenVerdict(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Accepted-GitHub-Permissions", "contents=read")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
	})
	if _, err := c.GetContent(context.Background(), "darkmem", ".github/workflows/ci.yml", "a1f9c3e"); !IsKind(err, ErrAuth) {
		t.Fatalf("err %v", err)
	}
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() {
		t.Fatalf("a missing Contents: read gave a verdict on the token: %+v", m)
	}
}
