package ghstorage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func cleanupConfig() *config.Config {
	cfg := testConfig()
	cfg.GitHubStorage = config.GitHubStorage{CleanupMaxAge: config.Duration(24 * time.Hour), Repos: map[string]config.StorageOverride{
		"homelab": {Cleanup: "off"}, "chicken-farm": {Cleanup: "6h"},
	}}
	return cfg
}

func artifactIDs(r model.RepoStorage) []int64 {
	out := []int64{}
	for _, a := range r.Artifacts {
		out = append(out, a.ID)
	}
	return out
}

func TestCleanupDeletesArtifactsOlderThanTheAge(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 30*time.Hour, model.GiB), ghArt(2, 20, now, time.Hour, 5), ghArt(3, 30, now, 40*time.Hour, 5))
	gh.addRepo("homelab", true, ghArt(4, 40, now, 100*time.Hour, 5))
	gh.addRepo("chicken-farm", true, ghArt(5, 50, now, 7*time.Hour, 3<<20))
	gh.runs["darkmem"] = map[string][]github.Run{"in_progress": {{ID: 30}}}

	s.pass(true, false)
	if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, []string{"DeleteArtifact darkmem 1", "DeleteArtifact chicken-farm 5"}) {
		t.Fatalf("deletes %v, want darkmem 1 then chicken-farm 5, oldest first", got)
	}
	for _, repo := range []string{"darkmem", "chicken-farm"} {
		for _, st := range ActiveStatuses {
			if len(gh.callsLike("ListRuns "+repo+" "+st)) != 1 {
				t.Errorf("runs of %s with status %s not listed", repo, st)
			}
		}
	}
	if got := gh.callsLike("ListRuns homelab"); len(got) != 0 {
		t.Errorf("homelab has no candidates but its runs were listed: %v", got)
	}
	st := s.Snapshot()
	if got := artifactIDs(row(t, st, "darkmem")); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Errorf("darkmem keeps %v, want 2 and 3 (3's run is in progress)", got)
	}
	if got := artifactIDs(row(t, st, "homelab")); !reflect.DeepEqual(got, []int64{4}) {
		t.Errorf("homelab keeps %v: its cleanup is off", got)
	}
	want := []model.Deletion{
		{At: now, Repo: "chicken-farm", Kind: "artifact", Name: "art-5", Bytes: 3 << 20, Age: "7h", By: "cleanup"},
		{At: now, Repo: "darkmem", Kind: "artifact", Name: "art-1", Bytes: model.GiB, Age: "30h", By: "cleanup"},
	}
	if !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v, want %+v", st.Deletions, want)
	}
	if got := eventsLike(s.Events, "cleanup deleted"); !reflect.DeepEqual(got, []string{"cleanup deleted 1 artifacts (1.0 GB)", "cleanup deleted 1 artifacts (3 MB)"}) {
		t.Errorf("events %q", got)
	}
}

func TestCleanupSkipsEveryRunNotCompleted(t *testing.T) {
	for _, status := range ActiveStatuses {
		t.Run(status, func(t *testing.T) {
			gh := newFakeGH()
			s, clock := newTestService(t, gh, cleanupConfig())
			gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 30*time.Hour, 5))
			gh.runs["darkmem"] = map[string][]github.Run{status: {{ID: 10}}}
			s.pass(true, false)
			if got := gh.callsLike("Delete"); len(got) != 0 {
				t.Fatalf("deleted %v of a run that is %s", got, status)
			}
		})
	}
}

func TestCleanupDeletesAtMost100OldestFirst(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	var a, b []github.Artifact
	for i := range 60 {
		a = append(a, ghArt(int64(1000+i), int64(i), clock.Now(), time.Duration(100+2*i)*time.Hour, 1))
		b = append(b, ghArt(int64(2000+i), int64(i), clock.Now(), time.Duration(101+2*i)*time.Hour, 1))
	}
	gh.addRepo("a", true, a...)
	gh.addRepo("b", true, b...)
	s.pass(true, false)
	got := gh.callsLike("Delete")
	if len(got) != MaxCleanupDeletes || got[0] != "DeleteArtifact b 2059" || got[99] != "DeleteArtifact a 1010" {
		t.Fatalf("%d deletes from %v to %v, want 100 from b 2059 (oldest) to a 1010", len(got), got[0], got[len(got)-1])
	}
	if n := row(t, s.Snapshot(), "a").ArtifactCount + row(t, s.Snapshot(), "b").ArtifactCount; n != 20 {
		t.Fatalf("%d artifacts left, want the 20 youngest", n)
	}
}

func TestCleanupRecordsNothingForAnArtifactAlreadyGone(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 30*time.Hour, 5))
	gh.gone[1] = true
	s.pass(true, false)
	st := s.Snapshot()
	if row(t, st, "darkmem").ArtifactCount != 0 || len(st.Deletions) != 0 || len(eventsLike(s.Events, "cleanup")) != 0 {
		t.Fatalf("row %+v, deletions %+v: a 404 leaves the row and records nothing", row(t, st, "darkmem"), st.Deletions)
	}
}

