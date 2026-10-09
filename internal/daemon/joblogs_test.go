package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// logLines formats lines as GitHub writes a job log, one second apart from
// actionsT0.
func logLines(lines ...string) []byte {
	var sb strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&sb, "%s %s\n", actionsT0.Add(time.Duration(i)*time.Second).Format("2006-01-02T15:04:05.0000000Z"), l)
	}
	return []byte(sb.String())
}

// logJobOf gives gh a completed darkmem job of run 102 with one step,
// "Lint", whose log holds lines.
func logJobOf(gh *fakeGH, id int64, name string, lines ...string) github.Job {
	start, end := actionsT0, actionsT0.Add(time.Minute)
	j := github.Job{ID: id, RunID: 102, Name: name, Status: "completed", Conclusion: "failure", StartedAt: &start, CompletedAt: &end,
		Steps: []github.Step{{Number: 1, Name: "Lint", Status: "completed", Conclusion: "failure", StartedAt: &start, CompletedAt: &end}}}
	gh.rmu.Lock()
	defer gh.rmu.Unlock()
	if gh.jobByID == nil {
		gh.jobByID = map[int64]github.Job{}
		gh.logs = map[int64][]byte{}
		gh.logErr = map[int64]error{}
	}
	gh.jobByID[id] = j
	gh.logs[id] = logLines(lines...)
	return j
}

func setParsedLogBytes(t *testing.T, n int64) {
	old := parsedLogBytes
	parsedLogBytes = n
	t.Cleanup(func() { parsedLogBytes = old })
}

func setLogRequestWindow(t *testing.T, d time.Duration) {
	old := logRequestWindow
	logRequestWindow = d
	t.Cleanup(func() { logRequestWindow = old })
}

// waitCalls waits up to 3 s for n calls starting with prefix.
func waitCalls(t *testing.T, gh *fakeGH, prefix string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for gh.count(prefix) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d %s calls, want %d", gh.count(prefix), prefix, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJobLogParsesAndKeepsTheLog(t *testing.T) {
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "##[group]Run ruff check .", "ruff check .", "##[endgroup]", "##[error]F401 unused")
	ctx := context.Background()
	for range 2 {
		l, err := b.JobLog(ctx, "DarkMem", 1002)
		if err != nil {
			t.Fatal(err)
		}
		if l.JobID != 1002 || l.TooLarge || l.Expired || len(l.Steps) != 1 || len(l.Steps[0].Lines) != 4 || l.Steps[0].Lines[3].Kind != "error" {
			t.Fatalf("log %+v", l)
		}
	}
	if gh.count("GetJob darkmem 1002") != 2 || gh.count("JobLog darkmem 1002") != 1 {
		t.Fatalf("calls %v", gh.calls)
	}
	if b.logs.notes.maxEntries != annotationEntries || b.logs.parsed.maxSize != parsedLogBytes {
		t.Fatalf("bounds %d entries, %d bytes", b.logs.notes.maxEntries, b.logs.parsed.maxSize)
	}
}

func TestJobLogRefusals(t *testing.T) {
	b, m, gh := newBackend(t)
	j := logJobOf(gh, 1002, "Backend tests", "x")
	j.Status = "in_progress"
	gh.jobByID[1002] = j
	ctx := context.Background()
	var ae *api.Error
	for name, call := range map[string]func(repo string) error{
		"log": func(repo string) error { _, err := b.JobLog(ctx, repo, 1002); return err },
		"raw": func(repo string) error { _, err := b.RawJobLog(ctx, repo, 1002); return err },
	} {
		if err := call("darkmem"); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "job has not finished" {
			t.Fatalf("%s unfinished: %v", name, err)
		}
		if err := call("nope"); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != "repo nope is not configured or watched" {
			t.Fatalf("%s uncovered: %v", name, err)
		}
	}
	if gh.count("JobLog") != 0 {
		t.Fatalf("an unfinished job's log was downloaded: %v", gh.calls)
	}
	m.degraded = "401 Bad credentials"
	if _, err := b.JobLog(ctx, "darkmem", 1002); apiStatus(err) != 503 {
		t.Fatalf("degraded: %v", err)
	}
	if _, err := b.JobLog(ctx, "darkmem", 9); apiStatus(err) != 503 || gh.count("GetJob darkmem 9") != 0 {
		t.Fatalf("degraded made calls: %v", gh.calls)
	}
}

func TestJobLogOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		want     model.JobLog
		status   int
		retained bool
	}{
		{"expired", &github.APIError{Status: 410, Kind: github.ErrGone}, model.JobLog{JobID: 1002, Expired: true}, 0, true},
		{"too large", github.ErrTooLarge, model.JobLog{JobID: 1002, TooLarge: true}, 0, true},
		{"failed", errors.New("boom"), model.JobLog{}, 502, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			logJobOf(gh, 1002, "Backend tests", "x")
			gh.logErr[1002] = tc.err
			for i := range 2 {
				l, err := b.JobLog(context.Background(), "darkmem", 1002)
				if apiStatus(err) != tc.status || l.JobID != tc.want.JobID || l.Expired != tc.want.Expired || l.TooLarge != tc.want.TooLarge {
					t.Fatalf("call %d: log %+v err %v", i, l, err)
				}
			}
			want := 2
			if tc.retained {
				want = 1
			}
			if n := gh.count("JobLog"); n != want {
				t.Fatalf("%d downloads, want %d", n, want)
			}
		})
	}
}

func TestJobLogParsedCacheIsBoundedByLogSize(t *testing.T) {
	setParsedLogBytes(t, int64(len(logLines("a long enough line")))+10)
	b, _, gh := newBackend(t)
	logJobOf(gh, 1, "one", "a long enough line")
	logJobOf(gh, 2, "two", "a long enough line")
	ctx := context.Background()
	for _, id := range []int64{1, 2, 1} {
		if _, err := b.JobLog(ctx, "darkmem", id); err != nil {
			t.Fatal(err)
		}
	}
	if n := gh.count("JobLog darkmem 1"); n != 2 {
		t.Fatalf("job 1 downloaded %d times, want 2: only one log fits", n)
	}
}

func TestJobLogCallersShareOneDownload(t *testing.T) {
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "x")
	gh.logWait = make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := b.JobLog(context.Background(), "darkmem", 1002)
			done <- err
		}()
	}
	waitCalls(t, gh, "GetJob", 2)
	waitCalls(t, gh, "JobLog", 1)
	time.Sleep(50 * time.Millisecond)
	close(gh.logWait)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := gh.count("JobLog"); n != 1 {
		t.Fatalf("%d downloads, want 1", n)
	}
}

func TestJobLogStopsWaitingButKeepsTheDownload(t *testing.T) {
	setLogRequestWindow(t, 20*time.Millisecond)
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "x")
	gh.logWait = make(chan struct{})
	ctx := context.Background()
	var ae *api.Error
	if _, err := b.JobLog(ctx, "darkmem", 1002); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "GitHub is still sending this log; try again shortly" {
		t.Fatalf("slow download: %v", err)
	}
	close(gh.logWait)
	deadline := time.Now().Add(3 * time.Second)
	for {
		b.logs.mu.Lock()
		_, ok := b.logs.parsed.get(1002)
		b.logs.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the download never filled the cache")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if l, err := b.JobLog(ctx, "darkmem", 1002); err != nil || len(l.Steps) != 1 || gh.count("JobLog") != 1 {
		t.Fatalf("after the download: %+v err %v, %d downloads", l, err, gh.count("JobLog"))
	}
}

func TestLogDownloadsRunFourAtOnce(t *testing.T) {
	b, _, gh := newBackend(t)
	for id := int64(1); id <= 6; id++ {
		logJobOf(gh, id, fmt.Sprintf("job %d", id), "x")
	}
	gh.logWait = make(chan struct{})
	done := make(chan error, 6)
	for id := int64(1); id <= 6; id++ {
		go func() {
			_, err := b.JobLog(context.Background(), "darkmem", id)
			done <- err
		}()
	}
	waitCalls(t, gh, "JobLog", 4)
	time.Sleep(50 * time.Millisecond)
	if n := gh.count("JobLog"); n != 4 {
		t.Fatalf("%d downloads started, want 4", n)
	}
	close(gh.logWait)
	for range 6 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	gh.rmu.Lock()
	peak := gh.logPeak
	gh.rmu.Unlock()
	if peak != 4 || gh.count("JobLog") != 6 {
		t.Fatalf("peak %d, %d downloads", peak, gh.count("JobLog"))
	}
}

