package daemon

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// labelScanRuns is how many recent runs a label check reads, besides the
// queued, in-progress and waiting ones.
const labelScanRuns = 20

// labelScanDeadline bounds one scan; what was gathered by then is kept as partial.
var labelScanDeadline = 60 * time.Second

type labelEntry struct {
	running bool
	started time.Time
	result  *model.LabelCheck
	cancel  context.CancelFunc // set while running
}

// labelChecks holds each repo's last label scan, keyed by the lower-cased
// repo name. Results belong to one token generation and are dropped when
// the token changes; classification against labels happens in the TUI, so a
// config change never makes a stored result wrong.
type labelChecks struct {
	mu      sync.Mutex
	gen     uint64
	closed  bool
	entries map[string]*labelEntry
}

// drop cancels the entry's scan and forgets it; c.mu must be held. A
// cancelled scan finds its entry gone and publishes nothing.
func (c *labelChecks) drop(key string) {
	if e := c.entries[key]; e != nil {
		if e.cancel != nil {
			e.cancel()
		}
		delete(c.entries, key)
	}
}

// refresh drops everything from an older token and the entries of repos no
// longer configured; c.mu must be held.
func (c *labelChecks) refresh(gen uint64, cfg *config.Config) {
	if c.entries == nil {
		c.entries = map[string]*labelEntry{}
	}
	if gen != c.gen {
		for key := range c.entries {
			c.drop(key)
		}
		c.gen = gen
	}
	for key := range c.entries {
		if cfg.Repo(key) == nil {
			c.drop(key)
		}
	}
}

// forgetLabelCheck drops a repo's scan, results and throttle; called when the
// repo is added or finally removed, so a re-added repo starts clean.
func (b *Backend) forgetLabelCheck(name string) {
	c := &b.checks
	c.mu.Lock()
	c.drop(strings.ToLower(name))
	c.mu.Unlock()
}

// dropLabelChecks applies a token change now, so a scan running under the
// old token stops spending API calls instead of running to its deadline.
func (b *Backend) dropLabelChecks() {
	c := &b.checks
	c.mu.Lock()
	c.refresh(b.GH.TokenMeta().Generation, b.Store.Config())
	c.mu.Unlock()
}

// Close cancels every running label scan; the daemon calls it on shutdown.
func (b *Backend) Close() {
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for key := range c.entries {
		c.drop(key)
	}
}

// StartLabelCheck starts a background scan of the repo's recent jobs. A
// scan already running is joined; a new one runs at most once a minute.
func (b *Backend) StartLabelCheck(repo string) error {
	name, err := b.repoName(repo)
	if err != nil {
		return err
	}
	if err := b.githubErr(); err != nil {
		return err
	}
	key := strings.ToLower(name)
	now := b.now()
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	gen := b.GH.TokenMeta().Generation
	if c.closed {
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: "the daemon is shutting down"}
	}
	c.refresh(gen, b.Store.Config())
	e := c.entries[key]
	if e == nil {
		e = &labelEntry{}
		c.entries[key] = e
	}
	if e.running {
		return nil
	}
	if !e.started.IsZero() && now.Sub(e.started) < time.Minute {
		return &api.Error{Status: http.StatusTooManyRequests, Msg: "this repo was checked less than a minute ago",
			RetryAt: e.started.Add(time.Minute)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), labelScanDeadline)
	e.running, e.started, e.cancel = true, now, cancel
	go b.scanLabels(ctx, name, key, e)
	return nil
}

// scanLabels runs one scan and publishes it into e, unless e was dropped or
// replaced meanwhile (token change, repo removed or re-added, shutdown).
func (b *Backend) scanLabels(ctx context.Context, name, key string, e *labelEntry) {
	groups, err := gatherLabels(ctx, b.GH, name)
	at := b.now()
	res := &model.LabelCheck{State: "done", CheckedAt: &at, Groups: groups}
	if err != nil {
		res.Partial = true
		if !errors.Is(err, context.DeadlineExceeded) {
			res.Error = err.Error()
		}
	}
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if c.entries[key] != e {
		return
	}
	e.running, e.result = false, res
}

// LabelCheck returns the repo's last scan without starting one.
func (b *Backend) LabelCheck(repo string) (model.LabelCheck, error) {
	name, err := b.repoName(repo)
	if err != nil {
		return model.LabelCheck{}, err
	}
	if err := b.degradedErr(); err != nil {
		return model.LabelCheck{}, err
	}
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	gen := b.GH.TokenMeta().Generation
	c.refresh(gen, b.Store.Config())
	res := model.LabelCheck{State: "not_checked", Groups: []model.LabelGroup{}}
	e := c.entries[strings.ToLower(name)]
	if e == nil {
		return res, nil
	}
	if e.result != nil {
		res = *e.result
	}
	if e.running {
		res.State = "checking"
	}
	return res, nil
}

// gatherLabels reads the jobs of the repo's recent, queued, in-progress and
// waiting runs and groups them by the labels they request.
func gatherLabels(ctx context.Context, gh GitHub, repo string) ([]model.LabelGroup, error) {
	seen := map[int64]bool{}
	var runs []github.Run
	add := func(rs []github.Run) {
		for _, r := range rs {
			if !seen[r.ID] {
				seen[r.ID] = true
				runs = append(runs, r)
			}
		}
	}
	recent, err := gh.ListRecentRuns(ctx, repo, labelScanRuns)
	if err != nil {
		return []model.LabelGroup{}, err
	}
	add(recent)
	for _, status := range []string{"queued", "in_progress", "waiting"} {
		rs, err := gh.ListRuns(ctx, repo, status)
		if err != nil {
			return []model.LabelGroup{}, err
		}
		add(rs)
	}
	g := &labelGrouper{byKey: map[string]*model.LabelGroup{}, names: map[string]map[string]bool{}}
	for _, run := range runs {
		// A failed pagination still returns the jobs of the pages before it.
		jobs, err := gh.ListJobs(ctx, repo, run.ID)
		for _, j := range jobs {
			g.add(j)
		}
		if err != nil {
			return g.groups(), err
		}
	}
	return g.groups(), nil
}

type labelGrouper struct {
	byKey map[string]*model.LabelGroup
	names map[string]map[string]bool
	order []string
}

func (g *labelGrouper) add(j github.Job) {
	labels := make([]string, 0, len(j.Labels))
	for _, l := range j.Labels {
		labels = append(labels, strings.ToLower(strings.TrimSpace(l)))
	}
	sort.Strings(labels)
	key := strings.Join(labels, "\x00")
	grp := g.byKey[key]
	if grp == nil {
		grp = &model.LabelGroup{Labels: labels, Jobs: []string{}}
		g.byKey[key], g.names[key] = grp, map[string]bool{}
		g.order = append(g.order, key)
	}
	grp.Count++
	if j.CreatedAt.After(grp.LastSeen) {
		grp.LastSeen = j.CreatedAt
	}
	name := j.Name
	if j.WorkflowName != "" {
		name = j.WorkflowName + " / " + j.Name
	}
	if !g.names[key][name] {
		g.names[key][name] = true
		if len(grp.Jobs) < 3 {
			grp.Jobs = append(grp.Jobs, name)
		} else {
			grp.More++
		}
	}
}

// groups returns the groups, most recently seen first.
func (g *labelGrouper) groups() []model.LabelGroup {
	out := []model.LabelGroup{}
	for _, k := range g.order {
		out = append(out, *g.byKey[k])
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].LastSeen.After(out[b].LastSeen) })
	return out
}
