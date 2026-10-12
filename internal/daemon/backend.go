package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/activity"
	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/changes"
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
	Pending() []model.HistoryEntry
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
	GetRun(ctx context.Context, repo string, id int64) (*github.Run, error)
	GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*github.Run, error)
	ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]github.Job, error)
	GetJob(ctx context.Context, repo string, jobID int64) (*github.Job, error)
	ListArtifacts(ctx context.Context, repo string, runID int64) ([]github.Artifact, error)
	GetContent(ctx context.Context, repo, path, ref string) ([]byte, error)
	JobLog(ctx context.Context, repo string, jobID int64, max int64) ([]byte, error)
	OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)
	ArtifactURL(ctx context.Context, repo string, id int64) (string, error)
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
	// CheckTokenOwner validates a candidate token when there is no runner
	// repository to read: the token must list at least one repository of the owner.
	CheckTokenOwner func(ctx context.Context, token string) error
	// Wake asks the run loop for an immediate tick after a config change or a
	// runner stop, so a pause stops idle runners at once, and a stopped
	// runner's replacement starts without waiting for the poll interval; nil
	// in tests.
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
	// SetupPending reports an unfinished first-run setup; nil reports false.
	SetupPending func() bool
	// Changes is the hub GET /changes subscribes to; nil until the daemon has started.
	Changes *changes.Hub
	flags   setupFlagCache
	checks  labelChecks

	stepsMu    sync.Mutex
	steps      map[string]stepsEntry
	stepsGen   uint64 // the token generation steps was filled under
	stepsCalls map[string]*stepsCall

	activityMu  sync.Mutex
	activity    map[string]*activityEntry
	activityGen atomic.Int64

	actions actionsCache
	wfFiles wfCache
	details detailCache
	logs    logCache
}

var _ api.Backend = (*Backend)(nil)

// Metrics returns the sampler's series and current host figures.
func (b *Backend) Metrics() model.Metrics {
	if b.Sampler == nil {
		return model.Metrics{Samples: []model.MetricSample{}}
	}
	return b.Sampler.Metrics()
}

// activityTTL is how long a built activity response is reused, so several
// open Dashboards polling every 5 s read the history file once between them.
const activityTTL = 5 * time.Second

type activityEntry struct {
	mu  sync.Mutex
	at  time.Time
	ok  bool
	gen int64 // activityGen when the build began
	val model.Activity
}

// Activity builds the activity view for window in loc, narrowed to one
// repository when repo is set, reusing one built less than activityTTL ago.
// Each window, zone and repository has its own lock, so a slow 30d build
// never holds up a 1h poll. Repository names match case-insensitively.
func (b *Backend) Activity(ctx context.Context, window, repo string, loc *time.Location) (model.Activity, error) {
	key := window + "|" + loc.String()
	if repo != "" {
		key += "|" + strings.ToLower(repo)
	}
	e := b.activityEntry(key)
	e.mu.Lock()
	defer e.mu.Unlock()
	gen := b.activityGen.Load()
	now := b.now()
	if e.ok && e.gen == gen && now.Sub(e.at) < activityTTL {
		return e.val, nil
	}
	cfg := b.Store.Config()
	// Instances, then pending records, then history: a job moves through
	// them in that order, and it is recorded in the next place before it
	// leaves the one before, so a job that moves during the build is read in
	// at least one of them.
	insts := b.M.Status().Instances
	pend := b.M.Pending()
	hist, err := b.Hist.Query("", "", time.Time{}, 0)
	if err != nil {
		return model.Activity{}, err
	}
	in := activity.Input{
		Window: window, Loc: loc, Now: now, Retention: cfg.HistoryRetention.D(), Repo: repo,
		History: hist, Pending: pend, Instances: insts,
	}
	if cfg.Mode == config.ModeQueue {
		c := cfg.GlobalMax
		in.Capacity = &c
	}
	for _, r := range cfg.Repos {
		in.Repos = append(in.Repos, r.Name)
	}
	if b.Sampler != nil {
		in.Minutes, in.Hours = b.Sampler.Minutes(), b.Sampler.Hours()
	}
	e.val, e.at, e.gen, e.ok = activity.Build(in), now, gen, true
	return e.val, nil
}

// staleActivity makes every cached activity view stale, one being built now
// included; the change hub calls it before telling browsers to refetch.
func (b *Backend) staleActivity([]string) { b.activityGen.Add(1) }

