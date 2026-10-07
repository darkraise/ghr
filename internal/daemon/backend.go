package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/metrics"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/storage"
	"github.com/darkraise/ghr/internal/toolchain"
)

// Manager is the part of *runner.Manager the backend uses.
type Manager interface {
	Status() model.Status
	RunnerLog(id, cursor string) (model.LogChunk, error)
	RunnerContainers(ctx context.Context, id string) ([]model.Container, error)
	RunnerRepoAndRun(id string) (repo string, runID int64, runnerName string, err error)
	Kill(ctx context.Context, id string) error
	ClearDegraded()
	PausedUntil() time.Time
	StartPruneScope(scope string) error
	LastPrune() *model.LastPrune
	QueueUpdate(ctx context.Context) error
	CancelUpdate() error
}

// GitHub is the part of the GitHub client the backend uses.
type GitHub interface {
	GetRepo(ctx context.Context, repo string) (*github.Repository, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	ForgetCache()
	TokenMeta() github.TokenMeta
	ListRunners(ctx context.Context, repo string) ([]github.Runner, error)
	GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error)
	DeleteRunner(ctx context.Context, repo string, id int64) error
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error)
	ListUserRepos(ctx context.Context) ([]github.UserRepo, error)
}

// StorageService is the part of *storage.Service the backend uses.
type StorageService interface {
	Snapshot() model.Storage
	Refresh() error
	Available(ctx context.Context, tool string) ([]model.ToolchainChoice, error)
	Install(tool, spec string) error
	InstallPreset(name string) error
	Remove(tool, version string) error
	Clear(name string) error
}

type Backend struct {
	Store  *Store
	M      Manager
	GH     GitHub
	Events *events.Ring
	Hist   *history.Store
	// Space is the storage service: the snapshot, toolchains and caches.
	Space StorageService
	// CheckToken validates a candidate token by reading repo, its runners and its runs with it.
	CheckToken func(ctx context.Context, token, repo string) error
	// Wake asks the run loop for an immediate tick after a config change, so a
	// pause stops idle runners at once; nil in tests.
	Wake func()
	// Now is the clock for label-check throttling; nil means time.Now.
	Now func() time.Time
	// Sampler supplies the metrics series; nil serves an empty one.
	Sampler *metrics.Sampler
	// WebApplied is the web block the running listener started with. Only a
	// restart applies a change, so a reload that changes it warns.
	WebApplied config.Web
	// WebSetupRequired reports whether the web listener runs with no password
	// set; nil, as without a listener, reports false.
	WebSetupRequired func() bool
	checks           labelChecks

	stepsMu    sync.Mutex
	steps      map[string]stepsEntry
	stepsCalls map[string]*stepsCall
}

var _ api.Backend = (*Backend)(nil)

// Metrics returns the sampler's series and current host figures.
func (b *Backend) Metrics() model.Metrics {
	if b.Sampler == nil {
		return model.Metrics{Samples: []model.MetricSample{}}
	}
	return b.Sampler.Metrics()
}

func (b *Backend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// errNotRemoving aborts a FinalizeRemovals update for a repo resumed meanwhile.
var errNotRemoving = errors.New("repo is no longer being removed")

func (b *Backend) Status() model.Status {
	st := b.M.Status()
	if b.WebSetupRequired != nil {
		st.WebSetupRequired = b.WebSetupRequired()
	}
	return st
}
func (b *Backend) EventsAfter(seq int64) []model.Event { return b.Events.After(seq) }
func (b *Backend) Config() any                         { return b.Store.Config() }

func (b *Backend) History(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	return b.Hist.Query(repo, conclusion, limit)
}

func notFoundIfUnknown(err error) error {
	var u runner.ErrUnknownRunner
	if errors.As(err, &u) {
		return api.NotFound(err.Error())
	}
	return err
}

func (b *Backend) RunnerLog(id, cursor string) (model.LogChunk, error) {
	c, err := b.M.RunnerLog(id, cursor)
	return c, notFoundIfUnknown(err)
}

func (b *Backend) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	cs, err := b.M.RunnerContainers(ctx, id)
	return cs, notFoundIfUnknown(err)
}

// stepsTTL bounds how often one runner's steps are fetched from GitHub,
// however many browser tabs poll its detail view.
const stepsTTL = 5 * time.Second

type stepsEntry struct {
	at    time.Time
	steps []model.Step
}

// stepsCall is one GitHub fetch that concurrent callers for a runner share.
type stepsCall struct {
	done  chan struct{}
	steps []model.Step
	err   error
}

