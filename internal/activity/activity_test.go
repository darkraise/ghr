package activity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/darkraise/ghr/internal/model"
)

var now = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

func at(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, time.UTC) }

func ptr[T any](v T) *T { return &v }

func job(id string, start, end time.Time, conclusion string) model.HistoryEntry {
	return model.HistoryEntry{ID: id, Repo: "darkmem", RunNumber: "41", Workflow: "ci", JobName: "build",
		Conclusion: conclusion, StartedAt: start, FinishedAt: end}
}

func laneIDs(a model.Activity) [][]string {
	out := [][]string{}
	for _, l := range a.Lanes {
		ids := []string{}
		for _, r := range l.Runs {
			ids = append(ids, r.InstanceID)
		}
		out = append(out, ids)
	}
	return out
}

func TestValid(t *testing.T) {
	for _, w := range []string{"1h", "3h", "24h", "7d", "30d"} {
		if !Valid(w) {
			t.Errorf("%s rejected", w)
		}
	}
	for _, w := range []string{"", "2h", "1d", "1H"} {
		if Valid(w) {
			t.Errorf("%q accepted", w)
		}
	}
}

func TestLanesPackRunsIntoFreeLanes(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Capacity: ptr(4), History: []model.HistoryEntry{
		job("a", at(13, 10), at(13, 20), "success"),
		job("b", at(13, 15), at(13, 30), "success"),
		job("c", at(13, 20), at(13, 25), "failure"), // starts as a ends: reuses its lane
	}})
	got := laneIDs(a)
	want := [][]string{{"a", "c"}, {"b"}, {}, {}}
	if len(got) != len(want) || strings.Join(got[0], ",") != "a,c" || strings.Join(got[1], ",") != "b" || len(got[2]) != 0 || len(got[3]) != 0 {
		t.Fatalf("lanes %v, want %v", got, want)
	}
	if !a.From.Equal(at(13, 5)) || !a.To.Equal(now) || len(a.Buckets) != 0 {
		t.Fatalf("range %v to %v, buckets %d", a.From, a.To, len(a.Buckets))
	}
}

func TestLanesGrowPastCapacityAndTieByInstanceID(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Capacity: ptr(1), History: []model.HistoryEntry{
		job("b", at(13, 30), at(13, 40), "success"),
		job("a", at(13, 30), at(13, 35), "success"),
	}})
	if got := laneIDs(a); len(got) != 2 || got[0][0] != "a" || got[1][0] != "b" {
		t.Fatalf("lanes %v", got)
	}
	none := Build(Input{Window: "1h", Now: now})
	if len(none.Lanes) != 0 || none.Capacity != nil {
		t.Fatalf("all mode with no runs: %+v", none.Lanes)
	}
}

func TestRunsOutsideTheWindowAreLeftOut(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, History: []model.HistoryEntry{job("old", at(12, 0), at(12, 30), "success")}})
	if len(a.Lanes) != 0 {
		t.Fatalf("lanes %v", laneIDs(a))
	}
	b := Build(Input{Window: "3h", Now: now, History: []model.HistoryEntry{job("old", at(12, 0), at(12, 30), "success")}})
	if len(b.Lanes) != 1 {
		t.Fatalf("3h lanes %v", laneIDs(b))
	}
}

func TestLiveInstances(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now,
		History: []model.HistoryEntry{job("done", at(13, 10), at(13, 20), "success")},
		Instances: []model.InstanceStatus{
			{ID: "s", Repo: "ghr", State: "starting", Since: at(14, 4)},
			{ID: "w", Repo: "darkmem", State: "idle", Since: at(14, 0)},
			{ID: "r", Repo: "darkmem", State: "busy", Since: at(13, 59), Job: &model.JobInfo{
				RunNumber: "42", Workflow: "ci", Name: "test", HTMLURL: "https://example/42", StartedAt: at(14, 1)}},
			{ID: "x", Repo: "darkmem", State: "cleaning", Since: at(14, 3)},
			{ID: "done", Repo: "darkmem", State: "cleaning", Since: at(13, 20)},
		}})
	runs := map[string]model.ActivityRun{}
	for _, l := range a.Lanes {
		for _, r := range l.Runs {
			runs[r.InstanceID] = r
		}
	}
	if len(runs) != 4 {
		t.Fatalf("runs %v", laneIDs(a))
	}
	check := func(id, state string, from time.Time) {
		t.Helper()
		sg := runs[id].Segments
		if len(sg) != 1 || sg[0].State != state || !sg[0].From.Equal(from) || sg[0].To != nil {
			t.Fatalf("%s segments %+v", id, sg)
		}
	}
	check("s", "starting", at(14, 4))
	check("w", "warm", at(14, 0))
	check("r", "running", at(14, 1))
	if r := runs["r"]; r.Job != "test" || r.RunNumber != "42" || r.Workflow != "ci" || r.HTMLURL != "https://example/42" {
		t.Fatalf("running run %+v", r)
	}
	if sg := runs["done"].Segments; len(sg) != 1 || sg[0].State != "succeeded" || sg[0].To == nil || !sg[0].To.Equal(at(13, 20)) {
		t.Fatalf("finished run %+v", sg)
	}
}

