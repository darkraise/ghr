package history

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
		{"B", "", time.Time{}, 0, "after"},
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

func TestQueryFailureMeansEveryFailedConclusion(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i, c := range []string{"success", "failure", "timed_out", "startup_failure", "action_required", "cancelled", "skipped", "unknown"} {
		if err := s.Append(model.HistoryEntry{ID: c, Repo: "a", RunID: int64(i + 1), Conclusion: c, FinishedAt: t0.Add(time.Duration(-i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	for conclusion, want := range map[string]string{
		"failure":   "failure,timed_out,startup_failure,action_required",
		"timed_out": "timed_out",
		"unknown":   "unknown",
	} {
		got, err := s.Query("", conclusion, time.Time{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		if strings.Join(ids, ",") != want {
			t.Errorf("Query(%q) = %v, want %s", conclusion, ids, want)
		}
	}
}

func TestWritesCountsOnlyRealWrites(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	var seen []int64
	// The hook reads the store, which deadlocks unless the lock was released first.
	s.SetOnChange(func() { seen = append(seen, s.Writes()) })
	if err := s.Prune(time.Now()); err != nil || s.Writes() != 0 {
		t.Fatalf("prune with no file: writes %d err %v", s.Writes(), err)
	}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	e := model.HistoryEntry{ID: "a1", Repo: "a", RunID: 1, FinishedAt: t0}
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 1 {
		t.Fatalf("a duplicate append counted: writes %d", s.Writes())
	}
	before, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A rewrite renames a new file into place, so the same file means no rewrite.
	if after, err := os.Stat(s.Path); err != nil || !os.SameFile(before, after) || s.Writes() != 1 {
		t.Fatalf("a prune that drops nothing: writes %d, hook saw %v, err %v", s.Writes(), seen, err)
	}
	if err := s.Prune(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 2 || !slices.Equal(seen, []int64{1, 2}) {
		t.Fatalf("writes %d, hook saw %v", s.Writes(), seen)
	}
	s.SetOnChange(nil)
	if err := s.Append(model.HistoryEntry{ID: "b1", Repo: "b", RunID: 2, FinishedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 3 || len(seen) != 2 {
		t.Fatalf("after removing the hook: writes %d, hook saw %v", s.Writes(), seen)
	}
}

func TestHistoryHookUnderConcurrency(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	var calls atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := s.Append(model.HistoryEntry{ID: fmt.Sprint(i), RunID: int64(i)}); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			s.SetOnChange(func() { calls.Add(1) })
			s.Writes()
		}()
	}
	wg.Wait()
	if s.Writes() != 4 {
		t.Fatalf("writes %d, want 4", s.Writes())
	}
}

// entryLine is a history line of exactly n bytes, without its newline.
func entryLine(id string, n int) string {
	head := `{"id":"` + id + `","repo":"`
	return head + strings.Repeat("x", n-len(head)-2) + `"}`
}

func TestReadAllSkipsWhatItCannotDecode(t *testing.T) {
	good := `{"id":"good","repo":"a","run_id":1}`
	for name, c := range map[string]struct {
		file    string
		ids     string
		skipped int
	}{
		"empty file":                    {"", "", 0},
		"a line of exactly 1 MiB":       {entryLine("edge", maxLine) + "\n" + good + "\n", "edge,good", 0},
		"a line one byte over":          {good + "\n" + entryLine("big", maxLine+1) + "\n", "good", 1},
		"an oversized line between two": {good + "\n" + entryLine("big", 2*maxLine) + "\n" + good + "\n", "good,good", 1},
		"an oversized last line":        {good + "\n" + entryLine("big", 2*maxLine), "good", 1},
		"a line that is not JSON":       {"garbage\n" + good + "\n", "good", 1},
		"a last line with no newline":   {good + "\n" + good, "good,good", 0},
	} {
		s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
		if err := os.WriteFile(s.Path, []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		got, skipped, err := s.readAll()
		var ids []string
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		if err != nil || strings.Join(ids, ",") != c.ids || skipped != c.skipped {
			t.Errorf("%s: entries %v, skipped %d, err %v; want %s and %d", name, ids, skipped, err, c.ids, c.skipped)
		}
	}
}

// One line too long to decode must not stop the store: appends and queries
// keep working, the skip is logged once, and the next prune removes the line.
func TestOversizedLineIsSkippedAndPruned(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if err := s.Append(model.HistoryEntry{ID: "old", Repo: "a", RunID: 1, FinishedAt: t0}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(entryLine("big", 2*maxLine) + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(model.HistoryEntry{ID: "new", Repo: "a", RunID: 2, FinishedAt: t0.Add(time.Hour)}); err != nil {
		t.Fatalf("append beside an oversized line: %v", err)
	}
	got, err := s.Query("", "", time.Time{}, 0)
	if err != nil || len(got) != 2 || got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("query beside an oversized line: %+v %v", got, err)
	}
	want := "history: " + s.Path + ": skipped 1 line(s) longer than 1 MiB or not valid JSON\n"
	if n := strings.Count(logs.String(), "skipped"); n != 1 || !strings.HasSuffix(logs.String(), want) {
		t.Fatalf("%d skip lines, want one ending %q:\n%s", n, want, logs.String())
	}
	writes := s.Writes()
	if err := s.Prune(t0.Add(-time.Hour)); err != nil { // a cutoff before every entry
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || len(data) > 1024 || s.Writes() != writes+1 {
		t.Fatalf("after the prune: %d bytes, writes %d (was %d), err %v", len(data), s.Writes(), writes, err)
	}
	if got, err := s.Query("", "", time.Time{}, 0); err != nil || len(got) != 2 {
		t.Fatalf("the prune lost entries: %+v %v", got, err)
	}
	if n := strings.Count(logs.String(), "skipped"); n != 1 {
		t.Fatalf("%d skip lines after the prune:\n%s", n, logs.String())
	}

	// The same when the oversized line is all the file holds.
	if err := os.WriteFile(s.Path, []byte(entryLine("big", 2*maxLine)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(s.Path); err != nil || st.Size() != 0 || s.Writes() != writes+2 {
		t.Fatalf("a file of one oversized line: size %d, writes %d (was %d), err %v", st.Size(), s.Writes(), writes, err)
	}
}

func TestSkippedLineIsLoggedAgainAfterTheFileWasMissing(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	for round := 1; round <= 2; round++ {
		if err := os.WriteFile(s.Path, []byte("not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Query("", "", time.Time{}, 0); err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(logs.String(), "skipped"); n != round {
			t.Fatalf("round %d: %d skip lines, want %d:\n%s", round, n, round, logs.String())
		}
		if err := os.Remove(s.Path); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Query("", "", time.Time{}, 0); err != nil {
			t.Fatal(err)
		}
	}
}

// A prune leaves the file 0600 because it replaces it, but a prune with
// nothing to drop writes nothing, so an append sets the mode itself.
func TestAppendLeavesTheFile0600(t *testing.T) {
	for name, before := range map[string]func(path string) error{
		"a file it creates": func(string) error { return nil },
		"a file it finds 0644": func(path string) error {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				return err
			}
			return os.Chmod(path, 0o644)
		},
	} {
		s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
		if err := before(s.Path); err != nil {
			t.Fatal(err)
		}
		if err := s.Append(model.HistoryEntry{ID: "a", Repo: "a", RunID: 1}); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(s.Path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, err %v; want -rw-------", name, st.Mode().Perm(), err)
		}
	}
}

// rewriteInPlace swaps old for new, which must be as long, in the file and
// restores its modification time, so only a read of the content can tell.
func rewriteInPlace(t *testing.T, path, old, new string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != len(new) || !bytes.Contains(b, []byte(old)) {
		t.Fatalf("cannot swap %q for %q in %q", old, new, b)
	}
	if err := os.WriteFile(path, bytes.Replace(b, []byte(old), []byte(new), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
}

func ids(es []model.HistoryEntry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	return strings.Join(out, ",")
}

func TestStoreAgreesWithAFreshReaderAfterEveryWrite(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	check := func(step, want string) {
		t.Helper()
		got, err := s.Query("", "", time.Time{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := (&Store{Path: s.Path}).Query("", "", time.Time{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if ids(got) != want || ids(fresh) != want {
			t.Fatalf("%s: store has %q, a fresh reader %q, want %q", step, ids(got), ids(fresh), want)
		}
	}
	check("empty", "")
	for i, id := range []string{"e1", "e2", "e3"} {
		if err := s.Append(model.HistoryEntry{ID: id, Repo: "a", RunID: int64(i), FinishedAt: t0.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		check("append "+id, map[string]string{"e1": "e1", "e2": "e2,e1", "e3": "e3,e2,e1"}[id])
	}
	if err := s.Prune(t0.Add(30 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	check("prune", "e3,e2")
	if err := s.Append(model.HistoryEntry{ID: "e4", Repo: "a", RunID: 4, FinishedAt: t0.Add(4 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	check("append after prune", "e4,e3,e2")
	if err := s.Append(model.HistoryEntry{ID: "e4", Repo: "a", RunID: 4}); err != nil {
		t.Fatal(err)
	}
	check("a duplicate", "e4,e3,e2")
}

func TestQueryResultsAreCopies(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if err := s.Append(model.HistoryEntry{ID: "keep", Repo: "a", RunID: 1}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Query("", "", time.Time{}, 0)
	got[0].ID = "changed"
	if again, _ := s.Query("", "", time.Time{}, 0); ids(again) != "keep" {
		t.Fatalf("a caller's edit reached the store: %q", ids(again))
	}
}

// A read parses the file only when its size, modification time or identity
// changed; the cache is what these tests catch by changing the content
// without changing those.
func TestReadsAreServedFromTheCacheUntilTheFileChanges(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if err := s.Append(model.HistoryEntry{ID: "aa", Repo: "a", RunID: 1}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "aa" {
		t.Fatalf("first read %q", ids(got))
	}
	rewriteInPlace(t, s.Path, `"aa"`, `"bb"`)
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "aa" {
		t.Fatalf("an unchanged file was parsed again: %q", ids(got))
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(s.Path, later, later); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "bb" {
		t.Fatalf("a newer modification time was not read: %q", ids(got))
	}
}

func TestAppendAndPruneLeaveTheCacheValid(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []model.HistoryEntry{
		{ID: "aa", Repo: "a", RunID: 1, FinishedAt: t0},
		{ID: "cc", Repo: "a", RunID: 2, FinishedAt: t0.Add(time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s.Query("", "", time.Time{}, 0)
	if err := s.Append(model.HistoryEntry{ID: "dd", Repo: "a", RunID: 3, FinishedAt: t0.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rewriteInPlace(t, s.Path, `"aa"`, `"xx"`)
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "dd,cc,aa" {
		t.Fatalf("after an append the file was parsed again: %q", ids(got))
	}
	if err := s.Prune(t0.Add(30 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	rewriteInPlace(t, s.Path, `"cc"`, `"yy"`)
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "dd,cc" {
		t.Fatalf("after a prune the file was parsed again: %q", ids(got))
	}
}

func TestFileRewrittenOutsideTheStoreIsReadAgain(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if err := s.Append(model.HistoryEntry{ID: "aa", Repo: "a", RunID: 1}); err != nil {
		t.Fatal(err)
	}
	s.Query("", "", time.Time{}, 0)

	other := `{"id":"zzzz","repo":"a","run_id":9}` + "\n"
	if err := os.WriteFile(s.Path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "zzzz" {
		t.Fatalf("a file of another size: %q", ids(got))
	}

	st, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	swap := filepath.Join(filepath.Dir(s.Path), "swap")
	if err := os.WriteFile(swap, []byte(strings.Replace(other, "zzzz", "wwww", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(swap, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(swap, s.Path); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Query("", "", time.Time{}, 0); ids(got) != "wwww" {
		t.Fatalf("a replacement file of the same size and time: %q", ids(got))
	}

	if err := os.Remove(s.Path); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Query("", "", time.Time{}, 0); len(got) != 0 {
		t.Fatalf("a removed file: %q", ids(got))
	}
}
