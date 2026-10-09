package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/darkraise/ghr/internal/github"
)

// fakeRuns answers the run details calls; fakeGH embeds it. Each call is
// recorded as "<Method> <repo> <id>", and a missing entry answers 404.
type fakeRuns struct {
	rmu         sync.Mutex
	runByID     map[int64]github.Run        // GetRun
	runErr      error                       // returned by GetRun when set
	runWait     chan struct{}               // when set, GetRun waits for it or the context
	attemptRuns map[string]github.Run       // GetRunAttempt, by attemptKey
	attemptJobs map[string][]github.Job     // ListAttemptJobs, by attemptKey
	jobByID     map[int64]github.Job        // GetJob
	artifacts   map[int64][]github.Artifact // ListArtifacts, by run ID
	content     map[string][]byte           // GetContent, by contentKey
	contentErr  error                       // returned by GetContent when set
	logs        map[int64][]byte            // JobLog, by job ID
	logErr      map[int64]error             // returned by JobLog for that job
	logWait     chan struct{}               // when set, JobLog waits for it or the context
	logLen      map[int64]int64             // OpenJobLog's length for that job, when set (-1: none declared)
	logClosed   int                         // bodies OpenJobLog returned that were closed
	logReads    int                         // Read calls on bodies OpenJobLog returned
	logCtx      context.Context             // the context of the last OpenJobLog call
	artURL      string                      // returned by ArtifactURL
	artErr      error                       // returned by ArtifactURL when set
	calls       []string
	logInFlight int
	logPeak     int
}

func attemptKey(id int64, attempt int) string { return fmt.Sprintf("%d/%d", id, attempt) }

func contentKey(repo, path, ref string) string { return repo + ":" + ref + ":" + path }

func ghNotFound() error {
	return &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
}

func (f *fakeRuns) record(method, repo string, id any) {
	f.rmu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s %s %v", method, repo, id))
	f.rmu.Unlock()
}

// count is how many recorded calls are prefix or start with prefix and a
// space, so "GetRun" does not count GetRunAttempt calls.
func (f *fakeRuns) count(prefix string) int {
	f.rmu.Lock()
	defer f.rmu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == prefix || strings.HasPrefix(c, prefix+" ") {
			n++
		}
	}
	return n
}

func (f *fakeRuns) GetRun(ctx context.Context, repo string, id int64) (*github.Run, error) {
	f.record("GetRun", repo, id)
	if f.runWait != nil {
		select {
		case <-f.runWait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.rmu.Lock()
	defer f.rmu.Unlock()
	if f.runErr != nil {
		return nil, f.runErr
	}
	r, ok := f.runByID[id]
	if !ok {
		return nil, ghNotFound()
	}
	return &r, nil
}

func (f *fakeRuns) GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*github.Run, error) {
	f.record("GetRunAttempt", repo, attemptKey(id, attempt))
	f.rmu.Lock()
	defer f.rmu.Unlock()
	r, ok := f.attemptRuns[attemptKey(id, attempt)]
	if !ok {
		return nil, ghNotFound()
	}
	return &r, nil
}

func (f *fakeRuns) ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]github.Job, error) {
	f.record("ListAttemptJobs", repo, attemptKey(id, attempt))
	f.rmu.Lock()
	defer f.rmu.Unlock()
	return f.attemptJobs[attemptKey(id, attempt)], nil
}

func (f *fakeRuns) GetJob(ctx context.Context, repo string, jobID int64) (*github.Job, error) {
	f.record("GetJob", repo, jobID)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	j, ok := f.jobByID[jobID]
	if !ok {
		return nil, ghNotFound()
	}
	return &j, nil
}

func (f *fakeRuns) ListArtifacts(ctx context.Context, repo string, runID int64) ([]github.Artifact, error) {
	f.record("ListArtifacts", repo, runID)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	return f.artifacts[runID], nil
}

func (f *fakeRuns) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	f.record("GetContent", repo, ref+":"+path)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	if f.contentErr != nil {
		return nil, f.contentErr
	}
	data, ok := f.content[contentKey(repo, path, ref)]
	if !ok {
		return nil, ghNotFound()
	}
	return data, nil
}

func (f *fakeRuns) JobLog(ctx context.Context, repo string, jobID int64, limit int64) ([]byte, error) {
	f.record("JobLog", repo, jobID)
	f.rmu.Lock()
	f.logInFlight++
	f.logPeak = max(f.logPeak, f.logInFlight)
	wait := f.logWait
	f.rmu.Unlock()
	defer func() {
		f.rmu.Lock()
		f.logInFlight--
		f.rmu.Unlock()
	}()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.rmu.Lock()
	defer f.rmu.Unlock()
	if err := f.logErr[jobID]; err != nil {
		return nil, err
	}
	data, ok := f.logs[jobID]
	if !ok {
		return nil, ghNotFound()
	}
	if int64(len(data)) > limit {
		return nil, github.ErrTooLarge
	}
	return data, nil
}

// OpenJobLog answers from logs and logErr like JobLog, without waiting on
// logWait.
func (f *fakeRuns) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	f.record("OpenJobLog", repo, jobID)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	f.logCtx = ctx
	if err := f.logErr[jobID]; err != nil {
		return nil, 0, err
	}
	data, ok := f.logs[jobID]
	if !ok {
		return nil, 0, ghNotFound()
	}
	size := int64(len(data))
	if n, ok := f.logLen[jobID]; ok {
		size = n
	}
	return &fakeBody{r: bytes.NewReader(data), f: f}, size, nil
}

// fakeBody counts its reads in logReads and its closes in logClosed.
type fakeBody struct {
	r io.Reader
	f *fakeRuns
}

func (b *fakeBody) Read(p []byte) (int, error) {
	b.f.rmu.Lock()
	b.f.logReads++
	b.f.rmu.Unlock()
	return b.r.Read(p)
}

func (b *fakeBody) Close() error {
	b.f.rmu.Lock()
	b.f.logClosed++
	b.f.rmu.Unlock()
	return nil
}

func (f *fakeRuns) ArtifactURL(ctx context.Context, repo string, id int64) (string, error) {
	f.record("ArtifactURL", repo, id)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	return f.artURL, f.artErr
}
