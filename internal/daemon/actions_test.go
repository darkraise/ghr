package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
)

func testRun(id int64, status, conclusion string, start time.Time) github.Run {
	return github.Run{ID: id, Name: "ci", DisplayTitle: fmt.Sprintf("run %d", id), RunNumber: id, Status: status,
		Conclusion: conclusion, CreatedAt: start, RunStartedAt: &start, UpdatedAt: start.Add(time.Minute)}
}

func TestCoveredRepos(t *testing.T) {
	cfg := &config.Config{Repos: []config.Repo{{Name: "darkcloud", Paused: true}, {Name: "darkmem"}}, WatchRepos: []string{"docs"}}
	got := coveredRepos(cfg)
	want := []coveredRepo{{repo: "darkcloud"}, {repo: "darkmem"}, {repo: "docs", watched: true}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("covered %+v", got)
	}
}

func TestActionsRunFallsBackToCreatedAtAndName(t *testing.T) {
	created := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	r := github.Run{ID: 7, Name: "deploy", RunNumber: 3, Status: "completed", Conclusion: "failure", CreatedAt: created,
		HeadBranch: "master", Event: "push", Actor: github.Account{Login: "darkraise"}, HTMLURL: "https://example/7"}
	got := actionsRun("darkmem", true, r, map[string]bool{ghrRunKey("DarkMem", 7): true})
	want := model.ActionsRun{Repo: "darkmem", ID: 7, RunNumber: 3, Workflow: "deploy", Title: "deploy", Branch: "master",
		Event: "push", Actor: "darkraise", Status: "completed", Conclusion: "failure", StartedAt: created,
		HTMLURL: "https://example/7", GHR: true, Watched: true}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	started := created.Add(time.Minute)
	r.RunStartedAt, r.DisplayTitle = &started, "Fix it"
	if got := actionsRun("darkmem", false, r, nil); !got.StartedAt.Equal(started) || got.Title != "Fix it" || got.GHR {
		t.Fatalf("with run_started_at %+v", got)
	}
}

func TestActionsErr(t *testing.T) {
	retry := time.Date(2026, 10, 9, 14, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		err   error
		text  string
		retry *time.Time
		keeps bool
	}{
		{fmt.Errorf("get: %w", context.DeadlineExceeded), "GitHub did not answer in time", nil, false},
		{&github.APIError{Status: 404, Kind: github.ErrNotFound}, "token cannot see darkraise/docs: add it to the PAT's repository access first", nil, false},
		{&github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: retry}, "GitHub rate limit; API calls are paused", &retry, true},
		{&github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"}, "GitHub rejected the token: github: 401 Bad credentials", nil, true},
		{&api.Error{Status: 429, Msg: "GitHub rate limit; API calls are paused", RetryAt: retry}, "GitHub rate limit; API calls are paused", &retry, true},
		{&api.Error{Status: 503, Msg: "GitHub is rejecting the token: 401"}, "GitHub is rejecting the token: 401", nil, true},
		{errors.New("connection reset"), "connection reset", nil, false},
	} {
		text, at := actionsErr(c.err, "darkraise", "docs")
		if text != c.text || (at == nil) != (c.retry == nil) || (at != nil && !at.Equal(*c.retry)) {
			t.Errorf("actionsErr(%v) = %q, %v", c.err, text, at)
		}
		if keepsRuns(c.err) != c.keeps {
			t.Errorf("keepsRuns(%v) = %v", c.err, !c.keeps)
		}
	}
}

func TestSortActions(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	rs := []model.ActionsRun{
		{Repo: "b", ID: 1, Status: "completed", StartedAt: t0},
		{Repo: "a", ID: 2, Status: "queued", StartedAt: t0.Add(-time.Hour)},
		{Repo: "a", ID: 3, Status: "completed", StartedAt: t0},
		{Repo: "a", ID: 4, Status: "completed", StartedAt: t0.Add(time.Minute)},
		{Repo: "a", ID: 5, Status: "in_progress", StartedAt: t0},
		{Repo: "a", ID: 0, Status: "completed", StartedAt: t0},
		{Repo: "Zoo", ID: 6, Status: "completed", StartedAt: t0},
		{Repo: "alpha", ID: 7, Status: "completed", StartedAt: t0},
	}
	sortActions(rs)
	var ids []int64
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	if fmt.Sprint(ids) != "[5 2 4 0 3 7 1 6]" {
		t.Fatalf("order %v", ids)
	}
}

