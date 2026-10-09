package daemon

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/joblog"
	"github.com/darkraise/ghr/internal/model"
)

const (
	parseLimit        = 4 << 20
	rawLimit          = 32 << 20
	logSlots          = 4
	annotationEntries = 500
	logRetryTTL       = 10 * time.Minute
)

var (
	// parsedLogBytes bounds the parsed-log cache by the raw logs' total size;
	// tests shrink it before the cache is first used.
	parsedLogBytes int64 = 32 << 20
	logDeadline          = 60 * time.Second
	// logRouteWait and rawLogDeadline bound each log request from its start
	// (job read, slot wait and download), keeping the answer under the web
	// listener's 60 s WriteTimeout; a parsed download that outlasts the wait
	// still finishes and fills the cache for the next call.
	logRouteWait   = 50 * time.Second
	rawLogDeadline = 50 * time.Second
)

type logOutcome int

const (
	logParsed logOutcome = iota
	logExpired
	logTooLarge
	logFailed
)

// jobNotes is one finished job's annotations and the count beyond the cap.
type jobNotes struct {
	items   []model.Annotation
	omitted int
}

type logResult struct {
	outcome logOutcome
	log     model.JobLog
	notes   jobNotes
	err     error // set for logFailed
}

// logCall is one log download that the log route and the annotations route
// share.
type logCall struct {
	done chan struct{}
	res  logResult
}

// logMiss is an expired, too large or failed outcome, kept logRetryTTL.
type logMiss struct {
	at  time.Time
	res logResult
}

type logCache struct {
	mu sync.Mutex
	// slots limits log downloads across the whole daemon, which runs one
	// Backend.
	slots  chan struct{}
	calls  map[int64]*logCall
	parsed *lru[int64, model.JobLog]
	notes  *lru[int64, jobNotes]
	misses map[int64]logMiss
}

// initLocked creates the cache on first use; the caller holds mu.
func (c *logCache) initLocked() {
	if c.slots != nil {
		return
	}
	c.slots = make(chan struct{}, logSlots)
	c.calls = map[int64]*logCall{}
	c.parsed = newLRU[int64, model.JobLog](1<<30, parsedLogBytes)
	c.notes = newLRU[int64, jobNotes](annotationEntries, 0)
	c.misses = map[int64]logMiss{}
}

// logJob is what a download needs to parse a job's log.
type logJob struct {
	repo  string
	id    int64
	name  string
	steps []model.Step
}

// startLogLocked joins the job's download in flight or starts one; the
// caller holds mu.
func (b *Backend) startLogLocked(j logJob) *logCall {
	c := &b.logs
	if call, ok := c.calls[j.id]; ok {
		return call
	}
	call := &logCall{done: make(chan struct{})}
	c.calls[j.id] = call
	go b.downloadLog(j, call)
	return call
}

// downloadLog runs detached from every request, so a caller that stops
// waiting still leaves the result in the caches.
func (b *Backend) downloadLog(j logJob, call *logCall) {
	c := &b.logs
	c.slots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), logDeadline)
	data, err := b.GH.JobLog(ctx, j.repo, j.id, parseLimit)
	cancel()
	<-c.slots

	var res logResult
	switch {
	case err == nil:
		steps := joblog.Parse(data, j.steps)
		items, omitted := joblog.Annotations(j.id, j.name, steps)
		res = logResult{outcome: logParsed, log: model.JobLog{JobID: j.id, Steps: steps}, notes: jobNotes{items: items, omitted: omitted}}
	case errors.Is(err, github.ErrTooLarge):
		res.outcome = logTooLarge
	case github.IsKind(err, github.ErrGone):
		res.outcome = logExpired
	default:
		res.outcome, res.err = logFailed, err
		log.Printf("job %d log: %v", j.id, err)
	}

	c.mu.Lock()
	delete(c.calls, j.id)
	if res.outcome == logParsed {
		c.parsed.add(j.id, res.log, int64(len(data)))
		c.notes.add(j.id, res.notes, 0)
		delete(c.misses, j.id)
	} else {
		c.misses[j.id] = logMiss{at: b.now(), res: res}
	}
	c.mu.Unlock()
	call.res = res
	close(call.done)
}

