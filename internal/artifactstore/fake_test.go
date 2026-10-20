package artifactstore

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
	"github.com/darkraise/ghr/internal/s3/s3test"
)

// fakeGH is a GitHub whose answers the test sets. Calls records every call
// as "Method repo [name]". Repository keys are lower-case.
type fakeGH struct {
	mu        sync.Mutex
	repos     []github.UserRepo
	reposErr  error
	vars      map[string]string                  // repo → GHR_ARTIFACT_STORE value
	secrets   map[string]string                  // repo → GHR_ARTIFACT_KEY value
	readErr   map[string]error                   // repo → the answer of every read of its variable and secret
	writeErr  map[string]error                   // repo → the answer of every write to its variable and secret
	runs      map[string]map[string][]github.Run // repo → status → runs
	runsErr   map[string]error
	remaining int
	calls     []string
}

func newFakeGH() *fakeGH {
	return &fakeGH{
		remaining: -1,
		vars:      map[string]string{}, secrets: map[string]string{},
		readErr: map[string]error{}, writeErr: map[string]error{},
		runs: map[string]map[string][]github.Run{}, runsErr: map[string]error{},
	}
}

func (f *fakeGH) call(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

// callsLike returns the recorded calls that start with prefix.
func (f *fakeGH) callsLike(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// addRepo makes repo one of the owner's repositories.
func (f *fakeGH) addRepo(repo string, private bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos = append(f.repos, github.UserRepo{Name: repo, Private: private, Owner: github.Account{Login: "darkraise"}})
}

func (f *fakeGH) ListUserRepos(context.Context) ([]github.UserRepo, error) {
	f.call("ListUserRepos")
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.repos), f.reposErr
}

func (f *fakeGH) ListRuns(_ context.Context, repo, status string) ([]github.Run, error) {
	f.call("ListRuns %s %s", repo, status)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.runsErr[strings.ToLower(repo)]; err != nil {
		return nil, err
	}
	return slices.Clone(f.runs[strings.ToLower(repo)][status]), nil
}

func (f *fakeGH) GetVariable(_ context.Context, repo, name string) (string, bool, error) {
	f.call("GetVariable %s %s", repo, name)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(repo)
	if err := f.readErr[key]; err != nil {
		return "", false, err
	}
	v, ok := f.vars[key]
	return v, ok, nil
}

func (f *fakeGH) SetVariable(_ context.Context, repo, name, value string) error {
	f.call("SetVariable %s %s", repo, name)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(repo)
	if err := f.writeErr[key]; err != nil {
		return err
	}
	f.vars[key] = value
	return nil
}

func (f *fakeGH) DeleteVariable(_ context.Context, repo, name string) (bool, error) {
	f.call("DeleteVariable %s %s", repo, name)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(repo)
	if err := f.writeErr[key]; err != nil {
		return false, err
	}
	_, ok := f.vars[key]
	delete(f.vars, key)
	return ok, nil
}

func (f *fakeGH) SecretExists(_ context.Context, repo, name string) (bool, error) {
	f.call("SecretExists %s %s", repo, name)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(repo)
	if err := f.readErr[key]; err != nil {
		return false, err
	}
	_, ok := f.secrets[key]
	return ok, nil
}

func (f *fakeGH) SetSecret(_ context.Context, repo, name, value string) error {
	f.call("SetSecret %s %s", repo, name)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(repo)
	if err := f.writeErr[key]; err != nil {
		return err
	}
	f.secrets[key] = value
	return nil
}

func (f *fakeGH) DeleteSecret(_ context.Context, repo, name string) (bool, error) {
	f.call("DeleteSecret %s %s", repo, name)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(repo)
	if err := f.writeErr[key]; err != nil {
		return false, err
	}
	_, ok := f.secrets[key]
	delete(f.secrets, key)
	return ok, nil
}

func (f *fakeGH) RateRemaining() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remaining
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

const (
	adminID     = "ADMINKEYID01"
	adminSecret = "admin-secret-value"
	jobID       = "JOBKEYID0001"
	jobSecret   = "job-secret-value"
)

// testEnv is a service on a fake S3 server and a fake GitHub. The config
// covers darkmem, a private repository of the owner, and sets up the store;
// the key file holds an admin and a job key.
type testEnv struct {
	t     *testing.T
	s     *Service
	gh    *fakeGH
	srv   *s3test.Server
	clock *fakeClock
	cfg   *config.Config
	dir   string
}

// newTestEnv builds the environment. Its service runs no loop: the test runs
// passes with e.s.pass(). The clock starts at 2026-10-15 12:00 UTC.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	e := &testEnv{t: t, gh: newFakeGH(), srv: s3test.NewServer(t, "gha"), dir: t.TempDir(),
		clock: &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}}
	e.cfg = &config.Config{Owner: "darkraise", Repos: []config.Repo{{Name: "darkmem"}},
		ArtifactStore: config.ArtifactStore{Endpoint: e.srv.URL(), Bucket: "gha"}}
	e.gh.addRepo("darkmem", true)
	e.setKeys(Keys{Admin: &Key{ID: adminID, Secret: adminSecret, SetAt: e.clock.Now()},
		Job: &Key{ID: jobID, Secret: jobSecret, SetAt: e.clock.Now()}})
	e.s = e.newService()
	return e
}

