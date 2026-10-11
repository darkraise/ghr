// Package joblog splits a GitHub Actions job log into its steps and reads
// the annotations out of it.
package joblog

import (
	"sort"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// PerJob caps the annotations one job contributes.
const PerJob = 50

// slack widens each step's window: log timestamps and step times are
// rounded differently.
const slack = time.Second

var kinds = []string{"group", "endgroup", "error", "warning", "notice", "command", "debug"}

// split reads one log line's timestamp; ok is false for a line without one.
func split(line string) (at time.Time, text string, ok bool) {
	ts, rest, found := strings.Cut(line, " ")
	if !found {
		ts, rest = line, ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, line, false
	}
	return t, rest, true
}

// kind removes a known ##[kind] prefix.
func kind(text string) (string, string) {
	if !strings.HasPrefix(text, "##[") {
		return "", text
	}
	for _, k := range kinds {
		if p := "##[" + k + "]"; strings.HasPrefix(text, p) {
			return k, text[len(p):]
		}
	}
	return "", text
}

// Steps the runner names itself. They print no "##[group]Run " header.
const (
	setUpJob       = "Set up job"
	initContainers = "Initialize containers"
	stopContainers = "Stop containers"
	completeJob    = "Complete job"
)

// completeRunner is the step a job-completed hook adds between the post steps
// and "Complete job"; ghr sets that hook on every runner it manages. Unlike
// the steps above it prints a "##[group]Run " header, as "Set up runner", the
// job-started hook's step, does.
const completeRunner = "Complete runner"

// The lines the runner prints first for a post step and for "Complete job".
const (
	postHeader     = "Post job cleanup."
	completeHeader = "Cleaning up orphan processes"
)

// runnerPosts marks the runner's post steps. They are found by position:
// walking back from the last step past "Complete job", "Complete runner" and
// "Stop containers", the unbroken run of steps whose name starts with "Post ".
// A step a person named that way earlier in the job is not one.
func runnerPosts(steps []model.Step) []bool {
	post := make([]bool, len(steps))
	i := len(steps) - 1
	for i >= 0 && (steps[i].Name == completeJob || steps[i].Name == completeRunner || steps[i].Name == stopContainers) {
		i--
	}
	for ; i >= 0 && strings.HasPrefix(steps[i].Name, "Post "); i-- {
		post[i] = true
	}
	return post
}

// regular reports whether a step prints a "##[group]Run " header of its own;
// post says whether it is one of the runner's post steps.
func regular(s model.Step, post bool) bool {
	switch s.Name {
	case setUpJob, initContainers, stopContainers, completeJob:
		return false
	}
	return !post
}

// Parse splits raw into one LogStep per step, in the order given. Steps that
// never started and skipped steps get no lines. Lines without a timestamp stay
// with the line before them.
//
// One pointer, the current step, moves forward through the steps. GitHub
// rounds step times to whole seconds, so times alone cannot split steps that
// share a second; the lines the runner prints first for a step do:
//   - a "##[group]Run " line, when the current step already has lines, moves to
//     the first later regular step whose window holds the line's time;
//   - a "Post job cleanup." line moves to the first later runner's post step
//     whose window holds the line's time, unless the current step is such a
//     step with no lines yet;
//   - a "Cleaning up orphan processes" line moves to "Complete job" when that
//     is the last step with times;
//   - any other line more than a second past the current step's end moves to
//     the next step that has not ended.
//
// A line with no step to move to stays with the current step.
func Parse(raw []byte, steps []model.Step) []model.LogStep {
	out := make([]model.LogStep, len(steps))
	for i, s := range steps {
		out[i] = model.LogStep{Number: s.Number, Name: s.Name, Lines: []model.LogLine{}}
	}
	if len(steps) == 0 {
		return out
	}
	post := runnerPosts(steps)
	// timed holds the steps a line can go to. A skipped step has times, equal
	// ones, and prints nothing, so it is left out.
	var timed []int
	// complete is the place in timed of "Complete job" when that is the last
	// step with times, and -1 otherwise.
	complete := -1
	// first takes every line when timed is empty.
	first := -1
	for i, s := range steps {
		skipped := s.Conclusion == "skipped"
		if first < 0 && !skipped {
			first = i
		}
		if s.StartedAt == nil {
			continue
		}
		complete = -1
		if skipped {
			continue
		}
		timed = append(timed, i)
		if s.Name == completeJob {
			complete = len(timed) - 1
		}
	}
	// With every step skipped there is no step a line may go to.
	if first < 0 {
		return out
	}
	// end is a step's completion plus slack, or no bound while it has none.
	end := func(k int) (time.Time, bool) {
		c := steps[timed[k]].CompletedAt
		if c == nil {
			return time.Time{}, false
		}
		return c.Add(slack), true
	}
	contains := func(k int, t time.Time) bool {
		if t.Before(steps[timed[k]].StartedAt.Add(-slack)) {
			return false
		}
		e, ok := end(k)
		return !ok || !t.After(e)
	}

	text := strings.TrimPrefix(string(raw), "\uFEFF")
	text = strings.TrimSuffix(text, "\n")
	p := 0
	// next is the first place after p that fits, or p when none does.
	next := func(fits func(k int) bool) int {
		for k := p + 1; k < len(timed); k++ {
			if fits(k) {
				return k
			}
		}
		return p
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		at, body, ok := split(line)
		if ok && len(timed) > 0 {
			cur := timed[p]
			hasLines := len(out[cur].Lines) > 0
			switch {
			case strings.HasPrefix(body, "##[group]Run ") && hasLines:
				p = next(func(k int) bool { return regular(steps[timed[k]], post[timed[k]]) && contains(k, at) })
			case body == postHeader:
				if !post[cur] || hasLines {
					p = next(func(k int) bool { return post[timed[k]] && contains(k, at) })
				}
			case body == completeHeader && complete >= 0:
				p = complete
			default:
				if e, bounded := end(p); bounded && at.After(e) {
					p = next(func(k int) bool {
						e2, b2 := end(k)
						return !b2 || !e2.Before(at)
					})
				}
			}
		}
		idx := first
		if len(timed) > 0 {
			idx = timed[p]
		}
		k, t := kind(body)
		out[idx].Lines = append(out[idx].Lines, model.LogLine{N: len(out[idx].Lines) + 1, Kind: k, Text: t})
	}
	return out
}

// LevelRank orders annotation levels: errors, then warnings, then notices.
func LevelRank(level string) int {
	switch level {
	case "error":
		return 0
	case "warning":
		return 1
	case "notice":
		return 2
	}
	return 3
}

// Annotations returns one job's error, warning and notice lines, duplicates
// (same level, step and message) merged, sorted by level, step and line, and
// capped at PerJob; omitted counts the rest.
func Annotations(jobID int64, jobName string, steps []model.LogStep) (items []model.Annotation, omitted int) {
	items = []model.Annotation{}
	seen := map[string]bool{}
	for _, s := range steps {
		for _, l := range s.Lines {
			if LevelRank(l.Kind) > 2 {
				continue
			}
			key := l.Kind + "\x00" + l.Text
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, model.Annotation{Level: l.Kind, JobID: jobID, JobName: jobName,
				Step: s.Number, StepName: s.Name, Line: l.N, Message: l.Text})
		}
		clear(seen)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := LevelRank(a.Level), LevelRank(b.Level); ra != rb {
			return ra < rb
		}
		if a.Step != b.Step {
			return a.Step < b.Step
		}
		return a.Line < b.Line
	})
	if len(items) > PerJob {
		omitted = len(items) - PerJob
		items = items[:PerJob]
	}
	return items, omitted
}