func flaggedLog(id int64, o logOutcome) model.JobLog {
	return model.JobLog{JobID: id, TooLarge: o == logTooLarge, Expired: o == logExpired, Steps: []model.LogStep{}}
}

// finishedJob reads a job of a covered repository and refuses one that has
// not completed.
func (b *Backend) finishedJob(ctx context.Context, repo string, jobID int64) (logJob, error) {
	name, _, err := b.coveredName(repo)
	if err != nil {
		return logJob{}, err
	}
	if err := b.githubErr(); err != nil {
		return logJob{}, err
	}
	gctx, cancel := context.WithTimeout(ctx, detailDeadline)
	job, err := b.GH.GetJob(gctx, name, jobID)
	cancel()
	if err != nil {
		return logJob{}, detailErr(err, b.Store.Config().Owner, name)
	}
	if job.Status != "completed" {
		return logJob{}, api.Conflict("job has not finished")
	}
	return logJob{repo: name, id: jobID, name: job.Name, steps: modelSteps(job.Steps)}, nil
}

// JobLog returns a finished job's log split into steps. An expired or too
// large log comes back flagged; a failed download is tried again.
func (b *Backend) JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error) {
	wctx, cancel := context.WithTimeout(ctx, logRouteWait)
	defer cancel()
	j, err := b.finishedJob(wctx, repo, jobID)
	if err != nil {
		return model.JobLog{}, err
	}
	c := &b.logs
	c.mu.Lock()
	c.initLocked()
	if l, ok := c.parsed.get(jobID); ok {
		c.mu.Unlock()
		return l, nil
	}
	if m, ok := c.misses[jobID]; ok && m.res.outcome != logFailed && b.now().Sub(m.at) < logRetryTTL {
		c.mu.Unlock()
		return flaggedLog(jobID, m.res.outcome), nil
	}
	call := b.startLogLocked(j)
	c.mu.Unlock()
	select {
	case <-call.done:
	case <-wctx.Done():
		if err := ctx.Err(); err != nil {
			return model.JobLog{}, err
		}
		return model.JobLog{}, &api.Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is still sending this log; try again shortly"}
	}
	switch call.res.outcome {
	case logParsed:
		return call.res.log, nil
	case logFailed:
		return model.JobLog{}, detailErr(call.res.err, b.Store.Config().Owner, j.repo)
	}
	return flaggedLog(jobID, call.res.outcome), nil
}

// RawJobLog returns a finished job's whole log, up to 32 MiB, uncached.
func (b *Backend) RawJobLog(ctx context.Context, repo string, jobID int64) (model.RawLog, error) {
	rctx, cancel := context.WithTimeout(ctx, rawLogDeadline)
	defer cancel()
	j, err := b.finishedJob(rctx, repo, jobID)
	if err != nil {
		return model.RawLog{}, err
	}
	c := &b.logs
	c.mu.Lock()
	c.initLocked()
	slots := c.slots
	c.mu.Unlock()
	select {
	case slots <- struct{}{}:
	case <-rctx.Done():
		if err := ctx.Err(); err != nil {
			return model.RawLog{}, err
		}
		return model.RawLog{}, &api.Error{Status: http.StatusServiceUnavailable, Msg: "ghr is reading other logs; try again shortly"}
	}
	defer func() { <-slots }()
	data, err := b.GH.JobLog(rctx, j.repo, jobID, rawLimit)
	switch {
	case errors.Is(err, github.ErrTooLarge):
		return model.RawLog{}, &api.Error{Status: http.StatusRequestEntityTooLarge, Msg: "log is larger than 32 MiB; open it on GitHub"}
	case github.IsKind(err, github.ErrGone):
		return model.RawLog{}, &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps this job's log"}
	case err != nil:
		return model.RawLog{}, detailErr(err, b.Store.Config().Owner, j.repo)
	}
	return model.RawLog{Text: string(data)}, nil
}
