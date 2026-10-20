package artifactstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

func TestDeleteObjects(t *testing.T) {
	e := newTestEnv(t)
	e.cfg.WatchRepos = []string{"homelab"}
	now := e.clock.Now()
	a := e.putArtifact("darkmem", 10, "dist", 5, 2*time.Hour)
	b := e.putArtifact("darkmem", 11, "logs", 7, time.Hour)
	c := e.putArtifact("homelab", 5, "site", 9, time.Hour)
	e.s.pass()
	missing := "darkraise/darkmem/99/nope.zip"
	id, err := e.s.Delete("DarkMem", []string{a, a, missing})
	if err != nil {
		t.Fatal(err)
	}
	e.s.Wait()
	st := e.s.Snapshot()
	want := &model.GitHubOperation{ID: id, Kind: "delete-objects", Repo: "darkmem", Total: 2, Done: 2, Bytes: 5,
		Failed: []model.OperationFailure{}, StartedAt: now, FinishedAt: &now}
	if !reflect.DeepEqual(st.Operation, want) {
		t.Errorf("operation %+v, want %+v", st.Operation, want)
	}
	if want := []model.Deletion{{At: now, Repo: "darkmem", Kind: "artifact", Name: "dist", Bytes: 5, Age: "2h", By: "manual", Store: "rustfs"}}; !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v", st.Deletions)
	}
	if got := e.srv.Keys(); !reflect.DeepEqual(got, []string{b, c}) {
		t.Errorf("keys left %v", got)
	}
	if r := row(t, st, "darkmem"); r.Count != 1 || r.Items[0].Key != b {
		t.Errorf("darkmem row %+v, want only %s", r, b)
	}
	if got := eventsLike(e.s.Events, "artifact store: deleted"); !reflect.DeepEqual(got, []string{"artifact store: deleted 1 artifacts (5 B)"}) {
		t.Errorf("events %q", got)
	}
}

func TestDeleteFailures(t *testing.T) {
	e := newTestEnv(t)
	a := e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	e.s.pass()
	e.srv.FailNext("delete-objects", 403, "AccessDenied")
	id, err := e.s.Delete("darkmem", []string{a})
	if err != nil {
		t.Fatal(err)
	}
	e.s.Wait()
	op := e.s.Snapshot().Operation
	if op.ID != id || op.Done != 0 || len(op.Failed) != 1 || !strings.HasPrefix(op.Failed[0].Error, "cannot delete from bucket gha: ") ||
		!strings.Contains(op.Failed[0].Error, "AccessDenied") || op.FinishedAt == nil {
		t.Fatalf("operation %+v", op)
	}
	if got := eventsLike(e.s.Events, "artifact store: deleted"); !reflect.DeepEqual(got, []string{"artifact store: deleted 0 artifacts (0 B); 1 failed"}) {
		t.Fatalf("events %q", got)
	}
	if len(e.srv.Keys()) != 1 || row(t, e.s.Snapshot(), "darkmem").Count != 1 {
		t.Fatal("a refused delete removed the object")
	}
}

func TestDeleteRefusals(t *testing.T) {
	many := make([]string, maxDeleteKeys+1)
	for i := range many {
		many[i] = fmt.Sprintf("darkraise/darkmem/%d/dist.zip", i+1)
	}
	for _, tc := range []struct {
		name string
		set  func(e *testEnv)
		keys []string
		bad  bool  // a BadInput
		err  error // otherwise this error
	}{
		{"another repository's key", nil, []string{"darkraise/homelab/5/site.zip"}, true, nil},
		{"another owner's key", nil, []string{"someone/darkmem/5/site.zip"}, true, nil},
		{"a .. segment", nil, []string{"darkraise/darkmem/../homelab/5/site.zip"}, true, nil},
		{"an empty segment", nil, []string{"darkraise/darkmem//site.zip"}, true, nil},
		{"no keys", nil, nil, true, nil},
		{"too many keys", nil, many, true, nil},
		{"not set up", func(e *testEnv) { e.cfg.ArtifactStore = config.ArtifactStore{} }, []string{"darkraise/darkmem/5/site.zip"}, false, ErrNotConfigured},
		{"no admin key", func(e *testEnv) { e.setKeys(Keys{}) }, []string{"darkraise/darkmem/5/site.zip"}, false, ErrNoAdminKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			if tc.set != nil {
				tc.set(e)
			}
			_, err := e.s.Delete("darkmem", tc.keys)
			var bad BadInput
			if tc.bad && !errors.As(err, &bad) || !tc.bad && !errors.Is(err, tc.err) {
				t.Fatalf("Delete = %v, want a BadInput %v or %v", err, tc.bad, tc.err)
			}
			if op := e.s.Snapshot().Operation; op != nil || len(e.srv.Requests()) != 0 {
				t.Fatalf("operation %+v and requests %v after a refusal", op, e.srv.Requests())
			}
		})
	}
}

