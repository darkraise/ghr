package joblog

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func at(hms string) *time.Time {
	t, err := time.Parse(time.RFC3339, "2026-10-03T"+hms+"Z")
	if err != nil {
		panic(err)
	}
	return &t
}

// Trimmed from a darkmem job log: "Create test databases", "Lint" and "Test"
// start in the same second, which timestamps alone cannot split.
var darkmemSteps = []model.Step{
	{Number: 1, Name: "Set up job", StartedAt: at("02:52:41"), CompletedAt: at("02:52:44")},
	{Number: 2, Name: "Set up runner", StartedAt: at("02:52:44"), CompletedAt: at("02:52:44")},
	{Number: 3, Name: "Initialize containers", StartedAt: at("02:52:44"), CompletedAt: at("02:52:54")},
	{Number: 4, Name: "Create test databases", StartedAt: at("02:52:54"), CompletedAt: at("02:52:54")},
	{Number: 5, Name: "Lint", StartedAt: at("02:52:54"), CompletedAt: at("02:52:54")},
	{Number: 6, Name: "Test", StartedAt: at("02:52:54"), CompletedAt: at("02:53:31")},
	{Number: 7, Name: "Run tests"},
	{Number: 8, Name: "Complete job", StartedAt: at("02:53:32"), CompletedAt: at("02:53:33")},
}

const darkmemLog = "\xef\xbb\xbf2026-10-03T02:52:41.5104326Z Current runner version: '2.338.0'\r\n" +
	"2026-10-03T02:52:44.0101010Z ##[group]Run ./setup-runner\r\n" +
	"2026-10-03T02:52:44.0202020Z runner ready\n" +
	"2026-10-03T02:52:44.0303030Z ##[endgroup]\n" +
	"2026-10-03T02:52:44.9000000Z ##[group]Run docker compose up -d\n" +
	"2026-10-03T02:52:54.1000000Z ##[group]Run createdb test_a test_b\n" +
	"  continued without a timestamp\n" +
	"2026-10-03T02:52:54.1200000Z ##[endgroup]\n" +
	"2026-10-03T02:52:54.2000000Z ##[group]Run ruff check .\n" +
	"2026-10-03T02:52:54.2100000Z ##[endgroup]\n" +
	"2026-10-03T02:52:54.3000000Z \x1b[31mFound 1 error.\x1b[0m\n" +
	"2026-10-03T02:52:54.3100000Z ##[error]src/cache.py:12:5: F401 'os' imported but unused\n" +
	"2026-10-03T02:52:54.3200000Z ##[warning]ruff 0.6 is outdated\n" +
	"2026-10-03T02:52:54.4000000Z ##[group]Run pytest -q\n" +
	"2026-10-03T02:52:55.0000000Z ##[command]/usr/bin/pytest -q\n" +
	"2026-10-03T02:53:00.0000000Z ##[debug]collected 12 items\n" +
	"2026-10-03T02:53:20.0000000Z ##[notice]Retried 1 flaky test\n" +
	"2026-10-03T02:53:25.0000000Z ##[group]Run echo done\n" +
	"2026-10-03T02:53:30.0000000Z ##[endgroup]\n" +
	"2026-10-03T02:53:33.0000000Z Cleaning up orphan processes\n"

func TestParseSplitsTheDarkmemLog(t *testing.T) {
	got := Parse([]byte(darkmemLog), darkmemSteps)
	var counts []int
	for i, s := range got {
		if s.Number != darkmemSteps[i].Number || s.Name != darkmemSteps[i].Name || s.Lines == nil {
			t.Fatalf("step %d: %+v", i, s)
		}
		for j, l := range s.Lines {
			if l.N != j+1 {
				t.Fatalf("step %d line %d numbered %d", s.Number, j+1, l.N)
			}
		}
		counts = append(counts, len(s.Lines))
	}
	if want := []int{1, 3, 1, 3, 5, 6, 0, 1}; !reflect.DeepEqual(counts, want) {
		t.Fatalf("line counts %v, want %v", counts, want)
	}
	for _, tc := range []struct {
		step, n    int
		kind, text string
	}{
		{1, 1, "", "Current runner version: '2.338.0'"},
		{2, 1, "group", "Run ./setup-runner"},
		{2, 3, "endgroup", ""},
		{3, 1, "group", "Run docker compose up -d"},
		{4, 2, "", "  continued without a timestamp"},
		{5, 3, "", "\x1b[31mFound 1 error.\x1b[0m"},
		{5, 4, "error", "src/cache.py:12:5: F401 'os' imported but unused"},
		{5, 5, "warning", "ruff 0.6 is outdated"},
		{6, 2, "command", "/usr/bin/pytest -q"},
		{6, 3, "debug", "collected 12 items"},
		{6, 4, "notice", "Retried 1 flaky test"},
		// No later step's window holds 02:53:25, so the pointer stays on Test.
		{6, 5, "group", "Run echo done"},
		{8, 1, "", "Cleaning up orphan processes"},
	} {
		l := got[tc.step-1].Lines[tc.n-1]
		if l.Kind != tc.kind || l.Text != tc.text {
			t.Errorf("step %d line %d = %q %q, want %q %q", tc.step, tc.n, l.Kind, l.Text, tc.kind, tc.text)
		}
	}
}

