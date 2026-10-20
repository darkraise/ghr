package artifactstore

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

const day = 24 * time.Hour

// putExpiring stores an artifact of run that expires at expires.
func (e *testEnv) putExpiring(repo string, run int64, name string, age time.Duration, expires time.Time) string {
	key := artifact.ObjectKey("darkraise", repo, run, name)
	e.put(key, 5, age, artifact.Meta{RunID: run, Attempt: 1, Expires: &expires}.Map())
	return key
}

func TestCleanup(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.WatchRepos = []string{"homelab"}
	e.cfg.ArtifactStore.Repos = map[string]config.ArtifactRepo{"homelab": {Cleanup: "off"}}
	now := e.clock.Now()
	e.gh.runs["darkmem"] = map[string][]github.Run{"in_progress": {{ID: 1}, {ID: 5}}}
	expired := e.putExpiring("darkmem", 1, "expired", time.Hour, now.Add(-time.Minute))
	notYet := e.putExpiring("darkmem", 2, "not-yet", 30*day, now.Add(time.Hour))
	old := e.putArtifact("darkmem", 3, "old", 5, 8*day)
	young := e.putArtifact("darkmem", 4, "young", 5, 6*day)
	active := e.putArtifact("darkmem", 5, "active", 5, 8*day)
	off := e.putArtifact("homelab", 6, "old", 5, 30*day)
	offExpired := e.putExpiring("homelab", 7, "expired", time.Hour, now.Add(-time.Hour))
	uncovered := e.putArtifact("old-repo", 8, "x", 5, 8*day)
	e.put("someone/else/1/a.zip", 5, 100*day, nil)
	e.put("darkraise/notes.txt", 5, 100*day, nil)

	e.s.pass()
	want := []string{"darkraise/notes.txt", active, notYet, young, off, "someone/else/1/a.zip"}
	slices.Sort(want)
	if got := e.srv.Keys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys left %v, want %v", got, want)
	}
	st := e.s.Snapshot()
	// Newest first: the expired objects were deleted first, each group oldest
	// first and then by key.
	wantDel := []model.Deletion{
		{At: now, Repo: "old-repo", Kind: "artifact", Name: "x", Bytes: 5, Age: "8d", By: "cleanup", Store: "rustfs"},
		{At: now, Repo: "darkmem", Kind: "artifact", Name: "old", Bytes: 5, Age: "8d", By: "cleanup", Store: "rustfs"},
		{At: now, Repo: "homelab", Kind: "artifact", Name: "expired", Bytes: 5, Age: "1h", By: "expired", Store: "rustfs"},
		{At: now, Repo: "darkmem", Kind: "artifact", Name: "expired", Bytes: 5, Age: "1h", By: "expired", Store: "rustfs"},
	}
	if !reflect.DeepEqual(st.Deletions, wantDel) {
		t.Fatalf("deletions %+v\nwant %+v", st.Deletions, wantDel)
	}
	if r := row(t, st, "darkmem"); r.Count != 3 {
		t.Errorf("darkmem has %d items after the cleanup, want 3", r.Count)
	}
	for _, key := range []string{expired, old, offExpired, uncovered} {
		if _, ok := e.srv.Object(key); ok {
			t.Errorf("%s was kept", key)
		}
	}
	want2 := []string{"artifact store: cleanup deleted 1 artifacts (5 B)", "artifact store: cleanup deleted 2 artifacts (10 B)", "artifact store: cleanup deleted 1 artifacts (5 B)"}
	if got := eventsLike(e.s.Events, "cleanup deleted"); len(got) != 3 || !reflect.DeepEqual(sortedCopy(got), sortedCopy(want2)) {
		t.Errorf("events %q", got)
	}
	if got := e.gh.callsLike("ListRuns homelab"); len(got) != 0 {
		t.Errorf("runs listed for a repository whose cleanup is off: %v", got)
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func TestCleanupIsCappedPerPass(t *testing.T) {
	e := newTestEnv(t)
	for i := range MaxDeletes + 5 {
		e.putArtifact("darkmem", int64(i+1), "dist", 5, 8*day+time.Duration(MaxDeletes+5-i)*time.Minute)
	}
	e.s.pass()
	got := e.srv.Keys()
	if len(got) != 5 {
		t.Fatalf("%d keys left after one pass, want 5", len(got))
	}
	for _, k := range got {
		if _, _, run, _, _ := artifact.ParseObjectKey(k); run <= MaxDeletes {
			t.Fatalf("keys left %v, want the 5 newest", got)
		}
	}
	e.s.pass()
	if got := e.srv.Keys(); len(got) != 0 {
		t.Fatalf("keys left after the second pass %v", got)
	}
}

func TestExpiredObjectsAreCappedFirst(t *testing.T) {
	e := newTestEnv(t)
	now := e.clock.Now()
	for i := range MaxDeletes {
		e.putExpiring("darkmem", int64(i+1), "dist", time.Hour, now.Add(-time.Minute))
	}
	old := e.putArtifact("darkmem", 1000, "old", 5, 30*day)
	e.s.pass()
	if got := e.srv.Keys(); !reflect.DeepEqual(got, []string{old}) {
		t.Fatalf("keys left %v, want the old one: expired objects come first", got)
	}
	if n := len(e.gh.callsLike("ListRuns")); n != 0 {
		t.Fatalf("%d run listings with the pass already full", n)
	}
}

func TestAnExpiryPastTheRetentionCapIsCapped(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.ArtifactStore.Repos = map[string]config.ArtifactRepo{"darkmem": {Cleanup: "off"}}
	now := e.clock.Now()
	far := now.AddDate(10, 0, 0)
	capped := e.putExpiring("darkmem", 1, "capped", artifact.MaxRetentionDays*day+time.Hour, far)
	kept := e.putExpiring("darkmem", 2, "kept", artifact.MaxRetentionDays*day-time.Hour, far)
	e.s.pass()
	if got := e.srv.Keys(); !reflect.DeepEqual(got, []string{kept}) {
		t.Fatalf("keys left %v, want only %s: %s is past the %d-day cap", got, kept, capped, artifact.MaxRetentionDays)
	}
	if d := e.s.Snapshot().Deletions; len(d) != 1 || d[0].By != "expired" {
		t.Fatalf("deletions %+v, want one expired", d)
	}
}

func TestAFailedActiveRunCheckSkipsOnlyTheAgeRule(t *testing.T) {
	e := newTestEnv(t)
	e.s.Describe = func(err error, repo string) string { return "described: " + err.Error() }
	e.gh.runsErr["darkmem"] = errors.New("boom")
	old := e.putArtifact("darkmem", 3, "old", 5, 8*day)
	e.putExpiring("darkmem", 4, "expired", time.Hour, e.clock.Now().Add(-time.Minute))
	e.s.pass()
	e.s.pass()
	if got := e.srv.Keys(); !reflect.DeepEqual(got, []string{old}) {
		t.Fatalf("keys left %v, want the old one kept and the expired one deleted", got)
	}
	if got := eventsLike(e.s.Events, "artifact store: cleanup stopped"); !reflect.DeepEqual(got, []string{"artifact store: cleanup stopped: described: boom"}) {
		t.Fatalf("events %q, want one across two passes", got)
	}
}

func TestCleanupWaitsForAGoodListing(t *testing.T) {
	e := newTestEnv(t)
	e.putArtifact("darkmem", 3, "old", 5, 8*day)
	e.srv.FailNext("list", 500, "InternalError")
	e.s.pass()
	if n := e.requests("POST "); n != 0 || len(e.srv.Keys()) != 1 {
		t.Fatalf("%d delete requests after a failed listing, want 0", n)
	}
}

// vanishingBucket answers every Head as a bucket deleted after the listing
// would: 404 without a body.
type vanishingBucket struct{ Bucket }

func (vanishingBucket) Head(context.Context, string) (s3.Head, error) {
	return s3.Head{}, &s3.Error{Status: http.StatusNotFound}
}

func TestCleanupSkipsAListingWithVanishedObjects(t *testing.T) {
	e := newTestEnv(t)
	cached := e.putArtifact("darkmem", 3, "cached", 5, 6*day)
	e.s.pass()
	e.clock.Add(2 * day)
	e.putArtifact("darkmem", 4, "new", 5, 8*day)
	inner := e.s.NewBucket
	e.s.NewBucket = func(a config.ArtifactStore, cred s3.Credentials) (Bucket, error) {
		b, err := inner(a, cred)
		return vanishingBucket{b}, err
	}
	e.s.pass()
	if n := e.requests("POST "); n != 0 {
		t.Fatalf("%d delete requests after a listing whose objects vanished, want 0", n)
	}
	if _, ok := e.srv.Object(cached); !ok {
		t.Fatalf("%s was deleted after a listing whose objects vanished", cached)
	}
	e.s.NewBucket = inner
	e.s.pass()
	if got := e.srv.Keys(); len(got) != 0 {
		t.Fatalf("keys left %v after a good listing", got)
	}
}

// missingBucketAbort answers every Abort as a bucket that no longer exists.
type missingBucketAbort struct{ Bucket }

func (missingBucketAbort) Abort(context.Context, string, string) error {
	return &s3.Error{Status: http.StatusNotFound, Code: "NoSuchBucket"}
}

func TestAbortOnAMissingBucketIsNotCounted(t *testing.T) {
	e := newTestEnv(t)
	e.srv.SeedUpload("darkraise/darkmem/1/old.zip", e.clock.Now().Add(-25*time.Hour))
	inner := e.s.NewBucket
	e.s.NewBucket = func(a config.ArtifactStore, cred s3.Credentials) (Bucket, error) {
		b, err := inner(a, cred)
		return missingBucketAbort{b}, err
	}
	e.s.pass()
	if got := eventsLike(e.s.Events, "aborted"); len(got) != 0 {
		t.Fatalf("events %q, want none: the bucket answered NoSuchBucket", got)
	}
}

func TestCleanupRereadsThePolicy(t *testing.T) {
	e := newTestEnv(t)
	e.putArtifact("darkmem", 3, "old", 5, 8*day)
	e.gh.runs["darkmem"] = map[string][]github.Run{}
	cfg := *e.cfg
	turnedOff := cfg
	turnedOff.ArtifactStore.CleanupMaxAge = "off"
	e.s.Config = func() *config.Config {
		if len(e.gh.callsLike("ListRuns")) > 0 {
			return &turnedOff
		}
		return &cfg
	}
	e.s.pass()
	if got := e.srv.Keys(); len(got) != 1 {
		t.Fatalf("keys left %v, want the object kept: the policy was turned off during the pass", got)
	}
}

func TestCleanupAbortsOldUploads(t *testing.T) {
	e := newTestEnv(t)
	now := e.clock.Now()
	e.srv.SeedUpload("darkraise/darkmem/1/old.zip", now.Add(-25*time.Hour))
	e.srv.SeedUpload("darkraise/darkmem/2/new.zip", now.Add(-23*time.Hour))
	e.srv.SeedUpload("someone/else/1/a.zip", now.Add(-48*time.Hour))
	e.s.pass()
	if got := e.srv.Uploads(); !reflect.DeepEqual(got, []string{"darkraise/darkmem/2/new.zip", "someone/else/1/a.zip"}) {
		t.Fatalf("uploads left %v", got)
	}
	if got := eventsLike(e.s.Events, "aborted"); !reflect.DeepEqual(got, []string{"artifact store: aborted 1 unfinished uploads started more than a day ago"}) {
		t.Fatalf("events %q", got)
	}
}

func TestAgeAndSizeText(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{{90 * time.Second, "1m"}, {3 * time.Hour, "3h"}, {47 * time.Hour, "47h"}, {50 * time.Hour, "2d"}, {-time.Minute, "0m"}} {
		if got := ageText(tc.d); got != tc.want {
			t.Errorf("ageText(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	for _, tc := range []struct {
		n    int64
		want string
	}{{5, "5 B"}, {2048, "2 KB"}, {3 << 20, "3 MB"}, {model.GiB + model.GiB/2, "1.5 GB"}} {
		if got := sizeText(tc.n); got != tc.want {
			t.Errorf("sizeText(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