// activityEntry returns key's cache entry, creating it if needed. Creating
// one also drops expired entries nobody holds, so a client cycling through
// zones does not keep 30-day responses alive.
func (b *Backend) activityEntry(key string) *activityEntry {
	b.activityMu.Lock()
	defer b.activityMu.Unlock()
	if b.activity == nil {
		b.activity = map[string]*activityEntry{}
	}
	e, ok := b.activity[key]
	if !ok {
		now := b.now()
		for k, o := range b.activity {
			if o.mu.TryLock() {
				if !o.ok || now.Sub(o.at) >= activityTTL {
					delete(b.activity, k)
				}
				o.mu.Unlock()
			}
		}
		e = &activityEntry{}
		b.activity[key] = e
	}
	return e
}

func (b *Backend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// tokenGen is the GitHub client's token generation, which rises each time
// the token changes. A cache of data fetched with the token remembers the
// generation it was filled under and is emptied once it differs. A fetch
// reads the generation before it starts: the new token is stored before the
// generation rises, so a fetch may use a newer token than its generation
// (its result is then dropped) but never an older one. A cache reads it
// while holding its own lock; the client takes no lock of the daemon's, so
// the cache's lock always comes first.
func (b *Backend) tokenGen() uint64 { return b.GH.TokenMeta().Generation }

// errNotRemoving aborts a FinalizeRemovals update for a repo resumed meanwhile.
var errNotRemoving = errors.New("repo is no longer being removed")

func (b *Backend) Status() model.Status {
	st := b.M.Status()
	if b.WebSetupRequired != nil {
		st.WebSetupRequired = b.WebSetupRequired()
	}
	if b.SetupPending != nil {
		st.SetupPending = b.SetupPending()
	}
	if b.GH != nil {
		if l := b.GH.TokenMeta().RateLimit; l != nil {
			st.RateLimit = *l
		}
	}
	return st
}
func (b *Backend) EventsAfter(seq int64) []model.Event { return b.Events.After(seq) }
func (b *Backend) Config() any                         { return b.Store.Config() }

func (b *Backend) History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	return b.Hist.Query(repo, conclusion, since, limit)
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

// RunnerSteps returns the steps of the job a runner is running. Callers for
// one runner share a fetch, and its answer is kept stepsTTL unless the token
// changed while it ran.
func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	live := map[string]bool{}
	for _, in := range b.M.Status().Instances {
		live[in.ID] = true
	}
	now := b.now()
	b.stepsMu.Lock()
	gen := b.tokenGen()
	if gen != b.stepsGen {
		b.steps, b.stepsGen = nil, gen
	}
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

	c.steps, c.err = b.fetchStepsSafely(ctx, id)

	b.stepsMu.Lock()
	delete(b.stepsCalls, id)
	if c.err == nil && live[id] && b.tokenGen() == gen {
		if b.steps == nil {
			b.steps = map[string]stepsEntry{}
		}
		b.steps[id] = stepsEntry{at: b.now(), steps: c.steps}
	}
	b.stepsMu.Unlock()
	close(c.done)
	return c.steps, c.err
}

// fetchStepsSafely runs the fetch detached from ctx, because joined callers
// share it and one of them closing its tab must not cancel it for the
// others. A panic becomes an error: the call must still leave b.stepsCalls
// and close done, or every later request for the runner would wait on it for
// good.
func (b *Backend) fetchStepsSafely(ctx context.Context, id string) (steps []model.Step, err error) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("runner %s steps: panic: %v", id, r)
			steps, err = nil, errors.New("could not read the runner's steps")
		}
	}()
	return b.fetchSteps(fctx, id)
}

// fetchSteps answers an unknown runner and a runner without a job before it
// asks whether GitHub may be called, so both still answer while the token is
// rejected or calls are paused.
func (b *Backend) fetchSteps(ctx context.Context, id string) ([]model.Step, error) {
	repo, runID, runnerName, err := b.M.RunnerRepoAndRun(id)
	if err != nil {
		return nil, notFoundIfUnknown(err)
	}
	if runID == 0 {
		return nil, nil
	}
	if err := b.githubErr(); err != nil {
		return nil, err
	}
	jobs, err := b.GH.ListJobs(ctx, repo, runID)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.RunnerName != runnerName {
			continue
		}
		return modelSteps(j.Steps), nil
	}
	return nil, nil
}

