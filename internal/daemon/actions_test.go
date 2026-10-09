package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
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
