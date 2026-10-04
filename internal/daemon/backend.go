package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
)

// Manager is the part of *runner.Manager the backend uses.
type Manager interface {
	Status() model.Status
	RunnerLog(id, cursor string) (model.LogChunk, error)
	RunnerContainers(ctx context.Context, id string) ([]model.Container, error)
	RunnerRepoAndRun(id string) (repo string, runID int64, runnerName string, err error)
	Kill(ctx context.Context, id string) error
	ClearDegraded()
}

// GitHub is the part of the GitHub client the backend uses.
type GitHub interface {
	GetRepo(ctx context.Context, repo string) (*github.Repository, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	ForgetCache()
}

type Backend struct {
	Store  *Store
	M      Manager
	GH     GitHub
	Events *events.Ring
	Hist   *history.Store
	// CheckToken validates a candidate token by reading repo, its runners and its runs with it.
	CheckToken func(ctx context.Context, token, repo string) error
	// Wake asks the run loop for an immediate tick after a config change, so a
	// pause stops idle runners at once; nil in tests.
	Wake func()
}

var _ api.Backend = (*Backend)(nil)

// errNotRemoving aborts a FinalizeRemovals update for a repo resumed meanwhile.
var errNotRemoving = errors.New("repo is no longer being removed")

func (b *Backend) Status() model.Status                { return b.M.Status() }
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

func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
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
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
	return nil
}
