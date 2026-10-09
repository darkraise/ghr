package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newBlobServer serves body and records the Authorization header of each request.
func newBlobServer(t *testing.T, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &auth
}

func TestJobLogFollowsTheRedirectWithoutTheToken(t *testing.T) {
	blob, auth := newBlobServer(t, "2026-10-03T13:56:10.0000000Z hello\n")
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("API call without the token")
		}
		w.Header().Set("Location", blob.URL+"/log?sig=1")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("X-RateLimit-Remaining", "4321")
		w.WriteHeader(http.StatusFound)
	})
	for i := 0; i < 2; i++ {
		got, err := c.JobLog(context.Background(), "darkmem", 1002, 1<<20)
		if err != nil || string(got) != "2026-10-03T13:56:10.0000000Z hello\n" {
			t.Fatalf("pass %d: log %q err %v", i, got, err)
		}
	}
	if len(*auth) != 2 || (*auth)[0] != "" || (*auth)[1] != "" {
		t.Fatalf("blob server saw Authorization %q", *auth)
	}
	if c.RateRemaining() != 4321 {
		t.Fatalf("rate remaining %d", c.RateRemaining())
	}
	if len(f.requests) != 2 || f.requests[0] != "GET /repos/darkraise/darkmem/actions/jobs/1002/logs" {
		t.Fatalf("requests %v", f.requests)
	}
	if len(c.cache) != 0 {
		t.Fatalf("redirect kept in the ETag cache: %v", c.cache)
	}
}

func TestJobLogStopsAtMax(t *testing.T) {
	blob, _ := newBlobServer(t, "0123456789")
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, blob.URL+"/log", http.StatusFound)
	})
	if got, err := c.JobLog(context.Background(), "darkmem", 1, 10); err != nil || string(got) != "0123456789" {
		t.Fatalf("at max: %q %v", got, err)
	}
	if _, err := c.JobLog(context.Background(), "darkmem", 1, 9); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over max: %v", err)
	}
}

func TestDownloadErrors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		status  int
		headers map[string]string
		call    func(c *Client) error
		kind    ErrKind
		plain   string
	}{
		{name: "expired log", status: 410, kind: ErrGone,
			call: func(c *Client) error { _, err := c.JobLog(ctx, "darkmem", 1, 10); return err }},
		{name: "expired artifact", status: 410, kind: ErrGone,
			call: func(c *Client) error { _, err := c.ArtifactURL(ctx, "darkmem", 11); return err }},
		{name: "missing permission", status: 403, headers: map[string]string{"X-Accepted-GitHub-Permissions": "actions=read"}, kind: ErrAuth,
			call: func(c *Client) error { _, err := c.ArtifactURL(ctx, "darkmem", 11); return err }},
		{name: "no redirect", status: 200,
			call:  func(c *Client) error { _, err := c.ArtifactURL(ctx, "darkmem", 11); return err },
			plain: "github: /repos/darkraise/darkmem/actions/artifacts/11/zip answered 200 without a download address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"nope"}`)
			})
			err := tc.call(c)
			if tc.plain != "" {
				if err == nil || err.Error() != tc.plain {
					t.Fatalf("err %v", err)
				}
				return
			}
			if !IsKind(err, tc.kind) {
				t.Fatalf("err %#v, want kind %d", err, tc.kind)
			}
		})
	}
}

func TestJobLogErrorsHideTheSignedURL(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)
	for _, tc := range []struct {
		name     string
		loc      string
		timeout  time.Duration
		deadline bool
	}{
		{name: "unreachable", loc: closed.URL + "/log?sig=secret"},
		{name: "malformed", loc: "http://%zz/log?sig=secret"},
		{name: "deadline", loc: slow.URL + "/log?sig=secret", timeout: 50 * time.Millisecond, deadline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tc.loc)
				w.WriteHeader(http.StatusFound)
			})
			ctx := context.Background()
			if tc.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			_, err := c.JobLog(ctx, "darkmem", 1, 10)
			if err == nil || strings.Contains(err.Error(), "sig=") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err %v", err)
			}
			if tc.deadline && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err %v, want a deadline", err)
			}
		})
	}
}

func TestArtifactURLErrorsHideTheSignedURL(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://%zz/actions-results/x.zip?sig=secret")
		w.WriteHeader(http.StatusFound)
	})
	_, err := c.ArtifactURL(context.Background(), "darkmem", 11)
	if err == nil || strings.Contains(err.Error(), "sig=") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err %v", err)
	}
}

func TestArtifactURLDoesNotFollow(t *testing.T) {
	const signed = "https://productionresultssa1.blob.core.windows.net/actions-results/zip?sig=abc"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", signed)
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusFound)
	})
	got, err := c.ArtifactURL(context.Background(), "darkmem", 11)
	if err != nil || got != signed {
		t.Fatalf("url %q err %v", got, err)
	}
	if len(f.requests) != 1 || f.requests[0] != "GET /repos/darkraise/darkmem/actions/artifacts/11/zip" {
		t.Fatalf("requests %v", f.requests)
	}
	if len(c.cache) != 0 {
		t.Fatalf("redirect kept in the ETag cache: %v", c.cache)
	}
}

func TestDownloadsShareTheRateLimitSuspension(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(now.Add(time.Minute).Unix()))
		w.WriteHeader(403)
	})
	c.Now = func() time.Time { return now }
	if _, err := c.ArtifactURL(context.Background(), "darkmem", 11); !IsKind(err, ErrRateLimit) {
		t.Fatalf("first call: %v", err)
	}
	if got := c.SuspendedUntil(); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("SuspendedUntil = %v", got)
	}
	if _, err := c.JobLog(context.Background(), "darkmem", 1, 10); !IsKind(err, ErrRateLimit) {
		t.Fatalf("during suspension: %v", err)
	}
	if _, err := c.GetRepo(context.Background(), "darkmem"); !IsKind(err, ErrRateLimit) {
		t.Fatalf("API call during suspension: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests sent during suspension: %v", f.requests)
	}
}
