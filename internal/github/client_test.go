package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGH struct {
	mu       sync.Mutex
	requests []string
	handler  func(w http.ResponseWriter, r *http.Request)
}

func newClient(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*Client, *fakeGH) {
	t.Helper()
	f := &fakeGH{handler: h}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		f.handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New("darkraise", func() string { return "tok" })
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	return c, f
}

func TestListRunsPaginates(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
			fmt.Fprint(w, `{"workflow_runs":[{"id":1,"status":"queued"}]}`)
			return
		}
		fmt.Fprint(w, `{"workflow_runs":[{"id":2,"status":"queued"}]}`)
	})
	base = c.BaseURL
	runs, err := c.ListRuns(context.Background(), "darkcloud", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[1].ID != 2 {
		t.Fatalf("runs = %+v", runs)
	}
	if !strings.Contains(f.requests[0], "/repos/darkraise/darkcloud/actions/runs?per_page=100&status=queued") {
		t.Fatalf("request = %s", f.requests[0])
	}
}

func TestETagRevalidation(t *testing.T) {
	calls := 0
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, `{"jobs":[{"id":7,"status":"queued","labels":["self-hosted"]}]}`)
	})
	for i := 0; i < 2; i++ {
		jobs, err := c.ListJobs(context.Background(), "r", 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 1 || jobs[0].ID != 7 {
			t.Fatalf("pass %d: jobs = %+v", i, jobs)
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestClassify(t *testing.T) {
	reset := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		kind    ErrKind
		retryAt time.Time
	}{
		{"401", 401, nil, ErrAuth, time.Time{}},
		{"403 no rate headers", 403, map[string]string{"X-RateLimit-Remaining": "4000"}, ErrAuth, time.Time{}},
		{"403 primary limit", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())}, ErrRateLimit, reset},
		{"429 retry-after", 429, map[string]string{"Retry-After": "30"}, ErrRateLimit, time.Date(2026, 10, 3, 12, 0, 30, 0, time.UTC)},
		{"429 bad retry-after", 429, map[string]string{"Retry-After": "soon"}, ErrRateLimit, time.Date(2026, 10, 3, 12, 1, 0, 0, time.UTC)},
		{"404", 404, nil, ErrNotFound, time.Time{}},
		{"422", 422, nil, ErrUnprocessable, time.Time{}},
		{"502", 502, nil, ErrServer, time.Time{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"nope"}`)
			})
			c.Now = func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
			_, err := c.GetRepo(context.Background(), "r")
			if !IsKind(err, tc.kind) {
				t.Fatalf("err = %v, want kind %d", err, tc.kind)
			}
			if !tc.retryAt.IsZero() && !err.(*APIError).RetryAt.Equal(tc.retryAt) {
				t.Fatalf("retryAt = %v, want %v", err.(*APIError).RetryAt, tc.retryAt)
			}
		})
	}
}

func TestErrorNamesCallAndPermission(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Accepted-GitHub-Permissions", "actions=read")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
	})
	_, err := c.ListRuns(context.Background(), "darkcloud", "queued")
	ae, ok := err.(*APIError)
	if !ok || ae.Kind != ErrAuth || ae.Method != "GET" || ae.Path != "/repos/darkraise/darkcloud/actions/runs" || ae.Permissions != "actions=read" {
		t.Fatalf("err = %#v", err)
	}
}

func TestGenerateJITConfigBodyAndSpacing(t *testing.T) {
	var bodies []map[string]any
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		w.WriteHeader(201)
		fmt.Fprint(w, `{"runner":{"id":42,"name":"ghr-r-abc123"},"encoded_jit_config":"ENC"}`)
	})
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	var slept []time.Duration
	c.Now = func() time.Time { return now }
	c.Sleep = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
	for i := 0; i < 2; i++ {
		j, err := c.GenerateJITConfig(context.Background(), "r", "ghr-r-abc123", []string{"homelab"})
		if err != nil {
			t.Fatal(err)
		}
		if j.Runner.ID != 42 || j.EncodedJITConfig != "ENC" {
			t.Fatalf("jit = %+v", j)
		}
	}
	if bodies[0]["runner_group_id"] != float64(1) || bodies[0]["work_folder"] != "_work" || bodies[0]["name"] != "ghr-r-abc123" {
		t.Fatalf("body = %v", bodies[0])
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("second mutating call should wait 1s, slept %v", slept)
	}
}

func TestDeleteRunner404IsSuccess(t *testing.T) {
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	c.Sleep = func(time.Duration) {}
	if err := c.DeleteRunner(context.Background(), "r", 9); err != nil {
		t.Fatal(err)
	}
	if f.requests[0] != "DELETE /repos/darkraise/r/actions/runners/9" {
		t.Fatalf("request = %s", f.requests[0])
	}
}

func TestRateRemainingTracked(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4812")
		fmt.Fprint(w, `{"full_name":"darkraise/r","private":true}`)
	})
	if c.RateRemaining() != -1 {
		t.Fatal("expected -1 before any request")
	}
	repo, err := c.GetRepo(context.Background(), "r")
	if err != nil || !repo.Private {
		t.Fatalf("repo = %+v err = %v", repo, err)
	}
	if c.RateRemaining() != 4812 {
		t.Fatalf("remaining = %d", c.RateRemaining())
	}
}

func TestETag304KeepsPagination(t *testing.T) {
	var base string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if r.Header.Get("If-None-Match") == `"p`+page+`"` {
			w.WriteHeader(http.StatusNotModified) // no Link header on revalidation
			return
		}
		w.Header().Set("ETag", `"p`+page+`"`)
		if page == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
			fmt.Fprint(w, `{"workflow_runs":[{"id":1}]}`)
			return
		}
		fmt.Fprint(w, `{"workflow_runs":[{"id":2}]}`)
	})
	base = c.BaseURL
	for pass := 0; pass < 2; pass++ {
		runs, err := c.ListRuns(context.Background(), "r", "queued")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 2 {
			t.Fatalf("pass %d: runs = %+v", pass, runs)
		}
	}
}

func TestRateLimitSuspendsEveryRequest(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limited := true
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(now.Add(time.Minute).Unix()))
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(204)
	})
	c.Now = func() time.Time { return now }
	c.Sleep = func(d time.Duration) { now = now.Add(d) }
	if _, err := c.GetRepo(context.Background(), "r"); !IsKind(err, ErrRateLimit) {
		t.Fatalf("first call: %v", err)
	}
	if got := c.SuspendedUntil(); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("SuspendedUntil = %v", got)
	}
	limited = false
	if _, err := c.ListRunners(context.Background(), "r"); !IsKind(err, ErrRateLimit) {
		t.Fatalf("GET during suspension: %v", err)
	}
	if err := c.DeleteRunner(context.Background(), "r", 1); !IsKind(err, ErrRateLimit) {
		t.Fatalf("DELETE during suspension: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests sent during suspension: %v", f.requests)
	}
	now = now.Add(time.Minute)
	if !c.SuspendedUntil().IsZero() {
		t.Fatal("suspension should end at RetryAt")
	}
	if err := c.DeleteRunner(context.Background(), "r", 1); err != nil {
		t.Fatalf("after RetryAt: %v", err)
	}
	if len(f.requests) != 2 {
		t.Fatalf("requests = %v", f.requests)
	}
}

func TestSuspendedMutationFailsWithoutWaiting(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	var slept []time.Duration
	c.Now = func() time.Time { return now }
	c.Sleep = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
	for i := 0; i < 4; i++ {
		if err := c.DeleteRunner(context.Background(), "r", 1); !IsKind(err, ErrRateLimit) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if len(slept) != 0 || len(f.requests) != 1 {
		t.Fatalf("slept %v, requests %v", slept, f.requests)
	}
}

func TestMutationFailuresClassified(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   ErrKind
	}{{422, ErrUnprocessable}, {500, ErrServer}, {401, ErrAuth}} {
		c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			fmt.Fprint(w, `{"message":"no"}`)
		})
		c.Sleep = func(time.Duration) {}
		if _, err := c.GenerateJITConfig(context.Background(), "r", "ghr-r-abc123", []string{"homelab"}); !IsKind(err, tc.kind) {
			t.Errorf("JIT %d: err = %v", tc.status, err)
		}
		if err := c.DeleteRunner(context.Background(), "r", 9); !IsKind(err, tc.kind) {
			t.Errorf("DELETE %d: err = %v", tc.status, err)
		}
	}
}

func TestConcurrentMutationsAreSerialAndSpaced(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	var starts []time.Time
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		starts = append(starts, now)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		if r.Method == http.MethodPost {
			w.WriteHeader(201)
			fmt.Fprint(w, `{"runner":{"id":1},"encoded_jit_config":"E"}`)
			return
		}
		w.WriteHeader(204)
	})
	c.Now = clock
	c.Sleep = func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 2; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := c.GenerateJITConfig(context.Background(), "r", "ghr-r-abc123", []string{"homelab"})
			errs <- err
		}()
		go func() { defer wg.Done(); errs <- c.DeleteRunner(context.Background(), "r", 1) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maxInFlight != 1 {
		t.Fatalf("mutating calls overlapped: max in flight %d", maxInFlight)
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < time.Second {
			t.Fatalf("calls %d and %d only %v apart", i-1, i, gap)
		}
	}
}

func TestTokenMetaRecorded(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", "1798761600")
		w.Header().Set("GitHub-Authentication-Token-Expiration", "2026-12-31 23:59:59 UTC")
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud","private":true}`)
	})
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() || m.RateLimit != nil {
		t.Fatalf("meta before any call: %+v", m)
	}
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	m := c.TokenMeta()
	if !m.OK || m.CheckedAt.IsZero() || *m.RateRemaining != 4999 || *m.RateLimit != 5000 || m.RateReset.Unix() != 1798761600 {
		t.Fatalf("meta %+v", m)
	}
	if want := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC); m.ExpiresAt == nil || !m.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v", m.ExpiresAt)
	}
	gen := m.Generation
	c.ForgetCache()
	if m := c.TokenMeta(); m.Generation != gen+1 || !m.CheckedAt.IsZero() || m.ExpiresAt != nil || c.RateRemaining() != -1 {
		t.Fatalf("after ForgetCache: %+v remaining %d", m, c.RateRemaining())
	}
}