func TestCleanupStopsARepositoryAtItsFirstError(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 50*time.Hour, 5), ghArt(2, 20, now, 40*time.Hour, 5))
	gh.addRepo("other", true, ghArt(3, 30, now, 45*time.Hour, 5))
	gh.delErr[1] = forbidden
	s.pass(true, false)
	if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, []string{"DeleteArtifact darkmem 1", "DeleteArtifact other 3"}) {
		t.Fatalf("deletes %v, want darkmem to stop after its error", got)
	}
	if r := row(t, s.Snapshot(), "darkmem"); r.Error != forbidden.Error() || r.ArtifactCount != 2 {
		t.Fatalf("darkmem row %+v", r)
	}
}

func TestCleanupSkipsARepositoryWhoseRunsCannotBeListed(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 50*time.Hour, 5))
	gh.runsErr["darkmem"] = errors.New("boom")
	s.pass(true, false)
	if got := gh.callsLike("Delete"); len(got) != 0 {
		t.Fatalf("deleted %v without knowing which runs are in progress", got)
	}
	if r := row(t, s.Snapshot(), "darkmem"); r.Error != "boom" {
		t.Fatalf("row error %q, want boom", r.Error)
	}
}

func TestWritesStayWithinTheHourlyBudget(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	first := clock.Now()
	for i := range WriteBudget {
		if err := s.write(func() error { return nil }); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		clock.Add(time.Second)
	}
	err := s.write(func() error { t.Fatal("ran past the budget"); return nil })
	var be *BudgetError
	if !errors.As(err, &be) || !be.RetryAt.Equal(first.Add(time.Hour)) {
		t.Fatalf("err = %v, want a BudgetError until %v", err, first.Add(time.Hour))
	}
	if want := "GitHub write budget used up; try again after 2026-10-13T13:00:00Z"; err.Error() != want {
		t.Fatalf("message %q, want %q", err.Error(), want)
	}
	clock.Add(time.Hour - WriteBudget*time.Second)
	if err := s.write(func() error { return nil }); err != nil {
		t.Fatalf("an hour after the first write: %v", err)
	}
}

func TestCleanupStopsWhenTheBudgetIsUsed(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 50*time.Hour, 5))
	for range WriteBudget {
		_ = s.write(func() error { return nil })
	}
	s.pass(true, false)
	if got := gh.callsLike("Delete"); len(got) != 0 {
		t.Fatalf("deleted %v past the budget", got)
	}
	if r := row(t, s.Snapshot(), "darkmem"); r.Error != "" || r.ArtifactCount != 1 {
		t.Fatalf("row %+v: the budget is not the repository's error", r)
	}
}

func TestDeletionsKeepTheNewest(t *testing.T) {
	s, _ := newTestService(t, newFakeGH(), testConfig())
	s.mu.Lock()
	for i := range MaxDeletions + 5 {
		s.recordDeletion(model.Deletion{Name: fmt.Sprint(i)})
	}
	s.mu.Unlock()
	d := s.Snapshot().Deletions
	if len(d) != MaxDeletions || d[0].Name != "204" || d[len(d)-1].Name != "5" {
		t.Fatalf("%d deletions from %s to %s, want 200 from 204 to 5", len(d), d[0].Name, d[len(d)-1].Name)
	}
}

func TestSizeAndAgeTexts(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 1023: "1023 B", 2048: "2 KB", 340 << 20: "340 MB", 3 * model.GiB / 2: "1.5 GB"} {
		if got := sizeText(in); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[time.Duration]string{-time.Minute: "0m", 59 * time.Minute: "59m", 30 * time.Hour: "30h", 50 * time.Hour: "2d"} {
		if got := ageText(in); got != want {
			t.Errorf("ageText(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanupKeepsAPolicySavedDuringIt(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	cur := cleanupConfig()
	s, clock := newTestService(t, gh, cur)
	s.Config = func() *config.Config { return cur }
	s.pass(false, true)
	before := s.Snapshot().Quota.IncludedGBHours
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 30*time.Hour, 5))
	var want float64
	gh.onDelete = func(context.Context, string, int64) error {
		next := cleanupConfig()
		next.GitHubStorage.IncludedMB = 1000
		cur = next
		s.PolicyChanged()
		want = s.Snapshot().Quota.IncludedGBHours
		return nil
	}
	s.pass(true, false)
	if got := gh.callsLike("Delete"); len(got) != 1 {
		t.Fatalf("deletes %v, want one", got)
	}
	if got := s.Snapshot().Quota.IncludedGBHours; got != want || want == before {
		t.Fatalf("included %v after the pass, %v when saved, %v before: the pass wrote its old config back", got, want, before)
	}
}