func (b *Backend) KillRunner(ctx context.Context, id string) error {
	if err := b.M.Kill(ctx, id); err != nil {
		return notFoundIfUnknown(err)
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}

// update applies fn through the store (validate, save, swap) and wakes the loop.
// fn works on a copy, so a rejected change leaves no partial state anywhere.
// A warning is written as an event only when the change introduced it.
func (b *Backend) update(fn func(c *config.Config) error) error {
	var before []string
	warnings, err := b.Store.Update(func(c *config.Config) error {
		// Until fn runs, c is a copy of the config this update replaces.
		before, _ = c.Validate()
		return fn(c)
	})
	if err != nil {
		var ae *api.Error
		if errors.As(err, &ae) {
			return err
		}
		var se *SaveError
		if errors.As(err, &se) {
			log.Printf("save config: %v", se.Err)
			return &api.Error{Status: http.StatusInternalServerError, Msg: "could not save config.yaml"}
		}
		return api.BadRequest(err.Error())
	}
	for _, w := range warnings {
		if !slices.Contains(before, w) {
			b.Events.Add("warn", "", "config: %s", w)
		}
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
		// Sorted, so the unknown repository reported is always the same one.
		for _, name := range slices.Sorted(maps.Keys(p.Repos)) {
			rp := p.Repos[name]
			r := c.Repo(name)
			if r == nil {
				return api.NotFound("unknown repo " + name)
			}
			if rp.Max.Set {
				r.Max = rp.Max.Value
			}
			if rp.Warm.Set {
				r.Warm = rp.Warm.Value
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

// AddRepo adds a runner repository under the name GitHub reports for it,
// which is its new name when the typed one was redirected after a rename.
func (b *Backend) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	typed := strings.TrimSpace(req.Name)
	if typed == "" {
		return api.BadRequest("repo name is required")
	}
	cfg := b.Store.Config()
	// A fast path that saves the GitHub call. It knows only the typed name;
	// the check inside the update decides.
	if cfg.Repo(typed) != nil {
		return api.Conflict("repo " + typed + " is already configured")
	}
	repo, err := b.GH.GetRepo(ctx, typed)
	if github.IsKind(err, github.ErrNotFound) {
		return api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", cfg.Owner, typed))
	}
	if err != nil {
		return err
	}
	owner, name, _ := strings.Cut(repo.FullName, "/")
	// GitHub also redirects a repository that was transferred to another
	// owner, and every later call would go to the configured owner.
	if !strings.EqualFold(owner, cfg.Owner) {
		return api.BadRequest(fmt.Sprintf("%s now lives at %s; ghr serves only repositories of %s", typed, repo.FullName, cfg.Owner))
	}
	if !repo.Private && !req.AllowPublic {
		return api.Conflict(name + " is public; self-hosted runners must only serve private repos (pass --allow-public to override)")
	}
	if err := b.update(func(c *config.Config) error {
		if c.Repo(name) != nil {
			return api.Conflict("repo " + name + " is already configured")
		}
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) || strings.EqualFold(w, typed) })
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels, AllowPublic: req.AllowPublic})
		return nil
	}); err != nil {
		return err
	}
	b.forgetLabelCheck(name)
	b.Events.Add("info", name, "repo added")
	return nil
}

// WatchRepo adds name to watch_repos once the token can see it. Public
// repositories are allowed: no runner ever serves a watched one.
func (b *Backend) WatchRepo(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return api.BadRequest("repo name is required")
	}
	if strings.Contains(name, "/") {
		return api.BadRequest("repo name must be a repository name under owner, without a slash")
	}
	cfg := b.Store.Config()
	if cfg.Repo(name) != nil {
		return api.Conflict("repo " + name + " is already configured")
	}
	if cfg.Watched(name) {
		return api.Conflict("repo " + name + " is already watched")
	}
	_, err := b.GH.GetRepo(ctx, name)
	if github.IsKind(err, github.ErrNotFound) {
		return api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", cfg.Owner, name))
	}
	if err != nil {
		return err
	}
	if err := b.update(func(c *config.Config) error {
		if c.Repo(name) != nil || c.Watched(name) {
			return api.Conflict("repo " + name + " is already configured or watched")
		}
		c.WatchRepos = append(c.WatchRepos, name)
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "watching")
	return nil
}