func TestRawJobLogGivesUpWhenEverySlotIsBusy(t *testing.T) {
	setLogRequestWindow(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	for id := int64(1); id <= 5; id++ {
		logJobOf(gh, id, fmt.Sprintf("job %d", id), "x")
	}
	gh.logWait = make(chan struct{})
	done := make(chan error, 4)
	for id := int64(1); id <= 4; id++ {
		go func() {
			_, err := b.JobLog(context.Background(), "darkmem", id)
			done <- err
		}()
	}
	waitCalls(t, gh, "JobLog", 4)
	var ae *api.Error
	if _, err := b.RawJobLog(context.Background(), "darkmem", 5); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "ghr is reading other logs; try again shortly" {
		t.Fatalf("busy slots: %v", err)
	}
	if n := gh.count("JobLog darkmem 5"); n != 0 {
		t.Fatalf("job 5 was downloaded %d times while every slot was busy", n)
	}
	close(gh.logWait)
	for range 4 {
		<-done
	}
}

func TestRawJobLog(t *testing.T) {
	b, _, gh := newBackend(t)
	big := strings.Repeat("x", parseLimit)
	logJobOf(gh, 1002, "Backend tests", big)
	ctx := context.Background()
	if l, err := b.JobLog(ctx, "darkmem", 1002); err != nil || !l.TooLarge {
		t.Fatalf("parsed: %+v err %v", l, err)
	}
	for range 2 {
		raw, err := b.RawJobLog(ctx, "darkmem", 1002)
		if err != nil || !strings.Contains(raw.Text, big) {
			t.Fatalf("raw: %d bytes, err %v", len(raw.Text), err)
		}
	}
	if n := gh.count("JobLog"); n != 3 {
		t.Fatalf("%d downloads, want 3: the raw log is not cached", n)
	}
	var ae *api.Error
	for _, tc := range []struct {
		err    error
		status int
		msg    string
	}{
		{github.ErrTooLarge, 413, "log is larger than 32 MiB; open it on GitHub"},
		{&github.APIError{Status: 410, Kind: github.ErrGone}, 410, "GitHub no longer keeps this job's log"},
	} {
		gh.rmu.Lock()
		gh.logErr[1002] = tc.err
		gh.rmu.Unlock()
		if _, err := b.RawJobLog(ctx, "darkmem", 1002); !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg {
			t.Fatalf("%v: got %v", tc.err, err)
		}
	}
}

func TestJobLogPanicIsContained(t *testing.T) {
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "##[error]F401 unused")
	p := &panickyGH{GitHub: gh, jobLog: true}
	b.GH = p
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.JobLog(ctx, "darkmem", 1002); err == nil {
		t.Fatal("a panicking download answered without an error")
	}
	b.logs.mu.Lock()
	left := len(b.logs.calls)
	b.logs.mu.Unlock()
	if left != 0 || len(b.logs.slots) != 0 {
		t.Fatalf("%d calls and %d slots left held", left, len(b.logs.slots))
	}
}

func TestLogRouteTellsAQueuedDownloadFromAStartedOne(t *testing.T) {
	setLogRequestWindow(t, 100*time.Millisecond)
	b, _, gh := newBackend(t)
	for id := int64(1); id <= 5; id++ {
		logJobOf(gh, id, fmt.Sprintf("job %d", id), "x")
	}
	gh.logWait = make(chan struct{})
	release := sync.OnceFunc(func() { close(gh.logWait) })
	t.Cleanup(release)
	ctx := context.Background()
	const (
		started = "GitHub is still sending this log; try again shortly"
		queued  = "ghr is reading other logs; try again shortly"
	)
	type answer struct {
		id  int64
		err error
	}
	answers := make(chan answer, 6)
	ask := func(id int64) {
		_, err := b.JobLog(ctx, "darkmem", id)
		answers <- answer{id, err}
	}
	for id := int64(1); id <= 4; id++ {
		go ask(id)
	}
	waitCalls(t, gh, "JobLog", 4)
	go ask(5)
	go ask(5) // joins the first caller's queued download
	for range 6 {
		a := <-answers
		want := started
		if a.id == 5 {
			want = queued
		}
		var ae *api.Error
		if !errors.As(a.err, &ae) || ae.Status != 503 || ae.Msg != want {
			t.Errorf("job %d: %v, want 503 %q", a.id, a.err, want)
		}
	}
	var ae *api.Error
	if _, err := b.JobLog(ctx, "darkmem", 1); !errors.As(err, &ae) || ae.Msg != started {
		t.Fatalf("joining a started download: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		b.logs.mu.Lock()
		_, inFlight := b.logs.calls[5]
		_, missed := b.logs.misses[5]
		b.logs.mu.Unlock()
		if missed {
			t.Fatal("a busy download was cached as a miss")
		}
		if !inFlight {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queued download never gave up")
		}
		time.Sleep(5 * time.Millisecond)
	}
	release()
	for len(b.logs.slots) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the slots never freed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := b.JobLog(ctx, "darkmem", 5); err != nil || gh.count("JobLog darkmem 5") != 1 {
		t.Fatalf("after the slots freed: err %v, %d downloads of job 5", err, gh.count("JobLog darkmem 5"))
	}
}
