package ghstorage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// GitHub is the part of the GitHub client the service uses.
type GitHub interface {
	OwnerType(ctx context.Context) (bool, error)
	TokenLogin(ctx context.Context) (string, error)
	StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]github.UsageItem, error)
	ListRepoArtifacts(ctx context.Context, repo string) ([]github.Artifact, error)
	ListCaches(ctx context.Context, repo string) ([]github.Cache, error)
	DeleteArtifact(ctx context.Context, repo string, id int64) (bool, error)
	DeleteCache(ctx context.Context, repo string, id int64) (bool, error)
	Retention(ctx context.Context, repo string) (github.Retention, error)
	SetRetention(ctx context.Context, repo string, days int) error
	ListUserRepos(ctx context.Context) ([]github.UserRepo, error)
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	TokenMeta() github.TokenMeta
	SuspendedUntil() time.Time
	RateRemaining() int
}

var (
	ErrBusy   = errors.New("a GitHub storage check is already running")
	ErrClosed = errors.New("ghr is shutting down")
)

const (
	InventoryInterval = 15 * time.Minute
	BillingInterval   = time.Hour
	// Workers is how many repositories an inventory lists at once.
	Workers = 4
	// MinRateLeft is the token's hourly calls a pass leaves to the runner
	// manager: below it the pass is skipped.
	MinRateLeft = 500
	// MaxDeletions is how many deletions the snapshot keeps.
	MaxDeletions = 200
	// retentionTTL is how often a repository's retention is read.
	retentionTTL = 24 * time.Hour
	// deletedTTL is how long a deleted item is kept out of listings that
	// started before its delete.
	deletedTTL = time.Hour
)

// Service keeps the GitHub storage snapshot. Start it once; Close stops it.
type Service struct {
	GH       GitHub
	Config   func() *config.Config
	Events   *events.Ring
	Now      func() time.Time                    // nil means time.Now
	NewID    func() string                       // operation ids; nil makes random ones
	Describe func(err error, repo string) string // a repository's error text; nil means err.Error()
	Wake     func()                              // called after every snapshot change; must not block

	version atomic.Int64

	mu        sync.Mutex
	closed    bool
	running   bool // a pass runs
	checkedAt time.Time
	repos     []model.RepoStorage // sorted by artifact bytes, largest first
	listErr   string
	billing   *Billing
	quota     *model.GitHubQuota
	quotaErr  string
	level     string // the level the last event was written for
	isOrg     *bool  // read once; the owner does not change while the daemon runs
	retention map[string]retentionRead
	deleted   map[string]time.Time   // "artifact:<id>" or "cache:<id>" → when deleted
	deletions []model.Deletion       // newest first
	op        *model.GitHubOperation // the running or last finished operation
	opRunning bool

	cleanupErr map[string]string // lower-case repo → the failure the last cleanup event was written for; only the pass touches it

	writeMu sync.Mutex
	writes  []time.Time // guarded by writeMu: the writes of the last hour

	ctx    context.Context
	cancel context.CancelFunc
	kick   chan struct{}
	wg     sync.WaitGroup
}

type retentionRead struct {
	at      time.Time // when the read started
	written time.Time // when a write last set r; a read that started before it is stale
	r       github.Retention
	err     string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) describe(err error, repo string) string {
	if s.Describe != nil {
		return s.Describe(err, repo)
	}
	return err.Error()
}

// bump marks a snapshot change. It may run under s.mu: Wake must not block.
func (s *Service) bump() {
	s.version.Add(1)
	if s.Wake != nil {
		s.Wake()
	}
}

func (s *Service) init() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.kick = make(chan struct{}, 1)
	s.retention = map[string]retentionRead{}
	s.deleted = map[string]time.Time{}
	s.cleanupErr = map[string]string{}
}

// Start runs a full pass at once, then the inventory every
// InventoryInterval and the billing read every BillingInterval and just
// after the month changes.
func (s *Service) Start() {
	s.init()
	s.kick <- struct{}{}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop()
	}()
}

func (s *Service) loop() {
	inv := time.NewTimer(InventoryInterval)
	bill := time.NewTimer(BillingInterval)
	defer inv.Stop()
	defer bill.Stop()
	for {
		inventory, billing := false, false
		select {
		case <-s.ctx.Done():
			return
		case <-s.kick:
			inventory, billing = true, true
		case <-inv.C:
			inventory = true
		case <-bill.C:
			billing = true
		}
		s.pass(inventory, billing)
		if inventory {
			inv.Reset(InventoryInterval)
		}
		if billing {
			bill.Reset(billingDelay(s.now()))
		}
	}
}

