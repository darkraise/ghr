package activity

import (
	"math"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// wall returns the instant of local wall-clock y-mo-d h:00 in loc, which may
// overflow into the next day. When that wall time falls in a spring-forward
// gap, time.Date answers with an earlier instant (23:00 the day before for a
// missing midnight), so it steps forward to the first real instant at or
// after the gap.
func wall(y int, mo time.Month, d, h int, loc *time.Location) time.Time {
	t := time.Date(y, mo, d, h, 0, 0, 0, loc)
	want := time.Date(y, mo, d, h, 0, 0, 0, time.UTC)
	for {
		got := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
		if !got.Before(want) {
			return t
		}
		t = t.Add(time.Hour)
	}
}

// floor returns the start of the bucket holding t, in loc.
func floor(t time.Time, kind string, loc *time.Location) time.Time {
	t = t.In(loc)
	y, mo, d := t.Date()
	switch kind {
	case "hour":
		// Subtracting the local minutes keeps half-hour zones and repeated
		// DST hours on their own local hour boundaries.
		return t.Add(-time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond()))
	case "6h":
		return wall(y, mo, d, t.Hour()/6*6, loc)
	default:
		return wall(y, mo, d, 0, loc)
	}
}

// next returns the start of the bucket after the one starting at start.
// Six-hour and day buckets follow local wall time, so DST days run 23 or 25 hours.
func next(start time.Time, kind string, loc *time.Location) time.Time {
	start = start.In(loc)
	y, mo, d := start.Date()
	switch kind {
	case "hour":
		return start.Add(time.Hour)
	case "6h":
		return wall(y, mo, d, (start.Hour()/6+1)*6, loc)
	default:
		return wall(y, mo, d+1, 0, loc)
	}
}

// starts lists the start of n closed buckets followed by the open one holding now.
func starts(now time.Time, kind string, loc *time.Location, n int) []time.Time {
	out := []time.Time{floor(now, kind, loc)}
	for len(out) <= n {
		out = append([]time.Time{floor(out[0].Add(-time.Nanosecond), kind, loc)}, out...)
	}
	return out
}

func overlap(a0, a1, b0, b1 time.Time) time.Duration {
	s, e := a0, a1
	if b0.After(s) {
		s = b0
	}
	if b1.Before(e) {
		e = b1
	}
	if !e.After(s) {
		return 0
	}
	return e.Sub(s)
}

func tally(conclusion string, succeeded, failed, cancelled *int) {
	switch finishedState(conclusion) {
	case "succeeded":
		*succeeded++
	case "failed":
		*failed++
	default:
		*cancelled++
	}
}

// buckets builds sp.closed closed buckets and the open one. Busy time counts
// running and finished segments; the open bucket's percentage divides by the
// minutes elapsed in it. Runs are counted by finish time, and waiting and CPU
// come from the hourly rollups whose UTC hour starts inside the bucket.
func buckets(in Input, sp spec, rs []model.ActivityRun, loc *time.Location) (time.Time, []model.ActivityBucket) {
	now := in.Now
	ss := starts(now, sp.bucket, loc, sp.closed)
	out := make([]model.ActivityBucket, len(ss))
	for i, s := range ss {
		e := next(s, sp.bucket, loc)
		upto := e
		if now.Before(upto) {
			upto = now
		}
		b := model.ActivityBucket{Start: s.In(loc), End: e.In(loc)}
		busy := 0.0
		for _, r := range rs {
			for _, sg := range r.Segments {
				if sg.State == "starting" || sg.State == "warm" {
					continue
				}
				end := now
				if sg.To != nil {
					end = *sg.To
				}
				busy += overlap(sg.From, end, s, upto).Minutes()
			}
		}
		b.BusyMinutes = math.Round(busy*10) / 10
		if in.Capacity != nil && *in.Capacity > 0 && upto.After(s) {
			pct := math.Round(busy/(float64(*in.Capacity)*upto.Sub(s).Minutes())*1000) / 10
			b.BusyPct = &pct
		}
		for _, h := range in.History {
			if !h.FinishedAt.Before(s) && h.FinishedAt.Before(e) {
				tally(h.Conclusion, &b.Succeeded, &b.Failed, &b.Cancelled)
			}
		}
		cpuSum, cpuN := 0.0, 0
		for _, r := range in.Hours {
			if r.At.Before(s) || !r.At.Before(e) {
				continue
			}
			if b.WaitingMax == nil || r.QueuedMax > *b.WaitingMax {
				w := r.QueuedMax
				b.WaitingMax = &w
			}
			if r.CPUAvg != nil {
				cpuSum += *r.CPUAvg * float64(r.Samples)
				cpuN += r.Samples
			}
		}
		if cpuN > 0 {
			v := math.Round(cpuSum/float64(cpuN)*10) / 10
			b.CPUAvg = &v
		}
		out[i] = b
	}
	return ss[0], out
}

// repoHours counts each configured repository's runs by finish time over
// the last 24 local hours, oldest first, the last one in progress.
func repoHours(in Input, loc *time.Location) []model.ActivityRepo {
	ss := starts(in.Now, "hour", loc, 23)
	out := []model.ActivityRepo{}
	for _, name := range in.Repos {
		r := model.ActivityRepo{Repo: name, Hours: make([]model.ActivityHour, len(ss))}
		for i, s := range ss {
			e := next(s, "hour", loc)
			hr := model.ActivityHour{Start: s.In(loc)}
			for _, h := range in.History {
				if strings.EqualFold(h.Repo, name) && !h.FinishedAt.Before(s) && h.FinishedAt.Before(e) {
					tally(h.Conclusion, &hr.Succeeded, &hr.Failed, &hr.Cancelled)
				}
			}
			r.Hours[i] = hr
		}
		out = append(out, r)
	}
	return out
}
