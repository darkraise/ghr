package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

const (
	detailActiveTTL = 15 * time.Second
	detailDoneTTL   = 60 * time.Second
	detailEntries   = 64
)

// detailDeadline bounds one run details fetch; tests shorten it.
var detailDeadline = 20 * time.Second

// detailKey's attempt is 0 for "the latest".
type detailKey struct {
	repo    string
	id      int64
	attempt int
}

type detailEntry struct {
	at  time.Time
	val model.RunDetail
}

// detailCall is one fetch that concurrent callers for a key share.
type detailCall struct {
	done chan struct{}
	val  model.RunDetail
	err  error
}

type detailCache struct {
	mu      sync.Mutex
	gen     uint64 // the token generation entries was filled under
	entries map[detailKey]detailEntry
	calls   map[detailKey]*detailCall
	// histFailing holds whether the last fetch failed to read history, so a
	// read that keeps failing warns once.
	histFailing bool
}

// detailTTL is how long d is kept under k. The latest-attempt key (attempt
// 0) is always short-lived, because a re-run of a finished run changes which
// attempt it stands for; a numbered attempt that has completed never changes,
// unless its graph could not be read, which the next call tries again.
func detailTTL(k detailKey, d model.RunDetail) time.Duration {
	if k.attempt != 0 && d.Status == "completed" && d.GraphNote != noteUnavailable {
		return detailDoneTTL
	}
	return detailActiveTTL
}

// coveredName resolves a configured or watched repository, ignoring case, to
// its configured spelling, and reports whether it is watched.
func (b *Backend) coveredName(name string) (string, bool, error) {
	cfg := b.Store.Config()
	if r := cfg.Repo(name); r != nil {
		return r.Name, false, nil
	}
	for _, w := range cfg.WatchRepos {
		if strings.EqualFold(w, name) {
			return w, true, nil
		}
	}
	return "", false, api.NotFound("repo " + name + " is not configured or watched")
}

// detailErr gives an error from a run details call the status the routes
// answer with: 404 and 410 keep theirs, and the rest read as on the Actions
// page, so a 403 names the missing permission.
func detailErr(err error, owner, repo string) error {
	var ae *api.Error
	if err == nil || errors.As(err, &ae) {
		return err
	}
	switch {
	case github.IsKind(err, github.ErrNotFound):
		return api.NotFound("not found on GitHub")
	case github.IsKind(err, github.ErrGone):
		return &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps it"}
	}
	msg, retry := actionsErr(err, owner, repo)
	switch {
	case retry != nil:
		return &api.Error{Status: http.StatusTooManyRequests, Msg: msg, RetryAt: *retry}
	case errors.Is(err, context.DeadlineExceeded):
		return &api.Error{Status: http.StatusGatewayTimeout, Msg: msg}
	case github.IsKind(err, github.ErrAuth):
		return &api.Error{Status: http.StatusForbidden, Msg: msg}
	}
	return &api.Error{Status: http.StatusBadGateway, Msg: msg}
}

// ghrJobKey names the job a runner ran in a run. A ghr runner runs one job,
// and a re-run attempt lists each job it did not re-run under the runner
// that ran it, so the runner tells ghr's jobs apart from re-runs of the same
// job name on other runners.
func ghrJobKey(repo string, runID int64, runner string) string {
	return strings.ToLower(repo) + "#" + strconv.FormatInt(runID, 10) + "#" + strings.ToLower(runner)
}

// ghrJobs reads the same sources as ghrRuns, in the same order, keyed by the
// runner that ran each job.
func (b *Backend) ghrJobs() (map[string]bool, error) {
	insts := b.M.Status().Instances
	pend := b.M.Pending()
	hist, err := b.Hist.Query("", "", time.Time{}, 0)
	ours := map[string]bool{}
	if err != nil {
		return ours, err
	}
	for _, i := range insts {
		if i.Job != nil && i.Job.RunID != 0 {
			ours[ghrJobKey(i.Repo, i.Job.RunID, i.RunnerName)] = true
		}
	}
	for _, p := range pend {
		if p.RunID != 0 {
			ours[ghrJobKey(p.Repo, p.RunID, "ghr-"+p.Repo+"-"+p.ID)] = true
		}
	}
	for _, h := range hist {
		if h.RunID != 0 {
			ours[ghrJobKey(h.Repo, h.RunID, "ghr-"+h.Repo+"-"+h.ID)] = true
		}
	}
	return ours, nil
}

func modelSteps(steps []github.Step) []model.Step {
	out := make([]model.Step, 0, len(steps))
	for _, s := range steps {
		out = append(out, model.Step{Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: s.Conclusion,
			StartedAt: s.StartedAt, CompletedAt: s.CompletedAt})
	}
	return out
}

func runJob(repo string, runID int64, j github.Job, ours map[string]bool) model.RunJob {
	labels := j.Labels
	if labels == nil {
		labels = []string{}
	}
	return model.RunJob{
		ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion,
		StartedAt: j.StartedAt, CompletedAt: j.CompletedAt, RunnerName: j.RunnerName, Labels: labels,
		HTMLURL: j.HTMLURL, GHR: j.RunnerName != "" && ours[ghrJobKey(repo, runID, j.RunnerName)], Steps: modelSteps(j.Steps),
	}
}

