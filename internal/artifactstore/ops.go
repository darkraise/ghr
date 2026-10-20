package artifactstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

var ErrOperationRunning = errors.New("another self-hosted storage operation is running")

const (
	// maxDeleteKeys is how many keys one delete request may name.
	maxDeleteKeys = 1000
	// deleteChunk is how many keys one bucket call deletes, so the
	// operation's progress moves while a long delete runs.
	deleteChunk = 100
)

var linkTTLs = map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// adminBucket is the bucket reached with the admin key, for the API's
// requests.
func (s *Service) adminBucket() (Bucket, error) {
	a := s.Config().ArtifactStore
	if !a.Configured() {
		return nil, ErrNotConfigured
	}
	keys := s.loadKeys()
	if keys.Admin == nil {
		return nil, ErrNoAdminKey
	}
	return s.bucket(a, keys.Admin)
}

// Delete starts deleting, in the background, the named objects of repo. Every
// key must lie under the repository's prefix. A key the bucket confirms but
// the last listing did not hold counts as done but not deleted. It returns
// the operation's id.
func (s *Service) Delete(repo string, keys []string) (string, error) {
	cfg := s.Config()
	if !cfg.ArtifactStore.Configured() {
		return "", ErrNotConfigured
	}
	if len(keys) == 0 {
		return "", BadInput("give the keys of the artifacts to delete")
	}
	if len(keys) > maxDeleteKeys {
		return "", BadInput(fmt.Sprintf("delete at most %d artifacts at once", maxDeleteKeys))
	}
	prefix := artifact.RepoPrefix(cfg.Owner, repo)
	todo := slices.Compact(slices.Sorted(slices.Values(keys)))
	for _, k := range todo {
		if err := artifact.ScopeKey(k, prefix); err != nil {
			return "", BadInput(err.Error())
		}
	}
	b, err := s.adminBucket()
	if err != nil {
		return "", err
	}
	name := displayName(cfg, repo)
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.startOp("delete-objects", name, len(todo))
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		n, bytes := 0, int64(0)
		for chunk := range slices.Chunk(todo, deleteChunk) {
			if ctx.Err() != nil {
				break
			}
			gone, err := s.remove(ctx, cfg, b, s.picksFor(chunk))
			s.mu.Lock()
			s.op.Done += len(gone)
			for _, p := range gone {
				if p.listed {
					n++
					bytes += p.Size
					s.op.Bytes += p.Size
				}
			}
			s.bump()
			s.mu.Unlock()
			if err != nil {
				s.fail(model.OperationFailure{Repo: name, Error: "cannot delete from bucket " + cfg.ArtifactStore.Bucket + ": " + err.Error()})
				break
			}
			if len(gone) < len(chunk) {
				for _, k := range chunk {
					if !slices.ContainsFunc(gone, func(p pick) bool { return p.Key == k }) {
						s.fail(model.OperationFailure{Repo: name, Error: k + " was not deleted"})
					}
				}
			}
		}
		s.finishOp(name, "artifact store: deleted %d artifacts (%s)", n, sizeText(bytes))
	})
	return op.ID, nil
}