func setActionsDeadline(t *testing.T, d time.Duration) {
	old := actionsDeadline
	actionsDeadline = d
	t.Cleanup(func() { actionsDeadline = old })
}

// frozenClock gives b a clock the test moves; atomic, because builds read it
// from other goroutines.
func frozenClock(b *Backend, start time.Time) *atomic.Int64 {
	var ns atomic.Int64
	ns.Store(start.UnixNano())
	b.Now = func() time.Time { return time.Unix(0, ns.Load()).UTC() }
	return &ns
}

func recentCalls(gh *fakeGH) int {
	gh.lmu.Lock()
	defer gh.lmu.Unlock()
	return len(gh.recentCalls)
}

func waitInFlight(t *testing.T, gh *fakeGH, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		gh.lmu.Lock()
		n := gh.recentInFlight
		gh.lmu.Unlock()
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d calls in flight, want %d", n, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runIDs(a model.Actions) string {
	var ids []int64
	for _, r := range a.Runs {
		ids = append(ids, r.ID)
	}
	return fmt.Sprint(ids)
}

var actionsT0 = time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)

func TestActionsMergesCoveredRepos(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store.Update(func(c *config.Config) error { c.Repo("darkcloud").Paused = true; return nil }); err != nil {
		t.Fatal(err)
	}
	frozenClock(b, actionsT0)
	noStart := testRun(3, "completed", "failure", actionsT0.Add(-time.Hour))
	noStart.RunStartedAt = nil
	gh.recentBy = map[string][]github.Run{
		"darkcloud": {testRun(1, "completed", "success", actionsT0.Add(-10*time.Minute))},
		"darkmem":   {testRun(2, "in_progress", "", actionsT0.Add(-2*time.Minute)), noStart},
		"public":    {testRun(4, "completed", "success", actionsT0.Add(-5*time.Minute))},
	}
	if err := b.Hist.Append(model.HistoryEntry{ID: "h", Repo: "DarkMem", RunID: 3, Conclusion: "failure",
		StartedAt: actionsT0.Add(-time.Hour), FinishedAt: actionsT0.Add(-50 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", RunID: 2}}
	m.insts = []model.InstanceStatus{{ID: "i", Repo: "DARKCLOUD", State: "busy", Job: &model.JobInfo{RunID: 1}}}

	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2 4 1 3]" {
		t.Fatalf("order %s", runIDs(a))
	}
	for _, r := range a.Runs {
		if r.GHR != (r.ID != 4) || r.Watched != (r.ID == 4) {
			t.Errorf("run %d ghr %v watched %v", r.ID, r.GHR, r.Watched)
		}
	}
	if !a.Runs[3].StartedAt.Equal(actionsT0.Add(-time.Hour)) || !a.FetchedAt.Equal(actionsT0) {
		t.Fatalf("start %v fetched %v", a.Runs[3].StartedAt, a.FetchedAt)
	}
	want := []model.ActionsRepo{{Repo: "darkcloud"}, {Repo: "darkmem"}, {Repo: "public", Watched: true}}
	if len(a.Repos) != 3 || a.Repos[0] != want[0] || a.Repos[1] != want[1] || a.Repos[2] != want[2] {
		t.Fatalf("repos %+v", a.Repos)
	}
	if fmt.Sprint(gh.recentN) != "[50 50 50]" {
		t.Fatalf("asked for %v runs", gh.recentN)
	}
}

func TestActionsMarkAJobFinalizedMidBuild(t *testing.T) {
	b, m, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(1, "completed", "success", time.Now().Add(-time.Hour))}}
	m.onPending = func() {
		if err := b.Hist.Append(model.HistoryEntry{ID: "p", Repo: "darkmem", RunID: 1, Conclusion: "success",
			StartedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Runs) != 1 || !a.Runs[0].GHR {
		t.Fatalf("runs %+v", a.Runs)
	}
}

func TestActionsFailedHistoryMarksNothing(t *testing.T) {
	b, m, gh := newBackend(t)
	b.Hist = &history.Store{Path: t.TempDir()} // a directory: reading it fails
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", RunID: 1}}
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(1, "completed", "success", time.Now().Add(-time.Hour))}}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Runs) != 1 || a.Runs[0].GHR {
		t.Fatalf("runs %+v", a.Runs)
	}
}

