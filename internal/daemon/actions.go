package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

const actionRunsPerRepo = 50

type coveredRepo struct {
	repo    string
	watched bool
}

// coveredRepos lists the configured repositories, paused and removing ones
// included, then the watched ones, each in config order.
func coveredRepos(cfg *config.Config) []coveredRepo {
	out := make([]coveredRepo, 0, len(cfg.Repos)+len(cfg.WatchRepos))
	for _, r := range cfg.Repos {
		out = append(out, coveredRepo{repo: r.Name})
	}
	for _, w := range cfg.WatchRepos {
		out = append(out, coveredRepo{repo: w, watched: true})
	}
	return out
}

// ghrRunKey identifies a workflow run ghr's runners took part in.
func ghrRunKey(repo string, runID int64) string {
	return strings.ToLower(repo) + "#" + strconv.FormatInt(runID, 10)
}

// actionsRun converts a GitHub run. It starts at created_at when GitHub left
// run_started_at out, and its title falls back to the workflow name.
func actionsRun(repo string, watched bool, r github.Run, ours map[string]bool) model.ActionsRun {
	start := r.CreatedAt
	if r.RunStartedAt != nil && !r.RunStartedAt.IsZero() {
		start = *r.RunStartedAt
	}
	title := r.DisplayTitle
	if title == "" {
		title = r.Name
	}
	return model.ActionsRun{
		Repo: repo, ID: r.ID, RunNumber: r.RunNumber, Workflow: r.Name, Title: title, Branch: r.HeadBranch,
		Event: r.Event, Actor: r.Actor.Login, Status: r.Status, Conclusion: r.Conclusion,
		StartedAt: start, UpdatedAt: r.UpdatedAt, HTMLURL: r.HTMLURL,
		GHR: ours[ghrRunKey(repo, r.ID)], Watched: watched,
	}
}

// actionsErr is the text, and the retry time when calls are paused, that a
// repository whose runs could not be read shows.
func actionsErr(err error, owner, repo string) (string, *time.Time) {
	var ae *api.Error
	if errors.As(err, &ae) {
		if ae.RetryAt.IsZero() {
			return ae.Msg, nil
		}
		at := ae.RetryAt
		return ae.Msg, &at
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "GitHub did not answer in time", nil
	}
	var ge *github.APIError
	if errors.As(err, &ge) {
		switch ge.Kind {
		case github.ErrNotFound:
			return fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", owner, repo), nil
		case github.ErrRateLimit:
			at := ge.RetryAt
			return "GitHub rate limit; API calls are paused", &at
		case github.ErrAuth:
			return "GitHub rejected the token: " + ge.Error(), nil
		}
	}
	return err.Error(), nil
}

// keepsRuns reports whether a repository whose call failed with err keeps
// the runs of its last successful call: a paused, rate-limited or rejected
// token says nothing about the runs themselves.
func keepsRuns(err error) bool {
	var ae *api.Error
	if errors.As(err, &ae) {
		return true
	}
	return github.IsKind(err, github.ErrRateLimit) || github.IsKind(err, github.ErrAuth)
}

// Ties go by repository ignoring case, then ID, so the order is stable
// across polls.
func sortActions(rs []model.ActionsRun) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if ad, bd := a.Status == "completed", b.Status == "completed"; ad != bd {
			return !ad
		}
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.After(b.StartedAt)
		}
		if ar, br := strings.ToLower(a.Repo), strings.ToLower(b.Repo); ar != br {
			return ar < br
		}
		return a.ID < b.ID
	})
}
