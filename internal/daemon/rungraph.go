package daemon

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/wfgraph"
)

const (
	noteNoPermission = "Add Contents: read to the token to see which jobs depend on which."
	noteNoFile       = "This run has no workflow file to read."
	noteMismatch     = "The workflow file at this commit does not match the run's jobs."
	noteInvalid      = "ghr could not read %s."

	wfFileEntries = 64
	wfMissingTTL  = 10 * time.Minute
)

// wfFile is a workflow file's bytes, or the time GitHub answered 404 for it.
type wfFile struct {
	data      []byte
	missingAt time.Time
}

// wfCache keeps workflow files by repository, commit and path. A file at a
// commit never changes, so bytes do not expire; only "not found" does, and
// a 403, 429 or 5xx is never kept, so granting the permission shows the
// graph on the next call.
type wfCache struct {
	mu    sync.Mutex
	files *lru[string, wfFile]
}

func (b *Backend) workflowFile(ctx context.Context, repo, sha, path string) ([]byte, error) {
	c := &b.wfFiles
	key := strings.ToLower(repo) + "\x00" + sha + "\x00" + path
	c.mu.Lock()
	if c.files == nil {
		c.files = newLRU[string, wfFile](wfFileEntries, 0)
	}
	f, ok := c.files.get(key)
	c.mu.Unlock()
	if ok && f.missingAt.IsZero() {
		return f.data, nil
	}
	if ok && b.now().Sub(f.missingAt) < wfMissingTTL {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
	}
	data, err := b.GH.GetContent(ctx, repo, path, sha)
	switch {
	case err == nil:
		f = wfFile{data: data}
	case github.IsKind(err, github.ErrNotFound):
		f = wfFile{missingAt: b.now()}
	default:
		return nil, err
	}
	c.mu.Lock()
	c.files.add(key, f, 0)
	c.mu.Unlock()
	return data, err
}

// runGraph builds the job graph of a run from its workflow file at sha. When
// no graph can be drawn it returns the note that says why; an error means
// the run details call fails as a whole.
func (b *Backend) runGraph(ctx context.Context, repo, sha, path string, jobs []github.Job) (*model.RunGraph, string, error) {
	if !strings.HasPrefix(path, ".github/workflows/") {
		return nil, noteNoFile, nil
	}
	data, err := b.workflowFile(ctx, repo, sha, path)
	var ge *github.APIError
	switch {
	case github.IsKind(err, github.ErrNotFound):
		return nil, noteNoFile, nil
	case errors.As(err, &ge) && ge.Kind == github.ErrAuth && ge.Status == 403:
		return nil, noteNoPermission, nil
	case err != nil:
		return nil, "", err
	}
	w, err := wfgraph.Parse(data)
	if err != nil {
		return nil, fmt.Sprintf(noteInvalid, path), nil
	}
	in := make([]wfgraph.APIJob, 0, len(jobs))
	for _, j := range jobs {
		in = append(in, wfgraph.APIJob{ID: j.ID, Name: j.Name})
	}
	nodes, err := wfgraph.Build(w, in)
	switch {
	case errors.Is(err, wfgraph.ErrMismatch):
		return nil, noteMismatch, nil
	case err != nil:
		return nil, fmt.Sprintf(noteInvalid, path), nil
	}
	return &model.RunGraph{Nodes: nodes}, "", nil
}

// lru is a map bounded by entry count and, when maxSize is above 0, by the
// total of the sizes given to add; the least recently used entries go
// first. Callers hold their own lock.
type lru[K comparable, V any] struct {
	maxEntries int
	maxSize    int64
	size       int64
	order      *list.List // front is the most recently used
	items      map[K]*list.Element
}

type lruItem[K comparable, V any] struct {
	key  K
	val  V
	size int64
}

func newLRU[K comparable, V any](maxEntries int, maxSize int64) *lru[K, V] {
	return &lru[K, V]{maxEntries: maxEntries, maxSize: maxSize, order: list.New(), items: map[K]*list.Element{}}
}

func (c *lru[K, V]) get(k K) (V, bool) {
	e, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*lruItem[K, V]).val, true
}

// add stores v under k. An entry larger than maxSize on its own is not kept.
func (c *lru[K, V]) add(k K, v V, size int64) {
	c.remove(k)
	if c.maxSize > 0 && size > c.maxSize {
		return
	}
	c.items[k] = c.order.PushFront(&lruItem[K, V]{key: k, val: v, size: size})
	c.size += size
	for c.order.Len() > c.maxEntries || (c.maxSize > 0 && c.size > c.maxSize) {
		c.remove(c.order.Back().Value.(*lruItem[K, V]).key)
	}
}

func (c *lru[K, V]) remove(k K) {
	e, ok := c.items[k]
	if !ok {
		return
	}
	c.order.Remove(e)
	delete(c.items, k)
	c.size -= e.Value.(*lruItem[K, V]).size
}

func (c *lru[K, V]) len() int { return c.order.Len() }