func TestActionsWarnsOnceWhileHistoryReadsFail(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	b, _, _ := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	good := b.Hist
	build := func(at time.Duration, h *history.Store) {
		t.Helper()
		clock.Store(actionsT0.Add(at).UnixNano())
		b.Hist = h
		if _, err := b.Actions(ctx); err != nil {
			t.Fatal(err)
		}
	}
	bad := &history.Store{Path: t.TempDir()}
	build(0, bad)
	build(time.Minute, bad)
	if n := strings.Count(logs.String(), "cannot read history"); n != 1 {
		t.Fatalf("%d warnings while failing:\n%s", n, logs.String())
	}
	build(2*time.Minute, good)
	build(3*time.Minute, bad)
	if n := strings.Count(logs.String(), "cannot read history"); n != 2 {
		t.Fatalf("%d warnings after a recovery:\n%s", n, logs.String())
	}
}

func TestActionsRepoErrorKeepsTheOthers(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(2, "completed", "success", time.Now().Add(-time.Hour))}}
	gh.recentErr = map[string]error{"darkcloud": &github.APIError{Status: 404, Kind: github.ErrNotFound}}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2]" || a.Repos[0].Error != "token cannot see darkraise/darkcloud: add it to the PAT's repository access first" || a.Repos[1].Error != "" {
		t.Fatalf("runs %s repos %+v", runIDs(a), a.Repos)
	}
}

func TestActionsDeadlineOutlivesTheRequest(t *testing.T) {
	setActionsDeadline(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{}
	gh.recentWait = make(chan struct{}) // never closed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Repos) != 2 {
		t.Fatalf("repos %+v", a.Repos)
	}
	for _, r := range a.Repos {
		if r.Error != "GitHub did not answer in time" {
			t.Fatalf("repo %+v", r)
		}
	}
}

func TestActionsCacheFollowsTTLAndConfig(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{}
	for i, step := range []struct {
		at   time.Duration
		want int
	}{{0, 2}, {actionsTTL - time.Millisecond, 2}, {actionsTTL, 4}} {
		clock.Store(actionsT0.Add(step.at).UnixNano())
		if _, err := b.Actions(ctx); err != nil || recentCalls(gh) != step.want {
			t.Fatalf("step %d: %d calls, want %d, err %v", i, recentCalls(gh), step.want, err)
		}
	}
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Actions(ctx); err != nil || recentCalls(gh) != 7 {
		t.Fatalf("after a config change: %d calls, err %v", recentCalls(gh), err)
	}
}

func TestActionsCallersShareOneBuild(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{}
	gh.recentWait = make(chan struct{})
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			b.Actions(context.Background())
			done <- struct{}{}
		}()
	}
	waitInFlight(t, gh, 2)
	clock.Store(actionsT0.Add(actionsTTL + time.Second).UnixNano()) // the build outlasts the TTL
	close(gh.recentWait)
	<-done
	<-done
	if n := recentCalls(gh); n != 2 {
		t.Fatalf("%d calls, want 2: the waiting caller rebuilt instead of sharing", n)
	}
}

func TestActionsDegradedTokenMakesNoCalls(t *testing.T) {
	b, m, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{}
	m.degraded = "401 Bad credentials"
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recentCalls(gh) != 0 || len(a.Repos) != 2 {
		t.Fatalf("calls %d repos %+v", recentCalls(gh), a.Repos)
	}
	for _, r := range a.Repos {
		if r.Error != "GitHub is rejecting the token: 401 Bad credentials" || r.RetryAt != nil {
			t.Fatalf("repo %+v", r)
		}
	}
}