func TestConclusionStatesAndAbsentURL(t *testing.T) {
	for conclusion, state := range map[string]string{
		"success": "succeeded", "failure": "failed", "timed_out": "failed", "cancelled": "cancelled", "skipped": "skipped",
	} {
		a := Build(Input{Window: "1h", Now: now, History: []model.HistoryEntry{job("j", at(13, 30), at(13, 40), conclusion)}})
		if got := a.Lanes[0].Runs[0].Segments[0].State; got != state {
			t.Errorf("%s: state %s, want %s", conclusion, got, state)
		}
		data, _ := json.Marshal(a.Lanes[0].Runs[0])
		if strings.Contains(string(data), "html_url") {
			t.Errorf("absent html_url marshalled: %s", data)
		}
	}
}

func TestMinutesInTheWindow(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Minutes: []model.MetricSample{
		{At: at(12, 50), Queued: 9},
		{At: at(13, 10), Queued: 1, CPU: ptr(20.0), Mem: ptr(int64(1 << 30))},
		{At: at(14, 0), Queued: 3},
	}})
	if len(a.Waiting) != 2 || a.Waiting[0].Value != 1 || a.Waiting[1].Value != 3 {
		t.Fatalf("waiting %+v", a.Waiting)
	}
	if len(a.CPU) != 2 || a.CPU[0].CPU == nil || *a.CPU[0].CPU != 20 || a.CPU[1].CPU != nil {
		t.Fatalf("cpu %+v", a.CPU)
	}
}

func TestZoneAndHistoryFrom(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	a := Build(Input{Window: "1h", Now: now, Loc: loc, Retention: 30 * 24 * time.Hour,
		History: []model.HistoryEntry{job("j", at(13, 30), at(13, 40), "success")}})
	if a.TZ != "Asia/Ho_Chi_Minh" || a.To.Location() != loc || a.From.Location() != loc {
		t.Fatalf("zone %s %v %v", a.TZ, a.To.Location(), a.From.Location())
	}
	if !a.HistoryFrom.Equal(now.Add(-30*24*time.Hour)) || a.HistoryFrom.Location() != loc {
		t.Fatalf("history from %v", a.HistoryFrom)
	}
	if a.Lanes[0].Runs[0].Segments[0].From.Location() != loc {
		t.Fatal("segment times are not in the zone")
	}
	data, _ := json.Marshal(a)
	if !strings.Contains(string(data), `"to":"2026-10-03T21:05:00+07:00"`) {
		t.Fatalf("json %s", data)
	}
	if b := Build(Input{Window: "1h", Now: now}); b.TZ != "UTC" {
		t.Fatalf("nil zone: %s", b.TZ)
	}
}

func TestEmptySlicesMarshalAsArrays(t *testing.T) {
	data, _ := json.Marshal(Build(Input{Window: "1h", Now: now}))
	for _, k := range []string{`"lanes":[]`, `"buckets":[]`, `"waiting":[]`, `"cpu":[]`, `"repos":[]`} {
		if !strings.Contains(string(data), k) {
			t.Errorf("missing %s in %s", k, data)
		}
	}
}

func TestRepoFilter(t *testing.T) {
	hist := []model.HistoryEntry{
		job("mine", at(13, 10), at(13, 20), "success"),
		{ID: "theirs", Repo: "ghr", Conclusion: "success", StartedAt: at(13, 10), FinishedAt: at(13, 20)},
	}
	insts := []model.InstanceStatus{
		{ID: "r1", Repo: "DarkMem", State: "busy", Since: at(14, 0)},
		{ID: "r2", Repo: "ghr", State: "busy", Since: at(14, 0)},
	}
	minutes := []model.MetricSample{{At: at(14, 0), Queued: 2, CPU: ptr(10.0)}}
	hours := []model.MetricRollup{{At: at(13, 0), Samples: 60, QueuedMax: 3, CPUAvg: ptr(20.0)}}

	lanes := Build(Input{Window: "1h", Now: now, Repo: "darkmem", Capacity: ptr(4), Repos: []string{"DarkMem", "ghr"},
		History: hist, Instances: insts, Minutes: minutes, Hours: hours})
	ids := laneIDs(lanes)
	if lanes.Capacity != nil || len(ids) != 1 || strings.Join(ids[0], ",") != "mine,r1" {
		t.Fatalf("lanes %v capacity %v", ids, lanes.Capacity)
	}
	if len(lanes.Waiting) != 0 || len(lanes.CPU) != 0 {
		t.Fatalf("host figures kept: waiting %v cpu %v", lanes.Waiting, lanes.CPU)
	}
	if len(lanes.Repos) != 1 || lanes.Repos[0].Repo != "DarkMem" {
		t.Fatalf("repos %+v", lanes.Repos)
	}

	b := Build(Input{Window: "24h", Now: now, Repo: "darkmem", Capacity: ptr(4), Repos: []string{"DarkMem"},
		History: hist, Hours: hours})
	for _, bk := range b.Buckets {
		if bk.BusyPct != nil || bk.WaitingMax != nil || bk.CPUAvg != nil {
			t.Fatalf("bucket %v kept host figures: %+v", bk.Start, bk)
		}
	}
	if bk := bucketAt(t, b, at(13, 0)); bk.Succeeded != 1 || bk.BusyMinutes != 10 {
		t.Fatalf("13:00 bucket %+v", bk)
	}

	gone := Build(Input{Window: "24h", Now: now, Repo: "removed-repo", Repos: []string{"DarkMem"},
		History: []model.HistoryEntry{{ID: "old", Repo: "removed-repo", Conclusion: "failure", StartedAt: at(13, 0), FinishedAt: at(13, 5)}}})
	if len(gone.Repos) != 0 || bucketAt(t, gone, at(13, 0)).Failed != 1 {
		t.Fatalf("removed repo: repos %+v", gone.Repos)
	}
}