// picksFor turns keys into manual picks, with the details of those in the
// last listing.
func (s *Service) picksFor(keys []string) []pick {
	s.mu.Lock()
	defer s.mu.Unlock()
	listed := map[string]object{}
	for _, o := range s.objects {
		listed[o.Key] = o
	}
	out := make([]pick, 0, len(keys))
	for _, k := range keys {
		if o, ok := listed[k]; ok {
			out = append(out, pick{object: o, by: "manual", listed: true})
			continue
		}
		o := object{Object: s3.Object{Key: k}}
		if _, repo, run, name, ok := artifact.ParseObjectKey(k); ok {
			o.Repo, o.RunID, o.Name = strings.ToLower(repo), run, name
		}
		out = append(out, pick{object: o, by: "manual"})
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
	s.op = &model.GitHubOperation{ID: newID(), Kind: kind, Repo: repo, Total: total,
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

// Link signs a download link to one of the owner's objects, valid for ttl:
// "1h", "24h" or "7d".
func (s *Service) Link(key, ttl string) (model.ArtifactLink, error) {
	d, ok := linkTTLs[ttl]
	if !ok {
		return model.ArtifactLink{}, BadInput("ttl must be 1h, 24h or 7d")
	}
	cfg := s.Config()
	if !cfg.ArtifactStore.Configured() {
		return model.ArtifactLink{}, ErrNotConfigured
	}
	if err := artifact.ScopeKey(key, strings.ToLower(cfg.Owner)+"/"); err != nil {
		return model.ArtifactLink{}, BadInput(err.Error())
	}
	b, err := s.adminBucket()
	if err != nil {
		return model.ArtifactLink{}, err
	}
	url, expires, err := b.PresignGet(key, d)
	if err != nil {
		return model.ArtifactLink{}, err
	}
	return model.ArtifactLink{URL: url, ExpiresAt: expires}, nil
}

// Test checks with the admin key that the endpoint answers, that the owner's
// objects can be listed and that one of them can be read. Each check is
// reported; a check that could not run says why.
func (s *Service) Test(ctx context.Context) (model.ArtifactStoreTest, error) {
	cfg := s.Config()
	a := cfg.ArtifactStore
	b, err := s.adminBucket()
	if err != nil {
		return model.ArtifactStoreTest{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	check := func(name string, ok bool, detail string) model.ArtifactStoreCheck {
		return model.ArtifactStoreCheck{Name: name, OK: ok, Detail: detail}
	}
	objs, err := b.List(ctx, strings.ToLower(cfg.Owner)+"/")
	var se *s3.Error
	switch {
	case err == nil:
	case errors.As(err, &se):
		return model.ArtifactStoreTest{Checks: []model.ArtifactStoreCheck{
			check("reach", true, "reached "+a.Endpoint),
			check("list", false, "cannot list bucket "+a.Bucket+": "+err.Error()),
			check("head", false, "not run: the listing failed"),
		}}, nil
	default:
		return model.ArtifactStoreTest{Checks: []model.ArtifactStoreCheck{
			check("reach", false, "cannot reach "+a.Endpoint+": "+err.Error()),
			check("list", false, "not run: the endpoint cannot be reached"),
			check("head", false, "not run: the endpoint cannot be reached"),
		}}, nil
	}
	out := model.ArtifactStoreTest{Checks: []model.ArtifactStoreCheck{
		check("reach", true, "reached "+a.Endpoint),
		check("list", true, fmt.Sprintf("listed %d objects in bucket %s", len(objs), a.Bucket)),
	}}
	if len(objs) == 0 {
		out.Checks = append(out.Checks, check("head", true, "no artifacts stored yet, so none was read"))
		return out, nil
	}
	key := objs[0].Key
	if _, err := b.Head(ctx, key); err != nil {
		out.Checks = append(out.Checks, check("head", false, "cannot read the details of "+key+": "+err.Error()))
	} else {
		out.Checks = append(out.Checks, check("head", true, "read the details of "+key))
	}
	return out, nil
}

// RunObjects lists one run's objects now, newest first, with their details
// from the cache or read live.
func (s *Service) RunObjects(ctx context.Context, repo string, runID int64) ([]model.ArtifactStoreItem, error) {
	cfg := s.Config()
	if !cfg.ArtifactStore.Configured() {
		return nil, ErrNotConfigured
	}
	if repo == "" || repo == "." || repo == ".." || strings.ContainsAny(repo, `/\`) {
		return nil, BadInput(repo + " is not a repository name")
	}
	if runID <= 0 {
		return nil, BadInput("the run id must be a positive number")
	}
	b, err := s.adminBucket()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	listed, err := b.List(ctx, artifact.RunPrefix(cfg.Owner, repo, runID))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	cache := s.cache
	s.mu.Unlock()
	objs := s.details(ctx, b, classify(listed, cfg.Owner), cache)
	items := []model.ArtifactStoreItem{}
	s.mu.Lock()
	for _, o := range objs {
		if o.Repo != "" && o.RunID == runID && !s.isDeleted(o) {
			items = append(items, itemOf(o))
		}
	}
	s.mu.Unlock()
	slices.SortStableFunc(items, newestFirst)
	return items, nil
}