func TestTokenMetaRejectedAndUnparsedExpiry(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("GitHub-Authentication-Token-Expiration", "soon")
		w.WriteHeader(401)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	})
	if _, err := c.GetRepo(context.Background(), "darkcloud"); !IsKind(err, ErrAuth) {
		t.Fatalf("err %v", err)
	}
	if m := c.TokenMeta(); m.OK || m.CheckedAt.IsZero() || m.ExpiresAt != nil {
		t.Fatalf("meta %+v", m)
	}
}

// A response to a request sent with the previous token never updates the
// metadata of the new one.
func TestTokenMetaIgnoresOlderGeneration(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{})
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-release
		w.Header().Set("X-RateLimit-Remaining", "10")
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
	})
	done := make(chan error)
	go func() { _, err := c.GetRepo(context.Background(), "darkcloud"); done <- err }()
	<-arrived
	c.ForgetCache()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() || m.RateRemaining != nil || c.RateRemaining() != -1 {
		t.Fatalf("old response recorded: %+v", m)
	}
}

func TestSecondaryRateLimitIsNotAuth(t *testing.T) {
	msg := `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, msg)
	})
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }
	_, err := c.GetRepo(context.Background(), "darkcloud")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Kind != ErrRateLimit || !ae.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("err %#v", err)
	}
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() {
		t.Fatalf("a rate limit says nothing about the token: %+v", m)
	}
	msg = `{"message":"Resource not accessible by personal access token"}`
	c.ForgetCache()
	if _, err := c.GetRepo(context.Background(), "darkcloud"); !IsKind(err, ErrAuth) {
		t.Fatalf("permission error: %v", err)
	}
}

// Only a success or an authentication or permission failure is a verdict on
// the token; a server error, a 404 or a rate limit leaves the last verdict in
// place.
func TestTokenMetaOnlyCountsTokenVerdicts(t *testing.T) {
	status := 200
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(4000+status))
		if status == 429 {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud","message":"x"}`)
	})
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	first := c.TokenMeta()
	for _, status = range []int{500, 404, 429} {
		now = now.Add(time.Minute)
		if _, err := c.GetRepo(context.Background(), "darkcloud"); err == nil {
			t.Fatalf("status %d: no error", status)
		}
		c.mu.Lock()
		c.retryAt = time.Time{} // the next status must reach the server
		c.mu.Unlock()
		m := c.TokenMeta()
		if !m.OK || !m.CheckedAt.Equal(first.CheckedAt) || *m.RateRemaining != 4000+status {
			t.Fatalf("after %d: %+v", status, m)
		}
	}
	status = 401
	now = now.Add(time.Minute)
	if _, err := c.GetRepo(context.Background(), "darkcloud"); !IsKind(err, ErrAuth) {
		t.Fatalf("401: %v", err)
	}
	if m := c.TokenMeta(); m.OK || !m.CheckedAt.Equal(now) {
		t.Fatalf("after 401: %+v", m)
	}
}

