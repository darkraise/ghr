package daemon

import (
	"context"
	"sort"
	"time"

	"github.com/darkraise/ghr/internal/joblog"
	"github.com/darkraise/ghr/internal/model"
)

// annotationsWait bounds how long the annotations route waits for log
// downloads before it answers with what it has; tests shorten it.
var annotationsWait = 20 * time.Second

// RunAnnotations collects the annotations of an attempt's finished jobs from
// their logs. Downloads it starts keep running after it answers; the jobs
// still downloading count in Unread.
func (b *Backend) RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error) {
	d, err := b.RunDetail(ctx, repo, id, attempt)
	if err != nil {
		return model.RunAnnotations{}, err
	}
	notes := map[int64]jobNotes{}
	outcomes := map[int64]logOutcome{}
	calls := map[int64]*logCall{}
	c := &b.logs
	c.mu.Lock()
	c.initLocked(b.tokenGen())
	for _, j := range d.Jobs {
		if j.Status != "completed" || j.Conclusion == "skipped" {
			continue
		}
		if n, ok := c.notes.Get(j.ID); ok {
			notes[j.ID] = n
			continue
		}
		if l, ok := c.parsed.Get(j.ID); ok {
			items, omitted := joblog.Annotations(j.ID, j.Name, l.Steps)
			n := jobNotes{items: items, omitted: omitted}
			c.notes.Add(j.ID, n, 0)
			notes[j.ID] = n
			continue
		}
		if m, ok := c.misses[j.ID]; ok && b.now().Sub(m.at) < logRetryTTL {
			outcomes[j.ID] = m.res.outcome
			continue
		}
		calls[j.ID] = b.startLogLocked(logJob{repo: d.Repo, id: j.ID, name: j.Name, steps: j.Steps})
	}
	c.mu.Unlock()

	timer := time.NewTimer(annotationsWait)
	defer timer.Stop()
wait:
	for _, call := range calls {
		select {
		case <-call.done:
		case <-timer.C:
			break wait
		case <-ctx.Done():
			break wait
		}
	}

	out := model.RunAnnotations{Items: []model.Annotation{}, Omitted: map[int64]int{}}
	for _, j := range d.Jobs {
		if call, ok := calls[j.ID]; ok {
			select {
			case <-call.done:
			default:
				out.Unread++
				continue
			}
			switch call.res.outcome {
			case logParsed:
				notes[j.ID] = call.res.notes
			case logBusy:
				out.Unread++
				continue
			default:
				outcomes[j.ID] = call.res.outcome
			}
		}
		if n, ok := notes[j.ID]; ok {
			out.Items = append(out.Items, n.items...)
			if n.omitted > 0 {
				out.Omitted[j.ID] = n.omitted
			}
		}
		if o, ok := outcomes[j.ID]; ok {
			// logNone is counted nowhere: a job that never ran has no log to
			// expire, to be too large or to fail on.
			switch o {
			case logExpired:
				out.ExpiredLogs++
			case logTooLarge:
				out.TooLarge++
			case logFailed:
				out.Failed++
			}
		}
	}
	// Items are in job order and each job's are sorted, so a stable sort by
	// level gives level, then job, then step and line.
	sort.SliceStable(out.Items, func(i, k int) bool {
		return joblog.LevelRank(out.Items[i].Level) < joblog.LevelRank(out.Items[k].Level)
	})
	return out, nil
}
