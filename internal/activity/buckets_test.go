package activity

import (
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func bucketAt(t *testing.T, a model.Activity, start time.Time) model.ActivityBucket {
	t.Helper()
	for _, b := range a.Buckets {
		if b.Start.Equal(start) {
			return b
		}
	}
	t.Fatalf("no bucket at %v", start)
	return model.ActivityBucket{}
}

func TestBucketCountsAndAlignment(t *testing.T) {
	for _, c := range []struct {
		window string
		count  int
		first  time.Time
		size   time.Duration
	}{
		{"24h", 25, time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC), time.Hour},
		{"7d", 29, time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC), 6 * time.Hour},
		{"30d", 31, time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 24 * time.Hour},
	} {
		a := Build(Input{Window: c.window, Now: now})
		if len(a.Buckets) != c.count || !a.Buckets[0].Start.Equal(c.first) || !a.From.Equal(c.first) {
			t.Fatalf("%s: %d buckets from %v", c.window, len(a.Buckets), a.From)
		}
		last := a.Buckets[len(a.Buckets)-1]
		if !last.Start.Before(now) || !last.End.After(now) || last.End.Sub(last.Start) != c.size || len(a.Lanes) != 0 {
			t.Fatalf("%s: open bucket %v to %v", c.window, last.Start, last.End)
		}
	}
}

func TestBusyMinutesSplitAndOpenBucketDenominator(t *testing.T) {
	a := Build(Input{Window: "24h", Now: now, Capacity: ptr(2),
		History: []model.HistoryEntry{job("j", at(12, 50), at(13, 10), "success")},
		Instances: []model.InstanceStatus{{ID: "r", Repo: "darkmem", State: "busy", Since: at(14, 0),
			Job: &model.JobInfo{StartedAt: at(14, 0)}}},
	})
	b12, b13, b14 := bucketAt(t, a, at(12, 0)), bucketAt(t, a, at(13, 0)), bucketAt(t, a, at(14, 0))
	if b12.BusyMinutes != 10 || b13.BusyMinutes != 10 || b14.BusyMinutes != 5 {
		t.Fatalf("busy %v %v %v", b12.BusyMinutes, b13.BusyMinutes, b14.BusyMinutes)
	}
	if b12.BusyPct == nil || *b12.BusyPct != 8.3 || b14.BusyPct == nil || *b14.BusyPct != 50 {
		t.Fatalf("busy pct %v %v", b12.BusyPct, b14.BusyPct)
	}
	if b12.Succeeded != 0 || b13.Succeeded != 1 {
		t.Fatalf("counted by finish time: %d %d", b12.Succeeded, b13.Succeeded)
	}
	all := Build(Input{Window: "24h", Now: now, History: []model.HistoryEntry{job("j", at(12, 50), at(13, 10), "success")}})
	if bucketAt(t, all, at(13, 0)).BusyPct != nil {
		t.Fatal("busy pct without a capacity")
	}
}

func TestCountsByConclusion(t *testing.T) {
	a := Build(Input{Window: "24h", Now: now, History: []model.HistoryEntry{
		job("a", at(13, 0), at(13, 5), "success"),
		job("b", at(13, 0), at(13, 6), "failure"),
		job("c", at(13, 0), at(13, 7), "cancelled"),
		job("d", at(13, 0), at(13, 8), "skipped"),
	}})
	if b := bucketAt(t, a, at(13, 0)); b.Succeeded != 1 || b.Failed != 1 || b.Cancelled != 2 {
		t.Fatalf("counts %+v", b)
	}
}

func TestRollupsFillWaitingAndCPU(t *testing.T) {
	hours := []model.MetricRollup{
		{At: at(12, 0), Samples: 60, QueuedMax: 1, CPUAvg: ptr(40.0)},
		{At: at(13, 0), Samples: 60, QueuedMax: 3, CPUAvg: ptr(20.0)},
	}
	a := Build(Input{Window: "24h", Now: now, Hours: hours})
	if b := bucketAt(t, a, at(12, 0)); b.WaitingMax == nil || *b.WaitingMax != 1 || b.CPUAvg == nil || *b.CPUAvg != 40 {
		t.Fatalf("12:00 %+v", b)
	}
	if b := bucketAt(t, a, at(11, 0)); b.WaitingMax != nil || b.CPUAvg != nil {
		t.Fatalf("a bucket without rollups %+v", b)
	}
	w := Build(Input{Window: "7d", Now: now, Hours: hours})
	if b := bucketAt(t, w, at(12, 0)); b.WaitingMax == nil || *b.WaitingMax != 3 || b.CPUAvg == nil || *b.CPUAvg != 30 {
		t.Fatalf("6h bucket %+v", b)
	}
}