// billingDelay is how long after now the next billing read runs:
// BillingInterval, or a minute after the month changes when that is sooner.
func billingDelay(now time.Time) time.Duration {
	_, resets := MonthBounds(now)
	return min(BillingInterval, resets.Add(time.Minute).Sub(now))
}

// Kick asks for a full pass; kicks during a pass coalesce into one after it.
func (s *Service) Kick() {
	if s.kick == nil {
		return
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Refresh asks for a full pass now; ErrBusy while one runs.
func (s *Service) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.running:
		return ErrBusy
	}
	s.Kick()
	return nil
}

// TokenChanged clears what the old token's answers said, then asks for a
// full pass with the new one.
func (s *Service) TokenChanged() {
	s.mu.Lock()
	for i := range s.repos {
		s.repos[i].Error = ""
	}
	s.listErr, s.quotaErr = "", ""
	s.retention = map[string]retentionRead{}
	s.bump()
	s.mu.Unlock()
	s.Kick()
}

// PolicyChanged recomputes the quota from the last billing read with the
// current config, without calling GitHub.
func (s *Service) PolicyChanged() {
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recompute(cfg)
	s.bump()
}

// Version rises on every snapshot change.
func (s *Service) Version() int64 { return s.version.Load() }

// Close stops the passes and any operation; Wait returns once they stopped.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Wait() { s.wg.Wait() }

// pass reads the config once and runs an inventory, a billing read, or both.
// It is skipped while GitHub's rate limit is low or suspends calls.
func (s *Service) pass(inventory, billing bool) {
	cfg := s.Config()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.running = true
	// Last month's numbers are never shown as this month's.
	if s.quota != nil && !s.now().Before(s.quota.ResetsAt) {
		s.quota, s.billing, s.level = nil, nil, ""
		s.bump()
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	if !s.GH.SuspendedUntil().IsZero() {
		return
	}
	if s.rateLow() {
		return
	}
	gen := s.GH.TokenMeta().Generation
	if inventory {
		s.inventory(cfg, gen)
	}
	if billing {
		s.readBilling(cfg, gen)
	}
}

// rateLow reports whether the token has fewer than MinRateLeft calls left
// this hour.
func (s *Service) rateLow() bool {
	n := s.GH.RateRemaining()
	return n >= 0 && n < MinRateLeft
}

// keep reports whether a pass started under token generation gen may write
// its result: not once the service closed or the token changed.
func (s *Service) keep(gen uint64) bool {
	return s.ctx.Err() == nil && s.GH.TokenMeta().Generation == gen
}

func (s *Service) readBilling(cfg *config.Config, gen uint64) {
	ctx := s.ctx
	fail := func(msg string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.keep(gen) {
			s.quotaErr = msg
			s.bump()
		}
	}
	s.mu.Lock()
	known := s.isOrg
	s.mu.Unlock()
	isOrg := false
	if known != nil {
		isOrg = *known
	} else {
		v, err := s.GH.OwnerType(ctx)
		if err != nil {
			fail(billingErr(err, cfg.Owner, false))
			return
		}
		isOrg = v
		s.mu.Lock()
		s.isOrg = &v
		s.mu.Unlock()
	}
	// A user's billing endpoint reads the token user's own billing.
	if !isOrg {
		login, err := s.GH.TokenLogin(ctx)
		if err != nil {
			fail(billingErr(err, cfg.Owner, false))
			return
		}
		if !strings.EqualFold(login, cfg.Owner) {
			fail("the quota can only be read with a token of " + cfg.Owner)
			return
		}
	}
	now := s.now()
	year, month, _ := now.UTC().Date()
	items, err := s.GH.StorageUsage(ctx, isOrg, year, month)
	if err != nil {
		fail(billingErr(err, cfg.Owner, isOrg))
		return
	}
	b := Billing{Month: now.UTC().Format("2006-01"), ReadAt: now}
	b.Actions, b.Packages, b.PackagesDaily = SumUsage(items, now)
	cur := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.keep(gen) {
		return
	}
	s.billing, s.quotaErr = &b, ""
	s.recompute(cur)
	s.bump()
}

func billingErr(err error, owner string, isOrg bool) string {
	var ge *github.APIError
	if errors.As(err, &ge) && ge.Kind == github.ErrAuth && ge.Status == http.StatusForbidden {
		if isOrg {
			return "an organization admin's token with Administration: read is needed"
		}
		return "the token cannot read " + owner + "'s billing: grant the account permission Plan: read"
	}
	return err.Error()
}

// recompute rebuilds the quota from the last billing read and the artifact
// bytes now, and writes an event when the level changes. Call with s.mu held.
func (s *Service) recompute(cfg *config.Config) {
	if s.billing == nil {
		s.quota = nil
		return
	}
	var bytes int64
	for _, r := range s.repos {
		bytes += r.ArtifactBytes
	}
	q := ComputeQuota(*s.billing, bytes, cfg.GitHubStorage.Included(), cfg.GitHubStorage.Warn())
	s.quota = &q
	if q.Level == s.level {
		return
	}
	s.level = q.Level
	switch q.Level {
	case "":
		s.Events.Add("ok", "", "github storage: back under the warning levels for %s", q.Month)
	case "exhausted", "critical":
		s.Events.Add("error", "", "github storage: quota %s: %.1f of %.1f GB-hours used in %s", q.Level, q.UsedGBHours, q.IncludedGBHours, q.Month)
	default:
		s.Events.Add("warn", "", "github storage: quota %s: %.1f of %.1f GB-hours used in %s", q.Level, q.UsedGBHours, q.IncludedGBHours, q.Month)
	}
}

// listing is one repository's inventory result.
type listing struct {
	name      string
	private   bool
	artifacts []model.ArtifactItem
	caches    []model.CacheItem
	err       error
	skipped   bool           // the rate limit reserve was reached before it was listed
	retention *retentionRead // nil when not read this pass
}

func (s *Service) inventory(cfg *config.Config, gen uint64) {
	ctx := s.ctx
	all, err := s.GH.ListUserRepos(ctx)
	if err != nil {
		s.mu.Lock()
		if s.keep(gen) {
			s.listErr = s.describe(err, "")
			s.bump()
		}
		s.mu.Unlock()
		return
	}
	var ls []*listing
	for _, r := range all {
		if strings.EqualFold(r.Owner.Login, cfg.Owner) {
			ls = append(ls, &listing{name: r.Name, private: r.Private})
		}
	}
	now := s.now()
	s.mu.Lock()
	due := map[string]bool{}
	for _, l := range ls {
		if rr, ok := s.retention[strings.ToLower(l.name)]; !ok || now.Sub(rr.at) >= retentionTTL {
			due[l.name] = true
		}
	}
	s.mu.Unlock()
	sem := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	for _, l := range ls {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.list(ctx, l, due[l.name], now)
		}()
	}
	wg.Wait()

	cur := s.Config()
	s.mu.Lock()
	if !s.keep(gen) {
		s.mu.Unlock()
		return
	}
	s.merge(ls, now)
	s.checkedAt, s.listErr = now, ""
	s.recompute(cur)
	s.bump()
	s.mu.Unlock()
	s.cleanup(cfg, gen)
}

