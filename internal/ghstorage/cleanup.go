package ghstorage

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// ActiveStatuses are the run statuses of a run that has not completed. A
// deploy job waiting for an environment approval is "waiting".
var ActiveStatuses = []string{"queued", "in_progress", "waiting", "requested", "pending"}

// Pick is one artifact chosen for deletion.
type Pick struct {
	Repo string
	Item model.ArtifactItem
}

// Candidates are the items older than age, measured against now, whose run
// is not in active. An age of 0 is off and selects nothing.
func Candidates(items []model.ArtifactItem, age time.Duration, now time.Time, active map[int64]bool) []model.ArtifactItem {
	out := []model.ArtifactItem{}
	if age <= 0 {
		return out
	}
	for _, it := range items {
		if now.Sub(it.CreatedAt) > age && !active[it.RunID] {
			out = append(out, it)
		}
	}
	return out
}

// Oldest returns at most n picks, oldest first; ties go by repository, then
// id, so a capped pass always takes the same ones.
func Oldest(picks []Pick, n int) []Pick {
	out := slices.Clone(picks)
	slices.SortStableFunc(out, func(a, b Pick) int {
		if c := a.Item.CreatedAt.Compare(b.Item.CreatedAt); c != 0 {
			return c
		}
		if c := strings.Compare(a.Repo, b.Repo); c != 0 {
			return c
		}
		return cmp.Compare(a.Item.ID, b.Item.ID)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Preview counts what a cleanup age would delete now. With repo, it is that
// repository's proposed override; without, the proposed global age for every
// repository that has no override. It counts artifacts of runs still in
// progress too, which the cleanup itself skips.
func Preview(repos []model.RepoStorage, cfg config.GitHubStorage, repo string, age time.Duration, now time.Time) []model.StoragePreview {
	out := []model.StoragePreview{}
	for _, r := range repos {
		if repo != "" {
			if !strings.EqualFold(r.Name, repo) {
				continue
			}
		} else if _, ok := cfg.Override(r.Name); ok {
			continue
		}
		p := model.StoragePreview{Repo: r.Name}
		for _, it := range Candidates(r.Artifacts, age, now, nil) {
			p.Count++
			p.Bytes += it.Bytes
		}
		if p.Count > 0 {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b model.StoragePreview) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return strings.Compare(a.Repo, b.Repo)
	})
	return out
}