// UnwatchRepo never calls GitHub, so a repository GitHub no longer shows can
// always be unwatched. The check runs inside the update so two concurrent
// removals cannot both succeed.
func (b *Backend) UnwatchRepo(name string) error {
	if err := b.update(func(c *config.Config) error {
		if !c.Watched(name) {
			return api.NotFound("repo " + name + " is not watched")
		}
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) })
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "stopped watching")
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

// tokenCheckWorkers bounds how many repositories a new token is checked
// against at once.
const tokenCheckWorkers = 4

// SetToken replaces the token after checking it against every runner
// repository that is not being removed. Paused repositories are included: a
// token rotated while everything is paused still needs the permissions the
// runners use. The first rule that applies decides: a 401 from any repository
// refuses the token; a rate limit from any refuses with 429 and its retry
// time; every repository failing refuses with the first one's failure;
// otherwise the token is saved and each failing repository gets a warning
// event. With no repository to check against, the token must pass the owner
// check. A refused token changes nothing.
func (b *Backend) SetToken(ctx context.Context, token string) error {
	var repos []string
	for _, r := range b.Store.Config().Repos {
		if !r.Removing {
			repos = append(repos, r.Name)
		}
	}
	var errs []error
	if len(repos) == 0 {
		if err := b.CheckTokenOwner(ctx, token); err != nil {
			return err
		}
	} else {
		errs = b.checkTokenOn(ctx, token, repos)
		if err := judgeToken(repos, errs); err != nil {
			return err
		}
	}
	if err := b.Store.SetToken(token); err != nil {
		return err
	}
	b.GH.ForgetCache()
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
	for i, err := range errs {
		if err != nil {
			b.Events.Add("warn", "", "new token: %s: %v", repos[i], err)
		}
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}

// checkTokenOn runs CheckToken for every repository, tokenCheckWorkers at a
// time, and returns the results in the order of repos.
func (b *Backend) checkTokenOn(ctx context.Context, token string, repos []string) []error {
	errs := make([]error, len(repos))
	sem := make(chan struct{}, tokenCheckWorkers)
	var wg sync.WaitGroup
	for i, repo := range repos {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			errs[i] = b.CheckToken(ctx, token, repo)
		}()
	}
	wg.Wait()
	return errs
}

// judgeToken returns the refusal for a new token given each repository's
// check result, or nil when the token is to be saved. errors.As is needed
// because checkToken wraps GitHub's error with the permission it names.
func judgeToken(repos []string, errs []error) error {
	var ae *github.APIError
	for _, err := range errs {
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			return api.BadRequest("new token rejected: " + ae.Error())
		}
	}
	failed := 0
	for _, err := range errs {
		if errors.As(err, &ae) && ae.Kind == github.ErrRateLimit {
			return &api.Error{Status: http.StatusTooManyRequests, Msg: ae.Error(), RetryAt: ae.RetryAt}
		}
		if err != nil {
			failed++
		}
	}
	if failed == len(repos) {
		return api.BadRequest("new token rejected: " + repos[0] + ": " + errs[0].Error())
	}
	return nil
}

// Reload re-reads config.yaml and the token file, as SIGHUP does. On any
// error the previous values stay active. It wakes the run loop, which ticks;
// it never ticks itself. What the GitHub client holds for the token (its
// response cache, the token's metadata, a rate-limit suspension) is dropped
// only when the token changed. The degraded state is cleared every time, so
// a reload remains the way to say "try the token again now".
func (b *Backend) Reload() ([]string, error) {
	warnings, tokenChanged, err := b.Store.Reload()
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
	if tokenChanged {
		b.GH.ForgetCache()
	}
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
func (b *Backend) Storage() model.Storage { return b.storageFrom(b.M.Status()) }

// storageFrom is Storage with the disk reading taken from st.
func (b *Backend) storageFrom(st model.Status) model.Storage {
	s := b.Space.Snapshot()
	s.LastPrune = b.M.LastPrune()
	s.Docker.DiskPct = st.DiskPct
	return s
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
func (b *Backend) Token() model.TokenStatus { return tokenStatus(b.GH.TokenMeta(), b.M.Status()) }

// tokenStatus is the token's state from GitHub's last answers and the
// manager's degraded flag in st.
func tokenStatus(meta github.TokenMeta, st model.Status) model.TokenStatus {
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
	if st.Degraded {
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
			out = append(out, model.AvailableRepo{Name: r.Name, Private: r.Private, Configured: cfg.Repo(r.Name) != nil, Watched: cfg.Watched(r.Name)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}
