package ghstorage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrUnknownRepo      = errors.New("not in the last GitHub storage inventory")
	ErrOperationRunning = errors.New("another GitHub storage operation is running")
)

// BadInput is a request the service refuses as given; the daemon answers 400
// with its text.
type BadInput string

func (e BadInput) Error() string { return string(e) }

// maxRetentionDays is GitHub's longest retention, the limit while a
// repository's own is unknown.
const maxRetentionDays = 400

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// find is the index of repo's row, matched ignoring case. Call with s.mu held.
func (s *Service) find(repo string) (int, bool) {
	for i, r := range s.repos {
		if strings.EqualFold(r.Name, repo) {
			return i, true
		}
	}
	return 0, false
}

// Preview counts what a cleanup age would delete now, from the snapshot.
func (s *Service) Preview(cleanup, repo string) ([]model.StoragePreview, error) {
	age, err := config.ParseCleanup(cleanup)
	if cleanup == "" || err != nil || age < 0 {
		return nil, BadInput("cleanup must be off or a duration such as 2d")
	}
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo != "" {
		i, ok := s.find(repo)
		if !ok {
			return nil, ErrUnknownRepo
		}
		repo = s.repos[i].Name
	}
	return Preview(s.repos, cfg.GitHubStorage, repo, age, s.now()), nil
}

// DeleteArtifact deletes one artifact of the snapshot and answers when done.
func (s *Service) DeleteArtifact(ctx context.Context, repo string, id int64) error {
	s.mu.Lock()
	var it model.ArtifactItem
	found := false
	if i, ok := s.find(repo); ok {
		repo = s.repos[i].Name
		it, found = findItem(s.repos[i].Artifacts, func(a model.ArtifactItem) bool { return a.ID == id })
	}
	s.mu.Unlock()
	if !found {
		return ErrUnknownRepo
	}
	_, err := s.deleteArtifact(ctx, repo, it, "manual")
	return err
}

// DeleteCache deletes one cache entry of the snapshot and answers when done.
func (s *Service) DeleteCache(ctx context.Context, repo string, id int64) error {
	s.mu.Lock()
	var c model.CacheItem
	found := false
	if i, ok := s.find(repo); ok {
		repo = s.repos[i].Name
		c, found = findItem(s.repos[i].Caches, func(c model.CacheItem) bool { return c.ID == id })
	}
	s.mu.Unlock()
	if !found {
		return ErrUnknownRepo
	}
	_, err := s.deleteCache(ctx, repo, c, "manual")
	return err
}

func findItem[T any](items []T, match func(T) bool) (T, bool) {
	if i := slices.IndexFunc(items, match); i >= 0 {
		return items[i], true
	}
	var zero T
	return zero, false
}

// deleteCache deletes one cache entry within the write budget, as
// deleteArtifact does an artifact. Its age is the time since its last use.
func (s *Service) deleteCache(ctx context.Context, repo string, c model.CacheItem, by string) (bool, error) {
	var deleted bool
	err := s.write(func() error {
		var err error
		deleted, err = s.GH.DeleteCache(ctx, repo, c.ID)
		return err
	})
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeItem("cache", repo, c.ID)
	if deleted {
		now := s.now()
		age := AgeText(now.Sub(c.LastUsedAt))
		s.recordDeletion(model.Deletion{At: now, Repo: repo, Kind: "cache", Name: c.Key, Bytes: c.Bytes, Age: age, By: by})
		log.Printf("github storage: deleted cache %s (%s) in %s, age %s", c.Key, SizeText(c.Bytes), repo, age)
	}
	s.bump()
	return deleted, nil
}

