package ghstorage

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// blockDelete makes every delete wait until release is closed or the
// service closes, and closes entered when the first one starts waiting.
func blockDelete(gh *fakeGH) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	gh.onDelete = func(ctx context.Context, repo string, id int64) error {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return entered, release
}

func TestDeleteArtifactsByID(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 2*time.Hour, 5), ghArt(2, 20, now, time.Hour, 7))
	s.pass(true, false)
	id, err := s.DeleteArtifacts("DARKMEM", model.DeleteArtifactsRequest{IDs: []int64{1, 99}})
	if err != nil || id != "op-1" {
		t.Fatalf("DeleteArtifacts = %q, %v", id, err)
	}
	s.Wait()
	st := s.Snapshot()
	want := &model.GitHubOperation{ID: "op-1", Kind: "delete-artifacts", Repo: "darkmem", Total: 2, Done: 1, Bytes: 5,
		Failed: []model.OperationFailure{{Repo: "darkmem", ID: 99, Error: "not in the last inventory"}}, StartedAt: now, FinishedAt: &now}
	if !reflect.DeepEqual(st.Operation, want) {
		t.Errorf("operation %+v, want %+v", st.Operation, want)
	}
	if got := artifactIDs(row(t, st, "darkmem")); !reflect.DeepEqual(got, []int64{2}) {
		t.Errorf("artifacts left %v, want [2]", got)
	}
	if want := []model.Deletion{{At: now, Repo: "darkmem", Kind: "artifact", Name: "art-1", Bytes: 5, Age: "2h", By: "manual"}}; !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v", st.Deletions)
	}
	if got := eventsLike(s.Events, "deleted"); !reflect.DeepEqual(got, []string{"deleted 1 artifacts (5 B); 1 failed"}) {
		t.Errorf("events %q", got)
	}
}

func TestDeleteArtifactsOlderThanSkipsRunsNotCompleted(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 30*time.Hour, 5), ghArt(2, 20, now, 30*time.Hour, 5), ghArt(3, 30, now, time.Hour, 5))
	gh.runs["darkmem"] = map[string][]github.Run{"waiting": {{ID: 20}}}
	s.pass(true, false)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{OlderThan: "24h"}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, []string{"DeleteArtifact darkmem 1"}) {
		t.Fatalf("deletes %v, want only 1", got)
	}
	if op := s.Snapshot().Operation; op.Total != 1 || op.Done != 1 || len(op.Failed) != 0 {
		t.Fatalf("operation %+v", op)
	}
}

func TestBulkRequestsAreChecked(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	arts := func(r model.DeleteArtifactsRequest) func() error {
		return func() error { _, err := s.DeleteArtifacts("darkmem", r); return err }
	}
	caches := func(r model.DeleteCachesRequest) func() error {
		return func() error { _, err := s.DeleteCaches("darkmem", r); return err }
	}
	for _, tc := range []struct {
		name string
		call func() error
		want error
	}{
		{"ids and an age", arts(model.DeleteArtifactsRequest{IDs: []int64{1}, OlderThan: "2d"}), BadInput("give either ids or older_than")},
		{"neither ids nor an age", arts(model.DeleteArtifactsRequest{IDs: []int64{}}), BadInput("give either ids or older_than")},
		{"an age under an hour", arts(model.DeleteArtifactsRequest{OlderThan: "30m"}), BadInput("older_than must be a duration of at least 1h, such as 24h")},
		{"no duration", arts(model.DeleteArtifactsRequest{OlderThan: "soon"}), BadInput("older_than must be a duration of at least 1h, such as 24h")},
		{"cache ids and all", caches(model.DeleteCachesRequest{IDs: []int64{1}, All: true}), BadInput("give either ids or all")},
		{"neither cache ids nor all", caches(model.DeleteCachesRequest{}), BadInput("give either ids or all")},
		{"artifacts of an unknown repository", func() error {
			_, err := s.DeleteArtifacts("nope", model.DeleteArtifactsRequest{IDs: []int64{1}})
			return err
		}, ErrUnknownRepo},
		{"caches of an unknown repository", func() error {
			_, err := s.DeleteCaches("nope", model.DeleteCachesRequest{All: true})
			return err
		}, ErrUnknownRepo},
		{"retention of 0 days", func() error { _, err := s.SetRetentionAll(0); return err }, BadInput("days must be from 1 to 400")},
		{"retention over 400 days", func() error { _, err := s.SetRetentionAll(401); return err }, BadInput("days must be from 1 to 400")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if op := s.Snapshot().Operation; op != nil {
				t.Fatalf("a refused request started %+v", op)
			}
		})
	}
}

