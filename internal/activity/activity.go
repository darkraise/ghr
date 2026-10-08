// Package activity turns job history, live runners and metrics into the
// Dashboard's activity view: lanes of runs for short windows and time
// buckets for long ones.
package activity

import (
	"sort"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

type spec struct {
	lanes  bool
	span   time.Duration // lanes windows
	bucket string        // "hour", "6h" or "day"
	closed int           // closed buckets before the open one
}

var windows = map[string]spec{
	"1h":  {lanes: true, span: time.Hour},
	"3h":  {lanes: true, span: 3 * time.Hour},
	"24h": {bucket: "hour", closed: 24},
	"7d":  {bucket: "6h", closed: 28},
	"30d": {bucket: "day", closed: 30},
}

// Valid reports whether window is one Build accepts.
func Valid(window string) bool {
	_, ok := windows[window]
	return ok
}

type Input struct {
	Window    string
	Loc       *time.Location // nil means UTC
	Now       time.Time
	Capacity  *int // global_max in queue mode, nil in all mode
	Retention time.Duration
	History   []model.HistoryEntry
	Instances []model.InstanceStatus
	Repos     []string // configured repository names
	Minutes   []model.MetricSample
	Hours     []model.MetricRollup
	// Repo, when set, narrows the view to that repository; see forRepo.
	Repo string
}

// Build assembles the activity view for in.Window, which must be Valid.
func Build(in Input) model.Activity {
	if in.Repo != "" {
		in = forRepo(in)
	}
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	sp := windows[in.Window]
	now := in.Now
	a := model.Activity{
		Window: in.Window, TZ: loc.String(), To: now.In(loc), Capacity: in.Capacity,
		Lanes: []model.ActivityLane{}, Buckets: []model.ActivityBucket{},
		Waiting: []model.ActivityPoint{}, CPU: []model.ActivityCPU{}, Repos: []model.ActivityRepo{},
	}
	a.HistoryFrom = now.Add(-in.Retention).In(loc)
	rs := runs(in, loc)
	if sp.lanes {
		from := now.Add(-sp.span)
		a.From = from.In(loc)
		a.Lanes = pack(rs, from, now, in.Capacity)
		for _, m := range in.Minutes {
			if m.At.Before(from) || m.At.After(now) {
				continue
			}
			a.Waiting = append(a.Waiting, model.ActivityPoint{At: m.At.In(loc), Value: m.Queued})
			a.CPU = append(a.CPU, model.ActivityCPU{At: m.At.In(loc), CPU: m.CPU, Mem: m.Mem})
		}
	} else {
		from, bs := buckets(in, sp, rs, loc)
		a.From, a.Buckets = from.In(loc), bs
	}
	a.Repos = repoHours(in, loc)
	return a
}

// finishedState maps a job conclusion to a segment state; anything that is
// not a success, a cancellation or a skip counts as a failure.
func finishedState(conclusion string) string {
	switch conclusion {
	case "success":
		return "succeeded"
	case "cancelled":
		return "cancelled"
	case "skipped":
		return "skipped"
	default:
		return "failed"
	}
}

// forRepo keeps one repository's history and live instances, matching names
// as repoHours does, and its configured name if it has one. Capacity and the
// host metrics belong to the whole host, so they are dropped: buckets then
// carry no busy percentage, waiting or CPU, and lanes are not padded.
func forRepo(in Input) Input {
	var hist []model.HistoryEntry
	for _, h := range in.History {
		if strings.EqualFold(h.Repo, in.Repo) {
			hist = append(hist, h)
		}
	}
	var insts []model.InstanceStatus
	for _, i := range in.Instances {
		if strings.EqualFold(i.Repo, in.Repo) {
			insts = append(insts, i)
		}
	}
	var repos []string
	for _, r := range in.Repos {
		if strings.EqualFold(r, in.Repo) {
			repos = append(repos, r)
		}
	}
	in.History, in.Instances, in.Repos = hist, insts, repos
	in.Capacity, in.Minutes, in.Hours = nil, nil, nil
	return in
}

// runs builds one run per runner instance: a finished job from history, or
// a live instance in its current state. A cleaning instance is left out
// until its history entry lands; a live instance that already has one is
// shown from history.
func runs(in Input, loc *time.Location) []model.ActivityRun {
	seen := map[string]bool{}
	out := []model.ActivityRun{}
	for _, e := range in.History {
		seen[e.ID] = true
		to := e.FinishedAt.In(loc)
		out = append(out, model.ActivityRun{
			InstanceID: e.ID, Repo: e.Repo, Workflow: e.Workflow, Job: e.JobName, RunNumber: e.RunNumber, HTMLURL: e.HTMLURL,
			Segments: []model.ActivitySegment{{State: finishedState(e.Conclusion), From: e.StartedAt.In(loc), To: &to}},
		})
	}
	for _, i := range in.Instances {
		if seen[i.ID] {
			continue
		}
		r := model.ActivityRun{InstanceID: i.ID, Repo: i.Repo}
		switch i.State {
		case "starting":
			r.Segments = []model.ActivitySegment{{State: "starting", From: i.Since.In(loc)}}
		case "idle":
			r.Segments = []model.ActivitySegment{{State: "warm", From: i.Since.In(loc)}}
		case "busy":
			from := i.Since
			if j := i.Job; j != nil {
				r.Workflow, r.Job, r.RunNumber, r.HTMLURL = j.Workflow, j.Name, j.RunNumber, j.HTMLURL
				if !j.StartedAt.IsZero() {
					from = j.StartedAt
				}
			}
			r.Segments = []model.ActivitySegment{{State: "running", From: from.In(loc)}}
		default:
			continue
		}
		out = append(out, r)
	}
	return out
}

// span is a run's first start and its last end, now while it lasts.
func span(r model.ActivityRun, now time.Time) (time.Time, time.Time) {
	last := r.Segments[len(r.Segments)-1]
	end := now
	if last.To != nil {
		end = *last.To
	}
	return r.Segments[0].From, end
}

// pack places the runs overlapping [from, to) into lanes: sorted by start,
// then instance ID, each into the first lane whose last run ended by its
// start. There are at least capacity lanes when capacity is set.
func pack(rs []model.ActivityRun, from, to time.Time, capacity *int) []model.ActivityLane {
	var in []model.ActivityRun
	for _, r := range rs {
		s, e := span(r, to)
		if e.After(from) && s.Before(to) {
			in = append(in, r)
		}
	}
	sort.SliceStable(in, func(a, b int) bool {
		sa, _ := span(in[a], to)
		sb, _ := span(in[b], to)
		if !sa.Equal(sb) {
			return sa.Before(sb)
		}
		return in[a].InstanceID < in[b].InstanceID
	})
	lanes := []model.ActivityLane{}
	var ends []time.Time
	for _, r := range in {
		s, e := span(r, to)
		placed := false
		for i := range lanes {
			if !ends[i].After(s) {
				lanes[i].Runs = append(lanes[i].Runs, r)
				ends[i] = e
				placed = true
				break
			}
		}
		if !placed {
			lanes = append(lanes, model.ActivityLane{Runs: []model.ActivityRun{r}})
			ends = append(ends, e)
		}
	}
	if capacity != nil {
		for len(lanes) < *capacity {
			lanes = append(lanes, model.ActivityLane{Runs: []model.ActivityRun{}})
		}
	}
	return lanes
}
