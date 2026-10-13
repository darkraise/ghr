package ghstorage

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

var t0 = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func art(id int64, run int64, age time.Duration, bytes int64) model.ArtifactItem {
	return model.ArtifactItem{ID: id, Name: "a", Bytes: bytes, CreatedAt: t0.Add(-age), RunID: run}
}

func ids(items []model.ArtifactItem) []int64 {
	out := []int64{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestCandidates(t *testing.T) {
	items := []model.ArtifactItem{
		art(1, 10, 30*time.Hour, 1),
		art(2, 20, 24*time.Hour, 1), // exactly the age: not older
		art(3, 30, 25*time.Hour, 1),
		art(4, 40, time.Hour, 1),
	}
	for _, tc := range []struct {
		name   string
		age    time.Duration
		active map[int64]bool
		want   []int64
	}{
		{"older than the age", 24 * time.Hour, nil, []int64{1, 3}},
		{"a run not completed is skipped", 24 * time.Hour, map[int64]bool{30: true}, []int64{1}},
		{"off selects nothing", 0, nil, []int64{}},
		{"a short override", 30 * time.Minute, nil, []int64{1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(Candidates(items, tc.age, t0, tc.active)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Candidates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOldestFirstWithCap(t *testing.T) {
	picks := []Pick{
		{Repo: "b", Item: art(5, 1, 2*time.Hour, 1)},
		{Repo: "a", Item: art(7, 1, 9*time.Hour, 1)},
		{Repo: "b", Item: art(6, 1, 9*time.Hour, 1)},
		{Repo: "a", Item: art(3, 1, 9*time.Hour, 1)},
		{Repo: "a", Item: art(9, 1, 5*time.Hour, 1)},
	}
	got := Oldest(picks, 4)
	var order []string
	for _, p := range got {
		order = append(order, fmt.Sprintf("%s:%d", p.Repo, p.Item.ID))
	}
	if want := []string{"a:3", "a:7", "b:6", "a:9"}; !reflect.DeepEqual(order, want) {
		t.Errorf("Oldest = %v, want %v", order, want)
	}
	if picks[0].Item.ID != 5 {
		t.Error("Oldest reordered its argument")
	}
}

func TestPreview(t *testing.T) {
	repos := []model.RepoStorage{
		{Name: "darkmem", Artifacts: []model.ArtifactItem{art(1, 1, 50*time.Hour, 100), art(2, 2, 3*time.Hour, 7)}},
		{Name: "homelab", Artifacts: []model.ArtifactItem{art(3, 3, 72*time.Hour, 500)}},
		{Name: "chicken-farm", Artifacts: []model.ArtifactItem{art(4, 4, 49*time.Hour, 300), art(5, 5, 60*time.Hour, 1)}},
		{Name: "empty"},
	}
	cfg := config.GitHubStorage{Repos: map[string]config.StorageOverride{"HomeLab": {Cleanup: "off"}}}
	for _, tc := range []struct {
		name string
		repo string
		age  time.Duration
		want []model.StoragePreview
	}{
		{"global skips repositories with an override", "", 48 * time.Hour,
			[]model.StoragePreview{{Repo: "chicken-farm", Count: 2, Bytes: 301}, {Repo: "darkmem", Count: 1, Bytes: 100}}},
		{"one repository with its proposed override", "homelab", 6 * time.Hour,
			[]model.StoragePreview{{Repo: "homelab", Count: 1, Bytes: 500}}},
		{"the repository is matched ignoring case", "DARKMEM", time.Hour,
			[]model.StoragePreview{{Repo: "darkmem", Count: 2, Bytes: 107}}},
		{"off previews nothing", "", 0, []model.StoragePreview{}},
		{"nothing old enough", "", 400 * time.Hour, []model.StoragePreview{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Preview(repos, cfg, tc.repo, tc.age, t0); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Preview = %+v, want %+v", got, tc.want)
			}
		})
	}
}
