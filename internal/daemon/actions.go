package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
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
			// A 403 that is not a rate limit is usually a missing PAT
			// permission, not a bad token.
			if ge.Status == 403 && ge.Permissions != "" {
				return fmt.Sprintf("token lacks %s on %s/%s: grant it in the PAT settings", ge.Permissions, owner, repo), nil
			}
			if ge.Status == 403 {
				return "GitHub refused the request: " + ge.Message, nil
			}
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

const (
	actionsTTL     = 15 * time.Second
	actionsWorkers = 4
)

// actionsDeadline bounds one /actions build; tests shorten it.
var actionsDeadline = 20 * time.Second

// good keeps each repository's runs from its last successful call, keyed by
// lower-case name, so a paused, rate-limited or rejected repository keeps
// showing them; a config change or a new token starts it over. Only the
// build in call touches it while the lock is released.
type actionsCache struct {
	mu     sync.Mutex
	cfg    *config.Config
	gen    uint64 // the token generation val and good were fetched under
	at     time.Time
	ok     bool
	val    model.Actions
	good   map[string][]model.ActionsRun
	goodAt time.Time
	call   *actionsCall
	// histFailing holds whether the last build failed to read history, so a
	// read that keeps failing warns once rather than at every build.
	histFailing bool
}

// actionsCall is the build that callers under the same config and token
// generation share.
type actionsCall struct {
	done chan struct{}
	cfg  *config.Config
	gen  uint64
	val  model.Actions
	err  error
}

// Actions shares one build between callers, so the fetch runs detached from
// any one request and the cache age counts from when the build finished. The
// lock is not held during the fetch: a caller that joins a build leaves when
// its own request ends.
func (b *Backend) Actions(ctx context.Context) (model.Actions, error) {
	c := &b.actions
	for {
		c.mu.Lock()
		cfg := b.Store.Config()
		gen := b.tokenGen()
		if c.ok && c.cfg == cfg && c.gen == gen && b.now().Sub(c.at) < actionsTTL {
			val := c.val
			c.mu.Unlock()
			return val, nil
		}
		call := c.call
		if call == nil {
			if c.cfg != cfg || c.gen != gen {
				c.good, c.goodAt = map[string][]model.ActionsRun{}, time.Time{}
			}
			call = &actionsCall{done: make(chan struct{}), cfg: cfg, gen: gen}
			c.call = call
			c.mu.Unlock()
			b.buildActions(ctx, call)
			return call.val, call.err
		}
		c.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			return model.Actions{}, ctx.Err()
		}
		// A build that began under an older config or token is not an answer
		// for this caller, which goes round again to start its own.
		if call.cfg == cfg && call.gen == gen {
			return call.val, call.err
		}
	}
}

// buildActions fills call. It must leave c.call and close done even if it
// panics, or every later request would wait on the build for good.
func (b *Backend) buildActions(ctx context.Context, call *actionsCall) {
	c := &b.actions
	built := false
	defer func() {
		if !built {
			call.err = errors.New("could not read the runs")
		}
		c.mu.Lock()
		c.call = nil
		c.mu.Unlock()
		close(call.done)
	}()

	covered := coveredRepos(call.cfg)
	runs := make([][]github.Run, len(covered))
	errs := make([]error, len(covered))
	if skip := b.githubErr(); skip != nil {
		for i := range covered {
			errs[i] = skip
		}
	} else {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actionsDeadline)
		sem := make(chan struct{}, actionsWorkers)
		var wg sync.WaitGroup
		for i, r := range covered {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// A panic on this detached goroutine would end the daemon,
				// so it becomes this repository's error.
				defer func() {
					if p := recover(); p != nil {
						log.Printf("actions: %s: panic: %v", r.repo, p)
						runs[i], errs[i] = nil, errors.New("could not read the runs")
					}
				}()
				select {
				case sem <- struct{}{}:
				case <-fctx.Done():
					errs[i] = fctx.Err()
					return
				}
				defer func() { <-sem }()
				runs[i], errs[i] = b.GH.ListRecentRuns(fctx, r.repo, actionRunsPerRepo)
			}()
		}
		wg.Wait()
		cancel()
	}

	ours, herr := b.ghrRuns()
	c.mu.Lock()
	defer c.mu.Unlock()
	if herr != nil && !c.histFailing {
		log.Printf("actions: cannot read history, so no run is marked as ghr's: %v", herr)
	}
	c.histFailing = herr != nil
	cfg := call.cfg
	now := b.now()
	out := model.Actions{FetchedAt: now, Runs: []model.ActionsRun{}, Repos: make([]model.ActionsRepo, 0, len(covered))}
	// fetched_at keeps the last good build's time only while every
	// repository is served from kept runs.
	fresh, allKept := false, len(covered) > 0
	for i, r := range covered {
		row := model.ActionsRepo{Repo: r.repo, Watched: r.watched}
		key := strings.ToLower(r.repo)
		if errs[i] == nil {
			fresh, allKept = true, false
			got := make([]model.ActionsRun, 0, len(runs[i]))
			for _, run := range runs[i] {
				got = append(got, actionsRun(r.repo, r.watched, run, ours))
			}
			c.good[key] = got
			out.Runs = append(out.Runs, got...)
		} else {
			row.Error, row.RetryAt = actionsErr(errs[i], cfg.Owner, r.repo)
			if keepsRuns(errs[i]) {
				for _, k := range c.good[key] {
					k.GHR = ours[ghrRunKey(k.Repo, k.ID)]
					out.Runs = append(out.Runs, k)
				}
			} else {
				allKept = false
				delete(c.good, key)
			}
		}
		out.Repos = append(out.Repos, row)
	}
	if fresh {
		c.goodAt = now
	} else if allKept && !c.goodAt.IsZero() {
		out.FetchedAt = c.goodAt
	}
	sortActions(out.Runs)
	// The build is kept under the generation it began under, so one the
	// token changed during is never served: the next call starts over.
	c.cfg, c.gen, c.at, c.ok, c.val = cfg, call.gen, now, true, out
	call.val = out
	built = true
}

// ghrRuns reads live instances, then pending records, then history: a job
// moves through them in that order, so one that moves mid-read is still seen.
// A failed history read marks nothing and returns its error.
func (b *Backend) ghrRuns() (map[string]bool, error) {
	insts := b.M.Status().Instances
	pend := b.M.Pending()
	hist, err := b.Hist.Query("", "", time.Time{}, 0)
	ours := map[string]bool{}
	if err != nil {
		return ours, err
	}
	for _, i := range insts {
		if i.Job != nil && i.Job.RunID != 0 {
			ours[ghrRunKey(i.Repo, i.Job.RunID)] = true
		}
	}
	for _, p := range pend {
		if p.RunID != 0 {
			ours[ghrRunKey(p.Repo, p.RunID)] = true
		}
	}
	for _, h := range hist {
		if h.RunID != 0 {
			ours[ghrRunKey(h.Repo, h.RunID)] = true
		}
	}
	return ours, nil
}
