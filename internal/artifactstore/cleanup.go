package artifactstore

import (
	"cmp"
	"context"
	"errors"
	"log"
	"maps"
	"slices"
	"strings"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

// pick is one object chosen for deletion. By is "expired", "cleanup" or
// "manual"; listed tells whether the object was in the last listing, which
// is what a deletion record needs.
type pick struct {
	object
	by     string
	listed bool
}

// oldestFirst sorts picks by last-modified time, then key, so a capped pass
// always takes the same ones.
func oldestFirst(a, b pick) int {
	if c := a.LastModified.Compare(b.LastModified); c != 0 {
		return c
	}
	return cmp.Compare(a.Key, b.Key)
}

// cleanup deletes, at most MaxDeletes in all and oldest first, the expired
// objects whatever their run's state, then the objects older than their
// repository's cleanup age whose run has completed. It then aborts the
// owner's multipart uploads started more than UploadAbortAge ago. Keys that
// are not the owner's artifacts are never touched. The active-run check is
// a GitHub read, so the age rule waits while the token is below the reserve
// kept for the runner manager.
func (s *Service) cleanup(cfg *config.Config, b Bucket) {
	now := s.now()
	s.mu.Lock()
	objs := slices.Clone(s.objects)
	s.mu.Unlock()
	a := cfg.ArtifactStore
	var expired []pick
	aged := map[string][]pick{}
	for _, o := range objs {
		if o.Repo == "" || !o.Headed {
			continue
		}
		if o.Meta.Expires != nil {
			if !now.Before(*o.Meta.Expires) {
				expired = append(expired, pick{object: o, by: "expired", listed: true})
			}
			continue
		}
		if age, _ := a.CleanupFor(o.Repo); age > 0 && now.Sub(o.LastModified) > age {
			aged[o.Repo] = append(aged[o.Repo], pick{object: o, by: "cleanup", listed: true})
		}
	}
	slices.SortFunc(expired, oldestFirst)
	picks := expired[:min(len(expired), MaxDeletes)]
	if len(picks) < MaxDeletes && len(aged) > 0 {
		var due []pick
		for _, repo := range slices.Sorted(maps.Keys(aged)) {
			if s.ctx.Err() != nil {
				return
			}
			if s.rateLow() {
				log.Printf("artifact store: cleanup by age waits: the token is below the rate reserve")
				break
			}
			name := displayName(cfg, repo)
			ctx, cancel := context.WithTimeout(s.ctx, callTimeout)
			active, err := ghstorage.ActiveRuns(ctx, s.GH, name)
			cancel()
			if err != nil {
				s.cleanupFailed(name, s.describe(err, name))
				continue
			}
			delete(s.cleanupErr, repo)
			for _, p := range aged[repo] {
				if !active[p.RunID] {
					due = append(due, p)
				}
			}
		}
		slices.SortFunc(due, oldestFirst)
		picks = append(picks, due[:min(len(due), MaxDeletes-len(picks))]...)
	}
	// The policy may have been saved since the pass read it. A pick that no
	// longer qualifies by age is skipped, not replaced.
	cur := s.Config().ArtifactStore
	picks = slices.DeleteFunc(picks, func(p pick) bool {
		age, _ := cur.CleanupFor(p.Repo)
		return p.by == "cleanup" && (age == 0 || s.now().Sub(p.LastModified) <= age)
	})
	if len(picks) > 0 && s.ctx.Err() == nil {
		deleted, err := s.remove(s.ctx, cfg, b, picks)
		if err != nil {
			s.cleanupFailed("", "cannot delete from bucket "+a.Bucket+": "+err.Error())
		} else {
			delete(s.cleanupErr, "")
		}
		s.tally(cfg, deleted)
	}
	s.abortUploads(cfg, b)
}

// tally writes one event per repository with what the cleanup deleted.
func (s *Service) tally(cfg *config.Config, deleted []pick) {
	type sum struct {
		n     int
		bytes int64
	}
	sums := map[string]*sum{}
	var order []string
	for _, p := range deleted {
		t := sums[p.Repo]
		if t == nil {
			t = &sum{}
			sums[p.Repo] = t
			order = append(order, p.Repo)
		}
		t.n++
		t.bytes += p.Size
	}
	for _, repo := range order {
		t := sums[repo]
		s.Events.Add("info", displayName(cfg, repo), "artifact store: cleanup deleted %d artifacts (%s)", t.n, sizeText(t.bytes))
	}
}

// cleanupFailed logs every cleanup failure but writes an event only when the
// failure changes: a pass that keeps failing repeats every InventoryInterval
// and would flood the event ring. repo is empty for the bucket's own
// failures.
func (s *Service) cleanupFailed(repo, msg string) {
	log.Printf("artifact store: cleanup stopped in %q: %s", repo, msg)
	key := strings.ToLower(repo)
	if s.cleanupErr[key] == msg {
		return
	}
	s.cleanupErr[key] = msg
	s.Events.Add("warn", repo, "artifact store: cleanup stopped: %s", msg)
}

// remove deletes picks in one bucket call. Every key the bucket confirms
// leaves the snapshot and is kept out of listings already under way; a
// confirmed pick that was listed is recorded as a deletion. It returns the
// confirmed picks with the call's error.
func (s *Service) remove(ctx context.Context, cfg *config.Config, b Bucket, picks []pick) ([]pick, error) {
	keys := make([]string, len(picks))
	for i, p := range picks {
		keys[i] = p.Key
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	done, err := b.Delete(ctx, keys)
	cancel()
	confirmed := map[string]bool{}
	for _, k := range done {
		confirmed[k] = true
	}
	var gone []pick
	now := s.now()
	s.mu.Lock()
	for _, p := range picks {
		if !confirmed[p.Key] {
			continue
		}
		gone = append(gone, p)
		s.deleted[p.Key] = now
		if p.listed {
			s.recordDeletion(model.Deletion{At: now, Repo: displayName(cfg, p.Repo), Kind: "artifact", Name: p.Name,
				Bytes: p.Size, Age: ageText(now.Sub(p.LastModified)), By: p.by, Store: "rustfs"})
		}
	}
	s.objects = slices.DeleteFunc(s.objects, func(o object) bool { return confirmed[o.Key] })
	if len(gone) > 0 {
		s.bump()
	}
	s.mu.Unlock()
	for _, p := range gone {
		log.Printf("artifact store: deleted %s (%s), %s", p.Key, sizeText(p.Size), p.by)
	}
	return gone, err
}

// recordDeletion keeps the MaxDeletions newest deletions, newest first. Call
// with s.mu held.
func (s *Service) recordDeletion(d model.Deletion) {
	s.deletions = append([]model.Deletion{d}, s.deletions...)
	if len(s.deletions) > MaxDeletions {
		s.deletions = s.deletions[:MaxDeletions]
	}
}

// abortUploads aborts the owner's multipart uploads started more than
// UploadAbortAge ago. A server that cannot list uploads is left alone.
func (s *Service) abortUploads(cfg *config.Config, b Bucket) {
	ctx, cancel := context.WithTimeout(s.ctx, listTimeout)
	defer cancel()
	ups, err := b.ListUploads(ctx)
	if err != nil {
		log.Printf("artifact store: cannot list unfinished uploads: %v", err)
		return
	}
	prefix := strings.ToLower(cfg.Owner) + "/"
	now := s.now()
	n := 0
	for _, u := range ups {
		if !strings.HasPrefix(u.Key, prefix) || now.Sub(u.Initiated) <= UploadAbortAge {
			continue
		}
		if _, _, _, _, ok := artifact.ParseObjectKey(u.Key); !ok {
			continue
		}
		actx, cancel := context.WithTimeout(ctx, callTimeout)
		err := b.Abort(actx, u.Key, u.UploadID)
		cancel()
		if err != nil && (!s3.IsNotFound(err) || bucketMissing(err)) {
			log.Printf("artifact store: cannot abort the upload of %s: %v", u.Key, err)
			continue
		}
		n++
	}
	if n > 0 {
		s.Events.Add("info", "", "artifact store: aborted %d unfinished uploads started more than a day ago", n)
	}
}

// bucketMissing tells a deleted bucket apart from a missing object or
// upload, which s3.IsNotFound counts as the same.
func bucketMissing(err error) bool {
	var se *s3.Error
	return errors.As(err, &se) && se.Code == "NoSuchBucket"
}

// The deletion records and events read as GitHub storage's do.
var (
	sizeText = ghstorage.SizeText
	ageText  = ghstorage.AgeText
)
