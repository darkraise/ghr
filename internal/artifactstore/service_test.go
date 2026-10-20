package artifactstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
)

func TestNotSetUpIsIdle(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.ArtifactStore = config.ArtifactStore{}
	e.s.pass()
	st := e.s.Snapshot()
	if st.Configured || st.Repos == nil || st.Deletions == nil || st.AdminKey != nil || st.Error != "" || st.Endpoint != "" {
		t.Fatalf("snapshot %+v, want only configured false and empty lists", st)
	}
	if sum := e.s.Summary(); sum != nil {
		t.Errorf("summary %+v, want nil", sum)
	}
	if n, calls := len(e.srv.Requests()), e.gh.callsLike(""); n != 0 || len(calls) != 0 {
		t.Errorf("%d S3 requests and GitHub calls %v, want none", n, calls)
	}
	if err := e.s.Refresh(); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Refresh = %v, want ErrNotConfigured", err)
	}
}

func TestKeyProblemsStopTheListing(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string // the key file's content; empty writes only a job key
		want string
	}{
		{"no admin key", "", "no admin key set"},
		{"unreadable key file", "{", "cannot read the artifact store keys: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			if tc.file == "" {
				e.setKeys(Keys{Job: &Key{ID: jobID, Secret: jobSecret, SetAt: e.clock.Now()}})
			} else if err := os.WriteFile(e.s.KeysPath, []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
			e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
			e.s.pass()
			st := e.s.Snapshot()
			if !st.Configured || !strings.HasPrefix(st.Error, tc.want) || st.CheckedAt != nil || st.AdminKey != nil {
				t.Fatalf("snapshot %+v, want error %q and no listing", st, tc.want)
			}
			if sum := e.s.Summary(); sum == nil || sum.Error != st.Error {
				t.Errorf("summary %+v, want the snapshot's error", sum)
			}
			if n := len(e.srv.Requests()); n != 0 {
				t.Errorf("%d S3 requests without an admin key, want 0", n)
			}
		})
	}
}

func TestInventoryBuildsTheRows(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.WatchRepos = []string{"homelab", "quiet"}
	e.cfg.ArtifactStore.CapacityGB = 50
	e.cfg.ArtifactStore.Repos = map[string]config.ArtifactRepo{"DarkMem": {Enabled: true}, "homelab": {Cleanup: "off"}, "old-repo": {Enabled: true}}
	now := e.clock.Now()
	expires := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	e.put("darkraise/darkmem/10/dist.zip", 300, 2*time.Hour, artifact.Meta{RunID: 10, Attempt: 2, Workflow: "ci", Job: "build",
		SHA: "abc123", Ref: "master", Files: 3, Digest: "sha256:aa", Expires: &expires, Uploader: "ghr-artifact/v1"}.Map())
	e.putArtifact("darkmem", 11, "logs", 200, time.Hour)
	e.putArtifact("homelab", 5, "site", 600, 3*time.Hour)
	e.putArtifact("old-repo", 7, "x", 100, 5*time.Hour)
	e.put("someone/else/1/a.zip", 40, time.Hour, nil)
	e.put("darkraise/notes.txt", 10, time.Hour, nil)

	v := e.s.Version()
	e.s.pass()
	st := e.s.Snapshot()
	if st.CheckedAt == nil || !st.CheckedAt.Equal(now) || e.s.Version() == v || st.Error != "" {
		t.Fatalf("checked %v, error %q, version %d → %d", st.CheckedAt, st.Error, v, e.s.Version())
	}
	if st.Endpoint != e.srv.URL() || st.Bucket != "gha" || st.Region != config.DefaultArtifactRegion {
		t.Errorf("connection %q %q %q", st.Endpoint, st.Bucket, st.Region)
	}
	if st.AdminKey == nil || st.AdminKey.ID != ShortID(adminID) || st.JobKey == nil || st.JobKey.ID != ShortID(jobID) {
		t.Errorf("keys %+v %+v, want the shortened IDs", st.AdminKey, st.JobKey)
	}
	if st.UsedBytes != 1250 || st.OtherBytes != 50 || st.Count != 4 || st.CapacityBytes != 50_000_000_000 {
		t.Errorf("used %d, other %d, count %d, capacity %d", st.UsedBytes, st.OtherBytes, st.Count, st.CapacityBytes)
	}
	var names []string
	for _, r := range st.Repos {
		names = append(names, r.Name)
	}
	if want := []string{"homelab", "darkmem", "old-repo", "quiet"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rows %v, want %v (largest first, then by name)", names, want)
	}
	dm := row(t, st, "darkmem")
	if !dm.Enabled || dm.Cleanup != "7d" || dm.CleanupSource != "default" || dm.Bytes != 500 || dm.Count != 2 || len(dm.Items) != 2 {
		t.Fatalf("darkmem row %+v", dm)
	}
	if dm.Items[0].Key != "darkraise/darkmem/11/logs.zip" || dm.Items[0].Name != "logs" || dm.Items[0].RunID != 11 {
		t.Errorf("newest item %+v", dm.Items[0])
	}
	it := dm.Items[1]
	if it.Key != "darkraise/darkmem/10/dist.zip" || it.Name != "dist" || it.RunID != 10 || it.Attempt != 2 || it.Workflow != "ci" ||
		it.Job != "build" || it.Branch != "master" || it.SHA != "abc123" || it.Bytes != 300 || it.Files != 3 || it.Digest != "sha256:aa" ||
		!it.UploadedAt.Equal(now.Add(-2*time.Hour)) || it.ExpiresAt == nil || !it.ExpiresAt.Equal(expires) {
		t.Errorf("item %+v", it)
	}
	hl := row(t, st, "homelab")
	if hl.Enabled || hl.State != "off" || hl.Cleanup != "off" || hl.CleanupSource != "override" || hl.Count != 1 {
		t.Errorf("homelab row %+v", hl)
	}
	if q := row(t, st, "quiet"); q.Count != 0 || q.Items == nil || q.State != "off" {
		t.Errorf("quiet row %+v", q)
	}
	if o := row(t, st, "old-repo"); o.Enabled || o.State != "off" {
		t.Errorf("old-repo row %+v: an entry for an uncovered repository is ignored", o)
	}
	if n := e.heads(); n != 4 {
		t.Errorf("%d heads, want one per artifact of the owner", n)
	}
	sum := e.s.Summary()
	if sum == nil || sum.UsedBytes != 1250 || sum.CapacityBytes != 50_000_000_000 || sum.ReposOn != 1 || sum.Error != "" {
		t.Errorf("summary %+v", sum)
	}
}