// DeleteArtifacts starts deleting, in the background, either the named
// artifacts or those older than an age. The age skips the artifacts of runs
// that have not completed, as the cleanup does; named ones are deleted as
// named. It returns the operation's id.
func (s *Service) DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error) {
	if (len(req.IDs) > 0) == (req.OlderThan != "") {
		return "", BadInput("give either ids or older_than")
	}
	var age time.Duration
	if req.OlderThan != "" {
		d, err := config.ParseDuration(req.OlderThan)
		if err != nil || d.D() < time.Hour {
			return "", BadInput("older_than must be a duration of at least 1h, such as 24h")
		}
		age = d.D()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.find(repo)
	if !ok {
		return "", ErrUnknownRepo
	}
	name, items := s.repos[i].Name, slices.Clone(s.repos[i].Artifacts)
	op, err := s.startOp("delete-artifacts", name, len(req.IDs))
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		var todo []model.ArtifactItem
		if age > 0 {
			active, err := ActiveRuns(ctx, s.GH, name)
			if err != nil {
				s.fail(model.OperationFailure{Repo: name, Error: s.describe(err, name)})
			} else {
				todo = Candidates(items, age, s.now(), active)
			}
			s.mu.Lock()
			op.Total = len(todo)
			s.mu.Unlock()
		} else {
			todo = pickByID(name, items, req.IDs, func(a model.ArtifactItem) int64 { return a.ID }, s.fail)
		}
		n, bytes := s.deleteEach(ctx, name, len(todo), func(ctx context.Context, i int) (bool, int64, int64, error) {
			deleted, err := s.deleteArtifact(ctx, name, todo[i], "manual")
			return deleted, todo[i].Bytes, todo[i].ID, err
		})
		s.finishOp(name, "deleted %d artifacts (%s)", n, SizeText(bytes))
	})
	return op.ID, nil
}

// DeleteCaches starts deleting, in the background, either the named cache
// entries or all of the repository's. It returns the operation's id.
func (s *Service) DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error) {
	if (len(req.IDs) > 0) == req.All {
		return "", BadInput("give either ids or all")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.find(repo)
	if !ok {
		return "", ErrUnknownRepo
	}
	name, items := s.repos[i].Name, slices.Clone(s.repos[i].Caches)
	total := len(req.IDs)
	if req.All {
		total = len(items)
	}
	op, err := s.startOp("delete-caches", name, total)
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		todo := items
		if !req.All {
			todo = pickByID(name, items, req.IDs, func(c model.CacheItem) int64 { return c.ID }, s.fail)
		}
		n, bytes := s.deleteEach(ctx, name, len(todo), func(ctx context.Context, i int) (bool, int64, int64, error) {
			deleted, err := s.deleteCache(ctx, name, todo[i], "manual")
			return deleted, todo[i].Bytes, todo[i].ID, err
		})
		s.finishOp(name, "deleted %d caches (%s)", n, SizeText(bytes))
	})
	return op.ID, nil
}

// pickByID returns the items named by ids, in the order named, and reports
// each id not among them as failed.
func pickByID[T any](repo string, items []T, ids []int64, id func(T) int64, fail func(model.OperationFailure)) []T {
	var out []T
	for _, want := range ids {
		if it, ok := findItem(items, func(x T) bool { return id(x) == want }); ok {
			out = append(out, it)
		} else {
			fail(model.OperationFailure{Repo: repo, ID: want, Error: "not in the last inventory"})
		}
	}
	return out
}

// startOp makes a new operation the running one. Call with s.mu held.
func (s *Service) startOp(kind, repo string, total int) (*model.GitHubOperation, error) {
	switch {
	case s.closed:
		return nil, ErrClosed
	case s.opRunning:
		return nil, ErrOperationRunning
	}
	s.op = &model.GitHubOperation{ID: s.newID(), Kind: kind, Repo: repo, Total: total,
		Failed: []model.OperationFailure{}, StartedAt: s.now()}
	s.opRunning = true
	s.bump()
	return s.op, nil
}

// runOp runs an operation's work in the background; Close cancels its
// context and Wait waits for it.
func (s *Service) runOp(fn func(ctx context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn(s.ctx)
	}()
}

func (s *Service) fail(f model.OperationFailure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.op.Failed = append(s.op.Failed, f)
	s.bump()
}

// deleteEach runs del for items 0 to count-1, counting each on the running
// operation, and stops at the first error: the rest would fail the same way.
// It returns how many were deleted and their bytes; a 404 counts as done
// but not deleted.
func (s *Service) deleteEach(ctx context.Context, repo string, count int, del func(ctx context.Context, i int) (deleted bool, bytes, id int64, err error)) (int, int64) {
	n, total := 0, int64(0)
	for i := range count {
		if ctx.Err() != nil {
			break
		}
		deleted, bytes, id, err := del(ctx, i)
		if err != nil {
			s.fail(model.OperationFailure{Repo: repo, ID: id, Error: s.opErr(err, repo)})
			break
		}
		s.mu.Lock()
		s.op.Done++
		if deleted {
			n++
			total += bytes
			s.op.Bytes += bytes
		}
		s.bump()
		s.mu.Unlock()
	}
	return n, total
}