func (s *Service) list(ctx context.Context, l *listing, readRetention bool, now time.Time) {
	if s.rateLow() {
		l.skipped = true
		return
	}
	arts, err := s.GH.ListRepoArtifacts(ctx, l.name)
	if err != nil {
		l.err = err
		return
	}
	if s.rateLow() {
		l.skipped = true
		return
	}
	caches, err := s.GH.ListCaches(ctx, l.name)
	if err != nil {
		l.err = err
		return
	}
	l.artifacts = []model.ArtifactItem{}
	for _, a := range arts {
		if !a.Expired {
			l.artifacts = append(l.artifacts, model.ArtifactItem{ID: a.ID, Name: a.Name, Bytes: a.SizeInBytes,
				CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, RunID: a.WorkflowRun.ID, Branch: a.WorkflowRun.HeadBranch})
		}
	}
	l.caches = []model.CacheItem{}
	for _, c := range caches {
		l.caches = append(l.caches, model.CacheItem{ID: c.ID, Key: c.Key, Ref: c.Ref, Bytes: c.SizeInBytes, LastUsedAt: c.LastAccessedAt})
	}
	if readRetention {
		rr := retentionRead{at: now}
		if r, err := s.GH.Retention(ctx, l.name); err != nil {
			rr.err = s.describe(err, l.name)
		} else {
			rr.r = r
		}
		l.retention = &rr
	}
}