func TestTheSnapshotKeepsTheNewestItems(t *testing.T) {
	e := newTestEnv(t)
	for i := range MaxItems + 2 {
		e.putArtifact("darkmem", int64(i+1), "dist", 10, time.Duration(i)*time.Minute)
	}
	e.s.pass()
	st := e.s.Snapshot()
	dm := row(t, st, "darkmem")
	if dm.Count != MaxItems+2 || dm.Bytes != 10*(MaxItems+2) || len(dm.Items) != MaxItems || st.Count != MaxItems+2 {
		t.Fatalf("count %d, bytes %d, %d items; want every object counted and %d items", dm.Count, dm.Bytes, len(dm.Items), MaxItems)
	}
	if dm.Items[0].RunID != 1 || dm.Items[MaxItems-1].RunID != MaxItems {
		t.Fatalf("items run from run %d to %d, want the newest, 1 to %d", dm.Items[0].RunID, dm.Items[MaxItems-1].RunID, MaxItems)
	}
}

func cacheKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f cacheFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("cache file %s: %v", b, err)
	}
	keys := []string{}
	for _, c := range f.Entries {
		keys = append(keys, c.Key)
	}
	return keys
}

func TestTheMetadataCacheSavesHeads(t *testing.T) {
	e := newTestEnv(t)
	a := e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	b := e.putArtifact("darkmem", 11, "logs", 5, time.Hour)
	e.s.pass()
	e.s.pass()
	if n := e.heads(); n != 2 {
		t.Fatalf("%d heads after two passes, want 2: the second reads the cache", n)
	}
	e.clock.Add(time.Minute)
	e.put(a, 7, 0, artifact.Meta{RunID: 10, Attempt: 2}.Map())
	e.s.pass()
	if n := e.heads(); n != 3 {
		t.Fatalf("%d heads after a changed object, want 3", n)
	}
	if it := row(t, e.s.Snapshot(), "darkmem").Items[0]; it.Key != a || it.Attempt != 2 || it.Bytes != 7 {
		t.Fatalf("changed item %+v, want attempt 2 of 7 bytes", it)
	}
	bucket, err := e.s.bucket(e.cfg.ArtifactStore, &Key{ID: adminID, Secret: adminSecret})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Delete(context.Background(), []string{b}); err != nil {
		t.Fatal(err)
	}
	e.s.pass()
	if got := cacheKeys(t, e.s.CachePath); !reflect.DeepEqual(got, []string{a}) {
		t.Fatalf("cache keys %v, want only %s: an unlisted key leaves the cache", got, a)
	}

	restarted := e.newService()
	restarted.pass()
	if n := e.heads(); n != 3 {
		t.Fatalf("%d heads after a restart, want still 3: the cache file survives", n)
	}
	if it := row(t, restarted.Snapshot(), "darkmem").Items[0]; it.Attempt != 2 {
		t.Fatalf("item after a restart %+v, want its cached attempt", it)
	}
}

