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
	if want := []int{1, 4, 0, 3, 5, 6, 0, 1}; !reflect.DeepEqual(counts, want) {
		t.Fatalf("line counts %v, want %v", counts, want)
	}
	for _, tc := range []struct {
		step, n    int
		kind, text string
	}{
		{1, 1, "", "Current runner version: '2.338.0'"},
		{2, 1, "group", "Run ./setup-runner"},
		{2, 3, "endgroup", ""},
		// "Initialize containers" prints no Run header, so this one stays put.
		{2, 4, "group", "Run docker compose up -d"},
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
	skipped := []model.Step{
		{Number: 1, Name: "Build", Conclusion: "success", StartedAt: at("10:00:00"), CompletedAt: at("10:00:05")},
		{Number: 2, Name: "Publish", Conclusion: "skipped", StartedAt: at("10:00:05"), CompletedAt: at("10:00:05")},
		{Number: 3, Name: "Test", Conclusion: "success", StartedAt: at("10:00:05"), CompletedAt: at("10:00:09")},
	}
	namedPost := []model.Step{
		{Number: 1, Name: "Set up job", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")},
		{Number: 2, Name: "Post coverage comment", StartedAt: at("10:00:01"), CompletedAt: at("10:00:02")},
		{Number: 3, Name: "Build", StartedAt: at("10:00:02"), CompletedAt: at("10:00:03")},
		{Number: 4, Name: "Post Build", StartedAt: at("10:00:03"), CompletedAt: at("10:00:03")},
		{Number: 5, Name: "Complete job", StartedAt: at("10:00:03"), CompletedAt: at("10:00:03")},
	}
	trailingPost := []model.Step{
		{Number: 1, Name: "Set up job", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")},
		{Number: 2, Name: "Run actions/checkout@v4", StartedAt: at("10:00:01"), CompletedAt: at("10:00:02")},
		{Number: 3, Name: "Build", StartedAt: at("10:00:02"), CompletedAt: at("10:00:02")},
		{Number: 4, Name: "Post coverage comment", StartedAt: at("10:00:02"), CompletedAt: at("10:00:02")},
		{Number: 5, Name: "Post Run actions/checkout@v4", StartedAt: at("10:00:02"), CompletedAt: at("10:00:03")},
		{Number: 6, Name: "Complete job", StartedAt: at("10:00:03"), CompletedAt: at("10:00:03")},
	}
	orphanText := []model.Step{
		{Number: 1, Name: "Set up job", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")},
		{Number: 2, Name: "Build", StartedAt: at("10:00:01"), CompletedAt: at("10:00:10")},
		{Number: 3, Name: "Test", StartedAt: at("10:00:10"), CompletedAt: at("10:00:20")},
		{Number: 4, Name: "Complete job", StartedAt: at("10:00:20"), CompletedAt: at("10:00:21")},
	}
	containers := []model.Step{
		{Number: 1, Name: "Set up job", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")},
		{Number: 2, Name: "Initialize containers", StartedAt: at("10:00:01"), CompletedAt: at("10:00:05")},
		{Number: 3, Name: "Build", StartedAt: at("10:00:05"), CompletedAt: at("10:00:06")},
		{Number: 4, Name: "Stop containers", StartedAt: at("10:00:06"), CompletedAt: at("10:00:06")},
		{Number: 5, Name: "Complete job", StartedAt: at("10:00:06"), CompletedAt: at("10:00:06")},
	}
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
		{"a skipped step takes no lines",
			"2026-10-03T10:00:00Z a\n2026-10-03T10:00:05.2Z ##[group]Run go test\n2026-10-03T10:00:05.4Z ok\n", skipped,
			[][]model.LogLine{{{N: 1, Text: "a"}}, {}, {{N: 1, Kind: "group", Text: "Run go test"}, {N: 2, Text: "ok"}}}},
		{"a step a person named Post keeps its header and takes no cleanup line",
			"2026-10-03T10:00:00.1Z a\n2026-10-03T10:00:01.2Z Post job cleanup.\n" +
				"2026-10-03T10:00:01.4Z ##[group]Run gh pr comment\n2026-10-03T10:00:02.2Z ##[group]Run make\n" +
				"2026-10-03T10:00:03.1Z Post job cleanup.\n2026-10-03T10:00:03.3Z Cleaning up orphan processes\n", namedPost,
			[][]model.LogLine{
				{{N: 1, Text: "a"}, {N: 2, Text: "Post job cleanup."}},
				{{N: 1, Kind: "group", Text: "Run gh pr comment"}},
				{{N: 1, Kind: "group", Text: "Run make"}},
				{{N: 1, Text: "Post job cleanup."}},
				{{N: 1, Text: "Cleaning up orphan processes"}}}},
		{"the last step a person named Post is not a runner post step",
			"2026-10-03T10:00:00.1Z a\n2026-10-03T10:00:01.2Z ##[group]Run actions/checkout@v4\n" +
				"2026-10-03T10:00:02.1Z ##[group]Run make\n2026-10-03T10:00:02.3Z ##[group]Run gh pr comment\n" +
				"2026-10-03T10:00:02.6Z Post job cleanup.\n2026-10-03T10:00:03.1Z Cleaning up orphan processes\n", trailingPost,
			[][]model.LogLine{
				{{N: 1, Text: "a"}},
				{{N: 1, Kind: "group", Text: "Run actions/checkout@v4"}},
				{{N: 1, Kind: "group", Text: "Run make"}},
				{{N: 1, Kind: "group", Text: "Run gh pr comment"}},
				{{N: 1, Text: "Post job cleanup."}},
				{{N: 1, Text: "Cleaning up orphan processes"}}}},
		{"the cleanup text printed by a script is not the Complete job marker",
			"2026-10-03T10:00:00.1Z a\n2026-10-03T10:00:05Z Cleaning up orphan processes\n" +
				"2026-10-03T10:00:15Z still testing\n2026-10-03T10:00:20.2Z Cleaning up orphan processes\n", orphanText,
			[][]model.LogLine{
				{{N: 1, Text: "a"}},
				{{N: 1, Text: "Cleaning up orphan processes"}},
				{{N: 1, Text: "still testing"}},
				{{N: 1, Text: "Cleaning up orphan processes"}}}},
		{"a Run header never moves to a container step",
			"2026-10-03T10:00:00.1Z a\n2026-10-03T10:00:01.5Z ##[group]Run early\n" +
				"2026-10-03T10:00:03Z ##[group]Starting postgres service container\n" +
				"2026-10-03T10:00:05.2Z ##[group]Run make\n2026-10-03T10:00:06.5Z ##[group]Run late\n", containers,
			[][]model.LogLine{
				{{N: 1, Text: "a"}, {N: 2, Kind: "group", Text: "Run early"}},
				{{N: 1, Kind: "group", Text: "Starting postgres service container"}},
				{{N: 1, Kind: "group", Text: "Run make"}, {N: 2, Kind: "group", Text: "Run late"}},
				{}, {}}},
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

// Shaped like job 114400898499 of darkraise/ghr: the last regular step, the
// post steps and "Complete job" all start and end in the same second.
var postSteps = []model.Step{
	{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: at("05:48:50"), CompletedAt: at("05:48:50")},
	{Number: 2, Name: "Run actions/checkout@v4", Conclusion: "success", StartedAt: at("05:48:50"), CompletedAt: at("05:48:51")},
	{Number: 3, Name: "Run actions/setup-go@v5", Conclusion: "success", StartedAt: at("05:48:51"), CompletedAt: at("05:49:01")},
	{Number: 13, Name: "Run npm run build", Conclusion: "success", StartedAt: at("05:51:13"), CompletedAt: at("05:51:25")},
	{Number: 14, Name: "no inlined fonts", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
	{Number: 27, Name: "Post Run actions/setup-go@v5", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
	{Number: 28, Name: "Post Run actions/checkout@v4", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
	{Number: 29, Name: "Complete job", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
}

const postLog = "2026-10-03T05:48:50.0356954Z Current runner version: '2.337.0'\n" +
	"2026-10-03T05:48:50.8596515Z ##[group]Run actions/checkout@v4\n" +
	"2026-10-03T05:48:51.7647897Z ##[group]Run actions/setup-go@v5\n" +
	"2026-10-03T05:51:13.4998330Z ##[group]Run npm run build\n" +
	"2026-10-03T05:51:19.0988182Z built in 5.19s\n" +
	"2026-10-03T05:51:25.1999952Z ##[group]Run ls dist/assets/*.css > /dev/null\n" +
	"2026-10-03T05:51:25.2062670Z ##[endgroup]\n" +
	"2026-10-03T05:51:25.2274449Z Post job cleanup.\n" +
	"2026-10-03T05:51:25.5063663Z Cache hit occurred on the primary key setup-go\n" +
	"2026-10-03T05:51:25.5318701Z Post job cleanup.\n" +
	"2026-10-03T05:51:25.6228285Z git version 2.55.0\n" +
	"2026-10-03T05:51:25.7394992Z Cleaning up orphan processes\n" +
	"2026-10-03T05:51:25.7718673Z ##[warning]Node.js 20 is deprecated.\n"

// Shaped like job 114375054549 of darkraise/darkraise-ai-plugins: a step
// failed, so the first post step is skipped, with equal start and end times.
var skippedPostSteps = []model.Step{
	{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: at("03:04:02"), CompletedAt: at("03:04:03")},
	{Number: 2, Name: "Run actions/checkout@v5", Conclusion: "success", StartedAt: at("03:04:03"), CompletedAt: at("03:04:08")},
	{Number: 3, Name: "Run actions/setup-node@v5", Conclusion: "success", StartedAt: at("03:04:08"), CompletedAt: at("03:04:13")},
	{Number: 5, Name: "Run Windows regression suites", Conclusion: "failure", StartedAt: at("03:04:13"), CompletedAt: at("03:10:26")},
	{Number: 9, Name: "Post Run actions/setup-node@v5", Conclusion: "skipped", StartedAt: at("03:10:26"), CompletedAt: at("03:10:26")},
	{Number: 10, Name: "Post Run actions/checkout@v5", Conclusion: "success", StartedAt: at("03:10:26"), CompletedAt: at("03:10:28")},
	{Number: 11, Name: "Complete job", Conclusion: "success", StartedAt: at("03:10:28"), CompletedAt: at("03:10:28")},
}

const skippedPostLog = "2026-10-03T03:04:02.5104326Z Current runner version: '2.337.0'\n" +
	"2026-10-03T03:04:03.4199469Z ##[group]Run actions/checkout@v5\n" +
	"2026-10-03T03:04:08.6692765Z ##[group]Run actions/setup-node@v5\n" +
	"2026-10-03T03:04:13.8946258Z ##[group]Run node scripts/test-all.mjs --shard 5/6\n" +
	"2026-10-03T03:10:26.7660359Z ##[error]Process completed with exit code 1.\n" +
	"2026-10-03T03:10:26.7946449Z Post job cleanup.\n" +
	"2026-10-03T03:10:27.1000000Z git version 2.55.0.windows.1\n" +
	"2026-10-03T03:10:28.8334560Z Cleaning up orphan processes\n"

// Shaped like job 111871024678 of darkraise/darkmem, which a ghr runner ran:
// ghr's job hooks add "Set up runner" after "Set up job" and "Complete runner"
// between the post steps and "Complete job". Each prints its own Run header.
var hookSteps = []model.Step{
	{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: at("16:41:19"), CompletedAt: at("16:41:22")},
	{Number: 2, Name: "Set up runner", Conclusion: "success", StartedAt: at("16:41:22"), CompletedAt: at("16:41:22")},
	{Number: 3, Name: "Run actions/checkout@v4", Conclusion: "success", StartedAt: at("16:41:22"), CompletedAt: at("16:41:26")},
	{Number: 4, Name: "Run actions/setup-node@v4", Conclusion: "success", StartedAt: at("16:41:26"), CompletedAt: at("16:41:27")},
	{Number: 8, Name: "Build", Conclusion: "success", StartedAt: at("16:42:36"), CompletedAt: at("16:42:40")},
	{Number: 14, Name: "Post Run actions/setup-node@v4", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
	{Number: 15, Name: "Post Run actions/checkout@v4", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
	{Number: 16, Name: "Complete runner", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
	{Number: 17, Name: "Complete job", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
}

const hookLog = "2026-10-03T16:41:19.5104326Z Current runner version: '2.337.0'\n" +
	"2026-10-03T16:41:22.3125688Z A job started hook has been configured by the self-hosted runner administrator\n" +
	"2026-10-03T16:41:22.3198136Z ##[group]Run '/opt/ghr/hooks/job-started.sh'\n" +
	"2026-10-03T16:41:22.3209650Z ##[endgroup]\n" +
	"2026-10-03T16:41:22.3488745Z ##[group]Run actions/checkout@v4\n" +
	"2026-10-03T16:42:36.2000000Z ##[group]Run npx vite build\n" +
	"2026-10-03T16:42:39.9000000Z built in 3.4s\n" +
	"2026-10-03T16:42:40.5000000Z Post job cleanup.\n" +
	"2026-10-03T16:42:40.6000000Z Cache hit occurred on the primary key node-cache\n" +
	"2026-10-03T16:42:40.7000000Z Post job cleanup.\n" +
	"2026-10-03T16:42:40.9000000Z git version 2.55.0\n" +
	"2026-10-03T16:42:40.9390787Z A job completed hook has been configured by the self-hosted runner administrator\n" +
	"2026-10-03T16:42:40.9412987Z ##[group]Run '/opt/ghr/hooks/job-completed.sh'\n" +
	"2026-10-03T16:42:40.9422548Z ##[endgroup]\n" +
	"2026-10-03T16:42:40.9543007Z Cleaning up orphan processes\n"

func TestParseGivesPostStepsTheirLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   string
		steps []model.Step
		want  [][]string
	}{
		{"post steps in one second", postLog, postSteps, [][]string{
			{"Current runner version: '2.337.0'"},
			{"Run actions/checkout@v4"},
			{"Run actions/setup-go@v5"},
			{"Run npm run build", "built in 5.19s"},
			{"Run ls dist/assets/*.css > /dev/null", ""},
			{"Post job cleanup.", "Cache hit occurred on the primary key setup-go"},
			{"Post job cleanup.", "git version 2.55.0"},
			{"Cleaning up orphan processes", "Node.js 20 is deprecated."},
		}},
		{"a skipped post step", skippedPostLog, skippedPostSteps, [][]string{
			{"Current runner version: '2.337.0'"},
			{"Run actions/checkout@v5"},
			{"Run actions/setup-node@v5"},
			{"Run node scripts/test-all.mjs --shard 5/6", "Process completed with exit code 1."},
			{},
			{"Post job cleanup.", "git version 2.55.0.windows.1"},
			{"Cleaning up orphan processes"},
		}},
		// The line the runner prints ahead of each hook's Run header stays
		// with the step before it.
		{"the job hooks of a ghr runner", hookLog, hookSteps, [][]string{
			{"Current runner version: '2.337.0'", "A job started hook has been configured by the self-hosted runner administrator"},
			{"Run '/opt/ghr/hooks/job-started.sh'", ""},
			{"Run actions/checkout@v4"},
			{},
			{"Run npx vite build", "built in 3.4s"},
			{"Post job cleanup.", "Cache hit occurred on the primary key node-cache"},
			{"Post job cleanup.", "git version 2.55.0", "A job completed hook has been configured by the self-hosted runner administrator"},
			{"Run '/opt/ghr/hooks/job-completed.sh'", ""},
			{"Cleaning up orphan processes"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := [][]string{}
			for _, s := range Parse([]byte(tc.raw), tc.steps) {
				texts := []string{}
				for _, l := range s.Lines {
					texts = append(texts, l.Text)
				}
				got = append(got, texts)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lines %q\nwant  %q", got, tc.want)
			}
		})
	}
}

func TestParseGivesNoLinesWhenEveryStepIsSkipped(t *testing.T) {
	steps := []model.Step{
		{Number: 1, Name: "Build", Conclusion: "skipped", StartedAt: at("02:52:44"), CompletedAt: at("02:52:44")},
		{Number: 2, Name: "Test", Conclusion: "skipped"},
	}
	for name, raw := range map[string]string{
		"an empty log":     "",
		"a log with lines": "2026-10-03T02:52:44.0000000Z ##[group]Run make\n2026-10-03T02:52:44.1000000Z done\n",
	} {
		for _, s := range Parse([]byte(raw), steps) {
			if len(s.Lines) != 0 {
				t.Errorf("%s: skipped step %d got %d lines", name, s.Number, len(s.Lines))
			}
		}
	}
}
