package ghstorage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

const (
	// MaxCleanupDeletes is how many artifacts one cleanup pass deletes; the
	// rest wait for the next pass.
	MaxCleanupDeletes = 100
	// WriteBudget is how many writes ghr's storage work makes in any hour,
	// below GitHub's secondary limit of 500, so the runner manager keeps at
	// least 200 for its own.
	WriteBudget = 300
)

// BudgetError refuses a write once WriteBudget writes were made in the last
// hour.
type BudgetError struct {
	RetryAt time.Time
}

func (e *BudgetError) Error() string {
	return "GitHub write budget used up; try again after " + e.RetryAt.UTC().Format(time.RFC3339)
}

// write runs one GitHub write. Writes run one at a time, so a runner start
// waiting for the client's write slot waits behind at most one of them.
func (s *Service) write(fn func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := s.now()
	s.writes = slices.DeleteFunc(s.writes, func(t time.Time) bool { return now.Sub(t) >= time.Hour })
	if len(s.writes) >= WriteBudget {
		return &BudgetError{RetryAt: s.writes[0].Add(time.Hour)}
	}
	s.writes = append(s.writes, now)
	return fn()
}

// activeRuns lists the ids of repo's runs that have not completed.
func (s *Service) activeRuns(ctx context.Context, repo string) (map[int64]bool, error) {
	active := map[int64]bool{}
	for _, status := range ActiveStatuses {
		runs, err := s.GH.ListRuns(ctx, repo, status)
		if err != nil {
			return nil, err
		}
		for _, r := range runs {
			active[r.ID] = true
		}
	}
	return active, nil
}

// cleanup deletes the artifacts older than each repository's effective
// cleanup age, oldest first across repositories and at most
// MaxCleanupDeletes, skipping those of runs that have not completed.
func (s *Service) cleanup(cfg *config.Config, gen uint64) {
	ctx := s.ctx
	now := s.now()
	type due struct {
		repo  string
		age   time.Duration
		items []model.ArtifactItem
	}
	var todo []due
	s.mu.Lock()
	for _, r := range s.repos {
		age, _ := cfg.GitHubStorage.CleanupFor(r.Name)
		if len(Candidates(r.Artifacts, age, now, nil)) > 0 {
			todo = append(todo, due{r.Name, age, slices.Clone(r.Artifacts)})
		}
	}
	s.mu.Unlock()
	var picks []Pick
	for _, d := range todo {
		active, err := s.activeRuns(ctx, d.repo)
		if err != nil {
			s.setRowError(d.repo, s.describe(err, d.repo), gen)
			continue
		}
		for _, it := range Candidates(d.items, d.age, now, active) {
			picks = append(picks, Pick{Repo: d.repo, Item: it})
		}
	}
	type tally struct {
		n     int
		bytes int64
	}
	tallies := map[string]*tally{}
	var order []string
	stopped := map[string]bool{}
	for _, p := range Oldest(picks, MaxCleanupDeletes) {
		if stopped[p.Repo] {
			continue
		}
		if !s.keep(gen) {
			break
		}
		deleted, err := s.deleteArtifact(ctx, p.Repo, p.Item, "cleanup")
		var be *BudgetError
		if errors.As(err, &be) {
			log.Printf("github storage: cleanup stopped: %v", err)
			break
		}
		if err != nil {
			stopped[p.Repo] = true
			s.setRowError(p.Repo, s.describe(err, p.Repo), gen)
			continue
		}
		if deleted {
			t := tallies[p.Repo]
			if t == nil {
				t = &tally{}
				tallies[p.Repo] = t
				order = append(order, p.Repo)
			}
			t.n++
			t.bytes += p.Item.Bytes
		}
	}
	for _, repo := range order {
		t := tallies[repo]
		s.Events.Add("info", repo, "cleanup deleted %d artifacts (%s)", t.n, sizeText(t.bytes))
	}
	if len(order) > 0 {
		cur := s.Config()
		s.mu.Lock()
		if s.keep(gen) {
			s.recompute(cur)
			s.bump()
		}
		s.mu.Unlock()
	}
}

func (s *Service) setRowError(repo, msg string, gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.keep(gen) {
		return
	}
	for i := range s.repos {
		if strings.EqualFold(s.repos[i].Name, repo) {
			s.repos[i].Error = msg
		}
	}
	s.bump()
}

// deleteArtifact deletes one artifact within the write budget. An artifact
// already gone (404) leaves the snapshot too, but only a delete GitHub
// confirmed is recorded and logged.
func (s *Service) deleteArtifact(ctx context.Context, repo string, it model.ArtifactItem, by string) (bool, error) {
	var deleted bool
	err := s.write(func() error {
		var err error
		deleted, err = s.GH.DeleteArtifact(ctx, repo, it.ID)
		return err
	})
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeItem("artifact", repo, it.ID)
	if deleted {
		now := s.now()
		age := ageText(now.Sub(it.CreatedAt))
		s.recordDeletion(model.Deletion{At: now, Repo: repo, Kind: "artifact", Name: it.Name, Bytes: it.Bytes, Age: age, By: by})
		log.Printf("github storage: deleted artifact %s (%s) in %s, age %s", it.Name, sizeText(it.Bytes), repo, age)
	}
	s.bump()
	return deleted, nil
}

// recordDeletion keeps the MaxDeletions newest deletions, newest first. Call
// with s.mu held.
func (s *Service) recordDeletion(d model.Deletion) {
	s.deletions = append([]model.Deletion{d}, s.deletions...)
	if len(s.deletions) > MaxDeletions {
		s.deletions = s.deletions[:MaxDeletions]
	}
}

func sizeText(n int64) string {
	switch {
	case n >= model.GiB:
		return fmt.Sprintf("%.1f GB", float64(n)/model.GiB)
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func ageText(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", max(d, 0)/time.Minute)
}
