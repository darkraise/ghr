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

// Parse splits raw into one LogStep per step, in the order given. Steps that
// never started get no lines. Lines without a timestamp stay with the line
// before them.
func Parse(raw []byte, steps []model.Step) []model.LogStep {
	out := make([]model.LogStep, len(steps))
	for i, s := range steps {
		out[i] = model.LogStep{Number: s.Number, Name: s.Name, Lines: []model.LogLine{}}
	}
	if len(steps) == 0 {
		return out
	}
	var timed []int
	for i, s := range steps {
		if s.StartedAt != nil {
			timed = append(timed, i)
		}
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
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		at, body, ok := split(line)
		if ok && len(timed) > 0 {
			cur := &out[timed[p]]
			if strings.HasPrefix(body, "##[group]Run ") && len(cur.Lines) > 0 {
				for k := p + 1; k < len(timed); k++ {
					if contains(k, at) {
						p = k
						break
					}
				}
			} else if e, bounded := end(p); bounded && at.After(e) {
				for k := p + 1; k < len(timed); k++ {
					if e2, b2 := end(k); !b2 || !e2.Before(at) {
						p = k
						break
					}
				}
			}
		}
		idx := 0
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
