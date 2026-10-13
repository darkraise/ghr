package ghstorage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

var forbidden = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"}

func TestInventoryBuildsTheRows(t *testing.T) {
	gh := newFakeGH()
	cfg := testConfig()
	cfg.Repos = []config.Repo{{Name: "darkmem"}}
	cfg.WatchRepos = []string{"homelab"}
	cfg.GitHubStorage = config.GitHubStorage{CleanupMaxAge: config.Duration(48 * time.Hour),
		Repos: map[string]config.StorageOverride{"HOMELAB": {Cleanup: "off"}}}
	s, clock := newTestService(t, gh, cfg)
	now := clock.Now()
	expired := ghArt(3, 30, now, time.Hour, model.GiB)
	expired.Expired = true
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 2*time.Hour, model.GiB), ghArt(2, 20, now, time.Hour, model.GiB), expired)
	gh.addRepo("homelab", false, ghArt(4, 40, now, time.Hour, 5))
	gh.addRepo("chicken-farm", true)
	gh.repos = append(gh.repos, github.UserRepo{Name: "elsewhere", Owner: github.Account{Login: "someone"}})
	gh.caches["darkmem"] = []github.Cache{{ID: 7, Key: "go-mod", Ref: "refs/heads/main", SizeInBytes: 100, LastAccessedAt: now}}

	v := s.Version()
	s.pass(true, false)
	st := s.Snapshot()
	if st.CheckedAt == nil || !st.CheckedAt.Equal(now) || s.Version() == v {
		t.Fatalf("CheckedAt %v, version %d → %d", st.CheckedAt, v, s.Version())
	}
	var names []string
	for _, r := range st.Repos {
		names = append(names, r.Name)
	}
	if want := []string{"darkmem", "homelab", "chicken-farm"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rows %v, want %v (largest first, other owners left out)", names, want)
	}
	dm := row(t, st, "darkmem")
	if dm.ArtifactCount != 2 || dm.ArtifactBytes != 2*model.GiB || dm.Artifacts[0].ID != 2 || dm.Artifacts[1].RunID != 10 || dm.Artifacts[1].Branch != "main" {
		t.Errorf("darkmem artifacts %+v (expired dropped, newest first)", dm)
	}
	if dm.CacheCount != 1 || dm.CacheBytes != 100 || dm.Caches[0].Key != "go-mod" {
		t.Errorf("darkmem caches %+v", dm.Caches)
	}
	if dm.Visibility != "private" || !dm.Covered || dm.Cleanup != "2d" || dm.CleanupSource != "global" || dm.RetentionDays != 90 || dm.RetentionMaxDays != 400 {
		t.Errorf("darkmem row %+v", dm)
	}
	hl := row(t, st, "homelab")
	if hl.Visibility != "public" || !hl.Covered || hl.Cleanup != "off" || hl.CleanupSource != "override" {
		t.Errorf("homelab row %+v", hl)
	}
	cf := row(t, st, "chicken-farm")
	if cf.Covered || cf.Artifacts == nil || cf.Caches == nil {
		t.Errorf("chicken-farm row %+v", cf)
	}
	cfg.GitHubStorage.CleanupMaxAge = 0
	if r := row(t, s.Snapshot(), "darkmem"); r.Cleanup != "off" || r.CleanupSource != "none" {
		t.Errorf("after the policy changed: %s %s, want off none", r.Cleanup, r.CleanupSource)
	}
}

func TestRetentionIsReadOnceADay(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, clock := newTestService(t, gh, testConfig())
	s.pass(true, false)
	clock.Add(23 * time.Hour)
	s.pass(true, false)
	if n := len(gh.callsLike("Retention darkmem")); n != 1 {
		t.Fatalf("%d retention reads in 23 hours, want 1", n)
	}
	clock.Add(time.Hour)
	s.pass(true, false)
	if n := len(gh.callsLike("Retention darkmem")); n != 2 {
		t.Fatalf("%d retention reads after a day, want 2", n)
	}
}

