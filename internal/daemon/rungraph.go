package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/lru"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/wfgraph"
)

const (
	noteNoPermission = "Add Contents: read to the token to see which jobs depend on which."
	noteNoFile       = "This run has no workflow file to read."
	noteMismatch     = "The workflow file at this commit does not match the run's jobs."
	noteInvalid      = "ghr could not read %s."
	noteNoJobs       = "This run has no jobs yet."
	noteUnavailable  = "ghr could not read the workflow file just now; reload to try again."

	wfFileEntries = 64
	wfMissingTTL  = 10 * time.Minute
)

// pathRef matches a workflow path that carries the ref it was run from, as in
// ".github/workflows/ci.yml@refs/heads/main".
var pathRef = regexp.MustCompile(`^(\.github/workflows/[^@]+\.ya?ml)@.+$`)

// wfFile is a workflow file's bytes, the time GitHub answered 404 for it, or
// the mark that GitHub delivered it in a form that cannot be decoded.
type wfFile struct {
	data      []byte
	missingAt time.Time
	bad       bool
	// missingGen is the token generation the 404 at missingAt was answered
	// under: a file one token cannot see may be visible to the next.
	missingGen uint64
}

// wfCache keeps workflow files by repository, commit and path. A file at a
// commit never changes, so neither its bytes nor the mark that it cannot be
// decoded expire; only "not found" does, and a 403, 429 or 5xx is never
// kept, so granting the permission shows the graph on the next call.
type wfCache struct {
	mu    sync.Mutex
	files *lru.Cache[string, wfFile]
}

func (b *Backend) workflowFile(ctx context.Context, repo, sha, path string) ([]byte, error) {
	c := &b.wfFiles
	key := strings.ToLower(repo) + "\x00" + sha + "\x00" + path
	c.mu.Lock()
	if c.files == nil {
		c.files = lru.New[string, wfFile](wfFileEntries, 0)
	}
	f, ok := c.files.Get(key)
	gen := b.tokenGen()
	c.mu.Unlock()
	switch {
	case ok && f.bad:
		return nil, github.ErrBadContent
	case ok && f.missingAt.IsZero():
		return f.data, nil
	case ok && f.missingGen == gen && b.now().Sub(f.missingAt) < wfMissingTTL:
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
	}
	data, err := b.GH.GetContent(ctx, repo, path, sha)
	switch {
	case err == nil:
		f = wfFile{data: data}
	case github.IsKind(err, github.ErrNotFound):
		f = wfFile{missingAt: b.now(), missingGen: gen}
	case errors.Is(err, github.ErrBadContent):
		f = wfFile{bad: true}
	default:
		return nil, err
	}
	c.mu.Lock()
	c.files.Add(key, f, 0)
	c.mu.Unlock()
	return data, err
}

// runGraph builds the job graph of a run from its workflow file at sha. When
// no graph can be drawn it returns the note that says why, so a file that
// cannot be read costs the graph and not the run page.
func (b *Backend) runGraph(ctx context.Context, repo, sha, path string, jobs []github.Job) (graph *model.RunGraph, note string) {
	// A defect in reading a workflow file costs the graph, not the run page.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("run graph %s: panic: %v", path, r)
			graph, note = nil, fmt.Sprintf(noteInvalid, path)
		}
	}()
	// Without jobs there is nothing to lay out, so the file is not read.
	if len(jobs) == 0 {
		return nil, noteNoJobs
	}
	if m := pathRef.FindStringSubmatch(path); m != nil {
		path = m[1]
	}
	if !strings.HasPrefix(path, ".github/workflows/") {
		return nil, noteNoFile
	}
	data, err := b.workflowFile(ctx, repo, sha, path)
	var ge *github.APIError
	switch {
	case github.IsKind(err, github.ErrNotFound):
		return nil, noteNoFile
	case errors.As(err, &ge) && ge.Kind == github.ErrAuth && ge.Status == 403:
		return nil, noteNoPermission
	case errors.Is(err, github.ErrBadContent):
		return nil, fmt.Sprintf(noteInvalid, path)
	case err != nil:
		return nil, noteUnavailable
	}
	w, err := wfgraph.Parse(data)
	if err != nil {
		return nil, fmt.Sprintf(noteInvalid, path)
	}
	in := make([]wfgraph.APIJob, 0, len(jobs))
	for _, j := range jobs {
		in = append(in, wfgraph.APIJob{ID: j.ID, Name: j.Name})
	}
	nodes, err := wfgraph.Build(w, in)
	switch {
	case errors.Is(err, wfgraph.ErrMismatch):
		return nil, noteMismatch
	case err != nil:
		return nil, fmt.Sprintf(noteInvalid, path)
	}
	return &model.RunGraph{Nodes: nodes}, ""
}