func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	live := map[string]bool{}
	for _, in := range b.M.Status().Instances {
		live[in.ID] = true
	}
	now := b.now()
	b.stepsMu.Lock()
	for k, e := range b.steps {
		if !live[k] || now.Sub(e.at) >= stepsTTL {
			delete(b.steps, k)
		}
	}
	if e, ok := b.steps[id]; ok {
		b.stepsMu.Unlock()
		return e.steps, nil
	}
	if c, ok := b.stepsCalls[id]; ok {
		b.stepsMu.Unlock()
		select {
		case <-c.done:
			return c.steps, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &stepsCall{done: make(chan struct{})}
	if b.stepsCalls == nil {
		b.stepsCalls = map[string]*stepsCall{}
	}
	b.stepsCalls[id] = c
	b.stepsMu.Unlock()

	// Joined callers share this fetch, so one caller closing its tab must
	// not cancel it for the others.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	c.steps, c.err = b.fetchSteps(fctx, id)
	cancel()

	b.stepsMu.Lock()
	delete(b.stepsCalls, id)
	if c.err == nil && live[id] {
		if b.steps == nil {
			b.steps = map[string]stepsEntry{}
		}
		b.steps[id] = stepsEntry{at: b.now(), steps: c.steps}
	}
	b.stepsMu.Unlock()
	close(c.done)
	return c.steps, c.err
}

func (b *Backend) fetchSteps(ctx context.Context, id string) ([]model.Step, error) {
	repo, runID, runnerName, err := b.M.RunnerRepoAndRun(id)
	if err != nil {
		return nil, notFoundIfUnknown(err)
	}
	if runID == 0 {
		return nil, nil
	}
	jobs, err := b.GH.ListJobs(ctx, repo, runID)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.RunnerName != runnerName {
			continue
		}
		steps := make([]model.Step, 0, len(j.Steps))
		for _, s := range j.Steps {
			steps = append(steps, model.Step{Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: s.Conclusion})
		}
		return steps, nil
	}
	return nil, nil
}

func (b *Backend) KillRunner(ctx context.Context, id string) error {
	return notFoundIfUnknown(b.M.Kill(ctx, id))
}

// update applies fn through the store (validate, save, swap) and wakes the loop.
// fn works on a copy, so a rejected change leaves no partial state anywhere.
func (b *Backend) update(fn func(c *config.Config) error) error {
	warnings, err := b.Store.Update(fn)
	if err != nil {
		var ae *api.Error
		if errors.As(err, &ae) {
			return err
		}
		return api.BadRequest(err.Error())
	}
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}