func TestOneOperationRunsAtATime(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5))
	gh.caches["darkmem"] = []github.Cache{{ID: 7, Key: "k"}}
	s.pass(true, false)
	entered, release := blockDelete(gh)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1}}); err != nil {
		t.Fatal(err)
	}
	<-entered
	for name, call := range map[string]func() error{
		"artifacts": func() error {
			_, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1}})
			return err
		},
		"caches":    func() error { _, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true}); return err },
		"retention": func() error { _, err := s.SetRetentionAll(30); return err },
	} {
		if err := call(); !errors.Is(err, ErrOperationRunning) {
			t.Errorf("%s during an operation: %v, want ErrOperationRunning", name, err)
		}
	}
	if op := s.Snapshot().Operation; op.ID != "op-1" || op.FinishedAt != nil {
		t.Fatalf("operation %+v, want op-1 running", op)
	}
	close(release)
	s.Wait()
	if id, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true}); err != nil || id != "op-2" {
		t.Fatalf("after the first finished: %q, %v", id, err)
	}
}

func TestABulkDeleteStopsAtTheFirstError(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5), ghArt(2, 20, clock.Now(), time.Hour, 5))
	gh.delErr[1] = forbidden
	s.pass(true, false)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	op := s.Snapshot().Operation
	if op.Done != 0 || !reflect.DeepEqual(op.Failed, []model.OperationFailure{{Repo: "darkmem", ID: 1, Error: forbidden.Error()}}) {
		t.Fatalf("operation %+v", op)
	}
	if got := gh.callsLike("Delete"); len(got) != 1 {
		t.Fatalf("deletes %v, want the first only", got)
	}
	if got := eventsLike(s.Events, "deleted"); !reflect.DeepEqual(got, []string{"deleted 0 artifacts (0 B); 1 failed"}) {
		t.Errorf("events %q", got)
	}
}

func TestDeleteAllCaches(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true)
	gh.caches["darkmem"] = []github.Cache{
		{ID: 7, Key: "go-mod", SizeInBytes: 100, LastAccessedAt: now.Add(-3 * time.Hour)},
		{ID: 8, Key: "npm", SizeInBytes: 50, LastAccessedAt: now.Add(-50 * time.Hour)},
	}
	gh.gone[8] = true
	s.pass(true, false)
	if _, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	st := s.Snapshot()
	if op := st.Operation; op.Kind != "delete-caches" || op.Total != 2 || op.Done != 2 || op.Bytes != 100 {
		t.Errorf("operation %+v", op)
	}
	if r := row(t, st, "darkmem"); r.CacheCount != 0 {
		t.Errorf("caches left %+v", r.Caches)
	}
	if want := []model.Deletion{{At: now, Repo: "darkmem", Kind: "cache", Name: "go-mod", Bytes: 100, Age: "3h", By: "manual"}}; !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v, want only the cache GitHub deleted", st.Deletions)
	}
	if got := eventsLike(s.Events, "deleted"); !reflect.DeepEqual(got, []string{"deleted 1 caches (100 B)"}) {
		t.Errorf("events %q", got)
	}
}

func TestSingleDeletes(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, time.Hour, 5), ghArt(2, 20, now, time.Hour, 5), ghArt(3, 30, now, time.Hour, 5))
	gh.caches["darkmem"] = []github.Cache{{ID: 7, Key: "go-mod", SizeInBytes: 100}}
	gh.gone[7] = true
	gh.delErr[2] = forbidden
	s.pass(true, false)
	ctx := context.Background()
	if err := s.DeleteArtifact(ctx, "Darkmem", 1); err != nil {
		t.Fatalf("delete 1: %v", err)
	}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"an artifact deleted before", s.DeleteArtifact(ctx, "darkmem", 1), ErrUnknownRepo},
		{"an unknown repository", s.DeleteArtifact(ctx, "nope", 1), ErrUnknownRepo},
		{"an unknown cache", s.DeleteCache(ctx, "darkmem", 99), ErrUnknownRepo},
		{"a cache already gone", s.DeleteCache(ctx, "darkmem", 7), nil},
		{"GitHub refuses", s.DeleteArtifact(ctx, "darkmem", 2), forbidden},
	} {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, tc.err, tc.want)
		}
	}
	st := s.Snapshot()
	if r := row(t, st, "darkmem"); !reflect.DeepEqual(artifactIDs(r), []int64{2, 3}) || r.CacheCount != 0 {
		t.Errorf("row %+v", r)
	}
	if len(st.Deletions) != 1 || st.Deletions[0].Name != "art-1" || st.Deletions[0].By != "manual" || st.Operation != nil {
		t.Errorf("deletions %+v, operation %+v", st.Deletions, st.Operation)
	}
	for range WriteBudget {
		_ = s.write(func() error { return nil })
	}
	var be *BudgetError
	if err := s.DeleteArtifact(ctx, "darkmem", 3); !errors.As(err, &be) {
		t.Errorf("past the budget: %v, want a BudgetError", err)
	}
}