func TestDaylightSavingDays(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	fall := Build(Input{Window: "30d", Loc: ny, Now: time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)})
	if b := bucketAt(t, fall, time.Date(2026, 11, 1, 0, 0, 0, 0, ny)); b.End.Sub(b.Start) != 25*time.Hour {
		t.Fatalf("Nov 1 lasts %v", b.End.Sub(b.Start))
	}
	spring := Build(Input{Window: "30d", Loc: ny, Now: time.Date(2026, 3, 20, 17, 0, 0, 0, time.UTC)})
	if b := bucketAt(t, spring, time.Date(2026, 3, 8, 0, 0, 0, 0, ny)); b.End.Sub(b.Start) != 23*time.Hour {
		t.Fatalf("Mar 8 lasts %v", b.End.Sub(b.Start))
	}
	week := Build(Input{Window: "7d", Loc: ny, Now: time.Date(2026, 11, 3, 17, 0, 0, 0, time.UTC)})
	if b := bucketAt(t, week, time.Date(2026, 11, 1, 0, 0, 0, 0, ny)); b.End.Sub(b.Start) != 7*time.Hour {
		t.Fatalf("Nov 1 00:00 to 06:00 lasts %v", b.End.Sub(b.Start))
	}
}

func TestZoneChangesAlignment(t *testing.T) {
	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	a := Build(Input{Window: "30d", Loc: hcm, Now: now})
	if len(a.Buckets) != 31 {
		t.Fatalf("buckets = %d", len(a.Buckets))
	}
	for _, b := range a.Buckets {
		if b.Start.Hour() != 0 || b.Start.Minute() != 0 || b.Start.Location() != hcm || b.Start.UTC().Hour() != 17 {
			t.Fatalf("bucket starts %v (%v UTC)", b.Start, b.Start.UTC())
		}
	}
}

func TestRepoHours(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Repos: []string{"darkmem", "darkcloud"}, History: []model.HistoryEntry{
		job("a", at(13, 0), at(13, 10), "success"),
		job("b", at(13, 20), at(13, 40), "failure"),
		{ID: "c", Repo: "removed-repo", Conclusion: "success", StartedAt: at(13, 0), FinishedAt: at(13, 5)},
	}})
	if len(a.Repos) != 2 || a.Repos[0].Repo != "darkmem" || a.Repos[1].Repo != "darkcloud" {
		t.Fatalf("repos %+v", a.Repos)
	}
	hrs := a.Repos[0].Hours
	if len(hrs) != 24 || !hrs[23].Start.Equal(at(14, 0)) || !hrs[0].Start.Equal(time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("hours from %v to %v", hrs[0].Start, hrs[len(hrs)-1].Start)
	}
	if hrs[22].Succeeded != 1 || hrs[22].Failed != 1 || a.Repos[1].Hours[22].Succeeded != 0 {
		t.Fatalf("13:00 %+v", hrs[22])
	}
}

func TestMidnightDaylightSavingGap(t *testing.T) {
	scl, err := time.LoadLocation("America/Santiago") // Sep 6 2026 00:00 jumps to 01:00
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		window string
		now    time.Time
	}{
		{"30d", time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)},
		{"30d", time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}, // the changeover day itself
		{"7d", time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)},
		{"7d", time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)},
	} {
		a := Build(Input{Window: c.window, Loc: scl, Now: c.now})
		for i, b := range a.Buckets {
			if !b.End.After(b.Start) {
				t.Fatalf("%s at %v: bucket %d is empty: %v to %v", c.window, c.now, i, b.Start, b.End)
			}
			if i > 0 && !b.Start.Equal(a.Buckets[i-1].End) {
				t.Fatalf("%s at %v: gap before bucket %d: %v then %v", c.window, c.now, i, a.Buckets[i-1].End, b.Start)
			}
		}
		last := a.Buckets[len(a.Buckets)-1]
		if c.now.Before(last.Start) || !c.now.Before(last.End) {
			t.Fatalf("%s: open bucket %v to %v misses now %v", c.window, last.Start, last.End, c.now)
		}
	}
}
