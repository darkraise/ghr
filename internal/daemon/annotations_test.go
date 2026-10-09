package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func setAnnotationsWait(t *testing.T, d time.Duration) {
	old := annotationsWait
	annotationsWait = d
	t.Cleanup(func() { annotationsWait = old })
}

// annotatedRun gives gh run 102 of darkmem with the given jobs as its latest
// attempt's jobs.
func annotatedRun(gh *fakeGH, jobs ...github.Job) {
	detailRun(gh, "completed")
	gh.jobs = map[int64][]github.Job{102: jobs}
	gh.content = nil // the graph is not under test: the note path is enough
}

func annotationText(items []model.Annotation) string {
	var out []string
	for _, a := range items {
		out = append(out, fmt.Sprintf("%s %d %d:%d %s", a.Level, a.JobID, a.Step, a.Line, a.Message))
	}
	return fmt.Sprint(out)
}

func TestRunAnnotationsCollectsFinishedJobs(t *testing.T) {
	b, _, gh := newBackend(t)
	build := logJobOf(gh, 2001, "Build", "##[warning]old node", "##[error]build broke")
	test := logJobOf(gh, 2002, "test", "##[notice]flaky", "##[error]test broke")
	skipped := logJobOf(gh, 2003, "lint", "##[error]never read")
	skipped.Conclusion = "skipped"
	running := github.Job{ID: 2004, Name: "deploy", Status: "in_progress"}
	annotatedRun(gh, build, test, skipped, running)

	a, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "[error 2001 1:2 build broke error 2002 1:2 test broke warning 2001 1:1 old node notice 2002 1:1 flaky]"
	if got := annotationText(a.Items); got != want {
		t.Fatalf("items %s", got)
	}
	if a.Items[0].JobName != "Build" || a.Items[0].StepName != "Lint" || a.Omitted == nil || len(a.Omitted) != 0 ||
		a.Unread != 0 || a.ExpiredLogs != 0 || a.TooLarge != 0 || a.Failed != 0 {
		t.Fatalf("annotations %+v", a)
	}
	if gh.count("JobLog") != 2 {
		t.Fatalf("downloads %v", gh.calls)
	}
	if _, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0); err != nil || gh.count("JobLog") != 2 {
		t.Fatalf("second call: %d downloads, err %v", gh.count("JobLog"), err)
	}
}

func TestRunAnnotationsCapsEachJob(t *testing.T) {
	b, _, gh := newBackend(t)
	var lines []string
	for i := range 52 {
		lines = append(lines, fmt.Sprintf("##[error]problem %d", i))
	}
	annotatedRun(gh, logJobOf(gh, 2001, "Build", lines...))
	a, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 50 || a.Omitted[2001] != 2 {
		t.Fatalf("%d items, omitted %v", len(a.Items), a.Omitted)
	}
}

func TestRunAnnotationsReusesTheParsedLog(t *testing.T) {
	b, _, gh := newBackend(t)
	annotatedRun(gh, logJobOf(gh, 2001, "Build", "##[error]build broke"))
	ctx := context.Background()
	if _, err := b.JobLog(ctx, "darkmem", 2001); err != nil {
		t.Fatal(err)
	}
	b.logs.mu.Lock()
	b.logs.notes.remove(2001)
	b.logs.mu.Unlock()
	a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
	if err != nil || len(a.Items) != 1 || gh.count("JobLog") != 1 {
		t.Fatalf("items %+v, %d downloads, err %v", a.Items, gh.count("JobLog"), err)
	}
}

func TestRunAnnotationsCountsUnreadAfterTheWait(t *testing.T) {
	setAnnotationsWait(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	annotatedRun(gh, logJobOf(gh, 2001, "Build", "##[error]a"), logJobOf(gh, 2002, "test", "##[error]b"))
	gh.logWait = make(chan struct{})
	t.Cleanup(func() { close(gh.logWait) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
	if err != nil || a.Unread != 2 || len(a.Items) != 0 {
		t.Fatalf("annotations %+v err %v", a, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v", took)
	}
}

func TestRunAnnotationsCountsLogOutcomes(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	annotatedRun(gh, logJobOf(gh, 2001, "a", "x"), logJobOf(gh, 2002, "b", "x"), logJobOf(gh, 2003, "c", "x"))
	gh.logErr[2001] = &github.APIError{Status: 410, Kind: github.ErrGone}
	gh.logErr[2002] = github.ErrTooLarge
	gh.logErr[2003] = errors.New("boom")
	ctx := context.Background()
	for i, step := range []struct {
		at        time.Duration
		downloads int
	}{{0, 3}, {logRetryTTL - time.Second, 3}, {logRetryTTL, 6}} {
		clock.Store(actionsT0.Add(step.at).UnixNano())
		a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
		if err != nil || a.ExpiredLogs != 1 || a.TooLarge != 1 || a.Failed != 1 || a.Unread != 0 || len(a.Items) != 0 {
			t.Fatalf("step %d: %+v err %v", i, a, err)
		}
		if n := gh.count("JobLog"); n != step.downloads {
			t.Fatalf("step %d: %d downloads, want %d", i, n, step.downloads)
		}
	}
}

func TestRunAnnotationsPassesRunErrors(t *testing.T) {
	b, _, _ := newBackend(t)
	if _, err := b.RunAnnotations(context.Background(), "nope", 102, 0); apiStatus(err) != 404 {
		t.Fatalf("err %v", err)
	}
}