// heldDelete makes the bucket's deletes wait until release is closed, and
// closes entered when the first one starts waiting.
type heldDelete struct {
	Bucket
	entered, release chan struct{}
}

func (h *heldDelete) Delete(ctx context.Context, keys []string) ([]string, error) {
	close(h.entered)
	select {
	case <-h.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return h.Bucket.Delete(ctx, keys)
}

func TestASecondOperationIsRefused(t *testing.T) {
	e := newTestEnv(t)
	a := e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	e.s.pass()
	h := &heldDelete{entered: make(chan struct{}), release: make(chan struct{})}
	inner := e.s.NewBucket
	e.s.NewBucket = func(st config.ArtifactStore, cred s3.Credentials) (Bucket, error) {
		b, err := inner(st, cred)
		h.Bucket = b
		return h, err
	}
	if _, err := e.s.Delete("darkmem", []string{a}); err != nil {
		t.Fatal(err)
	}
	<-h.entered
	if _, err := e.s.Delete("darkmem", []string{a}); !errors.Is(err, ErrOperationRunning) {
		t.Fatalf("a second Delete = %v, want ErrOperationRunning", err)
	}
	close(h.release)
	e.s.Wait()
	if op := e.s.Snapshot().Operation; op.FinishedAt == nil || op.Done != 1 {
		t.Fatalf("operation %+v", op)
	}
}

func TestADeleteIsNotUndoneByAListingUnderWay(t *testing.T) {
	e := newTestEnv(t)
	a := e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
	b := e.putArtifact("darkmem", 11, "logs", 5, time.Hour)
	e.s.pass()
	bb := e.block()
	done := make(chan struct{})
	go func() { e.s.pass(); close(done) }()
	<-bb.entered
	if _, err := e.s.Delete("darkmem", []string{a}); err != nil {
		t.Fatal(err)
	}
	e.s.Wait()
	close(bb.release)
	<-done
	if r := row(t, e.s.Snapshot(), "darkmem"); r.Count != 1 || r.Items[0].Key != b {
		t.Fatalf("items %+v, want only %s: the listing still held %s", r.Items, b, a)
	}
	e.clock.Add(time.Minute)
	e.putArtifact("darkmem", 10, "dist", 6, 0)
	e.s.pass()
	if r := row(t, e.s.Snapshot(), "darkmem"); r.Count != 2 {
		t.Fatalf("items %+v, want %s back once uploaded again", r.Items, a)
	}
}

func TestLink(t *testing.T) {
	e := newTestEnv(t)
	key := "darkraise/darkmem/10/dist.zip"
	link, err := e.s.Link(key, "24h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link.URL, e.srv.URL()+"/gha/"+key+"?") || !strings.Contains(link.URL, "X-Amz-Expires=86400") ||
		!link.ExpiresAt.Equal(e.clock.Now().Add(24*time.Hour)) {
		t.Fatalf("link %+v", link)
	}
	if _, err := e.s.Link("darkraise/homelab/5/site.zip", "7d"); err != nil {
		t.Fatalf("a link to another of the owner's repositories: %v", err)
	}
	for _, tc := range []struct {
		name, key, ttl string
		set            func(e *testEnv)
		bad            bool
		err            error
	}{
		{"another ttl", key, "2h", nil, true, nil},
		{"another owner's key", "someone/else/1/a.zip", "1h", nil, true, nil},
		{"a .. segment", "darkraise/../someone/1/a.zip", "1h", nil, true, nil},
		{"not set up", key, "1h", func(e *testEnv) { e.cfg.ArtifactStore = config.ArtifactStore{} }, false, ErrNotConfigured},
		{"no admin key", key, "1h", func(e *testEnv) { e.setKeys(Keys{}) }, false, ErrNoAdminKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			if tc.set != nil {
				tc.set(e)
			}
			_, err := e.s.Link(tc.key, tc.ttl)
			var bad BadInput
			if tc.bad && !errors.As(err, &bad) || !tc.bad && !errors.Is(err, tc.err) {
				t.Fatalf("Link = %v, want a BadInput %v or %v", err, tc.bad, tc.err)
			}
		})
	}
}

func TestTestChecks(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(e *testEnv)
		want []model.ArtifactStoreCheck // a Detail is a prefix
	}{
		{"all good", func(e *testEnv) { e.putArtifact("darkmem", 10, "dist", 5, time.Hour) }, []model.ArtifactStoreCheck{
			{Name: "reach", OK: true, Detail: "reached https://"},
			{Name: "list", OK: true, Detail: "listed 1 objects in bucket gha"},
			{Name: "head", OK: true, Detail: "read the details of darkraise/darkmem/10/dist.zip"}}},
		{"an empty bucket", func(e *testEnv) {}, []model.ArtifactStoreCheck{
			{Name: "reach", OK: true, Detail: "reached "},
			{Name: "list", OK: true, Detail: "listed 0 objects in bucket gha"},
			{Name: "head", OK: true, Detail: "no artifacts stored yet, so none was read"}}},
		{"the listing is refused", func(e *testEnv) { e.srv.FailNext("list", 403, "SignatureDoesNotMatch") }, []model.ArtifactStoreCheck{
			{Name: "reach", OK: true, Detail: "reached "},
			{Name: "list", OK: false, Detail: "cannot list bucket gha: "},
			{Name: "head", OK: false, Detail: "not run: the listing failed"}}},
		{"the read fails", func(e *testEnv) {
			e.putArtifact("darkmem", 10, "dist", 5, time.Hour)
			e.srv.FailNext("head", 500, "InternalError")
		}, []model.ArtifactStoreCheck{
			{Name: "reach", OK: true, Detail: "reached "},
			{Name: "list", OK: true, Detail: "listed 1 objects"},
			{Name: "head", OK: false, Detail: "cannot read the details of darkraise/darkmem/10/dist.zip: "}}},
		{"the endpoint is down", func(e *testEnv) {
			dead := httptest.NewTLSServer(http.NotFoundHandler())
			dead.Close()
			e.cfg.ArtifactStore.Endpoint = dead.URL
		}, []model.ArtifactStoreCheck{
			{Name: "reach", OK: false, Detail: "cannot reach https://"},
			{Name: "list", OK: false, Detail: "not run: the endpoint cannot be reached"},
			{Name: "head", OK: false, Detail: "not run: the endpoint cannot be reached"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			tc.set(e)
			got, err := e.s.Test(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Checks) != len(tc.want) {
				t.Fatalf("checks %+v", got.Checks)
			}
			for i, c := range got.Checks {
				w := tc.want[i]
				if c.Name != w.Name || c.OK != w.OK || !strings.HasPrefix(c.Detail, w.Detail) {
					t.Errorf("check %d = %+v, want %+v", i, c, w)
				}
			}
		})
	}
	e := newTestEnv(t)
	e.setKeys(Keys{})
	if _, err := e.s.Test(context.Background()); !errors.Is(err, ErrNoAdminKey) {
		t.Errorf("without an admin key: %v, want ErrNoAdminKey", err)
	}
}

func TestRunObjects(t *testing.T) {
	e := newTestEnv(t)
	dist := e.putArtifact("darkmem", 10, "dist", 5, 2*time.Hour)
	logs := e.putArtifact("darkmem", 10, "logs", 7, time.Hour)
	e.putArtifact("darkmem", 100, "other-run", 5, time.Hour)
	e.putArtifact("homelab", 10, "same-run-id", 5, time.Hour)
	e.put("darkraise/darkmem/10/sub/x.zip", 5, time.Hour, nil)
	items, err := e.s.RunObjects(context.Background(), "DarkMem", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Key != logs || items[1].Key != dist || items[0].Attempt != 1 || items[0].Bytes != 7 {
		t.Fatalf("items %+v, want logs then dist with their details", items)
	}
	if n := e.heads(); n != 2 {
		t.Fatalf("%d heads before any pass, want 2", n)
	}
	e.s.pass()
	heads := e.heads()
	if _, err := e.s.RunObjects(context.Background(), "darkmem", 10); err != nil || e.heads() != heads {
		t.Fatalf("RunObjects after a pass: %v with %d heads, want the cache used", err, e.heads()-heads)
	}
	if items, err := e.s.RunObjects(context.Background(), "darkmem", 11); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("a run without artifacts: %v, %v; want an empty list", items, err)
	}
	for _, tc := range []struct {
		name string
		repo string
		run  int64
	}{{"run 0", "darkmem", 0}, {"a slash", "darkmem/x", 10}, {"dot dot", "..", 10}, {"empty", "", 10}} {
		var bad BadInput
		if _, err := e.s.RunObjects(context.Background(), tc.repo, tc.run); !errors.As(err, &bad) {
			t.Errorf("%s: %v, want a BadInput", tc.name, err)
		}
	}
	e.srv.FailNext("list", 500, "InternalError")
	if _, err := e.s.RunObjects(context.Background(), "darkmem", 10); err == nil || !strings.Contains(err.Error(), "InternalError") {
		t.Errorf("a failed listing: %v", err)
	}
	e.cfg.ArtifactStore = config.ArtifactStore{}
	if _, err := e.s.RunObjects(context.Background(), "darkmem", 10); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("not set up: %v, want ErrNotConfigured", err)
	}
}