func TestSetRetention(t *testing.T) {
	gh := newFakeGH()
	for _, r := range []string{"darkmem", "short", "unknown", "strict"} {
		gh.addRepo(r, true)
	}
	gh.retention["short"] = github.Retention{Days: 30, MaxDays: 90}
	gh.retErr["unknown"] = forbidden
	gh.setRetErr["strict"] = &github.APIError{Status: 422, Kind: github.ErrUnprocessable, Message: "Invalid retention days"}
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	ctx := context.Background()
	for _, tc := range []struct {
		repo string
		days int
		want error
	}{
		{"darkmem", 7, nil},
		{"DARKMEM", 8, nil},
		{"darkmem", 0, BadInput("days must be from 1 to 400")},
		{"short", 91, BadInput("days must be from 1 to 90")},
		{"unknown", 400, nil},
		{"unknown", 401, BadInput("days must be from 1 to 400")},
		{"strict", 5, BadInput("Invalid retention days")},
		{"nope", 5, ErrUnknownRepo},
	} {
		if err := s.SetRetention(ctx, tc.repo, tc.days); !errors.Is(err, tc.want) {
			t.Errorf("SetRetention(%s, %d) = %v, want %v", tc.repo, tc.days, err, tc.want)
		}
	}
	st := s.Snapshot()
	if d := row(t, st, "darkmem").RetentionDays; d != 8 {
		t.Errorf("darkmem retention %d, want 8 at once", d)
	}
	if got := gh.callsLike("SetRetention"); !reflect.DeepEqual(got, []string{"SetRetention darkmem 7", "SetRetention darkmem 8", "SetRetention unknown 400", "SetRetention strict 5"}) {
		t.Errorf("calls %v", got)
	}
	if st.Operation != nil {
		t.Errorf("a single change recorded an operation: %+v", st.Operation)
	}
}

func TestSetRetentionAll(t *testing.T) {
	gh := newFakeGH()
	for _, r := range []string{"a", "b", "c", "d"} {
		gh.addRepo(r, true)
	}
	gh.retention["b"] = github.Retention{Days: 30, MaxDays: 30}
	gh.setRetErr["c"] = forbidden
	gh.retErr["d"] = forbidden
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	if _, err := s.SetRetentionAll(60); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	st := s.Snapshot()
	want := []model.OperationFailure{{Repo: "b", Error: "b allows at most 30 days"}, {Repo: "c", Error: forbidden.Error()}}
	if op := st.Operation; op.Kind != "set-retention" || op.Repo != "" || op.Total != 4 || op.Done != 2 || !reflect.DeepEqual(op.Failed, want) {
		t.Errorf("operation %+v", op)
	}
	if got := gh.callsLike("SetRetention"); !reflect.DeepEqual(got, []string{"SetRetention a 60", "SetRetention c 60", "SetRetention d 60"}) {
		t.Errorf("calls %v", got)
	}
	if d := row(t, st, "a").RetentionDays; d != 60 {
		t.Errorf("a retention %d, want 60", d)
	}
	if got := eventsLike(s.Events, "set retention"); !reflect.DeepEqual(got, []string{"set retention to 60 days on 2 of 4 repositories; 2 failed"}) {
		t.Errorf("events %q", got)
	}
}

func TestPreviewChecksItsInput(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 2*time.Hour, 5))
	s.pass(true, false)
	for _, tc := range []struct {
		cleanup, repo string
		want          []model.StoragePreview
		err           error
	}{
		{"", "", nil, BadInput("cleanup must be off or a duration such as 2d")},
		{"soon", "", nil, BadInput("cleanup must be off or a duration such as 2d")},
		{"2d", "nope", nil, ErrUnknownRepo},
		{"off", "", []model.StoragePreview{}, nil},
		{"1h", "DARKMEM", []model.StoragePreview{{Repo: "darkmem", Count: 1, Bytes: 5}}, nil},
	} {
		got, err := s.Preview(tc.cleanup, tc.repo)
		if !errors.Is(err, tc.err) || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Preview(%q, %q) = %+v, %v; want %+v, %v", tc.cleanup, tc.repo, got, err, tc.want, tc.err)
		}
	}
}

func TestCloseCancelsAnOperation(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5), ghArt(2, 20, clock.Now(), time.Hour, 5))
	s.pass(true, false)
	entered, _ := blockDelete(gh)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	<-entered
	s.Close()
	stopped := make(chan struct{})
	go func() { s.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	if op := s.Snapshot().Operation; op.FinishedAt == nil || op.Done != 0 || len(gh.callsLike("Delete")) != 1 {
		t.Fatalf("operation %+v after Close", op)
	}
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{2}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v, want ErrClosed", err)
	}
}

func TestOperationIDsAreRandomWithoutNewID(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	s.NewID = nil
	s.pass(true, false)
	var ids []string
	for range 2 {
		id, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true})
		if err != nil {
			t.Fatal(err)
		}
		s.Wait()
		if b, err := hex.DecodeString(id); err != nil || len(b) != 8 {
			t.Fatalf("operation id %q, want 8 random bytes in hex", id)
		}
		if op := s.Snapshot().Operation; op.ID != id {
			t.Fatalf("snapshot operation %q, want %q", op.ID, id)
		}
		ids = append(ids, id)
	}
	if ids[0] == ids[1] {
		t.Errorf("two operations share the id %q", ids[0])
	}
}