func TestActionsKeepRunsWhileGitHubIsPaused(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(1, "completed", "success", actionsT0.Add(-time.Hour))}}
	if err := b.Hist.Append(model.HistoryEntry{ID: "h", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: actionsT0.Add(-time.Hour), FinishedAt: actionsT0.Add(-50 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	first, err := b.Actions(ctx)
	if err != nil || len(first.Runs) != 1 || !first.Runs[0].GHR {
		t.Fatalf("first %+v err %v", first, err)
	}
	clock.Store(actionsT0.Add(time.Minute).UnixNano())
	m.paused = actionsT0.Add(10 * time.Minute)
	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recentCalls(gh) != 2 || runIDs(a) != "[1]" || !a.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("calls %d runs %s fetched %v", recentCalls(gh), runIDs(a), a.FetchedAt)
	}
	for _, r := range a.Repos {
		if r.Error != "GitHub rate limit; API calls are paused" || r.RetryAt == nil || !r.RetryAt.Equal(m.paused) {
			t.Fatalf("repo %+v", r)
		}
	}

	// Kept runs are marked again on every build: a failed history read now
	// marks nothing.
	clock.Store(actionsT0.Add(2 * time.Minute).UnixNano())
	b.Hist = &history.Store{Path: t.TempDir()}
	if a, _ := b.Actions(ctx); runIDs(a) != "[1]" || a.Runs[0].GHR {
		t.Fatalf("kept run after a failed history read %+v", a.Runs)
	}

	// A config change drops the kept runs.
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if a, _ := b.Actions(ctx); runIDs(a) != "[]" {
		t.Fatalf("kept runs survived a config change: %s", runIDs(a))
	}
}

func TestActionsKeepRunsOnARateLimitedCall(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{
		"darkmem":   {testRun(1, "completed", "success", actionsT0.Add(-time.Hour))},
		"darkcloud": {testRun(5, "completed", "success", actionsT0.Add(-2*time.Hour))},
	}
	if _, err := b.Actions(ctx); err != nil {
		t.Fatal(err)
	}
	t1 := actionsT0.Add(time.Minute)
	clock.Store(t1.UnixNano())
	gh.recentBy["darkcloud"] = []github.Run{testRun(2, "completed", "success", t1.Add(-30*time.Minute))}
	gh.recentErr = map[string]error{"darkmem": &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: t1.Add(5 * time.Minute)}}
	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2 1]" || a.Repos[1].Error != "GitHub rate limit; API calls are paused" || !a.FetchedAt.Equal(t1) {
		t.Fatalf("runs %s repos %+v fetched %v", runIDs(a), a.Repos, a.FetchedAt)
	}

	t2 := t1.Add(time.Minute)
	clock.Store(t2.UnixNano())
	notFound := &github.APIError{Status: 404, Kind: github.ErrNotFound}
	gh.recentErr = map[string]error{"darkmem": notFound, "darkcloud": notFound}
	a, _ = b.Actions(ctx)
	if runIDs(a) != "[]" || !a.FetchedAt.Equal(t2) {
		t.Fatalf("an all-404 build kept runs %s or its old time %v", runIDs(a), a.FetchedAt)
	}
}

func TestActionsRunsAtMostFourCallsAtOnce(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	for _, name := range []string{"w1", "w2", "w3", "w4"} {
		gh.repos[name] = &github.Repository{Private: true}
		if err := b.WatchRepo(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	gh.recentBy = map[string][]github.Run{}
	gh.recentWait = make(chan struct{})
	done := make(chan model.Actions, 1)
	go func() {
		a, _ := b.Actions(ctx)
		done <- a
	}()
	waitInFlight(t, gh, 4)
	time.Sleep(50 * time.Millisecond)
	close(gh.recentWait)
	a := <-done
	gh.lmu.Lock()
	peak, calls := gh.recentPeak, len(gh.recentCalls)
	gh.lmu.Unlock()
	if peak != 4 || calls != 6 || len(a.Repos) != 6 {
		t.Fatalf("peak %d calls %d repos %d", peak, calls, len(a.Repos))
	}
}
