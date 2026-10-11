package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func setDetailDeadline(t *testing.T, d time.Duration) {
	old := detailDeadline
	detailDeadline = d
	t.Cleanup(func() { detailDeadline = old })
}

// detailRun gives gh run 102 of darkmem: attempt 2 of 2, two jobs per
// attempt, one artifact and the ci.yml of ciYAML.
func detailRun(gh *fakeGH, status string) {
	start := actionsT0.Add(-10 * time.Minute)
	run := github.Run{ID: 102, Name: "ci", DisplayTitle: "Fix the cache key", RunNumber: 42, HeadBranch: "fix/cache-key",
		Event: "pull_request", Actor: github.Account{Login: "octo-dev"}, Status: status, Conclusion: "failure",
		CreatedAt: start.Add(-time.Minute), RunStartedAt: &start, UpdatedAt: start.Add(4 * time.Minute),
		HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102", HeadSHA: "abc", Path: ciPath, RunAttempt: 2,
		PullRequests: []github.PullRequest{{Number: 87}}}
	first := run
	first.RunAttempt, first.Conclusion = 1, "success"
	jobs := func(base int64) []github.Job {
		return []github.Job{
			{ID: base + 1, RunID: 102, Name: "Build", Status: "completed", Conclusion: "success", RunnerName: "ghr-darkmem-a1b2c3",
				Labels: []string{"self-hosted"}, Steps: []github.Step{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"}}},
			{ID: base + 2, RunID: 102, Name: "test", Status: "completed", Conclusion: "failure"},
		}
	}
	gh.runByID = map[int64]github.Run{102: run}
	gh.jobs = map[int64][]github.Job{102: jobs(2000)}
	gh.attemptRuns = map[string]github.Run{attemptKey(102, 1): first}
	gh.attemptJobs = map[string][]github.Job{attemptKey(102, 1): jobs(1000)}
	gh.artifacts = map[int64][]github.Artifact{102: {{ID: 11, Name: "dist", SizeInBytes: 2400000}}}
	gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
}

func TestRunDetailAssemblesTheLatestAttempt(t *testing.T) {
	b, m, gh := newBackend(t)
	frozenClock(b, actionsT0)
	detailRun(gh, "completed")
	if err := b.Hist.Append(model.HistoryEntry{ID: "h", Repo: "DarkMem", RunID: 102, JobName: "Build", Conclusion: "success",
		StartedAt: actionsT0.Add(-9 * time.Minute), FinishedAt: actionsT0.Add(-8 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", RunID: 999, JobName: "test"}}

	d, err := b.RunDetail(context.Background(), "DarkMem", 102, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Repo != "darkmem" || d.Watched || d.Attempt != 2 || d.Attempts != 2 || d.Title != "Fix the cache key" || d.Workflow != "ci" ||
		d.Branch != "fix/cache-key" || d.Actor != "octo-dev" || d.HeadSHA != "abc" || fmt.Sprint(d.PullRequests) != "[87]" ||
		!d.StartedAt.Equal(actionsT0.Add(-10*time.Minute)) || !d.FetchedAt.Equal(actionsT0) {
		t.Fatalf("detail %+v", d)
	}
	if len(d.Jobs) != 2 || d.Jobs[0].ID != 2001 || !d.Jobs[0].GHR || d.Jobs[1].GHR || d.Jobs[1].Labels == nil || d.Jobs[1].Steps == nil ||
		len(d.Jobs[0].Steps) != 1 {
		t.Fatalf("jobs %+v", d.Jobs)
	}
	if d.Graph == nil || len(d.Graph.Nodes) != 2 || d.GraphNote != "" {
		t.Fatalf("graph %+v note %q", d.Graph, d.GraphNote)
	}
	if len(d.Artifacts) != 1 || d.Artifacts[0].SizeBytes != 2400000 {
		t.Fatalf("artifacts %+v", d.Artifacts)
	}
	if gh.count("GetRunAttempt") != 0 || len(gh.jobCalls) != 1 {
		t.Fatalf("calls %v, ListJobs %v", gh.calls, gh.jobCalls)
	}
}

func TestRunDetailEarlierAttempt(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	d, err := b.RunDetail(context.Background(), "darkmem", 102, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Attempt != 1 || d.Attempts != 2 || d.Conclusion != "success" || d.Jobs[0].ID != 1001 || len(gh.jobCalls) != 0 {
		t.Fatalf("detail %+v, ListJobs %v", d, gh.jobCalls)
	}
	if gh.count("GetRunAttempt darkmem 102/1") != 1 || gh.count("ListAttemptJobs darkmem 102/1") != 1 {
		t.Fatalf("calls %v", gh.calls)
	}
}

func TestRunDetailRepoAndAttemptBounds(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	ctx := context.Background()
	var ae *api.Error
	if _, err := b.RunDetail(ctx, "nope", 102, 0); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != "repo nope is not configured or watched" {
		t.Fatalf("uncovered: %v", err)
	}
	if len(gh.calls) != 0 {
		t.Fatalf("an uncovered repository called GitHub: %v", gh.calls)
	}
	if _, err := b.RunDetail(ctx, "darkmem", 102, 3); !errors.As(err, &ae) || ae.Status != 400 || ae.Msg != "run 102 has no attempt 3" {
		t.Fatalf("attempt 3: %v", err)
	}
	if _, err := b.Store.Update(func(c *config.Config) error { c.WatchRepos = []string{"Docs"}; return nil }); err != nil {
		t.Fatal(err)
	}
	gh.content = map[string][]byte{contentKey("Docs", ciPath, "abc"): []byte(ciYAML)}
	d, err := b.RunDetail(ctx, "docs", 102, 0)
	if err != nil || d.Repo != "Docs" || !d.Watched || gh.count("GetRun Docs 102") != 1 {
		t.Fatalf("watched: %+v err %v calls %v", d, err, gh.calls)
	}
}

func TestRunDetailErrors(t *testing.T) {
	retry := actionsT0.Add(30 * time.Minute)
	for _, tc := range []struct {
		name   string
		err    error
		status int
		msg    string
	}{
		{"missing permission", &github.APIError{Status: 403, Kind: github.ErrAuth, Permissions: "actions=read"}, 403,
			"token lacks actions=read on darkraise/darkmem: grant it in the PAT settings"},
		{"not found", &github.APIError{Status: 404, Kind: github.ErrNotFound}, 404, "not found on GitHub"},
		{"rate limit", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: retry}, 429, "GitHub rate limit; API calls are paused"},
		{"server", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"}, 502, "github: 502 Bad Gateway"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			gh.runErr = tc.err
			_, err := b.RunDetail(context.Background(), "darkmem", 102, 0)
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg {
				t.Fatalf("got %#v", err)
			}
			if tc.status == 429 && !ae.RetryAt.Equal(retry) {
				t.Fatalf("retry at %v", ae.RetryAt)
			}
		})
	}
}

func TestRunDetailRefusesWhileDegraded(t *testing.T) {
	b, m, gh := newBackend(t)
	m.degraded = "401 Bad credentials"
	_, err := b.RunDetail(context.Background(), "darkmem", 102, 0)
	if apiStatus(err) != 503 || len(gh.calls) != 0 {
		t.Fatalf("err %v calls %v", err, gh.calls)
	}
}

func TestRunDetailDeadlineOutlivesTheRequest(t *testing.T) {
	setDetailDeadline(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	gh.runWait = make(chan struct{}) // never closed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.RunDetail(ctx, "darkmem", 102, 0)
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Status != 504 || ae.Msg != "GitHub did not answer in time" {
		t.Fatalf("err %#v", err)
	}
}

func TestRunDetailLatestSharesAnEntryWithItsNumber(t *testing.T) {
	for _, order := range [][]int{{0, 2}, {2, 0}} {
		b, _, gh := newBackend(t)
		detailRun(gh, "completed")
		for _, a := range order {
			if _, err := b.RunDetail(context.Background(), "darkmem", 102, a); err != nil {
				t.Fatal(err)
			}
		}
		if n := gh.count("GetRun"); n != 1 {
			t.Fatalf("order %v: %d GetRun calls, want 1", order, n)
		}
	}
}

func TestRunDetailCacheLifetime(t *testing.T) {
	for _, tc := range []struct {
		status string
		ttl    time.Duration
	}{{"in_progress", detailActiveTTL}, {"completed", detailDoneTTL}} {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		detailRun(gh, tc.status)
		for i, step := range []struct {
			at   time.Duration
			want int
		}{{0, 1}, {tc.ttl - time.Millisecond, 1}, {tc.ttl, 2}} {
			clock.Store(actionsT0.Add(step.at).UnixNano())
			if _, err := b.RunDetail(context.Background(), "darkmem", 102, 0); err != nil || gh.count("GetRun") != step.want {
				t.Fatalf("%s step %d: %d calls, want %d, err %v", tc.status, i, gh.count("GetRun"), step.want, err)
			}
		}
	}
}

func TestRunDetailCacheHolds64Entries(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	detailRun(gh, "completed")
	run := gh.runByID[102]
	run.RunAttempt = 1
	for id := int64(1); id <= detailEntries; id++ {
		r := run
		r.ID = id
		gh.runByID[id] = r
	}
	ctx := context.Background()
	for id := int64(1); id <= detailEntries; id++ {
		clock.Store(actionsT0.Add(time.Duration(id) * time.Millisecond).UnixNano())
		if _, err := b.RunDetail(ctx, "darkmem", id, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(b.details.entries); n != detailEntries {
		t.Fatalf("%d entries, want %d", n, detailEntries)
	}
	if _, ok := b.details.entries[detailKey{repo: "darkmem", id: 1, attempt: 1}]; ok {
		t.Fatal("the oldest entry was kept")
	}
	if _, err := b.RunDetail(ctx, "darkmem", detailEntries, 1); err != nil || gh.count("GetRun darkmem 64") != 1 {
		t.Fatalf("the newest entry was not kept: err %v, calls %d", err, gh.count("GetRun darkmem 64"))
	}
}

func TestRunDetailCallersShareOneFetch(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	gh.runWait = make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := b.RunDetail(context.Background(), "darkmem", 102, 0)
			done <- err
		}()
	}
	deadline := time.Now().Add(3 * time.Second)
	for gh.count("GetRun") < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(gh.runWait)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := gh.count("GetRun"); n != 1 {
		t.Fatalf("%d GetRun calls, want 1", n)
	}
}

// panickyGH panics where its flags say, to check that a failure inside a
// GitHub call is contained.
type panickyGH struct {
	GitHub
	run, jobLog, content, openLog bool
}

func (p *panickyGH) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	if p.content {
		panic("boom")
	}
	return p.GitHub.GetContent(ctx, repo, path, ref)
}

func (p *panickyGH) GetRun(ctx context.Context, repo string, id int64) (*github.Run, error) {
	if p.run {
		panic("boom")
	}
	return p.GitHub.GetRun(ctx, repo, id)
}

func (p *panickyGH) JobLog(ctx context.Context, repo string, jobID int64, limit int64) ([]byte, error) {
	if p.jobLog {
		panic("boom")
	}
	return p.GitHub.JobLog(ctx, repo, jobID, limit)
}

func (p *panickyGH) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	if p.openLog {
		if f, ok := p.GitHub.(*fakeGH); ok {
			f.rmu.Lock()
			f.logCtx = ctx
			f.rmu.Unlock()
		}
		panic("boom")
	}
	return p.GitHub.OpenJobLog(ctx, repo, jobID)
}

func TestRunDetailPanicLeavesNoCallBehind(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	p := &panickyGH{GitHub: gh, run: true}
	b.GH = p
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ae *api.Error
	if _, err := b.RunDetail(ctx, "darkmem", 102, 0); !errors.As(err, &ae) || ae.Status != 502 {
		t.Fatalf("panic answered %v, want a 502", err)
	}
	b.details.mu.Lock()
	left := len(b.details.calls)
	b.details.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d calls left in flight", left)
	}
	p.run = false
	if _, err := b.RunDetail(ctx, "darkmem", 102, 0); err != nil {
		t.Fatalf("the next request failed: %v", err)
	}
}

func TestRunGraphPanicCostsOnlyTheGraph(t *testing.T) {
	b, _, gh := newBackend(t)
	b.GH = &panickyGH{GitHub: gh, content: true}
	g, note, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs())
	if err != nil || g != nil || note != fmt.Sprintf(noteInvalid, ciPath) {
		t.Fatalf("graph %v, note %q, err %v", g, note, err)
	}
}