// The daemon stores a new token before ForgetCache bumps the generation, so
// a request that read the old token just before a replacement must not be
// recorded against the new generation.
func TestTokenReplacedDuringRequest(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "10")
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
	})
	cur, replaced := "old", false
	c.Token = func() string {
		tok := cur
		if !replaced {
			replaced = true
			cur = "new"
			c.ForgetCache()
		}
		return tok
	}
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if m := c.TokenMeta(); m.Generation != 1 || !m.CheckedAt.IsZero() || m.RateRemaining != nil {
		t.Fatalf("old-token response recorded in the new generation: %+v", m)
	}
}

// A delayed response sent with the previous token restores neither a
// rate-limit suspension nor an ETag cache entry.
func TestOlderGenerationLeavesCacheAndSuspension(t *testing.T) {
	var mu sync.Mutex
	calls, inm := 0, ""
	release := make(chan struct{})
	arrived := make(chan struct{})
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		inm = r.Header.Get("If-None-Match")
		mu.Unlock()
		switch n {
		case 1:
			close(arrived)
			<-release
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
		case 2:
			fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
		}
	})
	done := make(chan error)
	go func() { _, err := c.GetRepo(context.Background(), "darkcloud"); done <- err }()
	<-arrived
	c.ForgetCache()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if inm != "" {
		t.Fatalf("old-generation ETag reused: %q", inm)
	}
	mu.Unlock()

	release2 := make(chan struct{})
	arrived2 := make(chan struct{})
	c2, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived2)
		<-release2
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "4102444800")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	})
	go func() { _, err := c2.GetRepo(context.Background(), "darkcloud"); done <- err }()
	<-arrived2
	c2.ForgetCache()
	close(release2)
	if err := <-done; !IsKind(err, ErrRateLimit) {
		t.Fatalf("err %v", err)
	}
	if until := c2.SuspendedUntil(); !until.IsZero() {
		t.Fatalf("old-generation rate limit suspended the new token until %v", until)
	}
}

func TestListRecentRunsTakesOnePage(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
		fmt.Fprint(w, `{"workflow_runs":[{"id":7,"status":"completed"},{"id":6,"status":"completed"}]}`)
	})
	base = c.BaseURL
	runs, err := c.ListRecentRuns(context.Background(), "darkcloud", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || len(f.requests) != 1 || !strings.HasSuffix(f.requests[0], "/repos/darkraise/darkcloud/actions/runs?per_page=20") {
		t.Fatalf("runs %+v requests %v", runs, f.requests)
	}
}

func TestRunnerLabelsDecoded(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"runners":[{"id":3,"name":"linux-1","status":"offline","busy":false,
			"labels":[{"id":1,"name":"self-hosted","type":"read-only"},{"id":9,"name":"darkcloud-linux","type":"custom"}]}]}`)
	})
	rs, err := c.ListRunners(context.Background(), "darkcloud")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || len(rs[0].Labels) != 2 || rs[0].Labels[1].Name != "darkcloud-linux" {
		t.Fatalf("runners %+v", rs)
	}
}
