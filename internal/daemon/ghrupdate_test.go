package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/selfupdate"
)

// ghrReleases is a fake darkraise/ghr whose releases/latest redirects to
// tag, or answers 502 while tag is empty.
type ghrReleases struct {
	mu    sync.Mutex
	tag   string
	calls atomic.Int64
}

func (g *ghrReleases) set(tag string) {
	g.mu.Lock()
	g.tag = tag
	g.mu.Unlock()
}

// serveGhrReleases points b's ghr release check at a fake repository.
func serveGhrReleases(t *testing.T, b *Backend, tag string) *ghrReleases {
	t.Helper()
	g := &ghrReleases{tag: tag}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.calls.Add(1)
		g.mu.Lock()
		tag := g.tag
		g.mu.Unlock()
		if tag == "" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, "/darkraise/ghr/releases/tag/"+tag, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	b.ReleaseBase, b.HTTP = srv.URL+"/darkraise/ghr", srv.Client()
	return g
}

// eventsWith lists the messages of b's events that start with prefix.
func eventsWith(b *Backend, prefix string) []string {
	var out []string
	for _, e := range b.Events.After(0) {
		if strings.HasPrefix(e.Msg, prefix) {
			out = append(out, e.Msg)
		}
	}
	return out
}

func TestGhrCheck(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Version = "v0.1.25"
	rel := serveGhrReleases(t, b, "v0.1.26")
	ctx := context.Background()
	b.checkGhr(ctx)
	b.checkGhr(ctx)
	g := b.Status().GhrUpdate
	if g.Installed != "v0.1.25" || g.Latest != "v0.1.26" || !g.Newer || g.Development || g.CheckedAt == nil || g.CheckError != "" {
		t.Fatalf("after a check %+v", g)
	}
	if got := eventsWith(b, "ghr v0.1.26 is available"); len(got) != 1 || got[0] != "ghr v0.1.26 is available (running v0.1.25)" {
		t.Fatalf("announcements %q", got)
	}
	checked := *g.CheckedAt

	rel.set("")
	b.checkGhr(ctx)
	g = b.Status().GhrUpdate
	if g.Latest != "v0.1.26" || g.CheckError == "" || !g.CheckedAt.Equal(checked) {
		t.Fatalf("after a failed check %+v", g)
	}
	if j := statusJob(t, b.Status(), config.JobGhrUpdateCheck); j.LastError == "" || j.LastFinish == nil {
		t.Fatalf("task after a failed check %+v", j)
	}

	rel.set("v0.1.27")
	b.checkGhr(ctx)
	if got := eventsWith(b, "ghr v0.1.27 is available"); len(got) != 1 {
		t.Fatalf("a newer release was not announced: %q", got)
	}
	if g := b.Status().GhrUpdate; g.CheckError != "" || g.Latest != "v0.1.27" {
		t.Fatalf("a good check kept the error %+v", g)
	}
}

func TestGhrCheckOnADevelopmentBuild(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Version = "dev"
	serveGhrReleases(t, b, "v0.1.26")
	b.checkGhr(context.Background())
	if g := b.Status().GhrUpdate; !g.Development || g.Newer || g.Latest != "v0.1.26" || g.Installed != "dev" {
		t.Fatalf("%+v", g)
	}
	if got := eventsWith(b, "ghr v0.1.26 is available"); len(got) != 0 {
		t.Fatalf("a development build announced %q", got)
	}
}

func TestGhrChecksFollowTheTask(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Version = "v0.1.26"
	rel := serveGhrReleases(t, b, "v0.1.26")
	b.StartGhrChecks()
	t.Cleanup(func() { b.StopBackground(); b.Wait() })
	waitFor(t, "the check at start", func() bool {
		return rel.calls.Load() == 1 && statusJob(t, b.Status(), config.JobGhrUpdateCheck).LastFinish != nil
	})
	if err := b.RunJob(config.JobGhrUpdateCheck); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the Run now check", func() bool { return rel.calls.Load() == 2 })
	b.StopBackground()
	b.Wait()
}

func TestStatusFingerprintFollowsGhrUpdate(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Version = "v0.1.25"
	before := b.fingerprints()["status"]
	b.ghrMu.Lock()
	b.ghr.latest = "v0.1.26"
	b.ghrMu.Unlock()
	if b.fingerprints()["status"] == before {
		t.Fatal("a new ghr release left the status fingerprint unchanged")
	}
}

func TestDefaultOptionsCheckGhrReleases(t *testing.T) {
	if got := DefaultOptions().ReleaseBase; got != selfupdate.DefaultBase {
		t.Fatalf("release base %q", got)
	}
}
