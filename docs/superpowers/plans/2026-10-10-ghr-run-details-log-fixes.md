# ghr Run Details Log Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close register rows 21 and 32 of the run details work: queued log downloads give up and say so, and the raw log streams as plain text instead of JSON.

**Architecture:** One `logRequestWindow` bounds every log request and a queued download's slot wait. A download records when it gets its slot, so the log route tells a queue from a slow GitHub. The GitHub client gains `OpenJobLog` (the body unread), the daemon's `openRawLog` holds a slot until its reader is closed, and the raw route copies that reader to the browser, settling every status before the first byte and aborting the connection after it.

**Tech Stack:** Go 1.26 standard library (`net/http`, `context`, `sync`, `io`), React 19 + TypeScript, TanStack Query, Vitest + Testing Library.

**Spec:** docs/superpowers/specs/2026-10-09-ghr-run-details-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 4 of 7 tasks total 4 or less; Tasks 2, 5 and 6 (total 5 or more) are delegated.

## Global Constraints

- Execution environment: the owner's Linux machine (bash, GNU coreutils `timeout`, Go 1.26 at `/usr/local/go/bin`, Node 24). If `go` is not on `PATH`, run `export PATH=$PATH:/usr/local/go/bin` first.
- Work on the branch `fix/run-details-logs` (Task 1 Step 0 creates it from `master`), never on `master`. Do not push `master`: CI turns every push to `master` into a release. Do not commit `.superpowers/`.
- Go checks, from the repository root: `gofmt -l internal cmd web/*.go` prints nothing; `go vet ./internal/... ./cmd/... ./web/` passes. Never run `go vet ./...` or `go test ./...`: they walk Go code under `web/node_modules`.
- Web checks, from `web/`: `npm test`, `npm run typecheck`, `npm run lint` (eslint with `--max-warnings 0`).
- Give every test command an explicit timeout (`timeout 300 …`).
- Copy, verbatim: 503 "ghr is reading other logs; try again shortly"; 503 "GitHub is still sending this log; try again shortly"; 413 "log is larger than 32 MiB; open it on GitHub"; 410 "GitHub no longer keeps this job's log"; 502 "GitHub stopped sending the log".
- Timings: `logRequestWindow` 50 s; raw slot wait 25 s from the request's start; parsed download deadline 60 s once it has a slot (`logDeadline`, unchanged); the raw route's write deadline 50 s from the route's start. The web listener's timeouts stay as they are.
- A wait that ends because the caller's own context ended keeps returning that context's error, as today: a request's context ends only when its client has gone (register row 15, closed).
- English only. Comments only where the why is not obvious.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**Go — daemon (`internal/daemon`)**

- `logRequestWindow` (Task 1): `var logRequestWindow = 50 * time.Second` in `joblogs.go`, replacing `logRouteWait` and `rawLogDeadline`.
- `errReadingOtherLogs() error` (Task 1, `joblogs.go`): `&api.Error{Status: http.StatusServiceUnavailable, Msg: "ghr is reading other logs; try again shortly"}`.
- `setLogRequestWindow(t *testing.T, d time.Duration)` (Task 1, `joblogs_test.go`): sets `logRequestWindow` for one test and restores it.
- `logBusy` (Task 2): a `logOutcome` after `logFailed`; never cached. `logCall.started chan struct{}` (Task 2): closed when the download gets its slot.
- `GitHub.OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)` (Task 4): added to the daemon's `GitHub` interface in `backend.go`.
- Fakes (Task 4): `fakeRuns.logLen map[int64]int64` (overrides `OpenJobLog`'s length for that job; -1 means none declared), `fakeRuns.logClosed int` (bodies `OpenJobLog` returned that were closed), `fakeRuns.logReads int` (`Read` calls on those bodies), `fakeRuns.logCtx context.Context` (the last `OpenJobLog` call's context), all guarded by `rmu`; `fakeRuns.OpenJobLog` recorded as `"OpenJobLog <repo> <id>"`, never waiting on `logWait`; `panickyGH.openLog bool` makes `OpenJobLog` panic.
- `rawLimit` (Task 5): becomes `var rawLimit int64 = 32 << 20` (tests shrink it); `rawSlotWait` (Task 5): `var rawSlotWait = 25 * time.Second`.
- `(b *Backend) openRawLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)` (Task 5): the reader holds a slot, GitHub's body and the request deadline until `Close`, which is safe to call more than once; on an error all three are already released. The length is always known (≥ 0).
- `(b *Backend) RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)` (Task 6): returns `b.openRawLog(ctx, repo, jobID)`.

**Go — GitHub client (`internal/github`)**

- `(c *Client) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)` (Task 3): the redirect from `…/actions/jobs/{jobID}/logs`, then a GET on `c.Blob` without `Authorization`; returns the 200 response's body unread and `resp.ContentLength` (-1 when none). `JobLog` reads through it.

**Go — API (`internal/api`)**

- `Backend.RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)` (Task 6), replacing the `model.RawLog` form.
- `GET /actions/{repo}/jobs/{jobId}/log/raw` (Task 6): on success `200`, `Content-Type: text/plain; charset=utf-8`, `Content-Length` = the length, the body byte for byte; backend errors through `respond`; a read error before the first byte answers 502 "GitHub stopped sending the log"; a failure after it panics with `http.ErrAbortHandler`.

**Web (`web/src`)**

- `api.rawJobLog(repo: string, jobId: number): Promise<Blob>` (Task 7), sending `X-GHR: 1`; a non-2xx answer throws `ApiError` like `request`.

## Assumptions (evidence)