// RunDetail returns one attempt of a run, 0 meaning the latest. Callers for
// the same key share one fetch, detached from any one request. The result is
// kept 15 s as the latest attempt; under its attempt number it is kept 15 s
// while the attempt runs and 60 s once it has completed.
func (b *Backend) RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error) {
	name, watched, err := b.coveredName(repo)
	if err != nil {
		return model.RunDetail{}, err
	}
	if err := b.githubErr(); err != nil {
		return model.RunDetail{}, err
	}
	c := &b.details
	key := detailKey{repo: strings.ToLower(name), id: id, attempt: attempt}
	c.mu.Lock()
	gen := b.tokenGen()
	if gen != c.gen {
		c.entries, c.gen = nil, gen
	}
	if e, ok := c.entries[key]; ok && b.now().Sub(e.at) < detailTTL(key, e.val) {
		c.mu.Unlock()
		return e.val, nil
	}
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.val, call.err
		case <-ctx.Done():
			return model.RunDetail{}, ctx.Err()
		}
	}
	call := &detailCall{done: make(chan struct{})}
	if c.calls == nil {
		c.calls = map[detailKey]*detailCall{}
	}
	c.calls[key] = call
	c.mu.Unlock()

	call.val, call.err = b.fetchDetailSafely(ctx, name, watched, id, attempt)
	call.err = detailErr(call.err, b.Store.Config().Owner, name)

	c.mu.Lock()
	delete(c.calls, key)
	if call.err == nil && b.tokenGen() == gen {
		now := b.now()
		for k, e := range c.entries {
			if now.Sub(e.at) >= detailTTL(k, e.val) {
				delete(c.entries, k)
			}
		}
		keys := []detailKey{key, {repo: key.repo, id: id, attempt: call.val.Attempt}}
		if call.val.Attempt == call.val.Attempts {
			keys = append(keys, detailKey{repo: key.repo, id: id})
		}
		for _, k := range keys {
			if _, ok := c.entries[k]; !ok && len(c.entries) >= detailEntries {
				c.evictOldest()
			}
			if c.entries == nil {
				c.entries = map[detailKey]detailEntry{}
			}
			c.entries[k] = detailEntry{at: now, val: call.val}
		}
	}
	c.mu.Unlock()
	close(call.done)
	return call.val, call.err
}

// fetchDetailSafely turns a panic in the fetch into an error: the call must
// still leave c.calls and close done, or every later request for the run
// would wait on it for good.
func (b *Backend) fetchDetailSafely(ctx context.Context, name string, watched bool, id int64, attempt int) (val model.RunDetail, err error) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detailDeadline)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("run %d detail: panic: %v", id, r)
			val, err = model.RunDetail{}, errors.New("could not read the run")
		}
	}()
	return b.fetchDetail(fctx, name, watched, id, attempt)
}

func (c *detailCache) evictOldest() {
	var oldest detailKey
	found := false
	for k, e := range c.entries {
		if !found || e.at.Before(c.entries[oldest].at) {
			oldest, found = k, true
		}
	}
	delete(c.entries, oldest)
}

func (b *Backend) fetchDetail(ctx context.Context, repo string, watched bool, id int64, attempt int) (model.RunDetail, error) {
	run, err := b.GH.GetRun(ctx, repo, id)
	if err != nil {
		return model.RunDetail{}, err
	}
	attempts := max(run.RunAttempt, 1)
	if attempt > attempts {
		return model.RunDetail{}, api.BadRequest(fmt.Sprintf("run %d has no attempt %d", id, attempt))
	}
	shown := run
	var jobs []github.Job
	if attempt == 0 || attempt == attempts {
		jobs, err = b.GH.ListJobs(ctx, repo, id)
	} else {
		shown, err = b.GH.GetRunAttempt(ctx, repo, id, attempt)
		if err == nil {
			jobs, err = b.GH.ListAttemptJobs(ctx, repo, id, attempt)
		}
	}
	if err != nil {
		return model.RunDetail{}, err
	}
	arts, err := b.GH.ListArtifacts(ctx, repo, id)
	if err != nil {
		return model.RunDetail{}, err
	}
	graph, note := b.runGraph(ctx, repo, shown.HeadSHA, shown.Path, jobs)

	ours, herr := b.ghrJobs()
	b.details.mu.Lock()
	if herr != nil && !b.details.histFailing {
		log.Printf("run details: cannot read history, so no job is marked as ghr's: %v", herr)
	}
	b.details.histFailing = herr != nil
	b.details.mu.Unlock()

	start := shown.CreatedAt
	if shown.RunStartedAt != nil && !shown.RunStartedAt.IsZero() {
		start = *shown.RunStartedAt
	}
	title := shown.DisplayTitle
	if title == "" {
		title = shown.Name
	}
	d := model.RunDetail{
		Repo: repo, Watched: watched, ID: id, RunNumber: shown.RunNumber, Attempt: attempts, Attempts: attempts,
		Workflow: shown.Name, Title: title, Branch: shown.HeadBranch, Event: shown.Event, Actor: shown.Actor.Login,
		Status: shown.Status, Conclusion: shown.Conclusion, HeadSHA: shown.HeadSHA, Path: shown.Path,
		PullRequests: make([]int, 0, len(shown.PullRequests)), StartedAt: start, UpdatedAt: shown.UpdatedAt,
		HTMLURL: shown.HTMLURL, Jobs: make([]model.RunJob, 0, len(jobs)), Graph: graph, GraphNote: note,
		Artifacts: make([]model.RunArtifact, 0, len(arts)), FetchedAt: b.now(),
	}
	if attempt != 0 {
		d.Attempt = attempt
	}
	for _, p := range shown.PullRequests {
		d.PullRequests = append(d.PullRequests, p.Number)
	}
	for _, j := range jobs {
		d.Jobs = append(d.Jobs, runJob(repo, id, j, ours))
	}
	for _, a := range arts {
		d.Artifacts = append(d.Artifacts, model.RunArtifact{ID: a.ID, Name: a.Name, SizeBytes: a.SizeInBytes, Expired: a.Expired, ExpiresAt: a.ExpiresAt})
	}
	return d, nil
}
