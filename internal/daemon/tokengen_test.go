package daemon

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// tokenCache is one cache of data fetched with the token: the GitHub call
// that fills it, how a test prepares it, the request that reads through it,
// how many of those GitHub calls it has made, and a request for another entry
// of the same cache (nil when a new token cannot reach the cache through
// another entry).
type tokenCache struct {
	name  string
	fetch string
	setup func(m *fakeManager, gh *fakeGH)
	read  func(b *Backend) error
	calls func(gh *fakeGH) int
	other func(b *Backend)
}

func tokenCaches() []tokenCache {
	ctx := context.Background()
	return []tokenCache{
		{"actions list", "ListRecentRuns",
			func(_ *fakeManager, gh *fakeGH) { gh.recentBy = map[string][]github.Run{} },
			func(b *Backend) error { _, err := b.Actions(ctx); return err },
			recentCalls,
			nil},
		{"run detail", "GetRun",
			func(_ *fakeManager, gh *fakeGH) { detailRun(gh, "completed") },
			func(b *Backend) error { _, err := b.RunDetail(ctx, "darkmem", 102, 0); return err },
			func(gh *fakeGH) int { return gh.count("GetRun darkmem 102") },
			func(b *Backend) { b.RunDetail(ctx, "darkmem", 103, 0) }},
		{"runner steps", "ListJobs",
			func(m *fakeManager, _ *fakeGH) { m.insts = []model.InstanceStatus{{ID: "aaaaaa"}} },
			func(b *Backend) error { _, err := b.RunnerSteps(ctx, "aaaaaa"); return err },
			jobCallCount,
			func(b *Backend) { b.RunnerSteps(ctx, "zzzzzz") }},
	}
}

// midFetchGH runs swap inside the first GitHub call named on, before that
// call is answered, so a test can replace the token while a fetch is in
// flight.
type midFetchGH struct {
	GitHub
	on      string
	swap    func()
	swapped atomic.Bool
}

func (g *midFetchGH) at(call string) {
	if call == g.on && g.swapped.CompareAndSwap(false, true) {
		g.swap()
	}
}

func (g *midFetchGH) ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error) {
	g.at("ListRecentRuns")
	return g.GitHub.ListRecentRuns(ctx, repo, n)
}

func (g *midFetchGH) GetRun(ctx context.Context, repo string, id int64) (*github.Run, error) {
	g.at("GetRun")
	return g.GitHub.GetRun(ctx, repo, id)
}

func (g *midFetchGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	g.at("ListJobs")
	return g.GitHub.ListJobs(ctx, repo, runID)
}

// What one token fetched is not served to the next.
func TestCachesAreRefilledAfterATokenChange(t *testing.T) {
	for _, c := range tokenCaches() {
		t.Run(c.name, func(t *testing.T) {
			b, m, gh := newBackend(t)
			frozenClock(b, actionsT0)
			c.setup(m, gh)
			if err := c.read(b); err != nil {
				t.Fatal(err)
			}
			one := c.calls(gh)
			if err := c.read(b); err != nil || one == 0 || c.calls(gh) != one {
				t.Fatalf("second read: %d calls after %d, err %v", c.calls(gh), one, err)
			}
			gh.ForgetCache()
			if err := c.read(b); err != nil || c.calls(gh) != 2*one {
				t.Fatalf("after a token change: %d calls, want %d, err %v", c.calls(gh), 2*one, err)
			}
		})
	}
}

// A fetch that was in flight when the token changed answers its caller and
// leaves nothing behind, also when a request for another entry has already
// moved the cache to the new token. The fetch after it is kept as usual.
func TestAFetchAcrossATokenChangeIsNotStored(t *testing.T) {
	for _, c := range tokenCaches() {
		t.Run(c.name, func(t *testing.T) {
			b, m, gh := newBackend(t)
			frozenClock(b, actionsT0)
			c.setup(m, gh)
			b.GH = &midFetchGH{GitHub: gh, on: c.fetch, swap: func() {
				gh.ForgetCache()
				if c.other != nil {
					c.other(b)
				}
			}}
			if err := c.read(b); err != nil {
				t.Fatalf("the caller of the fetch: %v", err)
			}
			one := c.calls(gh)
			if err := c.read(b); err != nil || one == 0 || c.calls(gh) != 2*one {
				t.Fatalf("next read: %d calls, want %d, err %v", c.calls(gh), 2*one, err)
			}
			if err := c.read(b); err != nil || c.calls(gh) != 2*one {
				t.Fatalf("third read: %d calls, want %d, err %v", c.calls(gh), 2*one, err)
			}
		})
	}
}