func TestParseEdges(t *testing.T) {
	one := []model.Step{{Number: 1, Name: "Only", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")}}
	untimed := []model.Step{{Number: 1, Name: "A"}, {Number: 2, Name: "B"}}
	for _, tc := range []struct {
		name  string
		raw   string
		steps []model.Step
		want  [][]model.LogLine
	}{
		{"no steps", "2026-10-03T10:00:00Z x\n", nil, [][]model.LogLine{}},
		{"unknown prefix stays text", "2026-10-03T10:00:00Z ##[section]Starting: Only\n", one,
			[][]model.LogLine{{{N: 1, Text: "##[section]Starting: Only"}}}},
		{"late line with no later step stays", "2026-10-03T10:00:00Z a\n2026-10-03T10:00:30Z b\n", one,
			[][]model.LogLine{{{N: 1, Text: "a"}, {N: 2, Text: "b"}}}},
		{"first line without a timestamp", "orphan\n2026-10-03T10:00:00Z a", one,
			[][]model.LogLine{{{N: 1, Text: "orphan"}, {N: 2, Text: "a"}}}},
		{"no step has times", "2026-10-03T10:00:00Z a\n", untimed,
			[][]model.LogLine{{{N: 1, Text: "a"}}, {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse([]byte(tc.raw), tc.steps)
			lines := [][]model.LogLine{}
			for _, s := range got {
				lines = append(lines, s.Lines)
			}
			if !reflect.DeepEqual(lines, tc.want) {
				t.Fatalf("lines %+v, want %+v", lines, tc.want)
			}
		})
	}
}

func TestAnnotationsMergeSortAndCap(t *testing.T) {
	steps := []model.LogStep{
		{Number: 2, Name: "Lint", Lines: []model.LogLine{
			{N: 1, Kind: "notice", Text: "cache hit"},
			{N: 2, Kind: "error", Text: "F401 'os' imported but unused"},
			{N: 3, Kind: "error", Text: "F401 'os' imported but unused"},
			{N: 4, Kind: "warning", Text: "F401 'os' imported but unused"},
			{N: 5, Kind: "debug", Text: "not an annotation"},
			{N: 6, Kind: "group", Text: "Run ruff"},
		}},
		{Number: 3, Name: "Test", Lines: []model.LogLine{
			{N: 1, Kind: "error", Text: "F401 'os' imported but unused"},
		}},
	}
	items, omitted := Annotations(1002, "Backend tests", steps)
	want := []model.Annotation{
		{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 2, StepName: "Lint", Line: 2, Message: "F401 'os' imported but unused"},
		{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 3, StepName: "Test", Line: 1, Message: "F401 'os' imported but unused"},
		{Level: "warning", JobID: 1002, JobName: "Backend tests", Step: 2, StepName: "Lint", Line: 4, Message: "F401 'os' imported but unused"},
		{Level: "notice", JobID: 1002, JobName: "Backend tests", Step: 2, StepName: "Lint", Line: 1, Message: "cache hit"},
	}
	if !reflect.DeepEqual(items, want) || omitted != 0 {
		t.Fatalf("items %+v omitted %d", items, omitted)
	}

	var many []model.LogLine
	for i := 1; i <= PerJob+3; i++ {
		many = append(many, model.LogLine{N: i, Kind: "error", Text: fmt.Sprintf("error %d", i)})
	}
	many = append(many, model.LogLine{N: PerJob + 4, Kind: "warning", Text: "late warning"})
	items, omitted = Annotations(7, "big", []model.LogStep{{Number: 1, Name: "s", Lines: many}})
	if len(items) != PerJob || omitted != 4 || items[PerJob-1].Message != fmt.Sprintf("error %d", PerJob) {
		t.Fatalf("%d items, omitted %d, last %+v", len(items), omitted, items[len(items)-1])
	}

	if items, omitted := Annotations(1, "clean", []model.LogStep{{Number: 1, Lines: []model.LogLine{}}}); items == nil || len(items) != 0 || omitted != 0 {
		t.Fatalf("clean job: %#v %d", items, omitted)
	}
}

func TestLevelRank(t *testing.T) {
	for level, want := range map[string]int{"error": 0, "warning": 1, "notice": 2, "debug": 3, "": 3} {
		if got := LevelRank(level); got != want {
			t.Errorf("LevelRank(%q) = %d, want %d", level, got, want)
		}
	}
}