// tooManyPages is the error a real client returns for a listing longer than
// 50 pages.
func tooManyPages(t *testing.T) error {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/next?%s>; rel="next"`, srv.URL, r.URL.RawQuery+"x"))
		fmt.Fprint(w, `{"artifacts":[]}`)
	}))
	defer srv.Close()
	c := github.New("darkraise", func() string { return "t" })
	c.BaseURL = srv.URL
	_, err := c.ListRepoArtifacts(context.Background(), "big")
	if !github.IsTooManyPages(err) {
		t.Fatalf("err = %v, want the 50-page error", err)
	}
	return err
}

func TestARepositoryErrorKeepsItsLastItems(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  func(t *testing.T) error
		want string
	}{
		{"missing permission", func(*testing.T) error { return forbidden }, "described: " + forbidden.Error()},
		{"too many pages", tooManyPages, "darkmem has more than 5,000 artifacts or caches, more than ghr lists: lower its retention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			s, clock := newTestService(t, gh, testConfig())
			s.Describe = func(err error, repo string) string { return "described: " + err.Error() }
			gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5))
			s.pass(true, false)
			gh.listErr["darkmem"] = tc.err(t)
			s.pass(true, false)
			r := row(t, s.Snapshot(), "darkmem")
			if r.Error != tc.want || r.ArtifactCount != 1 || r.Artifacts[0].ID != 1 {
				t.Fatalf("row %+v, want error %q and the last artifact", r, tc.want)
			}
		})
	}
}

func TestAFailedRepositoryListingKeepsTheRows(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	gh.reposErr = errors.New("boom")
	s.pass(true, false)
	st := s.Snapshot()
	if st.Error != "boom" || len(st.Repos) != 1 {
		t.Fatalf("error %q, %d rows; want boom and the last row", st.Error, len(st.Repos))
	}
}

func TestPassIsSkippedOnALowRateBudget(t *testing.T) {
	future := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		remaining int
		suspended time.Time
		skipped   bool
	}{
		{"unknown budget", -1, time.Time{}, false},
		{"enough left", MinRateLeft, time.Time{}, false},
		{"too few left", MinRateLeft - 1, time.Time{}, true},
		{"suspended", 4000, future, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			gh.remaining, gh.suspended = tc.remaining, tc.suspended
			s, _ := newTestService(t, gh, testConfig())
			s.pass(true, true)
			if skipped := len(gh.callsLike("")) == 0; skipped != tc.skipped {
				t.Fatalf("skipped = %v, want %v (calls %v)", skipped, tc.skipped, gh.callsLike(""))
			}
		})
	}
}

func TestATokenChangeDropsThePass(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	gh.onList = func(context.Context, string) error {
		gh.mu.Lock()
		gh.gen++
		gh.mu.Unlock()
		return nil
	}
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	if st := s.Snapshot(); st.CheckedAt != nil || len(st.Repos) != 0 {
		t.Fatalf("snapshot %+v kept a pass made under the old token", st)
	}
}

// blockList makes the listing of repo wait until release is closed, and
// closes entered when it starts waiting.
func blockList(gh *fakeGH, repo string) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	gh.onList = func(ctx context.Context, r string) error {
		if r == repo {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	return entered, release
}

func TestADeleteIsNotUndoneByAListingUnderWay(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5), ghArt(2, 20, clock.Now(), time.Hour, 5))
	s.pass(true, false)
	entered, release := blockList(gh, "darkmem")
	done := make(chan struct{})
	go func() { s.pass(true, false); close(done) }()
	<-entered
	s.mu.Lock()
	s.removeItem("artifact", "darkmem", 1)
	s.mu.Unlock()
	close(release)
	<-done
	r := row(t, s.Snapshot(), "darkmem")
	if r.ArtifactCount != 1 || r.Artifacts[0].ID != 2 {
		t.Fatalf("artifacts %+v, want only 2: the listing still held 1", r.Artifacts)
	}
}

func TestRefreshIsRefusedWhileAPassRuns(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	entered, release := blockList(gh, "darkmem")
	done := make(chan struct{})
	go func() { s.pass(true, false); close(done) }()
	<-entered
	if err := s.Refresh(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Refresh during a pass = %v, want ErrBusy", err)
	}
	close(release)
	<-done
	if err := s.Refresh(); err != nil || len(s.kick) != 1 {
		t.Fatalf("Refresh after the pass = %v with %d kicks queued, want nil and 1", err, len(s.kick))
	}
}

func TestCloseStopsAPassUnderWay(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	entered, _ := blockList(gh, "darkmem")
	s.Start()
	<-entered // Start runs the first pass at once
	s.Close()
	stopped := make(chan struct{})
	go func() { s.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	if st := s.Snapshot(); st.CheckedAt != nil {
		t.Fatalf("a cancelled pass wrote its result: %+v", st)
	}
	if err := s.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Refresh after Close = %v, want ErrClosed", err)
	}
}

// october is a usage answer of 300 GB-hours of Actions storage and 10 of
// Packages, 4 of them on the last full day before 2026-10-13 12:00.
func october() []github.UsageItem {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	return []github.UsageItem{
		{Date: day(5), SKU: github.SKUActionsStorage, Repo: "darkmem", Quantity: 300},
		{Date: day(11), SKU: github.SKUPackagesStorage, Quantity: 6},
		{Date: day(12), SKU: github.SKUPackagesStorage, Quantity: 4},
	}
}

func TestBillingReadsTheQuota(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	gh.summary = []github.SummaryItem{{SKU: github.SKUActionsStorage, Net: 0.25}, {SKU: github.SKUPackagesStorage, Net: 0.25}}
	gh.addRepo("darkmem", true, ghArt(1, 10, time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), time.Hour, model.GiB))
	s, clock := newTestService(t, gh, testConfig())
	s.pass(true, true)
	st := s.Snapshot()
	q := st.Quota
	if q == nil || st.QuotaError != "" {
		t.Fatalf("quota %+v, error %q", q, st.QuotaError)
	}
	if q.Month != "2026-10" || !q.ReadAt.Equal(clock.Now()) || q.UsedGBHours != 310 || q.ActionsGBHours != 300 || q.PackagesGBHours != 10 {
		t.Errorf("quota %+v", q)
	}
	// 1 GiB of artifacts plus 4 GB-hours of Packages a day.
	if q.RateGBHoursPerHour != 1+4.0/24 || q.OverAllowanceGBHours == nil || *q.OverAllowanceGBHours != 0.5 || q.Level != "exhausted" {
		t.Errorf("rate %v, over %v, level %q", q.RateGBHoursPerHour, q.OverAllowanceGBHours, q.Level)
	}
	if a := s.Alert(); a == nil || a.Level != "exhausted" || a.UsedPct != 85 {
		t.Errorf("alert %+v", a)
	}
	s.pass(false, true)
	if got := eventsLike(s.Events, "github storage: quota"); len(got) != 1 || got[0] != "github storage: quota exhausted: 310.0 of 363.3 GB-hours used in 2026-10" {
		t.Errorf("level events %q, want one", got)
	}
	if n := len(gh.callsLike("OwnerType")); n != 1 {
		t.Errorf("owner type read %d times, want once", n)
	}
	if got := gh.callsLike("StorageUsage"); got[0] != "StorageUsage false 2026-10" {
		t.Errorf("usage read as %q", got[0])
	}
}

func TestBillingErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func(gh *fakeGH)
		want  string
		usage bool // whether the usage endpoint is called
	}{
		{"another user's token", func(gh *fakeGH) { gh.login = "someone" },
			"the quota can only be read with a token of darkraise", false},
		{"no Plan permission", func(gh *fakeGH) { gh.usageErr = forbidden },
			"the token cannot read darkraise's billing: grant the account permission Plan: read", true},
		{"an organization without admin", func(gh *fakeGH) { gh.isOrg, gh.login, gh.usageErr = true, "someone", forbidden },
			"an organization admin's token with Administration: read is needed", true},
		{"anything else", func(gh *fakeGH) { gh.usageErr = errors.New("boom") }, "boom", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			tc.set(gh)
			s, _ := newTestService(t, gh, testConfig())
			s.pass(false, true)
			st := s.Snapshot()
			if st.QuotaError != tc.want || st.Quota != nil {
				t.Errorf("quota error %q, quota %+v; want %q", st.QuotaError, st.Quota, tc.want)
			}
			if called := len(gh.callsLike("StorageUsage")) > 0; called != tc.usage {
				t.Errorf("usage called = %v, want %v", called, tc.usage)
			}
		})
	}
}

func TestAFailedBillingReadKeepsTheLastQuota(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	s, clock := newTestService(t, gh, testConfig())
	s.pass(false, true)
	read := clock.Now()
	clock.Add(time.Hour)
	gh.usageErr = forbidden
	s.pass(false, true)
	st := s.Snapshot()
	if st.Quota == nil || !st.Quota.ReadAt.Equal(read) || st.QuotaError == "" {
		t.Fatalf("quota %+v, error %q; want the last read kept with the error", st.Quota, st.QuotaError)
	}
	gh.usageErr = nil
	gh.sumErr = errors.New("preview gone")
	s.pass(false, true)
	if q := s.Snapshot().Quota; q == nil || q.OverAllowanceGBHours != nil || !q.ReadAt.Equal(clock.Now()) {
		t.Fatalf("quota %+v: a failed summary must not fail the read", q)
	}
}

func TestTheMonthChangeClearsTheQuota(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error // November's read
	}{
		{"until November's read returns", nil},
		{"while November's read fails", errors.New("boom")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			gh.usage = october()
			s, clock := newTestService(t, gh, testConfig())
			clock.Add(18*24*time.Hour + 11*time.Hour) // 2026-10-31 23:00
			s.pass(false, true)
			if q := s.Snapshot().Quota; q == nil || q.Month != "2026-10" {
				t.Fatalf("quota %+v", q)
			}
			clock.Add(time.Hour + time.Minute)
			entered, release := make(chan struct{}), make(chan struct{})
			gh.onUsage = func(context.Context) error {
				close(entered)
				<-release
				return tc.err
			}
			done := make(chan struct{})
			go func() { s.pass(false, true); close(done) }()
			<-entered
			if q, a := s.Snapshot().Quota, s.Alert(); q != nil || a != nil {
				t.Fatalf("during November's read: quota %+v, alert %+v; want October's gone", q, a)
			}
			close(release)
			<-done
			st := s.Snapshot()
			if tc.err != nil {
				if st.Quota != nil || st.QuotaError != "boom" {
					t.Fatalf("quota %+v, error %q; want none and boom", st.Quota, st.QuotaError)
				}
				return
			}
			if st.Quota == nil || st.Quota.Month != "2026-11" {
				t.Fatalf("quota %+v, want November's", st.Quota)
			}
			if got := gh.callsLike("StorageUsage"); got[len(got)-1] != "StorageUsage false 2026-11" {
				t.Fatalf("last usage read %q", got[len(got)-1])
			}
		})
	}
}

func TestBillingDelay(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct {
		now  string
		want time.Duration
	}{
		{"2026-10-13T12:00:00Z", BillingInterval},
		{"2026-10-31T23:00:00Z", time.Hour},
		{"2026-10-31T23:30:00Z", 31 * time.Minute},
		{"2026-10-31T23:59:30Z", 90 * time.Second},
		{"2026-11-01T00:01:00Z", BillingInterval},
		{"2026-12-31T23:20:00Z", 41 * time.Minute},
		{"2026-10-31T18:30:00-05:00", 31 * time.Minute}, // 23:30 UTC: the month is UTC's, not the local one
	} {
		if got := billingDelay(at(tc.now)); got != tc.want {
			t.Errorf("billingDelay(%s) = %v, want %v", tc.now, got, tc.want)
		}
	}
}

func TestPolicyChangedRecomputesWithoutGitHub(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	cfg := testConfig()
	s, _ := newTestService(t, gh, cfg)
	s.pass(false, true)
	calls := len(gh.callsLike(""))
	cfg.GitHubStorage.IncludedMB = 1024
	v := s.Version()
	s.PolicyChanged()
	q := s.Snapshot().Quota
	if q.IncludedGBHours != 744 || q.Level != "" || s.Version() == v || len(gh.callsLike("")) != calls {
		t.Fatalf("included %v, level %q, version %d → %d, calls %d → %d", q.IncludedGBHours, q.Level, v, s.Version(), calls, len(gh.callsLike("")))
	}
	if got := eventsLike(s.Events, "github storage: back under the warning levels for 2026-10"); len(got) != 1 {
		t.Errorf("events %q, want the level's return", eventsLike(s.Events, ""))
	}
}

func TestTokenChangedClearsErrorsAndKicks(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	gh.listErr["darkmem"] = forbidden
	gh.login = "someone"
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, true)
	if st := s.Snapshot(); st.QuotaError == "" || row(t, st, "darkmem").Error == "" {
		t.Fatalf("snapshot %+v, want both errors", st)
	}
	s.TokenChanged()
	st := s.Snapshot()
	if st.QuotaError != "" || row(t, st, "darkmem").Error != "" || len(s.kick) != 1 {
		t.Fatalf("after TokenChanged: %+v, %d kicks", st, len(s.kick))
	}
}