- A request's context ends only when its client disconnects: Go clears the connection's read deadline before the handler runs (`/usr/local/go/src/net/http/server.go:697`, called at `:2060-2062`), and a probe server with `ReadTimeout: 1s` kept a GET handler's context live for 3 s (run 2026-10-10). So no task maps a cancelled caller context to a status.
- `panic(http.ErrAbortHandler)` aborts the response without logging a stack trace: `/usr/local/go/src/net/http/server.go:1869-1873`, read on 2026-10-10.
- The `net/http` client stops a body with a declared length at that length (`transfer.go:572-573`) and reports a short body as `io.ErrUnexpectedEOF` (`transfer.go:865`); a handler writing past its declared length gets `http.ErrContentLength` (`server.go:56`). Read on 2026-10-10. So `openRawLog` needs no cap reader for a declared length.
- Under the web tests' jsdom, `new Response(...).blob()` returns a `Blob` (`instanceof Blob`, `text()` works), and a body stream that errors makes `blob()` reject with the stream's own error ("connection dropped"): probed with a throwaway Vitest file on 2026-10-10.
- `newBlobServer` in `internal/github/download_test.go:16` writes a short body in one `Fprint`, so Go's server sends a `Content-Length`; a handler that calls `Flush` first sends none (chunked). Task 3's test uses both.
- Whether GitHub's job-log blob responses carry a `Content-Length` is unverified; `openRawLog` handles both cases, and no task depends on the answer.
- Task 6 scores coupling 2 although it changes `api.Backend.RawJobLog`'s signature: both implementers (`*daemon.Backend`, checked by `var _ api.Backend = (*Backend)(nil)` at `internal/daemon/backend.go:117`, and `fakeBackend`) change in that task and the compiler checks them, the precedent of the run details plan's Task 11 (`docs/superpowers/plans/2026-10-09-ghr-run-details.md:4385`). The daemon side is written first (Task 5) as `openRawLog`, so Task 6 only switches the interface.
- Task 4 has no new test: it adds an interface method and fakes, checked by `go vet` and the existing daemon suite. It is separate from Task 5 so each stays within Rule S.

## Task index

1. One window for log requests
2. Queued log downloads give up
3. GitHub OpenJobLog
4. Daemon OpenJobLog interface and fakes
5. Daemon raw log stream
6. Raw log route streams
7. Raw log save in the browser

---

### Task 1: One window for log requests

**Files:**
- Modify: `internal/daemon/joblogs.go` (the `var` block at lines 25-36, `JobLog` at 198-233, `RawJobLog` at 236-267)
- Test: `internal/daemon/joblogs_test.go` (a helper after `setParsedLogBytes` at line 45, `TestJobLogStopsWaitingButKeepsTheDownload` at 187, `TestRawJobLogGivesUpWhenEverySlotIsBusy` at 250)

**Interfaces:**
- Consumes: none
- Produces: `logRequestWindow`, `errReadingOtherLogs`, `setLogRequestWindow` (Contracts, Go — daemon)

**Items:** 21, 32

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 0: Create the branch**

```bash
git switch master && git switch -c fix/run-details-logs
```

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/joblogs_test.go`, add after `setParsedLogBytes` (line 45-49):

```go
func setLogRequestWindow(t *testing.T, d time.Duration) {
	old := logRequestWindow
	logRequestWindow = d
	t.Cleanup(func() { logRequestWindow = old })
}
```

In `TestJobLogStopsWaitingButKeepsTheDownload`, replace its first three lines:

```go
	old := logRouteWait
	logRouteWait = 20 * time.Millisecond
	t.Cleanup(func() { logRouteWait = old })
```

with:

```go
	setLogRequestWindow(t, 20*time.Millisecond)
```

In `TestRawJobLogGivesUpWhenEverySlotIsBusy`, replace its first three lines:

```go
	old := rawLogDeadline
	rawLogDeadline = 50 * time.Millisecond
	t.Cleanup(func() { rawLogDeadline = old })
```

with:

```go
	setLogRequestWindow(t, 50*time.Millisecond)