func (s *Service) opErr(err error, repo string) string {
	var be *BudgetError
	var bi BadInput
	if errors.As(err, &be) || errors.As(err, &bi) {
		return err.Error()
	}
	return s.describe(err, repo)
}

// finishOp ends the running operation and writes one event with its outcome.
func (s *Service) finishOp(repo, format string, args ...any) {
	s.mu.Lock()
	now := s.now()
	s.op.FinishedAt = &now
	s.opRunning = false
	failed := len(s.op.Failed)
	s.bump()
	s.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	if failed > 0 {
		s.Events.Add("warn", repo, "%s; %d failed", msg, failed)
		return
	}
	s.Events.Add("info", repo, "%s", msg)
}

// SetRetention sets one repository's artifact and log retention and answers
// when done. GitHub refusing the value is a BadInput with its message.
func (s *Service) SetRetention(ctx context.Context, repo string, days int) error {
	s.mu.Lock()
	i, ok := s.find(repo)
	var name string
	var most int
	if ok {
		name, most = s.repos[i].Name, s.repos[i].RetentionMaxDays
	}
	s.mu.Unlock()
	if !ok {
		return ErrUnknownRepo
	}
	if most == 0 {
		most = maxRetentionDays
	}
	if days < 1 || days > most {
		return BadInput(fmt.Sprintf("days must be from 1 to %d", most))
	}
	return s.setRetention(ctx, name, days)
}

func (s *Service) setRetention(ctx context.Context, repo string, days int) error {
	err := s.write(func() error {
		if err := s.GH.SetRetention(ctx, repo, days); err != nil {
			return err
		}
		s.publishRetention(repo, days)
		return nil
	})
	var ge *github.APIError
	if errors.As(err, &ge) && ge.Kind == github.ErrUnprocessable {
		return BadInput(ge.Message)
	}
	return err
}

// publishRetention records a retention GitHub accepted. It runs inside the
// write slot so two writes publish in the order they reached GitHub, and it
// is stamped with a time later than the start of any listing that read the
// old value.
func (s *Service) publishRetention(repo string, days int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(repo)
	rr := s.retention[key]
	rr.r.Days = days
	rr.written = s.now()
	old := rr.err
	rr.err = ""
	s.retention[key] = rr
	if i, ok := s.find(repo); ok {
		s.repos[i].RetentionDays = days
		if old != "" && s.repos[i].Error == old {
			s.repos[i].Error = ""
		}
	}
	s.bump()
}

// SetRetentionAll starts setting one retention on every repository of the
// snapshot in the background. A repository whose most allowed days are
// below the value is skipped and reported as failed. It returns the
// operation's id.
func (s *Service) SetRetentionAll(days int) (string, error) {
	if days < 1 || days > maxRetentionDays {
		return "", BadInput(fmt.Sprintf("days must be from 1 to %d", maxRetentionDays))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	type target struct {
		name string
		most int
	}
	var targets []target
	for _, r := range s.repos {
		targets = append(targets, target{r.Name, r.RetentionMaxDays})
	}
	op, err := s.startOp("set-retention", "", len(targets))
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		set := 0
		for _, t := range targets {
			if ctx.Err() != nil {
				break
			}
			if t.most != 0 && t.most < days {
				s.fail(model.OperationFailure{Repo: t.name, Error: fmt.Sprintf("%s allows at most %d days", t.name, t.most)})
				continue
			}
			err := s.setRetention(ctx, t.name, days)
			if err != nil {
				s.fail(model.OperationFailure{Repo: t.name, Error: s.opErr(err, t.name)})
				var be *BudgetError
				if errors.As(err, &be) {
					break
				}
				continue
			}
			set++
			s.mu.Lock()
			s.op.Done++
			s.bump()
			s.mu.Unlock()
		}
		s.finishOp("", "set retention to %d days on %d of %d repositories", days, set, len(targets))
	})
	return op.ID, nil
}