// newService builds another service on the same files, server, GitHub and
// clock, as a daemon restart would.
func (e *testEnv) newService() *Service {
	ev := events.New()
	ev.Now = e.clock.Now
	s := &Service{GH: e.gh, Config: func() *config.Config { return e.cfg }, Events: ev,
		KeysPath: filepath.Join(e.dir, "keys"), CachePath: filepath.Join(e.dir, "meta.json"), Now: e.clock.Now,
		NewBucket: func(a config.ArtifactStore, cred s3.Credentials) (Bucket, error) {
			c, err := s3.New(a.Endpoint, a.Bucket, a.RegionOrDefault(), cred)
			if err != nil {
				return nil, err
			}
			c.HTTP, c.Now = e.srv.HTTPClient(), e.clock.Now
			return c, nil
		}}
	s.init()
	e.t.Cleanup(func() { s.Close(); s.Wait() })
	return s
}

func (e *testEnv) setKeys(k Keys) {
	e.t.Helper()
	if err := SaveKeys(filepath.Join(e.dir, "keys"), k); err != nil {
		e.t.Fatal(err)
	}
}

// put stores an object of size bytes written age ago.
func (e *testEnv) put(key string, size int, age time.Duration, meta map[string]string) {
	e.srv.Put(key, s3test.Object{Body: bytes.Repeat([]byte("x"), size), Meta: meta, ContentType: "application/zip",
		LastModified: e.clock.Now().Add(-age)})
}

// putArtifact stores darkraise/<repo>/<run>/<name>.zip with the metadata an
// upload of that run writes.
func (e *testEnv) putArtifact(repo string, run int64, name string, size int, age time.Duration) string {
	key := artifact.ObjectKey("darkraise", repo, run, name)
	e.put(key, size, age, artifact.Meta{RunID: run, Attempt: 1, Files: 1, Uploader: "ghr-artifact/test"}.Map())
	return key
}

// requests counts the S3 requests whose "METHOD /path?query" starts with
// prefix.
func (e *testEnv) requests(prefix string) int {
	n := 0
	for _, r := range e.srv.Requests() {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// heads counts the object detail reads. The s3 client makes them as GETs of
// one object whose body it never reads, so they are the GETs whose path goes
// below the bucket.
func (e *testEnv) heads() int { return e.requests("GET /gha/") }

// row returns the snapshot's row for repo, failing the test without one.
func row(t *testing.T, st model.ArtifactStore, repo string) model.ArtifactStoreRepo {
	t.Helper()
	for _, r := range st.Repos {
		if r.Name == repo {
			return r
		}
	}
	t.Fatalf("no row for %s in %+v", repo, st.Repos)
	return model.ArtifactStoreRepo{}
}

// eventsLike returns the messages of the events whose text contains sub.
func eventsLike(ev *events.Ring, sub string) []string {
	out := []string{}
	for _, e := range ev.After(0) {
		if strings.Contains(e.Msg, sub) {
			out = append(out, e.Msg)
		}
	}
	return out
}

// blockingBucket's first List lists, then holds its answer until release is
// closed, and closes entered when it starts waiting: a listing under way.
type blockingBucket struct {
	Bucket
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *blockingBucket) List(ctx context.Context, prefix string) ([]s3.Object, error) {
	objs, err := b.Bucket.List(ctx, prefix)
	first := false
	b.once.Do(func() { first = true })
	if first {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return objs, err
}

// block makes the service's next listing wait; see blockingBucket.
func (e *testEnv) block() *blockingBucket {
	bb := &blockingBucket{entered: make(chan struct{}), release: make(chan struct{})}
	inner := e.s.NewBucket
	e.s.NewBucket = func(a config.ArtifactStore, cred s3.Credentials) (Bucket, error) {
		b, err := inner(a, cred)
		if err != nil {
			return nil, err
		}
		bb.Bucket = b
		return bb, nil
	}
	return bb
}