// PatchConfig applies a partial update. Resuming a repo that is being removed
// cancels the removal in the same write.
func (b *Backend) PatchConfig(p model.ConfigPatch) error {
	var cancelled []string
	err := b.update(func(c *config.Config) error {
		if p.Mode != nil {
			c.Mode = *p.Mode
		}
		if p.GlobalMax != nil {
			c.GlobalMax = *p.GlobalMax
		}
		// A slice, not a map, so the first bad duration reported is deterministic.
		for _, f := range []struct {
			name  string
			field *config.Duration
			v     *string
		}{
			{"poll_interval", &c.PollInterval, p.PollInterval},
			{"start_timeout", &c.StartTimeout, p.StartTimeout},
			{"idle_timeout", &c.IdleTimeout, p.IdleTimeout},
			{"history_retention", &c.HistoryRetention, p.HistoryRetention},
		} {
			if f.v == nil {
				continue
			}
			d, err := config.ParseDuration(*f.v)
			if err != nil {
				return api.BadRequest(f.name + ": " + err.Error())
			}
			*f.field = d
		}
		if p.DiskHighWater != nil {
			c.DiskHighWater = *p.DiskHighWater
		}
		if p.BuildCacheKeep != nil {
			c.BuildCacheKeep = *p.BuildCacheKeep
		}
		if p.Labels != nil {
			c.Labels = *p.Labels
		}
		if rl := p.RunnerLimits; rl != nil {
			if rl.MemoryMax != nil {
				c.RunnerLimits.MemoryMax = *rl.MemoryMax
			}
			if rl.CPUQuota != nil {
				c.RunnerLimits.CPUQuota = *rl.CPUQuota
			}
		}
		for name, rp := range p.Repos {
			r := c.Repo(name)
			if r == nil {
				return api.NotFound("unknown repo " + name)
			}
			if rp.Max != nil {
				r.Max = rp.Max
			}
			if rp.Warm != nil {
				r.Warm = rp.Warm
			}
			if rp.Labels != nil {
				r.Labels = *rp.Labels
			}
			if rp.CleanupNamePrefixes != nil {
				r.CleanupNamePrefixes = *rp.CleanupNamePrefixes
			}
			if rp.Paused != nil {
				r.Paused = *rp.Paused
				if !r.Paused && r.Removing {
					r.Removing = false
					cancelled = append(cancelled, r.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, name := range cancelled {
		b.Events.Add("info", name, "repo resumed; removal cancelled")
	}
	return nil
}

func (b *Backend) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return api.BadRequest("repo name is required")
	}
	if b.Store.Config().Repo(name) != nil {
		return api.Conflict("repo " + name + " is already configured")
	}
	repo, err := b.GH.GetRepo(ctx, name)
	if github.IsKind(err, github.ErrNotFound) {
		return api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", b.Store.Config().Owner, name))
	}
	if err != nil {
		return err
	}
	if !repo.Private && !req.AllowPublic {
		return api.Conflict(name + " is public; self-hosted runners must only serve private repos (pass --allow-public to override)")
	}
	if err := b.update(func(c *config.Config) error {
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels})
		return nil
	}); err != nil {
		return err
	}
	b.forgetLabelCheck(name)
	b.Events.Add("info", name, "repo added")
	return nil
}

// RemoveRepo pauses the repo and records the removal in config.yaml (paused +
// removing in one write), so it survives a restart; FinalizeRemovals deletes the
// repo once its runners are gone. A per-repo resume cancels it.
func (b *Backend) RemoveRepo(name string) error {
	if b.Store.Config().Repo(name) == nil {
		return api.NotFound("unknown repo " + name)
	}
	if err := b.update(func(c *config.Config) error {
		r := c.Repo(name)
		if r == nil {
			return api.NotFound("unknown repo " + name)
		}
		r.Paused = true
		r.Removing = true
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "repo removal requested; waiting for its runners to finish")
	return nil
}

// FinalizeRemovals deletes repos marked removing that have no instances left.
// The removing flag is re-checked inside the store's update, so a repo resumed
// after the instance check is kept.
func (b *Backend) FinalizeRemovals() {
	var pending []string
	for _, r := range b.Store.Config().Repos {
		if r.Removing {
			pending = append(pending, r.Name)
		}
	}
	if len(pending) == 0 {
		return
	}
	live := map[string]bool{}
	for _, i := range b.M.Status().Instances {
		live[strings.ToLower(i.Repo)] = true
	}
	for _, name := range pending {
		if live[strings.ToLower(name)] {
			continue
		}
		_, err := b.Store.Update(func(c *config.Config) error {
			r := c.Repo(name)
			if r == nil || !r.Removing {
				return errNotRemoving
			}
			out := c.Repos[:0]
			for _, x := range c.Repos {
				if !strings.EqualFold(x.Name, name) {
					out = append(out, x)
				}
			}
			c.Repos = out
			return nil
		})
		switch {
		case errors.Is(err, errNotRemoving):
		case err != nil:
			b.Events.Add("warn", name, "remove repo: %v", err)
		default:
			b.forgetLabelCheck(name)
			b.Events.Add("info", name, "repo removed")
		}
	}
}

// SetPausedAll pauses or resumes every repo. Resume-all leaves repos that are
// being removed paused; only a per-repo resume cancels a removal.
func (b *Backend) SetPausedAll(paused bool) error {
	if err := b.update(func(c *config.Config) error {
		for i := range c.Repos {
			if !c.Repos[i].Removing {
				c.Repos[i].Paused = paused
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if paused {
		b.Events.Add("warn", "", "all repos paused (drain)")
	} else {
		b.Events.Add("info", "", "all repos resumed")
	}
	return nil
}

func (b *Backend) SetToken(ctx context.Context, token string) error {
	cfg := b.Store.Config()
	if len(cfg.Repos) > 0 {
		if err := b.CheckToken(ctx, token, cfg.Repos[0].Name); err != nil {
			return api.BadRequest("new token rejected: " + err.Error())
		}
	}
	if err := b.Store.SetToken(token); err != nil {
		return err
	}
	b.GH.ForgetCache()
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
	return nil
}

// Reload re-reads config.yaml and the token file, as SIGHUP does. On any
// error the previous values stay active. It wakes the run loop, which ticks;
// it never ticks itself.
func (b *Backend) Reload() ([]string, error) {
	warnings, err := b.Store.Reload()
	if err != nil {
		b.Events.Add("error", "", "reload rejected, keeping previous config: %v", err)
		return nil, api.BadRequest(err.Error())
	}
	if w := b.Store.Config().Web; w.Listen != b.WebApplied.Listen || !slices.Equal(w.Hosts, b.WebApplied.Hosts) {
		warnings = append(warnings, "web settings changed; restart ghr to apply")
	}
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
	b.GH.ForgetCache()
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("info", "", "config and token reloaded")
	if b.Wake != nil {
		b.Wake()
	}
	if warnings == nil {
		warnings = []string{}
	}
	return warnings, nil
}

// Prune starts a standard manual prune; its progress goes to the events.
func (b *Backend) Prune() error { return b.PruneScope(runner.ScopeStandard) }

// PruneScope starts a manual prune of one scope.
func (b *Backend) PruneScope(scope string) error {
	err := b.M.StartPruneScope(scope)
	var busy model.BusyError
	switch {
	case errors.Is(err, runner.ErrUnknownScope):
		return api.BadRequest(err.Error())
	case errors.As(err, &busy), errors.Is(err, runner.ErrPruneRunning), errors.Is(err, runner.ErrUpdateRunning):
		return api.Conflict(err.Error())
	case errors.Is(err, runner.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}

// Storage is the storage snapshot with the last prune and the disk reading,
// which the runner manager keeps.
func (b *Backend) Storage() model.Storage {
	st := b.Space.Snapshot()
	st.LastPrune = b.M.LastPrune()
	st.Docker.DiskPct = b.M.Status().DiskPct
	return st
}

func (b *Backend) RefreshStorage() error { return storageErr(b.Space.Refresh()) }

// AvailableToolchains lists what an install of tool can ask for; an
// upstream failure is a 502 carrying its message.
func (b *Backend) AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error) {
	cs, err := b.Space.Available(ctx, tool)
	switch {
	case errors.Is(err, toolchain.ErrUnknownTool):
		return nil, api.BadRequest(err.Error())
	case err != nil:
		return nil, &api.Error{Status: http.StatusBadGateway, Msg: err.Error()}
	}
	return cs, nil
}

// InstallToolchain queues one install or a preset's installs.
func (b *Backend) InstallToolchain(req model.InstallRequest) error {
	switch {
	case req.Preset != "" && req.Tool != "":
		return api.BadRequest("give a preset or a tool, not both")
	case req.Preset != "":
		return storageErr(b.Space.InstallPreset(req.Preset))
	case req.Tool == "":
		return api.BadRequest("a tool or a preset is required")
	}
	return storageErr(b.Space.Install(req.Tool, req.Version))
}

func (b *Backend) RemoveToolchain(tool, version string) error {
	return storageErr(b.Space.Remove(tool, version))
}

func (b *Backend) ClearCache(name string) error { return storageErr(b.Space.Clear(name)) }

// storageErr gives a storage or toolchain error the status the API answers with.
func storageErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, toolchain.ErrUnknownTool), errors.Is(err, storage.ErrUnknownPreset), errors.Is(err, storage.ErrMissingVersion):
		return api.BadRequest(err.Error())
	case errors.Is(err, toolchain.ErrNotInstalled), errors.Is(err, storage.ErrUnknownCache):
		return api.NotFound(err.Error())
	case errors.Is(err, storage.ErrNotPresent), errors.Is(err, storage.ErrMeasuring):
		return api.Conflict(err.Error())
	case errors.Is(err, storage.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}

// Token reports what GitHub's responses said about the token; never the token.
func (b *Backend) Token() model.TokenStatus {
	meta := b.GH.TokenMeta()
	ts := model.TokenStatus{State: "unverified", RateRemaining: meta.RateRemaining, RateLimit: meta.RateLimit,
		RateReset: meta.RateReset, ExpiresAt: meta.ExpiresAt}
	if !meta.CheckedAt.IsZero() {
		at := meta.CheckedAt
		ts.CheckedAt = &at
		ts.State = "ok"
		if !meta.OK {
			ts.State = "rejected"
		}
	}
	if st := b.M.Status(); st.Degraded {
		ts.State, ts.Reason = "rejected", st.DegradedReason
	}
	return ts
}

var hexID = regexp.MustCompile(`^[0-9a-f]{6}$`)

// ghrOwned reports whether a registration name is in ghr's namespace,
// ghr-<repo>-<6 hex>. ghr registers a runner before it records the instance,
// so only the name can protect a runner that is still starting.
func ghrOwned(repo, name string) bool {
	prefix := "ghr-" + strings.ToLower(repo) + "-"
	n := strings.ToLower(name)
	return strings.HasPrefix(n, prefix) && hexID.MatchString(n[len(prefix):])
}

// repoName resolves a repo name case-insensitively to its configured spelling.
func (b *Backend) repoName(name string) (string, error) {
	r := b.Store.Config().Repo(name)
	if r == nil {
		return "", api.NotFound("unknown repo " + name)
	}
	return r.Name, nil
}

// degradedErr refuses calls to GitHub while it rejects the token.
func (b *Backend) degradedErr() error {
	if st := b.M.Status(); st.Degraded {
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is rejecting the token: " + st.DegradedReason}
	}
	return nil
}

// githubErr refuses a request that would call GitHub while the token is
// rejected or a rate limit pauses the API, so the call is not spent.
func (b *Backend) githubErr() error {
	if err := b.degradedErr(); err != nil {
		return err
	}
	if until := b.M.PausedUntil(); b.now().Before(until) {
		return &api.Error{Status: http.StatusTooManyRequests, Msg: "GitHub rate limit; API calls are paused", RetryAt: until}
	}
	return nil
}

// Registrations lists the runners GitHub has registered for the repo.
func (b *Backend) Registrations(ctx context.Context, repo string) ([]model.Registration, error) {
	name, err := b.repoName(repo)
	if err != nil {
		return nil, err
	}
	if err := b.githubErr(); err != nil {
		return nil, err
	}
	rs, err := b.GH.ListRunners(ctx, name)
	if err != nil {
		return nil, err
	}
	out := []model.Registration{}
	for _, r := range rs {
		labels := []string{}
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, model.Registration{ID: r.ID, Name: r.Name, Status: r.Status, Busy: r.Busy,
			Labels: labels, GHR: ghrOwned(name, r.Name)})
	}
	return out, nil
}

// DeleteRegistration deletes an offline registration outside ghr's
// namespace. The runner is re-read first; GitHub's delete has no "only if
// offline" condition, so the check is the last observed state.
func (b *Backend) DeleteRegistration(ctx context.Context, repo string, id int64) error {
	name, err := b.repoName(repo)
	if err != nil {
		return err
	}
	if err := b.githubErr(); err != nil {
		return err
	}
	r, err := b.GH.GetRunner(ctx, name, id)
	if github.IsKind(err, github.ErrNotFound) {
		return nil
	}
	if err != nil {
		return &api.Error{Status: http.StatusBadGateway, Msg: "could not check the runner before deleting it: " + err.Error()}
	}
	switch {
	case ghrOwned(name, r.Name):
		return api.Conflict(r.Name + " belongs to ghr, which removes its own registrations")
	case r.Busy:
		return api.Conflict(r.Name + " is running a job")
	case r.Status != "offline":
		return api.Conflict(r.Name + " is " + r.Status + "; only offline runners can be deleted")
	}
	if err := b.GH.DeleteRunner(ctx, name, id); err != nil {
		return err
	}
	b.Events.Add("info", name, "deleted runner registration %s", r.Name)
	return nil
}

// QueueRunnerUpdate checks GitHub for a newer runner and queues its install;
// the loop is woken so a free ghr starts it at once.
func (b *Backend) QueueRunnerUpdate(ctx context.Context) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	err := b.M.QueueUpdate(ctx)
	var current runner.UpToDateError
	switch {
	case err == nil:
		if b.Wake != nil {
			b.Wake()
		}
		return nil
	case errors.As(err, &current), errors.Is(err, runner.ErrUpdateRunning), errors.Is(err, runner.ErrNoDist):
		return api.Conflict(err.Error())
	case errors.Is(err, runner.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}

// CancelRunnerUpdate drops a queued runner update.
func (b *Backend) CancelRunnerUpdate() error {
	err := b.M.CancelUpdate()
	if errors.Is(err, runner.ErrUpdateRunning) {
		return api.Conflict(err.Error())
	}
	return err
}

// AvailableRepos lists the configured owner's repositories the token can
// access, by name ignoring case, marking those already configured. The token
// may also reach other owners' repositories; ghr cannot manage those.
func (b *Backend) AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error) {
	if err := b.githubErr(); err != nil {
		return nil, err
	}
	rs, err := b.GH.ListUserRepos(ctx)
	if err != nil {
		return nil, err
	}
	cfg := b.Store.Config()
	out := []model.AvailableRepo{}
	for _, r := range rs {
		if strings.EqualFold(r.Owner.Login, cfg.Owner) {
			out = append(out, model.AvailableRepo{Name: r.Name, Private: r.Private, Configured: cfg.Repo(r.Name) != nil})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}
