package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func TestAppendQueryPrune(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if got, err := s.Query("", "", time.Time{}, 0); err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v %v", got, err)
	}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, e := range []model.HistoryEntry{
		{ID: "a1", Repo: "a", Conclusion: "success", FinishedAt: t0},
		{ID: "b1", Repo: "b", Conclusion: "failure", FinishedAt: t0.Add(time.Hour)},
		{ID: "a2", Repo: "a", Conclusion: "failure", FinishedAt: t0.Add(2 * time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	got, _ := s.Query("a", "", time.Time{}, 0)
	if len(got) != 2 || got[0].ID != "a2" {
		t.Fatalf("repo filter newest first: %+v", got)
	}
	got, _ = s.Query("", "failure", time.Time{}, 1)
	if len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("conclusion+limit: %+v", got)
	}
	if err := s.Prune(t0.Add(30 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Query("", "", time.Time{}, 0)
	if len(got) != 2 || got[1].ID != "b1" {
		t.Fatalf("after prune: %+v", got)
	}
}

func TestQueryOrdersByFinishTime(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []model.HistoryEntry{
		{ID: "late", Repo: "a", RunID: 1, FinishedAt: t0.Add(2 * time.Hour)},
		{ID: "early", Repo: "a", RunID: 2, FinishedAt: t0},
		{ID: "mid", Repo: "a", RunID: 3, FinishedAt: t0.Add(time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Query("a", "", time.Time{}, 0)
	if len(got) != 3 || got[0].ID != "late" || got[1].ID != "mid" || got[2].ID != "early" {
		t.Fatalf("order = %+v", got)
	}
	got, _ = s.Query("a", "", time.Time{}, 1)
	if len(got) != 1 || got[0].ID != "late" {
		t.Fatalf("limit must keep the newest: %+v", got)
	}
}

func TestAppendSkipsDuplicateInstanceRun(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	e := model.HistoryEntry{ID: "abc123", Repo: "a", RunID: 7, Conclusion: "success", FinishedAt: time.Now()}
	for i := 0; i < 2; i++ {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	e.RunID = 8
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Query("", "", time.Time{}, 0)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (duplicate skipped), got %+v", got)
	}
}

func TestAppendAfterTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":"torn","repo":"a","run_id":1,"fin`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Store{Path: path}
	if err := s.Append(model.HistoryEntry{ID: "x", Repo: "a", RunID: 2, FinishedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query("", "", time.Time{}, 0)
	if err != nil || len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("record after torn tail: %+v %v", got, err)
	}
}

func TestQuerySince(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, e := range []model.HistoryEntry{
		{ID: "before", Repo: "a", RunID: 1, Conclusion: "success", FinishedAt: t0.Add(-time.Second)},
		{ID: "at", Repo: "a", RunID: 2, Conclusion: "failure", FinishedAt: t0},
		{ID: "after", Repo: "b", RunID: 3, Conclusion: "success", FinishedAt: t0.Add(time.Second)},
		{ID: "later", Repo: "a", RunID: 4, Conclusion: "success", FinishedAt: t0.Add(time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		repo, conclusion string
		since            time.Time
		limit            int
		want             string
	}{
		{"", "", time.Time{}, 0, "later,after,at,before"},
		{"", "", t0, 0, "later,after,at"},
		{"a", "", t0, 0, "later,at"},
		{"", "success", t0, 0, "later,after"},
		{"", "", t0, 2, "later,after"},
		{"", "", t0.Add(2 * time.Hour), 0, ""},
	} {
		got, err := s.Query(c.repo, c.conclusion, c.since, c.limit)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		if strings.Join(ids, ",") != c.want {
			t.Errorf("Query(%q, %q, %v, %d) = %v, want %s", c.repo, c.conclusion, c.since, c.limit, ids, c.want)
		}
	}
}