func TestAnUnreadableCacheIsRebuilt(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{"not JSON", "{"},
		{"another version", `{"version":9,"entries":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			a := e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
			if err := os.WriteFile(e.s.CachePath, []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
			e.s.pass()
			if n := e.heads(); n != 1 {
				t.Fatalf("%d heads, want the object read again", n)
			}
			if got := eventsLike(e.s.Events, "artifact store: discarded the metadata cache"); len(got) != 1 {
				t.Fatalf("events %q, want one warning", eventsLike(e.s.Events, ""))
			}
			if got := cacheKeys(t, e.s.CachePath); !reflect.DeepEqual(got, []string{a}) {
				t.Fatalf("rebuilt cache keys %v", got)
			}
		})
	}
}

func TestAFailedHeadIsReadAgain(t *testing.T) {
	e := newTestEnv(t)
	e.put("darkraise/darkmem/10/dist.zip", 5, time.Hour, artifact.Meta{RunID: 10, Attempt: 3}.Map())
	e.srv.FailNext("get", 500, "InternalError")
	e.s.pass()
	it := row(t, e.s.Snapshot(), "darkmem").Items[0]
	if it.RunID != 10 || it.Attempt != 0 {
		t.Fatalf("item %+v, want listed from its key without details", it)
	}
	e.s.pass()
	if it := row(t, e.s.Snapshot(), "darkmem").Items[0]; it.Attempt != 3 || e.heads() != 2 {
		t.Fatalf("item %+v after %d heads, want the details read on the next pass", it, e.heads())
	}
}

func TestAListingErrorKeepsTheLastListing(t *testing.T) {
	e := newTestEnv(t)
	e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	e.s.pass()
	listed := e.clock.Now()
	e.clock.Add(InventoryInterval)
	e.srv.FailNext("list", 503, "SlowDown")
	e.s.pass()
	st := e.s.Snapshot()
	if !strings.HasPrefix(st.Error, "cannot list bucket gha: ") || !strings.Contains(st.Error, "SlowDown") {
		t.Fatalf("error %q, want the listing's", st.Error)
	}
	if st.CheckedAt == nil || !st.CheckedAt.Equal(listed) || row(t, st, "darkmem").Count != 1 {
		t.Fatalf("snapshot %+v, want the last listing and its time", st)
	}
	if sum := e.s.Summary(); sum.Error != st.Error || sum.UsedBytes != 5 {
		t.Errorf("summary %+v", sum)
	}
	e.s.pass()
	if st := e.s.Snapshot(); st.Error != "" || !st.CheckedAt.Equal(e.clock.Now()) {
		t.Fatalf("after a good listing: error %q, checked %v", st.Error, st.CheckedAt)
	}
}

func TestRefreshIsRefusedWhileAPassRuns(t *testing.T) {
	e := newTestEnv(t)
	bb := e.block()
	done := make(chan struct{})
	go func() { e.s.pass(); close(done) }()
	<-bb.entered
	if err := e.s.Refresh(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Refresh during a pass = %v, want ErrBusy", err)
	}
	close(bb.release)
	<-done
	if err := e.s.Refresh(); err != nil || len(e.s.kick) != 1 {
		t.Fatalf("Refresh after the pass = %v with %d kicks queued, want nil and 1", err, len(e.s.kick))
	}
}

func TestCloseStopsAPassUnderWay(t *testing.T) {
	e := newTestEnv(t)
	e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	bb := e.block()
	e.s.Start()
	<-bb.entered // Start runs the first pass at once
	e.s.Close()
	stopped := make(chan struct{})
	go func() { e.s.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	if st := e.s.Snapshot(); st.CheckedAt != nil || st.Error != "" {
		t.Fatalf("a cancelled pass wrote its result: %+v", st)
	}
	if err := e.s.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Refresh after Close = %v, want ErrClosed", err)
	}
}

func TestSetKey(t *testing.T) {
	for _, tc := range []struct{ name, which, id, secret string }{
		{"unknown key", "root", "NEWKEY000001", "s"},
		{"empty id", "job", " ", "s"},
		{"secret on two lines", "admin", "NEWKEY000001", "a\nb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			var bad BadInput
			if err := e.s.SetKey(tc.which, tc.id, tc.secret); !errors.As(err, &bad) {
				t.Fatalf("SetKey = %v, want a BadInput", err)
			}
			if k, _ := LoadKeys(e.s.KeysPath); k.Job.ID != jobID || k.Admin.ID != adminID {
				t.Fatalf("keys %+v changed", k)
			}
		})
	}

	e := newTestEnv(t)
	e.s.loadKeys()
	if err := e.s.SetKey("job", " NEWJOBKEY0002 ", " new-job-secret\n"); err != nil {
		t.Fatal(err)
	}
	k, err := LoadKeys(e.s.KeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if k.Job.ID != "NEWJOBKEY0002" || k.Job.Secret != "new-job-secret" || !k.Job.SetAt.Equal(e.clock.Now()) || k.Admin.ID != adminID {
		t.Fatalf("keys %+v %+v", k.Admin, k.Job)
	}
	st := e.s.Snapshot()
	if st.JobKey == nil || st.JobKey.ID != ShortID("NEWJOBKEY0002") || len(e.s.kick) != 1 {
		t.Fatalf("job key %+v with %d kicks, want the new ID and a pass asked for", st.JobKey, len(e.s.kick))
	}
	b, _ := json.Marshal(st)
	for _, secret := range []string{"new-job-secret", adminSecret, jobSecret} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("the snapshot holds a secret: %s", b)
		}
	}
	if got := eventsLike(e.s.Events, "artifact store: job key set"); len(got) != 1 || got[0] != "artifact store: job key set ("+ShortID("NEWJOBKEY0002")+")" {
		t.Fatalf("events %q", eventsLike(e.s.Events, ""))
	}
}

func TestKeysChangedClearsTheListingError(t *testing.T) {
	e := newTestEnv(t)
	e.srv.FailNext("list", 403, "InvalidAccessKeyId")
	e.s.pass()
	if e.s.Snapshot().Error == "" {
		t.Fatal("no listing error")
	}
	v := e.s.Version()
	e.s.KeysChanged()
	if st := e.s.Snapshot(); st.Error != "" || len(e.s.kick) != 1 || e.s.Version() == v {
		t.Fatalf("error %q, %d kicks, version %d → %d", st.Error, len(e.s.kick), v, e.s.Version())
	}
}

func TestPolicyChanged(t *testing.T) {
	e := newTestEnv(t)
	e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	e.s.pass()
	v := e.s.Version()
	e.cfg.ArtifactStore.CleanupMaxAge = "off"
	e.s.PolicyChanged()
	if r := row(t, e.s.Snapshot(), "darkmem"); r.Cleanup != "off" || r.CleanupSource != "global" || e.s.Version() == v || len(e.s.kick) != 0 {
		t.Fatalf("cleanup %s %s, version %d → %d, %d kicks; want the policy shown without a pass", r.Cleanup, r.CleanupSource, v, e.s.Version(), len(e.s.kick))
	}
	e.cfg.ArtifactStore.Bucket = "other"
	e.s.PolicyChanged()
	st := e.s.Snapshot()
	if st.CheckedAt != nil || st.UsedBytes != 0 || row(t, st, "darkmem").Count != 0 || len(e.s.kick) != 1 {
		t.Fatalf("snapshot %+v with %d kicks, want the other bucket's listing hidden and a pass asked for", st, len(e.s.kick))
	}
}

func TestOnlyTheOwnersArtifactsAreHeaded(t *testing.T) {
	e := newTestEnv(t)
	e.put("someone/else/1/a.zip", 4, time.Hour, nil)
	e.put("darkraise/darkmem/notes/readme.md", 4, time.Hour, nil)
	e.s.pass()
	st := e.s.Snapshot()
	if n := e.heads(); n != 0 || st.OtherBytes != 8 || st.Count != 0 {
		t.Fatalf("%d heads, other %d, count %d; want other prefixes only counted", n, st.OtherBytes, st.Count)
	}
	if !slices.Equal(cacheKeys(t, e.s.CachePath), []string{}) {
		t.Fatalf("cache %v, want empty", cacheKeys(t, e.s.CachePath))
	}
}