// merge replaces the rows with the listings. A repository whose listing
// failed keeps its last items with the error, and one that was skipped keeps
// them without. Items deleted since the listing started are left out, so a
// delete is never undone by a listing that was already under way. Call with
// s.mu held.
func (s *Service) merge(ls []*listing, now time.Time) {
	for k, at := range s.deleted {
		if now.Sub(at) >= deletedTTL {
			delete(s.deleted, k)
		}
	}
	prev := map[string]model.RepoStorage{}
	for _, r := range s.repos {
		prev[strings.ToLower(r.Name)] = r
	}
	rows := make([]model.RepoStorage, 0, len(ls))
	for _, l := range ls {
		key := strings.ToLower(l.name)
		if l.retention != nil {
			if cur := s.retention[key]; !cur.written.After(l.retention.at) {
				s.retention[key] = *l.retention
			}
		}
		row := model.RepoStorage{Name: l.name, Visibility: "public", Artifacts: []model.ArtifactItem{}, Caches: []model.CacheItem{}}
		if l.private {
			row.Visibility = "private"
		}
		if l.err != nil || l.skipped {
			old := prev[key]
			row.Artifacts, row.Caches = old.Artifacts, old.Caches
			if l.err != nil {
				row.Error = s.describe(l.err, l.name)
				if github.IsTooManyPages(l.err) {
					row.Error = l.name + " has more than 5,000 artifacts or caches, more than ghr lists: lower its retention"
				}
			}
		} else {
			for _, a := range l.artifacts {
				if _, gone := s.deleted[itemKey("artifact", a.ID)]; !gone {
					row.Artifacts = append(row.Artifacts, a)
				}
			}
			for _, c := range l.caches {
				if _, gone := s.deleted[itemKey("cache", c.ID)]; !gone {
					row.Caches = append(row.Caches, c)
				}
			}
		}
		if rr, ok := s.retention[key]; ok {
			row.RetentionDays, row.RetentionMaxDays = rr.r.Days, rr.r.MaxDays
			if row.Error == "" {
				row.Error = rr.err
			}
		}
		rows = append(rows, row)
	}
	s.repos = rows
	s.sortRows()
}

// sortRows recounts every row and sorts the rows by artifact bytes, largest
// first, and each row's items newest first. Call with s.mu held.
func (s *Service) sortRows() {
	for i := range s.repos {
		r := &s.repos[i]
		r.ArtifactCount, r.ArtifactBytes, r.CacheCount, r.CacheBytes = len(r.Artifacts), 0, len(r.Caches), 0
		for _, a := range r.Artifacts {
			r.ArtifactBytes += a.Bytes
		}
		for _, c := range r.Caches {
			r.CacheBytes += c.Bytes
		}
		slices.SortStableFunc(r.Artifacts, func(a, b model.ArtifactItem) int { return b.CreatedAt.Compare(a.CreatedAt) })
		slices.SortStableFunc(r.Caches, func(a, b model.CacheItem) int { return b.LastUsedAt.Compare(a.LastUsedAt) })
	}
	slices.SortStableFunc(s.repos, func(a, b model.RepoStorage) int {
		if c := cmp.Compare(b.ArtifactBytes, a.ArtifactBytes); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

func itemKey(kind string, id int64) string { return fmt.Sprintf("%s:%d", kind, id) }

// removeItem takes a deleted artifact or cache out of its row and keeps
// later listings from bringing it back. Call with s.mu held.
func (s *Service) removeItem(kind, repo string, id int64) {
	s.deleted[itemKey(kind, id)] = s.now()
	for i := range s.repos {
		r := &s.repos[i]
		if !strings.EqualFold(r.Name, repo) {
			continue
		}
		if kind == "artifact" {
			r.Artifacts = slices.DeleteFunc(r.Artifacts, func(a model.ArtifactItem) bool { return a.ID == id })
		} else {
			r.Caches = slices.DeleteFunc(r.Caches, func(c model.CacheItem) bool { return c.ID == id })
		}
	}
	s.sortRows()
}

// Snapshot is the last inventory and billing read with the operation and
// the deletions. Each row's cleanup and coverage come from the current
// config, so a saved policy shows at once.
func (s *Service) Snapshot() model.GitHubStorage {
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := model.GitHubStorage{
		QuotaError: s.quotaErr,
		Error:      s.listErr,
		Repos:      make([]model.RepoStorage, 0, len(s.repos)),
		Deletions:  append([]model.Deletion{}, s.deletions...),
	}
	if !s.checkedAt.IsZero() {
		at := s.checkedAt
		out.CheckedAt = &at
	}
	if s.quota != nil {
		q := *s.quota
		out.Quota = &q
	}
	if s.op != nil {
		op := *s.op
		op.Failed = append([]model.OperationFailure{}, s.op.Failed...)
		out.Operation = &op
	}
	for _, r := range s.repos {
		r.Artifacts = append([]model.ArtifactItem{}, r.Artifacts...)
		r.Caches = append([]model.CacheItem{}, r.Caches...)
		age, src := cfg.GitHubStorage.CleanupFor(r.Name)
		r.Cleanup, r.CleanupSource = config.CleanupText(age), src
		r.Covered = cfg.Repo(r.Name) != nil || cfg.Watched(r.Name)
		out.Repos = append(out.Repos, r)
	}
	return out
}

// Alert is the quota's status alert, nil until a billing read succeeds.
func (s *Service) Alert() *model.StorageAlert {
	s.mu.Lock()
	defer s.mu.Unlock()
	return AlertFrom(s.quota)
}