```

and, because the four `JobLog` callers in that test now share the 50 ms window and answer 503 themselves, replace its last loop:

```go
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
```

with:

```go
	for range 4 {
		<-done
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'JobLog' -count=1`
Expected: FAIL to build with `undefined: logRequestWindow`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/joblogs.go`, replace these lines of the `var` block:

```go
	// logRouteWait and rawLogDeadline bound each log request from its start
	// (job read, slot wait and download), keeping the answer under the web
	// listener's 60 s WriteTimeout; a parsed download that outlasts the wait
	// still finishes and fills the cache for the next call.
	logRouteWait   = 50 * time.Second
	rawLogDeadline = 50 * time.Second
```

with:

```go
	// logRequestWindow bounds each log request from its start (job read,
	// slot wait, and download or copy), keeping the answer under the web
	// listener's 60 s timeouts; a parsed download that outlasts it still
	// finishes and fills the cache for the next call.
	logRequestWindow = 50 * time.Second
```

Add after `flaggedLog`:

```go
// errReadingOtherLogs is the 503 for a log request that found every download
// slot taken.
func errReadingOtherLogs() error {
	return &api.Error{Status: http.StatusServiceUnavailable, Msg: "ghr is reading other logs; try again shortly"}
}
```

In `JobLog`, replace `wctx, cancel := context.WithTimeout(ctx, logRouteWait)` with:

```go
	wctx, cancel := context.WithTimeout(ctx, logRequestWindow)
```

In `RawJobLog`, replace `rctx, cancel := context.WithTimeout(ctx, rawLogDeadline)` with:

```go
	rctx, cancel := context.WithTimeout(ctx, logRequestWindow)
```

and replace its last line inside `case <-rctx.Done():`:

```go
		return model.RawLog{}, &api.Error{Status: http.StatusServiceUnavailable, Msg: "ghr is reading other logs; try again shortly"}
```

with:

```go
		return model.RawLog{}, errReadingOtherLogs()
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ -count=1 && timeout 300 go test -race ./internal/daemon/ -run 'JobLog|LogDownloads' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && go vet ./internal/daemon/
git add internal/daemon/joblogs.go internal/daemon/joblogs_test.go
git commit -m "refactor(daemon): one window for log requests"
```

### Task 2: Queued log downloads give up

**Files:**
- Modify: `internal/daemon/joblogs.go` (the `logRequestWindow` comment, `logOutcome`, `logCall`, `startLogLocked`, `downloadLog`, `readLog`, `JobLog`)
- Modify: `internal/daemon/annotations.go` (the outcome tally at lines 74-78)
- Test: `internal/daemon/joblogs_test.go` (a new test at the end; add `"sync"` to the imports)
- Test: `internal/daemon/annotations_test.go` (a new test after `TestRunAnnotationsCountsUnreadAfterTheWait`)

**Interfaces:**
- Consumes: `logRequestWindow`, `errReadingOtherLogs`, `setLogRequestWindow` (Task 1)
- Produces: `logBusy`, `logCall.started` (Contracts, Go — daemon; used only inside this task)

**Items:** 21

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/joblogs_test.go`, add `"sync"` to the imports and add at the end of the file:

```go
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
```

In `internal/daemon/annotations_test.go`, add after `TestRunAnnotationsCountsUnreadAfterTheWait`:

```go
func TestRunAnnotationsCountsABusyDownloadAsUnread(t *testing.T) {
	setAnnotationsWait(t, 2*time.Second)
	setLogRequestWindow(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	for id := int64(1); id <= 4; id++ {
		logJobOf(gh, id, fmt.Sprintf("job %d", id), "x")
	}
	annotatedRun(gh, logJobOf(gh, 2001, "Build", "##[error]a"))
	gh.logWait = make(chan struct{})
	t.Cleanup(func() { close(gh.logWait) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for id := int64(1); id <= 4; id++ {
		go b.JobLog(ctx, "darkmem", id)
	}
	waitCalls(t, gh, "JobLog", 4)
	start := time.Now()
	a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
	if err != nil || a.Unread != 1 || a.Failed != 0 || len(a.Items) != 0 {
		t.Fatalf("annotations %+v err %v", a, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v for a download that could not start", took)
	}
	b.logs.mu.Lock()
	_, missed := b.logs.misses[2001]
	b.logs.mu.Unlock()
	if missed {
		t.Fatal("a busy download was cached as a miss")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'QueuedDownloadFromAStarted|BusyDownloadAsUnread' -count=1`
Expected: FAIL: job 5 answers "GitHub is still sending this log; try again shortly", and the annotations call waits 2 s.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/joblogs.go`, replace the `logRequestWindow` comment written in Task 1 with:

```go
	// logRequestWindow bounds each log request from its start (job read,
	// slot wait, and download or copy) and a queued download's wait for a
	// slot, keeping the answer under the web listener's 60 s timeouts; a
	// parsed download that outlasts it still finishes and fills the cache
	// for the next call.
```

Add `logBusy` to the outcomes:

```go
const (
	logParsed logOutcome = iota
	logExpired
	logTooLarge
	logFailed
	// logBusy is a download that got no slot within logRequestWindow; it is
	// never cached, so the next call starts it again.
	logBusy
)
```

Replace `logCall`:

```go
// logCall is one log download that the log route and the annotations route
// share.
type logCall struct {
	done    chan struct{}
	started chan struct{} // closed when the download gets its slot
	res     logResult
}
```

In `startLogLocked`, replace `call := &logCall{done: make(chan struct{})}` with:

```go
	call := &logCall{done: make(chan struct{}), started: make(chan struct{})}
```

Replace `downloadLog` and `readLog` with:

```go
// downloadLog runs detached from every request, so a caller that stops
// waiting still leaves the result in the caches.
func (b *Backend) downloadLog(j logJob, call *logCall) {
	c := &b.logs
	res, size := b.readLog(j, call.started)

	c.mu.Lock()
	delete(c.calls, j.id)
	switch res.outcome {
	case logParsed:
		c.parsed.add(j.id, res.log, size)
		c.notes.add(j.id, res.notes, 0)
		delete(c.misses, j.id)
	case logBusy:
	default:
		c.misses[j.id] = logMiss{at: b.now(), res: res}
	}
	c.mu.Unlock()
	call.res = res
	close(call.done)
}

// readLog downloads and parses one job's log once it gets a download slot,
// closing started then; one still queued after logRequestWindow ends busy.
// This runs on a detached goroutine, where a panic would end the daemon, so
// it becomes a failed outcome; the download slot is released either way.
func (b *Backend) readLog(j logJob, started chan struct{}) (res logResult, size int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("job %d log: panic: %v", j.id, r)
			res, size = logResult{outcome: logFailed, err: errors.New("could not read the log")}, 0
		}
	}()
	c := &b.logs
	queued := time.NewTimer(logRequestWindow)
	defer queued.Stop()
	select {
	case c.slots <- struct{}{}:
	case <-queued.C:
		return logResult{outcome: logBusy}, 0
	}
	close(started)
	data, err := func() ([]byte, error) {
		defer func() { <-c.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), logDeadline)
		defer cancel()
		return b.GH.JobLog(ctx, j.repo, j.id, parseLimit)
	}()

	switch {
	case err == nil:
		steps := joblog.Parse(data, j.steps)
		items, omitted := joblog.Annotations(j.id, j.name, steps)
		return logResult{outcome: logParsed, log: model.JobLog{JobID: j.id, Steps: steps}, notes: jobNotes{items: items, omitted: omitted}}, int64(len(data))
	case errors.Is(err, github.ErrTooLarge):
		return logResult{outcome: logTooLarge}, 0
	case github.IsKind(err, github.ErrGone):
		return logResult{outcome: logExpired}, 0
	}
	log.Printf("job %d log: %v", j.id, err)
	return logResult{outcome: logFailed, err: err}, 0
}
```

Add after `errReadingOtherLogs`:

```go
// logNotReady tells a download still queued for a slot from one GitHub is
// still sending.
func logNotReady(call *logCall) error {
	select {
	case <-call.started:
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is still sending this log; try again shortly"}
	default:
		return errReadingOtherLogs()
	}
}
```

In `JobLog`, replace from `select {` to the end of the function with:

```go
	select {
	case <-call.done:
	case <-wctx.Done():
		if err := ctx.Err(); err != nil {
			return model.JobLog{}, err
		}
		return model.JobLog{}, logNotReady(call)
	}
	switch call.res.outcome {
	case logParsed:
		return call.res.log, nil
	case logFailed:
		return model.JobLog{}, detailErr(call.res.err, b.Store.Config().Owner, j.repo)
	case logBusy:
		return model.JobLog{}, logNotReady(call)
	}
	return flaggedLog(jobID, call.res.outcome), nil
}
```

In `internal/daemon/annotations.go`, replace:

```go
			if call.res.outcome == logParsed {
				notes[j.ID] = call.res.notes
			} else {
				outcomes[j.ID] = call.res.outcome
			}
```

with:

```go
			switch call.res.outcome {
			case logParsed:
				notes[j.ID] = call.res.notes
			case logBusy:
				out.Unread++
				continue
			default:
				outcomes[j.ID] = call.res.outcome
			}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ -count=1 && timeout 300 go test -race ./internal/daemon/ -run 'JobLog|LogRoute|LogDownloads|RunAnnotations' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && go vet ./internal/daemon/
git add internal/daemon/joblogs.go internal/daemon/joblogs_test.go internal/daemon/annotations.go internal/daemon/annotations_test.go
git commit -m "fix(daemon): give up on log downloads stuck in queue"
```

### Task 3: GitHub OpenJobLog

**Files:**
- Modify: `internal/github/download.go` (`JobLog` at lines 73-100)
- Test: `internal/github/download_test.go` (add `"io"` and `"strconv"` to the imports, a row in `TestDownloadErrors`, a helper and a new test after `TestJobLogStopsAtMax`)

**Interfaces:**
- Consumes: none
- Produces: `Client.OpenJobLog` (Contracts, Go — GitHub client)

**Items:** 32

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/github/download_test.go`, add `"io"` and `"strconv"` to the imports. Add this row to the table in `TestDownloadErrors`, after the `"expired log"` row:

```go
		{name: "expired log, opened", status: 410, kind: ErrGone,
			call: func(c *Client) error { _, _, err := c.OpenJobLog(ctx, "darkmem", 1); return err }},
```

Add after `TestJobLogStopsAtMax`:

```go
const (
	gatedHead = "2026-10-03T13:56:10.0000000Z hello\n"
	gatedTail = "2026-10-03T13:56:11.0000000Z bye\n"
)

// gatedBlob serves gatedHead, flushes, and holds gatedTail back until release
// is called; sized declares a Content-Length. It refuses any Authorization.
func gatedBlob(t *testing.T, sized bool) (*httptest.Server, func()) {
	t.Helper()
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("blob server saw Authorization %q", r.Header.Get("Authorization"))
		}
		if sized {
			w.Header().Set("Content-Length", strconv.Itoa(len(gatedHead+gatedTail)))
		}
		fmt.Fprint(w, gatedHead)
		w.(http.Flusher).Flush()
		<-gate
		fmt.Fprint(w, gatedTail)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(release)
	return srv, release
}

func TestOpenJobLogReturnsTheBodyUnread(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sized bool
		size  int64
	}{
		{"with a length", true, int64(len(gatedHead + gatedTail))},
		{"without a length", false, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob, release := gatedBlob(t, tc.sized)
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, blob.URL+"/log?sig=1", http.StatusFound)
			})
			var body io.ReadCloser
			var size int64
			var err error
			opened := make(chan struct{})
			go func() {
				body, size, err = c.OpenJobLog(context.Background(), "darkmem", 1002)
				close(opened)
			}()
			select {
			case <-opened:
			case <-time.After(3 * time.Second):
				t.Fatal("OpenJobLog did not return while GitHub held the rest of the body")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer body.Close()
			release()
			data, err := io.ReadAll(body)
			if err != nil || string(data) != gatedHead+gatedTail || size != tc.size {
				t.Fatalf("body %q size %d err %v", data, size, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/github/ -run 'OpenJobLog|DownloadErrors' -count=1`
Expected: FAIL to build with `c.OpenJobLog undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/github/download.go`, replace the whole `JobLog` function, from its doc comment `// JobLog downloads a finished job's log, at most max bytes.` to its closing brace, with:

```go
// OpenJobLog starts a job log download and returns the body unread with its
// Content-Length, -1 when GitHub sends none. The body stays bound to ctx; the
// caller closes it.
func (c *Client) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	loc, err := c.redirectURL(ctx, c.repoURL(repo, fmt.Sprintf("/actions/jobs/%d/logs", jobID)))
	if err != nil {
		return nil, 0, hideURL(err, "log")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, 0, hideURL(err, "log")
	}
	resp, err := c.Blob.Do(req)
	if err != nil {
		return nil, 0, hideURL(err, "log")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("github: log download answered %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

// JobLog downloads a finished job's log, at most max bytes. The signed URL
// GitHub redirects to is fetched without the token.
func (c *Client) JobLog(ctx context.Context, repo string, jobID int64, max int64) ([]byte, error) {
	body, _, err := c.OpenJobLog(ctx, repo, jobID)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/github/ -count=1 && timeout 300 go test -race ./internal/github/ -run 'JobLog|Download' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && go vet ./internal/github/
git add internal/github/download.go internal/github/download_test.go
git commit -m "feat(github): open a job log without reading it"
```

### Task 4: Daemon OpenJobLog interface and fakes

**Files:**
- Modify: `internal/daemon/backend.go` (the `GitHub` interface at lines 44-63; add `"io"` to the imports)
- Modify: `internal/daemon/runs_fake_test.go` (the `fakeRuns` struct at lines 14-33, a method after `JobLog`; add `"bytes"` and `"io"` to the imports)
- Modify: `internal/daemon/rundetail_test.go` (`panickyGH` at line 266 and a method after its `JobLog`; add `"io"` to the imports)

**Interfaces:**
- Consumes: `Client.OpenJobLog` (Task 3)
- Produces: `GitHub.OpenJobLog` and the fakes (Contracts, Go — daemon)

**Items:** 32

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Add the interface method**

In `internal/daemon/backend.go`, add `"io"` to the imports and add to the `GitHub` interface, after `JobLog(...)`:

```go
	OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)
```

- [ ] **Step 2: Add the fakes**

In `internal/daemon/runs_fake_test.go`, add `"bytes"` and `"io"` to the imports and add two fields to `fakeRuns`, after `logWait`:

```go
	logLen      map[int64]int64             // OpenJobLog's length for that job, when set (-1: none declared)
	logClosed   int                         // bodies OpenJobLog returned that were closed
	logReads    int                         // Read calls on bodies OpenJobLog returned
	logCtx      context.Context             // the context of the last OpenJobLog call
```

Add after `func (f *fakeRuns) JobLog`:

```go
// OpenJobLog answers from logs and logErr like JobLog, without waiting on
// logWait.
func (f *fakeRuns) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	f.record("OpenJobLog", repo, jobID)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	f.logCtx = ctx
	if err := f.logErr[jobID]; err != nil {
		return nil, 0, err
	}
	data, ok := f.logs[jobID]
	if !ok {
		return nil, 0, ghNotFound()
	}
	size := int64(len(data))
	if n, ok := f.logLen[jobID]; ok {
		size = n
	}
	return &fakeBody{r: bytes.NewReader(data), f: f}, size, nil
}

// fakeBody counts its reads in logReads and its closes in logClosed.
type fakeBody struct {
	r io.Reader
	f *fakeRuns
}

func (b *fakeBody) Read(p []byte) (int, error) {
	b.f.rmu.Lock()
	b.f.logReads++
	b.f.rmu.Unlock()
	return b.r.Read(p)
}

func (b *fakeBody) Close() error {
	b.f.rmu.Lock()
	b.f.logClosed++
	b.f.rmu.Unlock()
	return nil
}
```

In `internal/daemon/rundetail_test.go`, add `"io"` to the imports, change the `panickyGH` field line `run, jobLog, content bool` to:

```go
	run, jobLog, content, openLog bool
```

and add after `func (p *panickyGH) JobLog`:

```go
func (p *panickyGH) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	if p.openLog {
		panic("boom")
	}
	return p.GitHub.OpenJobLog(ctx, repo, jobID)
}
```

- [ ] **Step 3: Check the package builds and the suite passes**

Run: `gofmt -l internal && go vet ./internal/daemon/ && timeout 300 go test ./internal/daemon/ -count=1`
Expected: no `gofmt` output, `vet` clean, PASS

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/runs_fake_test.go internal/daemon/rundetail_test.go
git commit -m "test(daemon): fake opening a job log"
```

### Task 5: Daemon raw log stream

**Files:**
- Modify: `internal/daemon/joblogs.go` (the `const` and `var` blocks at lines 17-36, new code after `JobLog`; add `"bytes"` and `"io"` to the imports)
- Test: `internal/daemon/joblogs_test.go` (helpers after `setLogRequestWindow`, the `"raw"` entry in `TestJobLogRefusals`, replacing `TestRawJobLog` and `TestRawJobLogGivesUpWhenEverySlotIsBusy`; add `"io"` to the imports)

**Interfaces:**
- Consumes: `GitHub.OpenJobLog` and the fakes (Task 4); `logRequestWindow`, `errReadingOtherLogs` (Task 1)
- Produces: `openRawLog`, `rawLimit` as a `var`, `rawSlotWait` (Contracts, Go — daemon)

**Items:** 32

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/joblogs_test.go`, add `"io"` to the imports and add after `setLogRequestWindow`:

```go
func setRawSlotWait(t *testing.T, d time.Duration) {
	old := rawSlotWait
	rawSlotWait = d
	t.Cleanup(func() { rawSlotWait = old })
}

func setRawLimit(t *testing.T, n int64) {
	old := rawLimit
	rawLimit = n
	t.Cleanup(func() { rawLimit = old })
}

// bodiesClosed is how many OpenJobLog bodies gh has seen closed.
func bodiesClosed(gh *fakeGH) int {
	gh.rmu.Lock()
	defer gh.rmu.Unlock()
	return gh.logClosed
}

// bodyReads is how many Read calls OpenJobLog bodies have had.
func bodyReads(gh *fakeGH) int {
	gh.rmu.Lock()
	defer gh.rmu.Unlock()
	return gh.logReads
}

// openCtx is the context of gh's last OpenJobLog call.
func openCtx(gh *fakeGH) context.Context {
	gh.rmu.Lock()
	defer gh.rmu.Unlock()
	return gh.logCtx
}
```

In `TestJobLogRefusals`, replace the `"raw"` entry with:

```go
		"raw": func(repo string) error { _, _, err := b.openRawLog(ctx, repo, 1002); return err },
```

Delete `TestRawJobLogGivesUpWhenEverySlotIsBusy` and `TestRawJobLog`, and add in their place:

```go
func TestRawLogGivesUpWhenEverySlotIsBusy(t *testing.T) {
	setRawSlotWait(t, 50*time.Millisecond)
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
	if _, _, err := b.openRawLog(context.Background(), "darkmem", 5); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "ghr is reading other logs; try again shortly" {
		t.Fatalf("busy slots: %v", err)
	}
	if n := gh.count("OpenJobLog"); n != 0 {
		t.Fatalf("a raw log was downloaded %d times while every slot was busy", n)
	}
	close(gh.logWait)
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestRawLog(t *testing.T) {
	b, _, gh := newBackend(t)
	big := strings.Repeat("x", parseLimit)
	logJobOf(gh, 1002, "Backend tests", big)
	ctx := context.Background()
	if l, err := b.JobLog(ctx, "darkmem", 1002); err != nil || !l.TooLarge {
		t.Fatalf("parsed: %+v err %v", l, err)
	}
	want := string(logLines(big))
	for range 2 {
		rc, size, err := b.openRawLog(ctx, "darkmem", 1002)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.logs.slots) != 1 {
			t.Fatalf("%d slots held while the log is open, want 1", len(b.logs.slots))
		}
		if n := bodyReads(gh); n != 0 {
			t.Fatalf("a body with a declared length was read %d times before openRawLog returned", n)
		}
		octx := openCtx(gh)
		if _, ok := octx.Deadline(); !ok || octx.Err() != nil {
			t.Fatalf("the download's context has no deadline or ended early: %v", octx.Err())
		}
		data, err := io.ReadAll(rc)
		if err != nil || string(data) != want || size != int64(len(want)) {
			t.Fatalf("raw: %d bytes, size %d, err %v", len(data), size, err)
		}
		rc.Close()
		rc.Close()
		if len(b.logs.slots) != 0 || octx.Err() == nil {
			t.Fatalf("%d slots held after closing twice, context err %v", len(b.logs.slots), octx.Err())
		}
		gh.rmu.Lock()
		gh.logReads = 0
		gh.rmu.Unlock()
	}
	if n, closed := gh.count("OpenJobLog"), bodiesClosed(gh); n != 2 || closed != 2 {
		t.Fatalf("%d raw downloads and %d bodies closed, want 2 and 2: the raw log is not cached", n, closed)
	}
}

func TestRawLogReadsABodyWithoutALength(t *testing.T) {
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "x")
	gh.logLen = map[int64]int64{1002: -1}
	want := string(logLines("x"))
	rc, size, err := b.openRawLog(context.Background(), "darkmem", 1002)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || string(data) != want || size != int64(len(want)) {
		t.Fatalf("raw %q size %d err %v", data, size, err)
	}
	if len(b.logs.slots) != 0 || bodiesClosed(gh) != 1 {
		t.Fatalf("%d slots held, %d bodies closed", len(b.logs.slots), bodiesClosed(gh))
	}
}

func TestRawLogErrorsReleaseEverything(t *testing.T) {
	setRawLimit(t, 64)
	const tooLarge = "log is larger than 32 MiB; open it on GitHub"
	for _, tc := range []struct {
		name   string
		setup  func(b *Backend, gh *fakeGH)
		status int    // 0: a plain error
		msg    string // "" checks the status only
		closed int    // GitHub bodies closed
		opened bool   // OpenJobLog was reached, so its context must have ended
	}{
		{"expired", func(b *Backend, gh *fakeGH) { gh.logErr[1002] = &github.APIError{Status: 410, Kind: github.ErrGone} },
			410, "GitHub no longer keeps this job's log", 0, true},
		{"failed", func(b *Backend, gh *fakeGH) { gh.logErr[1002] = errors.New("boom") }, 502, "", 0, true},
		{"declared too large", func(b *Backend, gh *fakeGH) { gh.logLen = map[int64]int64{1002: 65} }, 413, tooLarge, 1, true},
		{"too large without a length", func(b *Backend, gh *fakeGH) {
			gh.logs[1002] = []byte(strings.Repeat("x", 65))
			gh.logLen = map[int64]int64{1002: -1}
		}, 413, tooLarge, 1, true},
		{"panic", func(b *Backend, gh *fakeGH) { b.GH = &panickyGH{GitHub: gh, openLog: true} }, 0, "could not read the log", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			logJobOf(gh, 1002, "Backend tests", "x")
			tc.setup(b, gh)
			rc, _, err := b.openRawLog(context.Background(), "darkmem", 1002)
			if rc != nil || err == nil {
				t.Fatalf("reader %v err %v", rc, err)
			}
			if tc.status == 0 {
				if err.Error() != tc.msg {
					t.Fatalf("err %v, want %q", err, tc.msg)
				}
			} else if apiStatus(err) != tc.status || (tc.msg != "" && err.Error() != tc.msg) {
				t.Fatalf("err %v, want %d %q", err, tc.status, tc.msg)
			}
			if len(b.logs.slots) != 0 || bodiesClosed(gh) != tc.closed {
				t.Fatalf("%d slots held and %d bodies closed, want 0 and %d", len(b.logs.slots), bodiesClosed(gh), tc.closed)
			}
			if tc.opened && openCtx(gh).Err() == nil {
				t.Fatal("the download's context outlived the error")
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'RawLog|JobLogRefusals' -count=1`
Expected: FAIL to build with `b.openRawLog undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/joblogs.go`, add `"bytes"` and `"io"` to the imports. Remove `rawLimit = 32 << 20` from the `const` block and add to the `var` block, after `logRequestWindow`:

```go
	// rawLimit caps a raw log; tests shrink it.
	rawLimit int64 = 32 << 20
	// rawSlotWait is how long a raw log request, from its start, waits for a
	// download slot, leaving the rest of logRequestWindow for the copy.
	rawSlotWait = 25 * time.Second
```

Add after `logNotReady`:

```go
func errRawTooLarge() error {
	return &api.Error{Status: http.StatusRequestEntityTooLarge, Msg: "log is larger than 32 MiB; open it on GitHub"}
}
```

Add after `JobLog`:

```go
// rawLog is an open raw log; Close releases what openRawLog took, once.
type rawLog struct {
	io.Reader
	release func()
	once    sync.Once
}

func (l *rawLog) Close() error {
	l.once.Do(l.release)
	return nil
}

// openRawLog streams a finished job's whole log, up to rawLimit, uncached.
// The reader holds a download slot, GitHub's body and the request deadline
// until it is closed; on an error they are already released.
func (b *Backend) openRawLog(ctx context.Context, repo string, jobID int64) (rc io.ReadCloser, size int64, err error) {
	rctx, cancel := context.WithTimeout(ctx, logRequestWindow)
	release := []func(){cancel}
	releaseAll := func() {
		for i := len(release) - 1; i >= 0; i-- {
			release[i]()
		}
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("job %d raw log: panic: %v", jobID, r)
			rc, size, err = nil, 0, errors.New("could not read the log")
		}
		if err != nil {
			releaseAll()
		}
	}()
	slotCtx, stopSlotWait := context.WithTimeout(rctx, rawSlotWait)
	defer stopSlotWait()

	j, err := b.finishedJob(rctx, repo, jobID)
	if err != nil {
		return nil, 0, err
	}
	c := &b.logs
	c.mu.Lock()
	c.initLocked()
	slots := c.slots
	c.mu.Unlock()
	select {
	case slots <- struct{}{}:
		release = append(release, func() { <-slots })
	case <-slotCtx.Done():
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		return nil, 0, errReadingOtherLogs()
	}

	body, size, err := b.GH.OpenJobLog(rctx, j.repo, jobID)
	switch {
	case github.IsKind(err, github.ErrGone):
		return nil, 0, &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps this job's log"}
	case err != nil:
		return nil, 0, detailErr(err, b.Store.Config().Owner, j.repo)
	}
	release = append(release, func() { body.Close() })
	if size > rawLimit {
		return nil, 0, errRawTooLarge()
	}
	var r io.Reader = body
	if size < 0 {
		// Without a length the route could end the response only by closing
		// the connection, which a proxy speaking HTTP/1.0 to ghr cannot tell
		// from an abort, so the log is read first and sent with its length.
		data, rerr := io.ReadAll(io.LimitReader(body, rawLimit+1))
		if rerr != nil {
			return nil, 0, detailErr(rerr, b.Store.Config().Owner, j.repo)
		}
		if int64(len(data)) > rawLimit {
			return nil, 0, errRawTooLarge()
		}
		r, size = bytes.NewReader(data), int64(len(data))
	}
	return &rawLog{Reader: r, release: releaseAll}, size, nil
}
```

`RawJobLog` stays as it is until Task 6; it still compiles, since `rawLimit` keeps its value and `GH.JobLog` takes an `int64`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ -count=1 && timeout 300 go test -race ./internal/daemon/ -run 'RawLog|JobLog|LogRoute' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && go vet ./internal/daemon/
git add internal/daemon/joblogs.go internal/daemon/joblogs_test.go
git commit -m "feat(daemon): stream raw logs while holding a slot"
```

### Task 6: Raw log route streams

**Files:**
- Modify: `internal/api/server.go` (the `Backend` interface line 87, the raw route at lines 159-167, a helper after `respond`)
- Modify: `internal/daemon/joblogs.go` (replace `RawJobLog`)
- Test: `internal/api/api_test.go` (`fakeBackend` fields after `runErr` at line 61, its `RawJobLog` at 93-96, the raw lines of `TestRunDetailRoutes`, a new test at the end; add `"sync/atomic"` and `"testing/iotest"` to the imports)

**Interfaces:**
- Consumes: `openRawLog` (Task 5)
- Produces: `Backend.RawJobLog` and the raw route (Contracts, Go — API); the daemon's `RawJobLog` (Contracts, Go — daemon)

**Items:** 32

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/api/api_test.go`, add `"sync/atomic"` and `"testing/iotest"` to the imports and add to `fakeBackend`, after `runErr`:

```go
	rawBody       io.Reader     // what RawJobLog streams; "raw log" when nil
	rawSize       int64         // rawBody's length
	rawDelay      time.Duration // RawJobLog sleeps this long before answering
	rawClosed     atomic.Int32  // RawJobLog readers closed
```

Replace `func (f *fakeBackend) RawJobLog` with:

```go
func (f *fakeBackend) RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RawJobLog %s %d", repo, jobID))
	time.Sleep(f.rawDelay)
	if f.runErr != nil {
		return nil, 0, f.runErr
	}
	body, size := f.rawBody, f.rawSize
	if body == nil {
		body, size = strings.NewReader("raw log"), 7
	}
	return &countingBody{Reader: body, closed: &f.rawClosed}, size, nil
}

type countingBody struct {
	io.Reader
	closed *atomic.Int32
}

func (c *countingBody) Close() error {
	c.closed.Add(1)
	return nil
}
```

In `TestRunDetailRoutes`, replace:

```go
	var raw model.RawLog
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/jobs/1002/log/raw", nil, &raw); err != nil || raw.Text != "raw log" {
		t.Fatalf("raw %+v err %v", raw, err)
	}
```

with:

```go
	if resp, body, err := getRaw(c, "/actions/darkmem/jobs/1002/log/raw"); err != nil || resp.StatusCode != 200 || string(body) != "raw log" {
		t.Fatalf("raw %q err %v", body, err)
	}
```

Add at the end of the file:

```go
// getRaw fetches path from the API without decoding the body; resp is nil
// when no response arrived.
func getRaw(c *Client, path string) (*http.Response, []byte, error) {
	resp, err := c.HTTP.Get(c.Base + path)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp, body, err
}

// waitClosed waits up to 3 s for n RawJobLog readers to be closed: the
// handler closes its reader as it returns, which can be after the client
// has read the response.
func waitClosed(t *testing.T, b *fakeBackend, n int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for b.rawClosed.Load() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d readers closed, want %d", b.rawClosed.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRawLogRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   io.Reader
		size   int64
		status int
		want   string // the body, or the JSON error's message
		broken bool   // the body fails after the status line
	}{
		{name: "streams", body: strings.NewReader("line one\nline two\n"), size: 18, status: 200, want: "line one\nline two\n"},
		{name: "empty", body: strings.NewReader(""), size: 0, status: 200, want: ""},
		{name: "fails before the first byte", body: iotest.ErrReader(errors.New("boom")), size: 10, status: 502, want: "GitHub stopped sending the log"},
		{name: "fails after the first byte", body: io.MultiReader(strings.NewReader("line one\n"), iotest.ErrReader(errors.New("boom"))), size: 100, status: 200, broken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, b := setup(t)
			b.rawBody, b.rawSize = tc.body, tc.size
			resp, body, err := getRaw(c, "/actions/darkmem/jobs/1002/log/raw")
			if resp == nil || resp.StatusCode != tc.status {
				t.Fatalf("response %v err %v, want status %d", resp, err, tc.status)
			}
			switch {
			case tc.broken:
				if err == nil {
					t.Fatalf("a broken copy read as complete: %q", body)
				}
			case tc.status == 200:
				if err != nil || string(body) != tc.want || resp.ContentLength != tc.size || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("body %q length %d type %q err %v", body, resp.ContentLength, resp.Header.Get("Content-Type"), err)
				}
			default:
				var e struct {
					Error string `json:"error"`
				}
				if err != nil || json.Unmarshal(body, &e) != nil || e.Error != tc.want {
					t.Fatalf("error body %q err %v", body, err)
				}
			}
			waitClosed(t, b, 1)
		})
	}
}

func TestRawLogRouteDeadlineCountsFromTheRouteStart(t *testing.T) {
	old := rawWriteWindow
	rawWriteWindow = 50 * time.Millisecond
	t.Cleanup(func() { rawWriteWindow = old })
	c, b := setup(t)
	b.rawDelay = 100 * time.Millisecond
	if resp, body, err := getRaw(c, "/actions/darkmem/jobs/1002/log/raw"); err == nil {
		t.Fatalf("a write after the window completed: status %d body %q", resp.StatusCode, body)
	}
	waitClosed(t, b, 1)
}
```

The existing row `{"/actions/darkmem/jobs/1/log/raw", &Error{Status: http.StatusRequestEntityTooLarge, …}}` in `TestRunDetailRoutesPassBackendStatuses` stays: it checks that a backend error reaches the client as JSON before any body.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/api/ -run 'RawLogRoute|RunDetailRoutes' -count=1`
Expected: FAIL to build: `*fakeBackend` does not implement `Backend` (wrong type for method `RawJobLog`).

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, replace the interface line:

```go
	RawJobLog(ctx context.Context, repo string, jobID int64) (model.RawLog, error)
```

with:

```go
	RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error)
```

Replace the raw route's last two lines:

```go
		l, err := b.RawJobLog(r.Context(), r.PathValue("repo"), id)
		respond(w, l, err)
```

with:

```go
		deadline := time.Now().Add(rawWriteWindow)
		body, size, err := b.RawJobLog(r.Context(), r.PathValue("repo"), id)
		if err != nil {
			respond(w, nil, err)
			return
		}
		defer body.Close()
		streamLog(w, body, size, deadline)
```

Add after `respond`:

```go
// rawWriteWindow bounds a raw log request from the route's start, the same
// window as the daemon's logRequestWindow, which this package cannot import.
// It also covers the Unix socket listener, which has no WriteTimeout; tests
// shorten it.
var rawWriteWindow = 50 * time.Second

// streamLog copies a raw log of size bytes to the client. Until the first
// byte is written a failure still answers with a status; after it the
// handler aborts, so the connection drops short of Content-Length and the
// browser's download fails instead of saving a truncated log.
func streamLog(w http.ResponseWriter, body io.Reader, size int64, deadline time.Time) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(deadline)
	buf := make([]byte, 32<<10)
	n, err := io.ReadAtLeast(body, buf, 1)
	if err != nil && err != io.EOF {
		respond(w, nil, &Error{Status: http.StatusBadGateway, Msg: "GitHub stopped sending the log"})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf[:n]); err != nil {
		panic(http.ErrAbortHandler)
	}
	if n == 0 {
		return
	}
	// The status line goes out now, so a later abort reaches the client as a
	// broken body rather than as no response at all.
	if err := rc.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
	if _, err := io.CopyBuffer(w, body, buf); err != nil {
		panic(http.ErrAbortHandler)
	}
}
```

In `internal/daemon/joblogs.go`, delete the whole old `RawJobLog` function, from its comment `// RawJobLog returns a finished job's whole log, up to 32 MiB, uncached.` to its closing brace, and add in its place:

```go
// RawJobLog streams a finished job's whole log, up to 32 MiB, uncached; the
// reader holds a download slot until it is closed.
func (b *Backend) RawJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	return b.openRawLog(ctx, repo, jobID)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/api/ ./internal/daemon/ -count=1 && timeout 300 go test -race ./internal/api/ -run 'RawLogRoute|RunDetailRoutes' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && go vet ./internal/... ./cmd/...
git add internal/api/server.go internal/api/api_test.go internal/daemon/joblogs.go
git commit -m "feat(api): stream the raw log as plain text"
```

### Task 7: Raw log save in the browser

**Files:**
- Modify: `internal/model/model.go` (delete `RawLog` at lines 440-443)
- Modify: `web/src/api/client.ts` (the `RawLog` import at line 19, a helper after `request`, `rawJobLog` at line 124)
- Modify: `web/src/api/types.ts` (delete `RawLog` at lines 572-574)
- Modify: `web/src/pages/run.tsx` (`saveText` at lines 240-252, the `raw` mutation at 256-259)
- Test: `web/src/pages/run.test.tsx` (the test "saves the raw log through a Blob" at lines 203-223, a new test after it)

**Interfaces:**
- Consumes: the raw route (Contracts, Go — API)
- Produces: `api.rawJobLog` returning `Blob` (Contracts, Web)

**Items:** 32

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/run.test.tsx`, replace the test "saves the raw log through a Blob" with:

```tsx
  it("saves the raw log through a Blob", async () => {
    const text = "2026-10-03T13:55:25.0000000Z Set up job\n"
    const { calls, fetchMock } = mockApi(
      runRoutes({
        "GET /api/actions/darkmem/jobs/1002/log/raw": () => new Response(text, { headers: { "Content-Type": "text/plain; charset=utf-8" } }),
      }),
    )
    const blobs: Blob[] = []
    const create = vi.fn((obj: Blob | MediaSource) => {
      blobs.push(obj as Blob)
      return "blob:http://localhost/raw"
    })
    const revoke = vi.fn()
    // jsdom has no object URLs; a subclass keeps new URL(...) working.
    class FakeURL extends URL {
      static createObjectURL = create
      static revokeObjectURL = revoke
    }
    vi.stubGlobal("URL", FakeURL)
    const saved: string[] = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(`${this.download} ${this.href}`)
    })
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await user.click(await screen.findByRole("button", { name: "Raw log" }))
    await waitFor(() => expect(saved).toEqual(["darkmem-job-1002.log blob:http://localhost/raw"]))
    expect(blobs).toHaveLength(1)
    expect(await blobs.at(0)?.text()).toBe(text)
    expect(calls.some((c) => c.path === "/api/actions/darkmem/jobs/1002/log/raw")).toBe(true)
    const rawCall = fetchMock.mock.calls.find(([input]) => typeof input === "string" && input.endsWith("/log/raw"))
    expect(new Headers(rawCall?.[1]?.headers).get("X-GHR")).toBe("1")
    expect(rawCall?.[1]?.credentials).toBe("same-origin")
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:http://localhost/raw"))
    click.mockRestore()
  })

  it.each([
    ["an error answer", () => json({ error: "log is larger than 32 MiB; open it on GitHub" }, 413), "log is larger than 32 MiB; open it on GitHub"],
    [
      "a download that breaks",
      () =>
        new Response(
          new ReadableStream({
            start: (c) => {
              c.enqueue(new TextEncoder().encode("part"))
              c.error(new Error("connection dropped"))
            },
          }),
        ),
      "connection dropped",
    ],
  ])("toasts a raw log save that fails: %s", async (_name, route, message) => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log/raw": route }))
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await user.click(await screen.findByRole("button", { name: "Raw log" }))
    expect(await screen.findByText(message)).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/pages/run.test.tsx`
Expected: FAIL: "saves the raw log through a Blob" fails, because the page parses the plain-text answer as JSON. The two toast cases already pass (the current `request` throws the same errors) and guard the change.

- [ ] **Step 3: Write the implementation**

In `web/src/api/client.ts`, remove `RawLog,` from the type import, and add after `request`:

```ts
// A download whose body is the file itself, not JSON.
async function requestBlob(path: string): Promise<Blob> {
  const res = await fetch(path, { method: "GET", headers: { "X-GHR": "1" }, credentials: "same-origin" })
  if (res.ok) return res.blob()
  const err = await toError(res)
  if (res.status === 401) onUnauthorized()
  throw err
}
```

Replace the `rawJobLog` line with:

```ts
  rawJobLog: (repo: string, jobId: number) => requestBlob(`/api/actions/${seg(repo)}/jobs/${jobId}/log/raw`),
```

In `web/src/api/types.ts`, delete:

```ts
export interface RawLog {
  text: string
}
```

and the blank line after it.

In `web/src/pages/run.tsx`, replace `saveText` (keeping its comment above it) with:

```tsx
function saveBlob(blob: Blob, fileName: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = fileName
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
```

and in `JobLogs` replace `onSuccess: (r) => saveText(r.text, rawLogFileName(repo, job.id)),` with:

```tsx
    onSuccess: (blob) => saveBlob(blob, rawLogFileName(repo, job.id)),
```

In `internal/model/model.go`, delete:

```go
// RawLog is GET /actions/{repo}/jobs/{jobId}/log/raw.
type RawLog struct {
	Text string `json:"text"`
}
```

and the blank line after it.

- [ ] **Step 4: Run the checks to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/pages/run.test.tsx src/api/client-pages.test.ts && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS, no type errors, no lint warnings.

Run, from the repository root: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/... ./web/ -count=1`
Expected: no `gofmt` output, `vet` clean, PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go web/src/api/client.ts web/src/api/types.ts web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): save the raw log from the response body"
```
