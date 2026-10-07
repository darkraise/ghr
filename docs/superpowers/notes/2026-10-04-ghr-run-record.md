# ghr plan execution record (2026-10-03 to 2026-10-04)

Verbatim copy of what the git-ignored SDD workspace held, saved so it outlives the workspace: the plan amendments A1-A40 (the plan file itself was never edited) and every ledger line containing `Ruling:`, each with what it costs if wrong.

## Plan amendments (amendments.md)

## A1 — Task 8
Reason: literal TABs inside a raw string can silently become spaces when the block is copied, and no test or end-to-end check would notice.
Cost if wrong: none; the escaped string is byte-identical to the intended one.
### Old
````text
		"--format", `{{.ID}}	{{.Label "com.docker.compose.project"}}	{{.Label "com.docker.compose.project.working_dir"}}`)
````
### New
````text
		"--format", "{{.ID}}\t{{.Label \"com.docker.compose.project\"}}\t{{.Label \"com.docker.compose.project.working_dir\"}}")
````

## A2 — Task 12
Reason: the comment claims tests shorten the interval, but no test references it.
Cost if wrong: none; behaviour is identical.
### Old
````text
// logPollInterval is how often `logs -f` polls; tests shorten it.
var logPollInterval = time.Second
````
### New
````text
// logPollInterval is how often `logs -f` polls.
const logPollInterval = time.Second
````

## A3 — Task 3
Reason: day-suffix multiplication overflows int64 silently, so out-of-range values parse to wrong (even positive) durations that pass validation.
Cost if wrong: only durations above ~292 years are refused; no realistic config is affected.
### Old
````text
	"fmt"
	"os"
````
### New
````text
	"fmt"
	"math"
	"os"
````

## A4 — Task 3
Reason: replace the overflowing day parse with a range-checked one.
Cost if wrong: only durations above ~292 years are refused.
### Old
````text
func ParseDuration(s string) (Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return Duration(time.Duration(n) * 24 * time.Hour), nil
	}
````
### New
````text
// maxDays is the largest day count whose nanoseconds fit in an int64.
const maxDays = int64(math.MaxInt64 / int64(24*time.Hour))

func ParseDuration(s string) (Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseInt(strings.TrimSuffix(s, "d"), 10, 64)
		if err != nil || n > maxDays || n < -maxDays {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return Duration(time.Duration(n) * 24 * time.Hour), nil
	}
````

## A5 — Task 3
Reason: boundary test so the overflow cannot return unnoticed.
Cost if wrong: none; the test only pins the documented range.
### Old
````text
	if *c.Repos[0].Max != 1 || c.Labels[0] != "homelab" {
		t.Fatal("clone shares memory")
	}
}
````
### New
````text
	if *c.Repos[0].Max != 1 || c.Labels[0] != "homelab" {
		t.Fatal("clone shares memory")
	}
}

func TestParseDurationDayBounds(t *testing.T) {
	if d, err := ParseDuration("106751d"); err != nil || d.D() != 106751*24*time.Hour {
		t.Fatalf("106751d = %v, %v", d, err)
	}
	for _, s := range []string{"106752d", "213504d", "-106752d", "xd"} {
		if _, err := ParseDuration(s); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}
````

## A6 — Task 3
Reason: lenient decoding silently drops misspelt keys and extra documents, and Save then erases them from disk.
Cost if wrong: hand-edited files with stray keys or a trailing `---` are refused instead of accepted.
### Old
````text
import (
	"errors"
	"fmt"
````
### New
````text
import (
	"bytes"
	"errors"
	"fmt"
	"io"
````

## A7 — Task 3
Reason: strict single-document decode in Parse.
Cost if wrong: as above.
### Old
````text
// Parse decodes YAML over the defaults and validates the result.
func Parse(data []byte) (*Config, []string, error) {
	c := defaults()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
````
### New
````text
// Parse decodes YAML over the defaults and validates the result. Unknown keys
// and extra documents are errors: Save would otherwise drop them silently.
func Parse(data []byte) (*Config, []string, error) {
	c := defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, nil, errors.New("parse config: expected exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
````

## A8 — Task 3
Reason: test that unknown keys and a second document are rejected.
Cost if wrong: none; it pins the strict-decoding contract.
### Old
````text
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid config was written")
	}
}
````
### New
````text
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid config was written")
	}
}

func TestParseRejectsUnknownKeysAndExtraDocuments(t *testing.T) {
	for name, y := range map[string]string{
		"unknown repo key": sample + "    maax: 1\n",
		"unknown top key":  "poll_intervall: 5s\n" + sample,
		"second document":  sample + "---\nowner: other\n",
	} {
		if _, _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
````

## A9 — Task 4
Reason: instances can carry a differently-cased repo name than the config after a case-only rename; coverage must ignore case.
Cost if wrong: none; identical behaviour when spellings match.
### Old
````text
		if i.Repo == repo && covers(i, now) {
````
### New
````text
		if strings.EqualFold(i.Repo, repo) && covers(i, now) {
````

## A10 — Task 4
Reason: the repo cap must count instances whose stored repo name differs from the config's only in case.
Cost if wrong: none; identical behaviour when spellings match.
### Old
````text
		if i.Repo == repo {
			n++ // starting, idle, busy and cleaning all count toward the repo cap
````
### New
````text
		if strings.EqualFold(i.Repo, repo) {
			n++ // starting, idle, busy and cleaning all count toward the repo cap
````

## A11 — Task 4
Reason: the warm count must include instances whose stored repo name differs only in case, or all mode keeps an extra warm runner forever.
Cost if wrong: none; identical behaviour when spellings match.
### Old
````text
		if i.Repo != repo || i.Stale {
````
### New
````text
		if !strings.EqualFold(i.Repo, repo) || i.Stale {
````

## A12 — Task 4
Reason: idle grouping must put case-variant spellings of one repo into one group so the warm count is applied once.
Cost if wrong: none; cfg.Repo is already case-insensitive, so the lowercased key resolves the same repo.
### Old
````text
		byRepo[i.Repo] = append(byRepo[i.Repo], i)
````
### New
````text
		// Repo names are case-insensitive; a case-only config rename leaves live
		// instances with the old spelling.
		key := strings.ToLower(i.Repo)
		byRepo[key] = append(byRepo[key], i)
````

## A13 — Task 4
Reason: regression test so the case-sensitive repo accounting cannot come back unnoticed.
Cost if wrong: none; it pins the case-insensitive contract.
### Old
````text
	if !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("got %v", got)
	}
}
````
### New
````text
	if !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("got %v", got)
	}
}

func TestRepoNamesIgnoreCase(t *testing.T) {
	busy := Instance{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-time.Hour), JobConfirmed: true}
	if got := repos(Plan(cfg("queue", 2, config.Repo{Name: "A"}), []Instance{busy}, Demand{"A": jobs("A", 1)}, t0)); len(got) != 0 {
		t.Fatalf("queue: repo cap must count a differently-cased instance, got %v", got)
	}
	idle := Instance{ID: "1", Repo: "a", State: Idle, StateSince: t0}
	if got := repos(Plan(cfg("all", 1, config.Repo{Name: "A"}), []Instance{idle}, Demand{}, t0)); len(got) != 0 {
		t.Fatalf("all: a differently-cased idle instance must satisfy warm, got %v", got)
	}
	al := cfg("all", 2, config.Repo{Name: "A", Warm: ptr(1)})
	got := IdleToStop(al, []Instance{
		{ID: "warm", Repo: "a", State: Idle, StateSince: t0.Add(-time.Hour)},
		{ID: "extra", Repo: "A", State: Idle, StateSince: t0.Add(-10 * time.Minute)},
	}, t0)
	if !reflect.DeepEqual(got, []string{"extra"}) {
		t.Fatalf("idle grouping must ignore case, got %v", got)
	}
}
````

## A14 — Task 5
Reason: an unparsable Retry-After becomes a zero-second suspension, so the daemon keeps polling into a rate limit.
Cost if wrong: an HTTP-date Retry-After waits 1 minute instead of until that date.
### Old
````text
			secs, _ := strconv.Atoi(ra)
````
### New
````text
			secs, err := strconv.Atoi(ra)
			if err != nil || secs < 0 {
				secs = 60
			}
````

## A15 — Task 5
Reason: pin the fallback for an unparsable Retry-After.
Cost if wrong: none; it only pins the fallback.
### Old
````text
		{"429 retry-after", 429, map[string]string{"Retry-After": "30"}, ErrRateLimit, time.Date(2026, 10, 3, 12, 0, 30, 0, time.UTC)},
````
### New
````text
		{"429 retry-after", 429, map[string]string{"Retry-After": "30"}, ErrRateLimit, time.Date(2026, 10, 3, 12, 0, 30, 0, time.UTC)},
		{"429 bad retry-after", 429, map[string]string{"Retry-After": "soon"}, ErrRateLimit, time.Date(2026, 10, 3, 12, 1, 0, 0, time.UTC)},
````

## A16 — Task 5
Reason: a suspended mutation must fail fast; today it sleeps up to 1 s under the lock and advances lastMutate without sending anything.
Cost if wrong: none outside a suspension; the error message is duplicated from do.
### Old
````text
	c.mutMu.Lock()
	defer c.mutMu.Unlock()
````
### New
````text
	c.mutMu.Lock()
	defer c.mutMu.Unlock()
	if until := c.SuspendedUntil(); !until.IsZero() {
		return nil, &APIError{Status: http.StatusTooManyRequests, Kind: ErrRateLimit, Message: "rate limited until " + until.Format(time.RFC3339), RetryAt: until}
	}
````

## A17 — Task 5
Reason: regression test so a suspended mutation can never wait for the spacing interval or send a request.
Cost if wrong: none; it only pins the fail-fast contract.
### Old
````text
	if len(f.requests) != 2 {
		t.Fatalf("requests = %v", f.requests)
	}
}
````
### New
````text
	if len(f.requests) != 2 {
		t.Fatalf("requests = %v", f.requests)
	}
}

func TestSuspendedMutationFailsWithoutWaiting(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	var slept []time.Duration
	c.Now = func() time.Time { return now }
	c.Sleep = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
	for i := 0; i < 4; i++ {
		if err := c.DeleteRunner(context.Background(), "r", 1); !IsKind(err, ErrRateLimit) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if len(slept) != 0 || len(f.requests) != 1 {
		t.Fatalf("slept %v, requests %v", slept, f.requests)
	}
}
````

## A18 — Task 6
Reason: a crash-torn tail with no newline makes the next record merge into it, so the retried finalize's history line is silently lost.
Cost if wrong: one Stat and one 1-byte read per Append.
### Old
````text
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
````
### New
````text
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// A crash mid-write can leave a fragment with no trailing newline; start a
	// new line so this record is not merged into the fragment and lost.
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
````

## A19 — Task 6
Reason: the torn-tail regression test needs os.WriteFile.
Cost if wrong: none.
### Old
````text
import (
	"path/filepath"
	"testing"
````
### New
````text
import (
	"os"
	"path/filepath"
	"testing"
````

## A20 — Task 6
Reason: regression test so a record appended after a torn tail stays readable.
Cost if wrong: none; it pins the recovery contract.
### Old
````text
	got, _ := s.Query("", "", 0)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (duplicate skipped), got %+v", got)
	}
}
````
### New
````text
	got, _ := s.Query("", "", 0)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (duplicate skipped), got %+v", got)
	}
}

func TestAppendAfterTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":"torn","repo":"a","run_id":1,"fin`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Store{Path: path}
	if err := s.Append(model.HistoryEntry{ID: "x", Repo: "a", RunID: 2, FinishedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query("", "", 0)
	if err != nil || len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("record after torn tail: %+v %v", got, err)
	}
}
````

## A21 — Task 6
Reason: a Close error after a successful Write is reported as success, so finalize could delete the pending record while the history line is missing.
Cost if wrong: none; identical when Close succeeds.
### Old
````text
	_, err = f.Write(append(line, '\n'))
	return err
}
````
### New
````text
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Close()
}
````

## A22 — Task 8
Reason: CommandTimeout does not bound Exec when a child process (such as a docker CLI plugin) inherits the output pipes; WaitDelay closes them after the kill.
Cost if wrong: a command whose child holds its output past a normal exit returns ErrWaitDelay after 5 s; none of the fixed commands do that.
### Old
````text
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
````
### New
````text
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Killing the command does not close pipes a child process inherited (a
	// docker CLI plugin, say); without WaitDelay, Output would wait on them forever.
	cmd.WaitDelay = 5 * time.Second
````

## A23 — Task 8
Reason: regression test so the inherited-pipe hang cannot return unnoticed; the single-process sleep test misses it.
Cost if wrong: an orphaned `sleep 30` lingers up to 30 s after the test.
### Old
````text
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Exec ignored CommandTimeout: %v", time.Since(start))
	}
}
````
### New
````text
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Exec ignored CommandTimeout: %v", time.Since(start))
	}
}

func TestExecTimesOutWhenAChildHoldsOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	defer func() { CommandTimeout = old }()
	start := time.Now()
	if _, err := Exec(context.Background(), "sh", "-c", "sleep 30 & sleep 30"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("Exec waited on an inherited pipe: %v", time.Since(start))
	}
}
````

## A24 — Task 8
Reason: Active needs errors.As to tell a normal non-zero systemctl exit from a failed query.
Cost if wrong: none; import only.
### Old
````text
	"bytes"
	"context"
	"fmt"
````
### New
````text
	"bytes"
	"context"
	"errors"
	"fmt"
````

## A25 — Task 8
Reason: a printed inactive state accompanied by a killed or failed query must be an error, per the Contracts, or a live runner is cleaned up.
Cost if wrong: a gone unit whose systemctl is signal-killed is retried next tick instead of cleaned now.
### Old
````text
	state := strings.TrimSpace(string(out))
	switch {
	case activeStates[state]:
		return true, nil
	case inactiveStates[state]:
		return false, nil
````
### New
````text
	state := strings.TrimSpace(string(out))
	// systemctl exits non-zero for inactive units; only a normal exit confirms
	// one, so a killed or failed query is never read as an exited runner.
	var exitErr *exec.ExitError
	confirmed := err == nil || (errors.As(err, &exitErr) && exitErr.Exited())
	switch {
	case activeStates[state]:
		return true, nil
	case inactiveStates[state] && confirmed:
		return false, nil
````

## A26 — Task 8
Reason: pin both sides: inactive plus a failed query is an error, and a real non-zero exit through Exec is a confirmed inactive.
Cost if wrong: none; the second half skips without sh.
### Old
````text
	if _, err := s.Active(ctx, "ghr-runner-gone00"); err == nil {
		t.Fatal("a cancelled context must be an error")
	}
}
````
### New
````text
	if _, err := s.Active(ctx, "ghr-runner-gone00"); err == nil {
		t.Fatal("a cancelled context must be an error")
	}
}

func TestSystemdActiveNeedsANormalExitToConfirmInactive(t *testing.T) {
	killed := Systemd{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("inactive\n"), errors.New("signal: killed")
	}}
	if _, err := killed.Active(context.Background(), "ghr-runner-kill00"); err == nil {
		t.Fatal("printed inactive with a failed query must be an error")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	exited := Systemd{Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		return Exec(ctx, "sh", "-c", "echo inactive; exit 3")
	}}
	if ok, err := exited.Active(context.Background(), "ghr-runner-gone00"); ok || err != nil {
		t.Fatalf("inactive with exit 3: %v %v", ok, err)
	}
}
````

## A27 — Task 9
Reason: The runner runs .sh hooks as `bash -e`, so a failing final `mv` exits non-zero and fails the job; also make jq fail on an empty job.json (pc-2).
Cost if wrong: Two `|| true` guards and a `-e` flag that never fire.
### Old
````text
jq -nc \
  --arg run_id "${GITHUB_RUN_ID:-}" --arg run_attempt "${GITHUB_RUN_ATTEMPT:-}" \
  --arg run_number "${GITHUB_RUN_NUMBER:-}" --arg workflow "${GITHUB_WORKFLOW:-}" \
  --arg job "${GITHUB_JOB:-}" --arg runner_name "${RUNNER_NAME:-}" \
  --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '$ARGS.named' > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null
exit 0
```

- [ ] **Step 4: Write `hooks/job-completed.sh`**

```bash
#!/usr/bin/env bash
# Runner post-job hook. Must always exit 0. Adds finished_at to job.json; when
# job.json is missing or unreadable it records finished_at alone.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
out="$(jq -c --arg f "$now" '. + {finished_at: $f}' "$dir/job.json" 2>/dev/null)" \
  || out="$(jq -nc --arg f "$now" '{finished_at: $f}' 2>/dev/null)" \
  || exit 0
printf '%s\n' "$out" > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null
````
### New
````text
# The runner runs .sh hooks as `bash -e`: a failing last command in an && list
# exits the shell, so the rename chain ends in `|| true`.
jq -nc \
  --arg run_id "${GITHUB_RUN_ID:-}" --arg run_attempt "${GITHUB_RUN_ATTEMPT:-}" \
  --arg run_number "${GITHUB_RUN_NUMBER:-}" --arg workflow "${GITHUB_WORKFLOW:-}" \
  --arg job "${GITHUB_JOB:-}" --arg runner_name "${RUNNER_NAME:-}" \
  --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '$ARGS.named' > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null || true
exit 0
```

- [ ] **Step 4: Write `hooks/job-completed.sh`**

```bash
#!/usr/bin/env bash
# Runner post-job hook. Must always exit 0. Adds finished_at to job.json; when
# job.json is missing or unreadable it records finished_at alone.
# The runner runs it as `bash -e`, so the rename chain ends in `|| true`.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
# jq -e exits 4 when it produces no output (an empty job.json), falling through.
out="$(jq -ce --arg f "$now" '. + {finished_at: $f}' "$dir/job.json" 2>/dev/null)" \
  || out="$(jq -nc --arg f "$now" '{finished_at: $f}' 2>/dev/null)" \
  || exit 0
printf '%s\n' "$out" > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null || true
````

## A28 — Task 9
Reason: Add regression tests for the empty job.json fallback and for exit 0 under `bash -e` when the rename fails.
Cost if wrong: Two extra tests that pass trivially.
### Old
````text
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}
````
### New
````text
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

func TestJobCompletedWithEmptyJobJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "job.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	rec := readRecord(t, dir)
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

// The runner invokes .sh hooks as `bash -e <file>`. A job.json directory holding a
// job.json.tmp directory makes the final mv fail; the hooks must still exit 0.
func TestHooksExitZeroUnderErrexitWhenRenameFails(t *testing.T) {
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR=") // applies runHook's bash/jq skip rules
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "job.json", "job.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"job-started.sh", "job-completed.sh"} {
		cmd := exec.Command("bash", "-e", script)
		cmd.Env = append(os.Environ(), "GHR_INSTANCE_DIR="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s under bash -e exited non-zero: %v\n%s", script, err, out)
		}
	}
}
````

## A29 — Task 11
Reason: `return v, c.call(..., &v)` depends on an evaluation order the Go spec leaves unspecified, so the decoded value could be returned as zero.
Cost if wrong: none; with the current compiler the behaviour is identical.
### Old
````text
func (c *Client) Status(ctx context.Context) (model.Status, error) {
	var s model.Status
	return s, c.call(ctx, http.MethodGet, "/status", nil, &s)
}

func (c *Client) Events(ctx context.Context, after int64) ([]model.Event, error) {
	var e []model.Event
	return e, c.call(ctx, http.MethodGet, fmt.Sprintf("/events?after=%d", after), nil, &e)
}

func (c *Client) History(ctx context.Context, repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("conclusion", conclusion)
	q.Set("limit", fmt.Sprint(limit))
	var h []model.HistoryEntry
	return h, c.call(ctx, http.MethodGet, "/history?"+q.Encode(), nil, &h)
}

// Log returns the runner's log after cursor ("" = from the start); pass the
// returned chunk's Next to continue.
func (c *Client) Log(ctx context.Context, id, cursor string) (model.LogChunk, error) {
	var l model.LogChunk
	return l, c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/log?cursor="+url.QueryEscape(cursor), nil, &l)
}

func (c *Client) Containers(ctx context.Context, id string) ([]model.Container, error) {
	var cs []model.Container
	return cs, c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/containers", nil, &cs)
}

func (c *Client) Steps(ctx context.Context, id string) ([]model.Step, error) {
	var s []model.Step
	return s, c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/steps", nil, &s)
}
````
### New
````text
// The getters below call before returning: Go leaves the order of
// `return v, f(&v)` unspecified, so v could be read before f fills it.
func (c *Client) Status(ctx context.Context) (model.Status, error) {
	var s model.Status
	err := c.call(ctx, http.MethodGet, "/status", nil, &s)
	return s, err
}

func (c *Client) Events(ctx context.Context, after int64) ([]model.Event, error) {
	var e []model.Event
	err := c.call(ctx, http.MethodGet, fmt.Sprintf("/events?after=%d", after), nil, &e)
	return e, err
}

func (c *Client) History(ctx context.Context, repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("conclusion", conclusion)
	q.Set("limit", fmt.Sprint(limit))
	var h []model.HistoryEntry
	err := c.call(ctx, http.MethodGet, "/history?"+q.Encode(), nil, &h)
	return h, err
}

// Log returns the runner's log after cursor ("" = from the start); pass the
// returned chunk's Next to continue.
func (c *Client) Log(ctx context.Context, id, cursor string) (model.LogChunk, error) {
	var l model.LogChunk
	err := c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/log?cursor="+url.QueryEscape(cursor), nil, &l)
	return l, err
}

func (c *Client) Containers(ctx context.Context, id string) ([]model.Container, error) {
	var cs []model.Container
	err := c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/containers", nil, &cs)
	return cs, err
}

func (c *Client) Steps(ctx context.Context, id string) ([]model.Step, error) {
	var s []model.Step
	err := c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/steps", nil, &s)
	return s, err
}
````

## A30 — Task 12
Reason: logs discards write errors, so a failed output stream exits 0 (Contracts: runtime error = exit 1).
Cost if wrong: a transient writer error ends `logs -f` early; the operator reruns it.
### Old
````text
		io.WriteString(out, chunk.Data)
````
### New
````text
		if _, err := io.WriteString(out, chunk.Data); err != nil {
			return err
		}
````

## A31 — Task 12
Reason: the write-error regression test needs context and errors.
Cost if wrong: none; imports only.
### Old
````text
	"bytes"
	"encoding/json"
	"io"
````
### New
````text
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
````

## A32 — Task 12
Reason: pin that a failed write ends logs (including -f) with an error instead of looping.
Cost if wrong: none; without the fix the test hangs until the 180 s timeout and fails.
### Old
````text
		t.Fatalf("history %d:\n%s", code, out)
	}
}
````
### New
````text
		t.Fatalf("history %d:\n%s", code, out)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestLogsReportsWriteError(t *testing.T) {
	fakeDaemon(t)
	if err := logs(context.Background(), newClient(), []string{"a3f9c1", "-f"}, failWriter{}); err == nil {
		t.Fatal("a failed write must be an error")
	}
}
````

## A33 — Task 12
Reason: drain/resume-all accept extra arguments and silently act on every repo instead of returning a usage error.
Cost if wrong: stray-argument callers get exit 2; none exist.
### Old
````text
	case "drain":
		return c.PauseAll(ctx)
	case "resume-all":
		return c.ResumeAll(ctx)
````
### New
````text
	case "drain", "resume-all":
		if err := need(args, 1, args[0]); err != nil {
			return err
		}
		if args[0] == "drain" {
			return c.PauseAll(ctx)
		}
		return c.ResumeAll(ctx)
````

## A34 — Task 12
Reason: pin that drain/resume-all with arguments are usage errors that never reach the daemon.
Cost if wrong: none; it pins the Contracts' exit-2 rule.
### Old
````text
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_new" {
		t.Fatalf("request %+v", r)
	}
}
````
### New
````text
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_new" {
		t.Fatalf("request %+v", r)
	}
}

func TestDrainAndResumeAllTakeNoArguments(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{{"drain", "darkcloud"}, {"resume-all", "darkcloud"}} {
		if code, _, _ := runCLI(t, "", args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("rejected commands reached the daemon: %v", *reqs)
	}
}
````

## A35 — Task 13
Reason: the case-insensitive repo accounting in Status and recordLastJob needs strings.
Cost if wrong: none; import only.
### Old
````text
	"sort"
	"strconv"
	"sync"
````
### New
````text
	"sort"
	"strconv"
	"strings"
	"sync"
````

## A36 — Task 13
Reason: history entries keep the repo's spelling at finish time; key lastJob case-insensitively so a case-only config rename does not hide it.
Cost if wrong: none; identical when spellings match.
### Old
````text
// recordLastJob keeps the newest finished job per repo; m.mu must be held or unshared.
func (m *Manager) recordLastJob(e model.HistoryEntry) {
	if cur, ok := m.lastJob[e.Repo]; !ok || e.FinishedAt.After(cur.FinishedAt) {
		m.lastJob[e.Repo] = e
	}
}
````
### New
````text
// recordLastJob keeps the newest finished job per repo, keyed by the lower-cased
// name because repo names are case-insensitive; m.mu must be held or unshared.
func (m *Manager) recordLastJob(e model.HistoryEntry) {
	key := strings.ToLower(e.Repo)
	if cur, ok := m.lastJob[key]; !ok || e.FinishedAt.After(cur.FinishedAt) {
		m.lastJob[key] = e
	}
}
````

## A37 — Task 13
Reason: live instances keep the repo spelling from spawn; Active and LastJob must match the config repo ignoring case.
Cost if wrong: none; identical when spellings match.
### Old
````text
		for _, i := range m.insts {
			if i.Repo == r.Name {
				rs.Active++
			}
		}
		if e, ok := m.lastJob[r.Name]; ok {
````
### New
````text
		for _, i := range m.insts {
			if strings.EqualFold(i.Repo, r.Name) {
				rs.Active++
			}
		}
		if e, ok := m.lastJob[strings.ToLower(r.Name)]; ok {
````

## A38 — Task 13
Reason: regression test so case-sensitive repo matching in Status cannot return unnoticed.
Cost if wrong: none; it pins the case-insensitive contract.
### Old
````text
	if got := h.m.Status().Repos[1].LastJob; got == nil || got.ID != "new" {
		t.Fatalf("last job %+v", got)
	}
}
````
### New
````text
	if got := h.m.Status().Repos[1].LastJob; got == nil || got.ID != "new" {
		t.Fatalf("last job %+v", got)
	}
}

func TestStatusIgnoresRepoCase(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.insts["dddddd"] = &instance{Meta: Meta{ID: "dddddd", Repo: "DarkMem"}, State: sched.Idle, StateSince: h.now}
	h.m.recordLastJob(model.HistoryEntry{ID: "renamed", Repo: "DARKMEM", FinishedAt: h.now})
	h.m.mu.Unlock()
	st := h.m.Status()
	if st.Repos[1].Active != 1 || st.Repos[1].LastJob == nil || st.Repos[1].LastJob.ID != "renamed" {
		t.Fatalf("repos %+v", st.Repos)
	}
}
````

## A39 — Task 20
Reason: The deferred receive blocks forever when a failure path skips the sentinel resend, which hides the real failure behind the go test timeout.
Cost if wrong: Only failing runs change, and they gain at most a 10 s wait before cleanup.
### Old
````text
	defer func() {
		cancel()
		<-done
	}()
````
### New
````text
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
````

## A40 — Task 24
Reason: Native Linux symlink replacement and dist cleanup are otherwise never exercised, because homelab has no CI and the Windows run emulates symlinks.
Cost if wrong: One extra offline test run of a few seconds on the LXC.
### Old
````text
cd github-runner && GHR_TOKEN=<pat> bash setup.sh && bash setup.sh && ghr status
```

Expected: both runs exit 0 (the second changes nothing), and `ghr status` lists the four repos with no error.
````
### New
````text
cd github-runner && bash tests/setup_test.sh && GHR_TOKEN=<pat> bash setup.sh && bash setup.sh && ghr status
```

Expected: the setup test prints "all assertions passed" without the "no symlink support" note (native symlinks); both setup runs exit 0 (the second changes nothing), and `ghr status` lists the four repos with no error.
````

## Rulings I made (ledger lines containing `Ruling:`)

- Ruling: Tasks 1-21 commit directly to master of D:/Repositories/Personal/ghr (no worktree) — the repo has no commits so a worktree cannot exist, and the plan's Global Constraints name master there; Tasks 22-23 commit to homelab master like prior ghr docs commits — if wrong, the commits are local and unpushed, so a branch can be cut afterwards
- Ruling: pre-flight — 4 findings, see preflight.md (2 amendments, 1 confirmed gap carried to Task 16, 1 parked) — a missed cross-task conflict surfaces in a task review instead
- Ruling: amendment A1 — Task 8 compose --format TABs become \t escapes; literal TABs can silently turn into spaces on copy — none, bytes identical
- Ruling: amendment A2 — Task 12 logPollInterval becomes a const; its comment was false — none, behaviour identical
- Task 16: Ruling: preflight-71 — add jobInfo helper in lifecycle.go and call it from readJobFiles and Adopt (details in preflight.md); carry into Task 16 dispatch — a small extra edit, no behaviour change
- Task 21: parked — stripTimes mis-masks in half-hour time zones — Ruling: goldens generated here and CI runs UTC; loud golden failure only on a developer machine in such a zone
- Ruling: Task 1 base is the empty tree 4b825dc — ghr has no commits, so there is no HEAD — review-package may need the empty-tree sha as base
- Task 1: parked — Codex cannot-verify: branch/remote URL not checkable from diff — Ruling: git shows master and origin https://github.com/darkraise/ghr.git as the brief requires; nothing pushed before Task 24
- Task 1: parked — Codex cannot-verify: file-creation tool and working-tree line endings — Ruling: git ls-files --eol shows i/lf w/lf for all six files; the Write-tool rule only guards heredoc backslash collapse and Task 1 has no backslashes
- Task 2: parked — Codex cannot-verify: vet/build/format results not in diff — Ruling: controller re-ran gofmt/vet/build/test on 7b03acb, all clean, brief has no test step
- Task 3: Ruling: pc-1 (Duration day overflow, Codex Important, plan-mandated) — AMEND A3-A5: ParseDuration range-checks day counts (maxDays), adds TestParseDurationDayBounds — only durations above ~292 years are refused
- Task 3: Ruling: pc-2 (lenient YAML decode, Codex Important, plan-mandated) — AMEND A6-A8: Parse uses KnownFields(true) and rejects a second document, adds TestParseRejectsUnknownKeysAndExtraDocuments — hand-edited files with stray keys are refused
- Task 3: parked — Codex cannot-verify: Go toolchain and LF normalisation — Ruling: go1.26.1 and all-LF verified by the controller and seat
- Task 4: Ruling: pc-1 (case-sensitive repo accounting, Codex Important, plan-mandated) — AMEND A9-A13: sched compares repo names with strings.EqualFold in Covering, repoActive and countStates, groups IdleToStop by lowercased name, adds TestRepoNamesIgnoreCase; fix round must show the new test failing before the fix — none, identical behaviour when spellings match
- Task 4: parked — pc-2 skipped test-first red phase — Ruling: code matches the plan, which was replayed test-first; a retroactive RED proves nothing, pc-1's fix round enforces red-then-green
- Task 4: parked — Codex cannot-verify: file-creation method, LF, toolchain, dependency pins — Ruling: go1.26.1, all-LF and yaml.v3 pin verified; same as Tasks 1 and 3
- Task 5: Ruling: pc-1 (suspended mutation sleeps and advances lastMutate, Codex Important) — AMEND: mutate checks SuspendedUntil right after taking mutMu and returns ErrRateLimit without sleeping; adds TestSuspendedMutationFailsWithoutWaiting — error message duplicated from do()
- Task 5: Ruling: pc-5 (unparseable Retry-After gives zero-second suspension, Codex Important) — AMEND: bad or negative Retry-After falls back to 60 s; adds a TestClassify row — an HTTP-date Retry-After waits 1 minute instead of until that date
- Task 5: parked — pc-2 ForgetCache racing an in-flight old-token request — Ruling: narrow self-healing race; token changes follow 401s; a generation counter is beyond the Contracts; worst case the new token waits out one suspension (<= ~1 h)
- Task 5: parked — pc-3 secondary-limit 403 without Retry-After treated as auth — Ruling: spec says other 403 -> degraded and re-check every 60 s, which already meets GitHub's one-minute wait; cost is a misleading "degraded" banner
- Task 5: parked — pc-4 Retry-After ignored when remaining quota is 0 — Ruling: Contracts and spec allow either header; cost is one extra rejected request
- Task 5: parked — pc-6 mutation lock and spacing sleep ignore context cancellation — Ruling: Contracts fix Sleep as func(time.Duration); shutdown tolerates it via the 30 s ShutdownWait; cost is slightly longer shutdown
- Task 5: parked — pc-7 error-body read failure bypasses rate-limit classification — Ruling: handled as a plain error by apiErr's per-repo backoff; next readable 429 suspends correctly
- Task 5: parked — Codex cannot-verify x2: module/toolchain/race config, and line endings/file-writing method — Ruling: go1.26.1, go 1.26, CI runs -race on ubuntu, all-LF; the brief has no heredoc-sensitive backslashes
- Task 6: Ruling: pc-1 (torn tail merges next record, Codex Important, plan-mandated) — AMEND: Append opens O_RDWR, stats the file and starts a new line when the last byte is not a newline; adds TestAppendAfterTornTail — one Stat and one 1-byte ReadAt per Append
- Task 6: Ruling: pc-3 (Close error ignored, Codex Important) — AMEND: Append returns f.Close() after a successful Write — none, identical when Close succeeds
- Task 6: parked — pc-2 1 MiB read limit vs unbounded Append — Ruling: the daemon is the only writer and records are a few hundred bytes of GitHub-bounded fields; if hit, check len(line) in Append
- Task 6: parked — Codex cannot-verify x2: HistoryEntry tags/toolchain/pins, and deployment wiring of history.jsonl — Ruling: HistoryEntry matches the Contracts, go1.26.1; wiring belongs to Tasks 20 and 22
- Task 7: parked — Codex cannot-verify: model.Event definition and tags outside the diff — Ruling: committed struct matches the Contracts entry field for field (Task 2, 7b03acb)
- Task 8: Ruling: pc-1 (CommandTimeout does not bound Exec when a child inherits the output pipes, Codex Important, plan-mandated) — AMEND: Exec sets cmd.WaitDelay = 5s; adds TestExecTimesOutWhenAChildHoldsOutput — a command whose child outlives a normal exit returns ErrWaitDelay after 5 s, which none of the fixed commands do
- Task 8: Ruling: pc-3 (Active reads a printed inactive as gone even when the query failed, Codex Important, plan-mandated) — AMEND: Active confirms inactive/failed/unknown only when err is nil or a normally exited *exec.ExitError; adds TestSystemdActiveNeedsANormalExitToConfirmInactive — a gone unit whose systemctl is signal-killed is retried next tick
- Task 8: parked — pc-2 stderr appended verbatim to Exec errors — Ruling: the only secret is the JIT config, passed as the run.sh --jitconfig argument and not echoed by systemd-run; dropping stderr would remove the only diagnostics; fix if a systemd-run version echoes the command is to redact u.Command[1:] in Systemd.Start
- Task 8: parked — Codex cannot-verify x2: module/toolchain/pins/LF, and Write-tool file creation — Ruling: go1.26.1 and all-LF verified; the committed system.go keeps the \t and \" escapes at lines 155, 161, 170 and 176
- Task 9: Ruling: pc-1 (failed mv breaks the always-exit-0 contract under bash -e, Codex Important, plan-mandated) — AMEND: both hooks end the write/rename chain with `|| true`; GitHub docs confirm .sh hooks run as `bash -e`; adds TestHooksExitZeroUnderErrexitWhenRenameFails — an extra guard on a rename that was going to fail anyway
- Task 9: Ruling: pc-2 (empty job.json becomes a bare newline, Codex Important, plan-mandated) — AMEND: job-completed.sh uses `jq -ce` so empty input falls through to the finished_at-only record; adds TestJobCompletedWithEmptyJobJSON — none
- Task 9: parked — Codex cannot-verify: hook mode 0755 and packaging — Ruling: Task 9's brief has no mode requirement; Task 10's release step chmods 0755 dist/ghr/* and Task 22 installs from the tarball
- Task 9: parked — Codex cannot-verify: Write-tool creation, toolchain, pins, LF — Ruling: scripts match the plan, LF verified, nothing corrupt visible
- Task 10: parked — Codex cannot-verify: reported build/format/YAML/test results — Ruling: controller re-ran build, gofmt and full tests on 5765ef6, all pass; release.yml matches the brief; the workflow run itself is Task 24's
- Task 10: parked — Codex cannot-verify: version variable and hook contents outside the diff — Ruling: var version = "dev" confirmed at cmd/ghr/main.go:11; hook contents belong to Task 9; the workflow chmod 0755 covers the packaged copies
- Task 11: Ruling: pc-1 (six client getters return a variable and fill it in the same return statement, Codex Important, plan-mandated) — AMEND: each getter assigns the call's error first, then returns the value; a behaviour-preserving refactor, so there is no failing-test step — none, identical with the current compiler
- Task 11: parked — pc-2 trailing JSON content accepted by decode — Ruling: the CLI and TUI send marshalled bodies, the socket is 0600, nothing is persisted that the caller did not send; cost is a hand-written request with trailing junk reported as success
- Task 11: parked — pc-3 oversized token body truncated at 64 KiB — Ruling: needs input about 700x a real token; Task 19 CheckToken rejects a bad token before storing when repos exist
- Task 11: parked — pc-4 writeJSON ignores marshal errors — Ruling: no backend value can fail to marshal (*config.Config, model.Status); cost is an empty 200 for a future unmarshalable value
- Task 11: parked — pc-5 unmatched routes and methods return plain-text 404/405 — Ruling: the Contracts' envelope covers backend errors; call() falls back to the status line and still returns *Error with the right Status
- Task 11: parked — pc-6 PUT /token labelled application/json — Ruling: the server ignores Content-Type and the only client is this package; cost is nil
- Task 11: parked — Codex cannot-verify x3: nested [] guarantee, model tags/toolchain/pins, file-creation tool — Ruling: the API's own lists normalise to []; nested nil slices belong to Tasks 13 and 19; env verified; Write tool used
- Task 12: Ruling: pc-1 (logs discards output write errors, Codex Important, plan-mandated) — AMEND: logs returns the io.WriteString error (one-shot and -f stop, exit 1); adds TestLogsReportsWriteError; printStatus write errors stay discarded (human-only table, cosmetic) — a transient writer error ends logs -f early
- Task 12: Ruling: pc-2 (drain and resume-all accept extra arguments and act on every repo, Codex Important, plan-mandated) — AMEND: both use need(args, 1, ...) so extra arguments are a usage error (exit 2); adds TestDrainAndResumeAllTakeNoArguments — stray-argument callers get exit 2, none exist
- Task 12: parked — Codex cannot-verify: api.DefaultSocket and server-side public-repo refusal — Ruling: DefaultSocket is /run/ghr/ghr.sock per the Contracts; the refusal belongs to Task 19's AddRepo
- Task 12: parked — Codex cannot-verify: usage string cut off in the diff — Ruling: usage lists every Contracts subcommand; help is handled by run
- Task 13: Ruling: pc-1 (Status matches repo names case-sensitively, hiding Active and LastJob after a case-only rename, Codex Important, plan-mandated; also the seat's earlier adjacent finding from Task 4) — AMEND A35-A38: Status counts Active with strings.EqualFold, lastJob is keyed by the lower-cased repo name in recordLastJob and Status, adds TestStatusIgnoresRepoCase; repoErr, fails, retryAt and demand stay keyed by the config spelling — none, identical when spellings match
- Task 13: parked — pc-2 stale compares recorded labels case-sensitively — Ruling: both sides come from config.CustomLabels (lower-cased) and Task 15 spawn records CustomLabels, so no path produces a case-only mismatch; cost if ghr.json is hand-edited is one runner restart
- Task 13: parked — Codex cannot-verify: exported runner API incl. RandomID — Ruling: Task 13's brief covers Init and Wait; RandomID is Task 15's (plan line 6573), Adopt/Reconcile Task 16, Tick Task 17, RunnerLog/RunnerContainers Task 14
- Task 13: parked — Codex cannot-verify: toolchain, pins, line endings — Ruling: go1.26.1, go 1.26, yaml pin with tidy at Task 21, all-LF
- Task 14: parked — Codex Important: required test-first order not followed (report says implementation was moved aside for the RED run) — Ruling: code is the plan's code, which was replayed test-first; a retroactive RED proves nothing (same as Task 4 pc-2); implementer's RED was a build failure (cleanupDocker undefined), GREEN all 11 new tests
- Task 14: Ruling: pc-1 (cleanup retry loses custom compose projects once their containers are gone, Codex Important, plan-mandated) — CONFIRMED-GAP: cleanupDocker records the discovered project names in <instance dir>/ghr-projects and projects() reads them back (tolerates a missing instance dir); adds TestCleanupRetryRemembersCustomProjects — a network that can never be removed keeps the instance cleaning with visible warnings instead of leaking silently
- Task 14: Ruling: pc-2 (archiving between Stat and Glob drops cursor offsets and replays the log, Codex Important, plan-mandated) — CONFIRMED-GAP: RunnerLog seeds next with the incoming offsets before the per-file Set; adds TestRunnerLogKeepsOffsetsOfUnlistedFiles — stale file names stay in the cursor, bounded by the runner's own logs
- Task 14: Ruling: pc-3 (archive pruning swallows ReadDir and RemoveAll errors, Codex Important, plan-mandated) — CONFIRMED-GAP: prune adds a warn event for ReadDir (not-exist stays silent) and RemoveAll failures, lastPrune still advances; adds TestPruneReportsLogArchiveFailure — at worst a few extra warn events
- Task 14: Ruling: pc-5 (256 KiB limit splits a UTF-8 character and the cursor skips its bytes, Codex Important, plan-mandated) — CONFIRMED-GAP: readFrom trims a trailing incomplete rune (trimPartialRune) so the next call re-reads it whole; adds TestRunnerLogKeepsCharactersWhole — a file ending in a truncated character holds back at most 3 bytes
- Task 14: parked — pc-4 RunnerLog discards readFrom errors and maps Stat errors to ErrUnknownRunner — Ruling: a vanished _diag (renamed to the archive) must stay silent, read errors on local regular files are hypothetical, Stat errors are not-exist in practice; a starting runner with no _diag returns 404 for a few seconds (deferred minor above)
- Task 14: parked — pc-6 middle disk reading never stored when the final read fails — Ruling: diskPct feeds only the status display and is overwritten by the next checkDisk within 10 ticks; fixing needs a code and test-assertion change for a few points of lag
- Task 14: parked — Codex cannot-verify x3: toolchain/Docker adapter, Task 15 archive ownership, lastPrune serialisation — Ruling: go1.26.1, all-LF, adapter reviewed in Task 8; archive and finish are Task 15's; lastPrune is touched only by Init, prune and Task 17 Tick, all on the single loop goroutine (plan lines 5342, 6045, 7754-7755, 9143, 9185-9186)
- Task 15: parked — pc-1 failed Start deletes the instance dir without confirming the unit is inactive — Ruling: spec error table says "unit fails to start: event logged; dir removed; registration deleted"; a unit systemd accepted just before a cancellation has no instance, so Task 16 Reconcile stops it and Adopt stops units without an instance dir; cost is one orphaned unit for up to ~5 minutes
- Task 15: Ruling: pc-2 (finalize deletes pending history on any read error, Codex and judge Important, plan-mandated) — CONFIRMED-GAP: finalize removes the pending file only when it is unparseable (json SyntaxError or UnmarshalTypeError), returns silently on not-exist, and keeps the file for retry on any other error; adds TestFinalizeKeepsUnreadablePending — an indefinitely unreadable file is retried silently every tick
- Task 15: parked — pc-3 spawn rollback ignores RemoveAll errors and reuses the caller's context — Ruling: Task 16 Reconcile deletes unmatched ghr-<repo>-<id> registrations and removes unmatched inactive instance dirs, the caller's context is cancelled only at shutdown and the next startup's Adopt and Reconcile cover it; cost is an orphan for up to ~5 minutes
- Task 15: Ruling: pc-4 (finish removes the instance from the map before finalize, so finalizePending can finalize the same file concurrently, Codex Important, plan-mandated) — CONFIRMED-GAP: finish deletes the instance from the map only after finalize returns (the no-job path still deletes first); no new test, the race needs a concurrency harness — a finished runner stays cleaning for one extra ListJobs call while holding its repo slot
- Task 15: Ruling: pc-5 (hook metadata ignored when Job already exists, Codex Important, plan-mandated) — CONFIRMED-GAP: readJobFiles fills an empty RunNumber and Workflow on an existing job by replacing the Job pointer with a copy; adds TestHookRecordFillsRunNumberOfConfirmedJob — none, only empty fields are filled
- Task 15: Ruling: pc-6 (pendingFor defaults StartedAt to StateSince which beginFinish resets, giving negative durations for a completion-only hook record, Codex Important, plan-mandated) — CONFIRMED-GAP: pendingFor defaults StartedAt to SpawnedAt; adds TestCompletionOnlyRecordHasNoNegativeDuration — duration overstated by the runner's idle time in that rare case
- Task 15: parked — pc-7 cleanup count lost across retries — Ruling: Cleanup feeds only the pending record and the completion event text; no resource is left behind; cost is an understated container count
- Task 15: parked — pc-8 systemd, runner-query and pending-scan errors swallowed — Ruling: treating an Active error as not-exited is the safe direction per the spec; Adopt returns the activeUnits error and Reconcile emits "reconcile: list units" every 30 ticks; a per-unit warning each tick would spam and needs state the Contracts do not define; cost is a silent slot hold if is-active alone keeps failing for one unit
- Task 15: parked — Codex cannot-verify x3: toolchain and pins, Task 16-17 integration, Tick gating — Ruling: go1.26.1, all-LF; Reconcile retries registrations and stops untracked units (plan 7363-7401), Tick gates refreshRunners and finalizePending on apiAllowed (7732-7750), and Task 17 confirm replaces the Job pointer so the shared pointer is safe
- Task 16: parked — Codex finding that the jobInfo extraction departs from the verbatim brief — Ruling: preflight-71 (preflight.md) is the ruling-seat verdict requiring exactly this helper, its two call sites and the lifecycle.go commit; judge-opus rejected the finding after reading it
- Task 16: Ruling: pc-1 (Adopt and Reconcile discard ListRunners, RemoveAll and ReadDir errors, judge Important and Codex Important, plan-mandated) — CONFIRMED-GAP: Reconcile adds a warn event when ListRunners fails (no apiErr, Task 17's gatherDemand already applies the error policy), Adopt reports "stopped and removed" only after RemoveAll succeeds and otherwise warns, Reconcile warns on a ReadDir error other than not-exist and on a failed orphan-dir RemoveAll; the SD.Active error stays silent (Task 15 pc-8); adds TestReconcileReportsListRunnersFailure — a repo whose runner listing keeps failing produces one extra warn event every 30 ticks
- Task 16: parked — cannot-verify x3: Reconcile overlapping spawn, lifecycle.go import cleanup, cleanup ordering and host behaviour — Ruling: spawn runs synchronously inside Tick before Reconcile on the loop goroutine and Run calls Init, Adopt, Reconcile before the loop (plan 7742-7747, 7888, 9143-9149, 9185-9186), background finish goroutines stay in insts until their dir is gone; strconv and model are still used in lifecycle.go and the build passes; ordering is Task 15's, daemon wiring Task 20's, host behaviour Task 24 Step 7
- Task 17: Ruling: pc-1 (spawn failures other than auth or rate limit keep trying the repo's remaining planned spawns every tick, Codex and judge Important, plan-mandated) — CONFIRMED-GAP: spawnPlanned records a failed repo (lower-cased name) for the rest of the tick and skips its later planned spawns; no apiErr and no backoff because the spec's error table says "JIT config failure: event logged; retried next tick"; adds TestSpawnFailureStopsRepoForTick — a repo whose JIT call keeps failing logs one error event per tick, which can fill the 1000-entry event ring in about 3 hours at a 10 s poll interval unless the spec gains a backoff row
- Task 17: Ruling: pc-2 (confirm overwrites a known job start time with the poll time when the API job has no started_at, Codex Important, judge Minor, plan-mandated) — CONFIRMED-GAP: confirm keeps the existing Job.StartedAt before falling back to now and uses the API value when present; adds a test asserting the start time is unchanged — none, identical when the API supplies started_at
- Task 17: parked — cannot-verify x2: Task 15 phases and apiErr, internal behaviour of Tasks 14-16 — Ruling: refreshRunners, stopIdle and finalize already call apiErr on auth and rate-limit errors (lifecycle.go:190-195, 480-487, 401-405) and later Tick phases re-check apiAllowed; the phases are covered by their own tasks
- Task 18: parked — pc-1 Update does not enforce the owner lock that Reload enforces — Ruling: the only production callers of Update (Task 19 PatchConfig, RemoveRepo and FinalizeRemovals) apply ConfigPatch and repo edits that carry no owner field (model.go:101-114); if a patch ever gains an owner field, copy Reload's comparison into Update before config.Save
- Task 18: parked — pc-2 SetToken accepts a blank token — Ruling: Task 11's PUT /token handler trims the body and returns 400 "empty token" before Backend.SetToken (plan 3801-3805) and the Task 12 CLI trims its input (4312); Task 19's SetToken runs CheckToken only when repos exist, so the blank check lives in Task 11 only; a future in-process caller would need the same check at the top of Store.SetToken
- Task 18: parked — cannot-verify x3: config Clone and Save, toolchain and pins, blank-token rejection upstream — Ruling: Clone is a YAML round-trip giving fresh memory for Repos and the Max and Warm pointers and Task 3's test asserts no shared memory (config.go:197-207); go1.26.1, all-LF; the blank-token check is in Task 11's handler
- Task 19: parked — pc-1 FinalizeRemovals changes config without calling Wake and drops validation warnings — Ruling: its only production caller is the loop's own tick closure in Task 20 (plan 9185-9189), a Wake there only queues an empty tick, the removed repo is already paused with no live instances, and the one validator warning cannot arise from deleting a different repo; cost is a caller outside the loop seeing the removal only at the next poll_interval tick
- Task 19: parked — pc-2 PatchConfig and AddRepo keep request-owned pointers and slices in the published snapshot — Ruling: handlers decode each body into a fresh value and drop it after the call, snapshots are read-only; a future in-process caller that reuses a request would need slices.Clone and *rp.Max copies
- Task 19: parked — pc-3 save failures return 400 — Ruling: the spec has no status rule for a failed config write, Store.Update (Task 18, complete) returns one error for validation and save failures, nothing branches on 400 versus 500 and the message names the cause
- Task 19: parked — pc-4 case-variant patch keys apply in map order — Ruling: the CLI and TUI send one key per patch; only a hand-built request over the 0600 socket can trigger it, and the result still passes validation
- Task 19: parked — pc-5 concurrent duplicate adds return 400 not 409 — Ruling: config stays consistent, the message says names must be unique and no client branches on 409; a one-line c.Repo(name) != nil Conflict check at the top of the AddRepo callback fixes it if it ever matters
- Task 19: parked — pc-6 TestResumeCancelsRemoval ignores setup errors — Ruling: TestRemoveRepoWaitsForRunners, TestRemovalSurvivesRestart and TestRejectedPatchKeepsRemoval already fail if RemoveRepo setup breaks, and deleting the cancellation code makes TestResumeCancelsRemoval hit its t.Fatal via the removing-implies-paused rule
- Task 19: parked — cv-1 GET /config can emit null for empty repos or labels (implementer concern, judge and Codex) — Ruling: only the events, history, containers and steps lists are normalised to [] in server.go and /config passes through writeJSON, but every consumer is a Go decoder (TUI decodes into config.Config, plan line 10307) and null decodes to a nil slice treated like an empty one; this also closes the Task 19 nested-nil-slice minor for Config; a non-Go client would need Backend.Config to return a clone with empty slices
- Task 19: parked — cv-2 and cv-3 Task 20 wiring and blank token — Ruling: Run builds Backend with CheckToken and Wake set (plan 9152-9166) and calls FinalizeRemovals only from the loop's tick closure after Tick; Task 11's PUT /token rejects a blank body before Backend.SetToken (server.go:114-125)
- Task 20: parked — pc-1 the probe, remove and listen sequence is not an exclusive lock — Ruling: the spec never asks for an exclusive lock and the Contracts choose "the bound socket is the single-daemon lock"; the race needs two starts within milliseconds (an operator running ghr daemon by hand while ghr.service starts); a real lock needs flock behind a linux build tag plus a Windows stub, a lock-file path in Options and Task 22 owning it, far bigger than the risk; cost is two managers adopting the same units until ghr.service is restarted
- Task 20: parked — pc-2 an unexpected Serve failure is only logged — Ruling: the spec says nothing on a control-socket failure, keeping runners alive is the safer default, Serve retries temporary Accept errors; if it ever happens the CLI and TUI get "no such file" while runners keep working and the operator runs systemctl restart ghr
- Task 20: parked — pc-3 Shutdown errors are discarded and Run returns nil after a timed-out Shutdown — Ruling: Shutdown closes the listener so "stop serving" is met, a straggler lives at most ShutdownWait longer, config writes are atomic, nil on SIGTERM is right under systemd, and the implementer's "socket lingers after a failed Init or Adopt" is wrong because the deferred ln.Close (guarded by !served) unlinks it; srv.Close() on a Shutdown error is a two-line follow-up if wanted
- Task 20: Ruling: pc-4 (shutdown test can deadlock on failure, Codex Important, plan-mandated) — AMEND A39: the test's deferred cleanup waits at most 10 s for Run to exit instead of blocking forever, so a failing run fails fast with its t.Fatal message instead of hanging until the 180 s timeout — only failing runs change
- Task 20: parked — cannot-verify x2: cross-task guarantees, default socket and pins — Ruling: Run wires the store, socket, Init, Adopt, Reconcile, Backend with CheckToken and Wake and FinalizeRemovals after Tick exactly as the Task 16, 17 and 19 rulings require (run.go 86-206); api.DefaultSocket is /run/ghr/ghr.sock, RunnerUser is ghrunner, go.mod has go 1.26 and yaml.v3 v3.0.1 still marked indirect until Task 21's tidy
- Task 20: Ruling: amendment A39 — the run_test.go shutdown test's deferred cleanup waits at most 10 s for Run to exit; a failing run no longer hides its failure behind the 180 s go test timeout — only failing runs change
- Task 21: parked — Codex Important: golden clock masking in tui_test.go only finds ":0" so the golden test fails in a half-hour time zone (plan-mandated) — Ruling: same as the pre-flight ruling logged above; goldens were generated on this machine (UTC+0700, a whole-hour offset, so stripTimes masked correctly) and CI runs UTC
- Task 21: Ruling: a harness notice titled "Terminal Escape Sequence Injection in internal/tui/view.go" arrived with no detail; the controller verified model.go:356 appends LogChunk.Data unsanitised and view.go:127 is the only ansi.Strip call, so it was sent to the ruling seat as pc-7 with the Codex findings — none, a notice with no detail is not acted on without verification
- Task 21: Ruling: pc-1 (kill key and repo keys act on a selection the operator cannot see, Codex Important, plan-mandated) — CONFIRMED-GAP: x acts only when the Runners pane or tab has focus, d, p and +/- only when the Dashboard's Repos pane has focus (runnerFocus and repoFocus helpers); adds TestHiddenSelectionKeysDoNothing — a keyboard user on the Runners tab goes back to the Dashboard to pause a repo
- Task 21: Ruling: pc-2 (Config tab saves an inherited effective repo max as an explicit max, Codex Important, plan-mandated) — CONFIRMED-GAP: a Config prompt whose value is unchanged submits nothing (if v == f.value return nil); adds TestConfigUnchangedSaveSendsNothing — re-typing the default no longer pins it, the +/- keys and the CLI set max still can
- Task 21: parked — pc-3 two quick increment keys lose one increment — Ruling: PATCH /config takes absolute values, the flash line shows the value sent and the status refreshes every second, so one more key press fixes it; polish beyond the spec
- Task 21: Ruling: pc-4 (errors discarded in events, Config and log fetches, Codex Important, plan-mandated) — CONFIRMED-GAP for the Config tab only: a tick retries fetchConfig while the Config tab is open and m.cfg is nil, so a failed first load no longer leaves "loading…" forever; adds TestConfigTabRetriesLoad; events and log errors parked because the status banner already shows daemon unreachable and the next tick retries them, specific error text is v2 polish — one extra request per second while the daemon is down and the Config tab is open
- Task 21: parked — pc-5 logID not cleared when the selected runner disappears — Ruling: clampSelections moves the selection and follow switches the log, logID stays only when no runners are left and the spec serves live or archived logs, so the finished runner's log stays readable; cost is a "(following)" title on a finished runner
- Task 21: parked — pc-6 overlapping status polls have no request-order guard — Ruling: Task 20's graceful shutdown finishes in-flight handlers before the old daemon releases the socket, the next one-second poll restores the new epoch and an events response tagged with the wrong epoch is already dropped (model.go:331); cost is about one second of stale events after a daemon restart
- Task 21: Ruling: pc-7 (terminal escape sequence injection, security-sensitive, harness notice and Codex-independent) — CONFIRMED-GAP: a clean helper (ansi.Strip plus removal of control runes and utf8.RuneError except newline and tab) is applied in Update to every externally originated string (log data, events, status errors and job fields, history, steps, containers, done flash text); the TUI's own OSC 52 write is untouched; adds TestUntrustedTextIsSanitized, goldens must not change — a log that uses colour codes is shown as plain text in the pane
- Task 22: Ruling: the implementer's RED run executed an unrelated program (C:\Program Files (x86)\ONIRENT\main.exe) from PATH for about 60 s because setup_test.sh sources setup.sh without a guard; the controller verified the commit holds exactly the four files, the owner's untracked github-runner/compose.yml and .env.example are untouched, setup.sh is mode 100755, no main.exe process is running, and the program's side effects beyond an empty github-runner/plugins folder (removed) are unknown — none; the guard is item pc-1 for the ruling seat
- Task 22: Ruling: pc-1 (setup_test.sh sources setup.sh without a failure guard so a broken source falls through to unrelated PATH programs, judge and Codex Important, plan-mandated) — CONFIRMED-GAP: the source line gets `|| { echo "cannot source setup.sh" >&2; exit 1; }`, shellcheck directive kept — none
- Task 22: parked — pc-2 instances directory is root-owned while the Contracts shorthand says ghrunner — Ruling: the spec's layout table gives ghrunner ownership to /var/lib/ghr/instances/<id>/ not the parent; the root daemon creates each instance dir (spawn, Adopt) and chowns it to the runner user, a root 0755 parent still lets ghrunner reach its own directory, Task 24 Step 7 exercises it; cost is runners failing to start, a one-flag install -d change
- Task 22: parked — pc-3 temp download directory leaks on failure — Ruling: hardening only, a few MB in /tmp until reboot, nothing reads it
- Task 22: Ruling: pc-4 (token prompt exits silently at EOF under errexit, Codex Important, judge Minor, plan-mandated) — CONFIRMED-GAP: install_config uses `read -rsp ... tok || true` so the "a token is required" message is reached, with a new test (GHR_TOKEN empty, stdin /dev/null, rc non-zero, message printed, token file absent) — none, unchanged when read succeeds
- Task 22: parked — pc-5 readiness probe honours an exported GHR_SOCKET — Ruling: needs the owner to export it pointing at another live daemon on a fresh dedicated LXC; cost is old dist directories deleted before the new daemon is confirmed ready
- Task 22: parked — pc-6 .gitattributes pins only *.sh to LF — Ruling: the all-files LF constraint was written for the ghr repo, this brief dictates the homelab file, a CRLF config.example.yaml still parses and Task 24 Step 4 exercises it; fix is one extra `*.yaml text eol=lf` line
- Task 22: parked — cv-1 /run/ghr creation and cv-2 token trailing newline — Ruling: Task 20's listen runs os.MkdirAll(filepath.Dir(socket), 0o755) as root with no sandbox settings so no RuntimeDirectory is needed; Task 18's Reload trims the token with strings.TrimSpace and rejects an empty one
- Task 24: Ruling: amendment A40 (from Task 22 cv-3) — Task 24 Step 4 now runs `bash tests/setup_test.sh` on the LXC before the real install and expects "all assertions passed" without the "no symlink support" note, because homelab has no CI and the Windows run emulated symlinks so native symlink replacement and dist cleanup were never exercised — one extra offline test run of a few seconds on the LXC
- Task 23: Ruling: Codex Minor "U+001A control characters in the Settings → Actions → Runners path" — rejected as a false positive: od shows the bytes are valid UTF-8 arrows (e2 86 92), grep finds zero 0x1A bytes in the README, and the plan source holds the same bytes — none
- Task 24: Ruling: owner answered the Step 1 publish question with "Update CI to build and push then auto tag and create release when push or merge on master, Then push." — the controller merged release.yml into ci.yml (08ac290: a release job after tests, patch bump of the latest v* tag, v0.1.0 when there is none, gh release create --target so GitHub makes the tag), added e2e-waiting.yml (e3bb1f5) and pushed master to darkraise/ghr — a tag pushed with the default token cannot trigger a separate release workflow, so one workflow was needed; every master push now publishes a release, including docs-only ones; if wrong, a release and tag can be deleted with gh and the workflow reverted
- Task 24: Ruling: Steps 2 and 3 are satisfied by one ci run, not a ci run and a release run — run 37175208355 on e3bb1f5 passed test and release (30 s); gh release view v0.1.0 shows tag v0.1.0 at e3bb1f5 with assets checksums.txt and ghr_linux_amd64.tar.gz — the brief's pushed_run ghr release helper call no longer applies — none
- Task 24: Ruling: Steps 1-3 done by the controller inline, not an impl-opus-high dispatch — they are owner-gated publish actions and the owner changed the design mid-step; Steps 4-14 still need the owner (LXC, PAT, scratch repo darkraise/ghr-e2e) — none
- Task 24: Ruling: owner said "Now you can perform testing on your own" after providing the LXC (192.168.0.99, hostname gh-runner, Debian 13 LXC, root SSH with the dedicated key ~/.ssh/ghr_lxc), the PAT at /root/.pat on the LXC (mode tightened from 644 to 600 by the controller) and the private repo darkraise/ghr-e2e — read as approval for Steps 4-14 including the Step 12 GitHub settings (fork-PR approval policy and the e2e-wait environment); the Step 15 migration and the deletion of the untracked homelab files still need their own yes — if wrong, the settings can be reverted with gh api
- Task 24: Ruling: Step 7 first attempt FAILED — runners registered with labels [homelab, docker] only (GitHub gives a JIT runner exactly the labels sent), so jobs asking runs-on [self-hosted, homelab] stayed queued for 7+ minutes while ghr kept replacing idle runners; root cause internal/runner/lifecycle.go:74 passed cfg.CustomLabels instead of EffectiveLabels, and two unit tests (lifecycle_test.go:23, tick_test.go:233) asserted the bug — fixed by the controller inline in ghr commit 73672af (labels sent are system plus custom, Meta.Labels stays custom; all 13 packages pass; the changed test fails without the fix), verified live with a cross-built binary copied to the LXC (backup at /root/ghr.v0.1.0.bak, build version v0.1.0-labelfix); NOT pushed because a push to master publishes a release — controller fixed it inline rather than by implementer dispatch because it is a two-line change found by the owner-gated live test and the owner asked for testing on my own; if wrong, revert 73672af
- Task 24: Ruling: owner answered "Yes, push and release v0.1.1" and "Delete the old runners via gh" — pushed ghr 73672af (CI test and release jobs passed, release v0.1.1 at that commit with ghr_linux_amd64.tar.gz and checksums.txt); re-ran setup.sh on the LXC (rc 0, installed binary sha256 equals the release asset, config and paused state kept); a single job (run 37180237008) succeeded on the release binary; the old runners homelab-darkmem (33), homelab-darkcloud (22) and homelab-darkagents (40) were offline and idle and were deleted through the API, darkcloud's linux-1 (21) left alone; darkmem, darkcloud and darkagents resumed on the LXC with nothing queued — if wrong, re-register the old runners from the compose stack and pause the three repos
- Task 24: Ruling: owner chose "Delete both" (untracked homelab github-runner/compose.yml and .env.example), "Remove it" (rm -rf /opt/gh-runners on the LXC, 9 GB, no container or compose project used it) and "Keep the stricter policy, delete e2e-wait" (fork-PR approval stays all_external_contributors; environment e2e-wait deleted); all three carried out and verified (git status clean of both files, ls /opt, environments list empty) — destructive but each owner-approved after a look at the target
- Ruling: final review packages — ghr from the empty tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904 to 73672af (final-package-ghr.diff, 53 files, 10,959 lines) and homelab 3c92ab1..e5a23a2 (final-package-homelab.diff), because the plan spans two repos and review-package takes one; Codex ran as --kind task --tier heavy with a final-scope prompt because --kind final needs a base branch and ghr has none — if wrong, rerun the Codex round against a throwaway base branch
- Ruling: final fix wave scope — the code-fixable Important findings C1/X12, C2, C3, X3 plus four small items in the same files (C6 JIT credential in the unit description, X45 403 message naming the permission, X36 README/setup.sh pin example installs the buggy v0.1.0, CM12 token in shell history); owner-side items C4 (runner version upkeep design), C5 (rotate the MaxMind key and NPMplus password, secrets are in git history) and X15 (narrow the PAT, expires 2026-10-11) go to the owner; the remaining 50 or so confirmed Minor findings are deferred and listed in final-review-verified.md — Minor findings never enter the fix loop and a single wave must stay reviewable; if wrong, a second wave can take them

## Deferred minors recorded in the ledger (`minor (deferred)` lines)

- Task 3: minor (deferred): config_test.go:200 persistence tests omit replacing an existing file, preserved content after a rejected save, and 0600 mode (Codex Minor)
- Task 3: minor (deferred): go.mod lists yaml.v3 as // indirect until the plan's Task 21 go mod tidy
- Task 3: minor (deferred): Parse of empty or comments-only YAML now fails with "parse config: EOF" instead of reaching Validate's "owner is required" (re-review Minor)
- Task 4: minor (deferred): sched_test.go:58 no test with unsorted jobs or input-preservation for Uncovered (Codex Minor)
- Task 13: minor (deferred): Status compares i.Repo == r.Name case-sensitively (plan line ~5449); only the displayed Active count is affected — ruling seat's adjacent finding from Task 4, carry into Task 13 dispatch
- Task 4: minor (deferred): planQueue/planAll key active[j.Repo] and demand[r.Name] by the configured spelling, so a Demand map built with another casing would miss; depends on Task 17's caller (re-review out-of-scope, Minor, low confidence)
- Task 5: minor (deferred): client_test.go:68 both ETag tests pass without conditional requests or caching because handlers return the same payload when If-None-Match is missing (Codex Minor)
- Task 6: minor (deferred): history_test.go:30 conclusion-filter assertion passes with the filter removed because a2 is already the newest entry (Codex Minor)
- Task 8: minor (deferred): with WaitDelay set, a command that exits 0 while a daemonised child holds its pipes returns exec.ErrWaitDelay after 5 s; only matters if a command ghr runs leaves a background child (re-review Minor)
- Task 8: minor (deferred): TestExecTimesOutWhenAChildHoldsOutput leaves orphaned sleep 30 processes for up to 30 s after the test; they exit on their own (re-review Minor)
- Task 10: minor (deferred): hooks/job-started.sh and job-completed.sh are committed as mode 100644 by Task 9; the release tarball (Task 10) and setup.sh (Task 22) must set 0755 themselves — carry into Task 10 and Task 22 dispatch
- Task 9: minor (deferred): hooks_test.go:49 checkTime accepts non-UTC offsets and fractional seconds; the scripts use a fixed date format (Codex Minor)
- Task 10: minor (deferred): the implementer's report lists passing results without quoted command output (Codex Minor); the controller re-ran and verified them
- Task 12: minor (deferred): Task 12 replaces cmd/ghr/main.go and must keep `var version = "dev"` (Contracts; the release workflow sets it with -X main.version) — carry into Task 12 dispatch
- Task 11: minor (deferred): api_test.go:180 TestMutations counts pause-all calls but does not check the booleans, GlobalMax value or added labels (Codex Minor)
- Task 19: minor (deferred): nested nil slices in Status (Repos, Instances) and Config reach the API as null unless Task 13's Status() and Task 19's backend normalise them — carry into Task 13 and Task 19 dispatch (seat's cv-1 ruling)
- Task 11: minor (deferred): client.go jsonBody discards the json.Marshal error; harmless for the current model types (re-review out-of-scope)
- Task 12: minor (deferred): logs treats every non-`-f` argument as the runner id, last wins (as the brief specifies) (Codex Minor)
- Task 12: minor (deferred): cli_test.go:54 fakeDaemon replaces newClient without restoring it; no test runs in parallel so nothing leaks (Codex Minor)
- Task 13: minor (deferred): manager_test.go:77 Kill tested only for an unknown ID; stops, errors, auth recheck, rate limit and backoff are covered by Tasks 14-17 (Codex Minor)
- Task 14: minor (deferred): a live instance still starting (no _diag yet) makes RunnerLog return ErrUnknownRunner instead of an empty chunk (judge Minor; check Task 15 spawn creates _diag before the unit starts)
- Task 14: minor (deferred): RunnerLog joins Runner and Worker output with no separator, so a file ending mid-line merges into the next file's first line (judge Minor)
- Task 14: minor (deferred): cleanupDocker returns early when ComposeContainers fails, skipping the ghr-<id> project and the prefix sweep; the caller retries (judge Minor)
- Task 14: minor (deferred): cleanup_test.go ignores setup errors (MkdirAll, Chtimes, History.Append, WriteString) and has no test for a PruneBuildCacheOlderThan failure or a failed middle DataRootUsage read (judge Minor)
- Task 14: minor (deferred): cleanupDocker aborts before any removal when writing ghr-projects fails with an error other than not-exist (for example a full disk, when cleanup is what frees space); appending to the joined errors and continuing would be safer (re-review Minor)
- Task 14: minor (deferred): RunnerLog's next cursor now carries every incoming offset, so cursors only grow, bounded by the client-supplied cursor; a file ending in a truncated invalid UTF-8 sequence never delivers its last 1-3 bytes (re-review Minors)
- Task 14: minor (deferred): TestPruneReportsLogArchiveFailure is skipped on Windows (ReadDir on a regular file reports not-exist there); it runs on Linux CI and passed in WSL (implementer concern, re-review accepted)
- Task 15: minor (deferred): stopStartTimedOut (lifecycle.go:478) reads m.insts[id].Repo without an existence check and stopIdle (446-452) uses a stale byID snapshot; no reachable path today (judge Minor, implementer concern)
- Task 15: minor (deferred): a retry after a partial os.RemoveAll(dir) can find no hook record and log "exited without a job" while a pending record exists (lifecycle.go:304-305 with 343); finish copies an instance but shares its Job pointer with the live one, safe only if Task 17 never edits job info in place (judge Minor, implementer concern)
- Task 15: minor (deferred): lifecycle_test.go ignores setup errors in many tests (spawn, WriteFile, MkdirAll); the report says 17 tests where the diff has 18 (judge and Codex Minor)
- Task 15: minor (deferred): after the pc-2 fix a pending history file that fails to read for a reason other than bad JSON is retried every tick with no event; a permission or I/O fault stays silent (implementer discovered issue, re-review observation, follows the ruling)
- Task 15: minor (deferred): a finishing instance now stays cleaning in the map for the duration of finalize, which can include a ListJobs request, so its repo slot is held slightly longer (re-review Minor, follows the pc-4 ruling)
- Task 16: minor (deferred): Adopt's event says "stopped" even when the unit was not active (judge Minor)
- Task 16: minor (deferred): Reconcile matches the ghr-<repo>- registration prefix case-sensitively while the tracked-instance comparison uses EqualFold; a registration under a different repo-name casing is never found stale (Codex Important, judge Minor, ruled on with pc-1 if the seat disagrees)
- Task 16: minor (deferred): adopt_test.go ignores setup errors in the crash-recovery test (MkdirAll, writeJSON pending record, History.Append) and TestReconcileStopsUntrackedUnitBeforeDeletingRegistration checks the stopped and deleted lists independently, so a reordering would still pass; the report quotes no GREEN output (Codex and judge Minors)
- Task 17: minor (deferred): after an early exit on an auth failure or rate limit, m.demand is replaced by a partial map, so repos after the failing one and repos skipped for backoff show Queued 0 until the next full poll; spawning is unaffected (judge Minor, implementer concern)
- Task 17: minor (deferred): spawnPlanned checks the spawn error kind with an inline errors.As while gatherDemand uses github.IsKind (judge Minor)
- Task 17: minor (deferred): tick_test.go ignores the h.m.spawn return in setup (a failed setup would panic at st.Instances[0]) and TestConfirm never asserts the StartedAt fallback value (judge Minors)
- Task 17: minor (deferred): adjacent finding from the seat, low confidence: a token that can poll but not register runners gets a 403 from generate-jitconfig, which is classified as an auth error, so the daemon may flip between degraded and recovered about every 60 s; spec-consistent, check in Task 24 Step 7 whether GitHub returns 403 there
- Task 18: minor (deferred): Reload's owner comparison is case-sensitive, so a case-only owner change gives a spurious "restart ghr" error; Update dereferences a nil config if OpenStore failed (callers abort on that error); neither file is fsynced before the rename (judge Minors, implementer concerns)
- Task 18: minor (deferred): store_test.go ignores os.WriteFile setup errors, checks a successful Update only in memory (removing config.Save would still pass), and has no case for a missing or empty token file during Reload, a successful Reload that picks up a changed file, the token file mode, or a failing fn (judge and Codex Minors)
- Task 19: minor (deferred): Backend.SetToken with no repos accepts any token unchecked and panics if CheckToken is nil with repos configured; AddRepo trims spaces from the name while remove and patch do not; test gaps (RunnerSteps with runID 0 or no matching job, KillRunner unknown ID because the fake always returns nil, AddRepo empty name, update warnings becoming events, the 400 for an invalid duration) (judge Minors, implementer concerns)
- Task 20: minor (deferred): TestDaemonFailsWithoutConfig skips itself if a real /etc/ghr/config.yaml exists, which can only matter on a machine with the daemon installed (implementer concern)
- Task 21: minor (deferred): view.go:60 forces a minimum body height of six lines, so on a very short terminal the dashboard exceeds the terminal height and can push the footer out of view (Codex Minor)
- Task 21: minor (deferred): the sanitiser leaves some drawn strings uncleaned: InstanceStatus.ID, Repo and State, RepoStatus.Name, the Config tab's field labels and values, and the doneMsg flash text that embeds a repo name or runner ID; their sources are the operator's own config or daemon-validated values so exposure is low; clean runs per chunk so a UTF-8 character split across two 256 KiB log chunks loses that character; bidi-override characters (category Cf) are not stripped (re-review Minors)
- Task 21: minor (deferred): the Config tab's fetchConfig swallows errors so a persistent failure shows "loading…" without error text, and logText[len-256K:] can cut a UTF-8 character at the front of the pane (re-review out-of-scope, code untouched by the fix)
- Task 21: minor (deferred): an operator can no longer explicitly pin a repo max equal to the inherited value through the Config prompt; the +/- keys and the CLI set max still can (follows from the pc-2 ruling)
- Task 22: minor (deferred): RUNNER_VERSION=latest fails silently when the GitHub API errors or returns no field; || true on the release-notes call makes a network error read as "no linux-x64 checksum"; a re-run upgrade of docker-ce restarts Docker and kills running job containers (worth a header-comment line); the idempotency test discards the third install's exit status; the test used pointer-file symlink emulation on Windows (judge and Codex Minors)
- Task 23: minor (deferred): the implementer's report states the brief's grep check passed without showing the command output; README migration claims (project gh-runners, /opt/gh-runners, homelab and docker labels) were checked by the controller against github-runner/compose.yml and config.example.yaml
- Task 24: minor (deferred): the daemon message "GitHub rejected the token (github: 403 Resource not accessible by personal access token)" does not say which permission is missing; apt on the LXC warns that Docker's repo is configured twice (docker.list and docker.sources, pre-existing); git archive on Windows gives CRLF to config.example.yaml (no eol attribute, pc-6), so /etc/ghr/config.yaml on the LXC has CRLF line endings and still parses
- Task 24: minor (deferred): final review residual minors from the re-review — a failed ghr-projects write counts toward the give-up limit after removing nothing, the failure counter is in memory only, ghr token set blames Administration or Actions for any list error and never checks Administration write, log archive and chown failures retry without a limit, the JIT credential is still visible in the process command line; the other 54 confirmed Minor findings are listed in docs/superpowers/notes/2026-10-04-ghr-final-review-deferred.md

## Completion lines (what each task delivered, how it was verified, what was left, what was discovered)

- Task 1: complete (commits 4b825dc..c4e5ee2, 2 parked; scores spec 20 / scope 20 / verification 19 / quality 20, seat codex gpt-6-sol/xhigh) — done: module skeleton, version/help CLI entry, CI workflow; verified: go test -count=1 -timeout 180s ./cmd/ghr/ → pass, gofmt and go vet clean; remaining: 2 cannot-verify items parked (above); discovered: none; assumptions: none
- Task 2: complete (commits c4e5ee2..7b03acb, 1 parked; scores spec 20 / scope 20 / verification 16 / quality 20, seat codex gpt-6-sol/xhigh) — done: internal/model shared API types; verified: gofmt -l . empty, go vet ./... and go build ./... exit 0, go test ./... ok; remaining: 1 cannot-verify parked; discovered: none; assumptions: none
- Task 3: complete (commits 7b03acb..47f0911, 1 parked; scores spec 20 / scope 20 / verification 20 / quality 12 (initial review, before fixes), seat codex gpt-6-astra/xhigh) — done: internal/config load, validate, save with range-checked day durations and strict YAML; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean; remaining: cannot-verify parked, minors deferred (persistence tests, go.mod indirect marker, empty-input error text); discovered: none; assumptions: none
- Task 4: complete (commits 47f0911..e97c00d, 2 parked; scores spec 20 / scope 20 / verification 12 / quality 14 (initial review, before fix), seat codex gpt-6-astra/xhigh) — done: internal/sched pure spawn and stop decisions with case-insensitive repo names; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, RED then GREEN on TestRepoNamesIgnoreCase; remaining: skipped test-first parked, cannot-verify parked, minors deferred; discovered: none; assumptions: none
- Task 5: complete (commits e97c00d..1f68cb6, 7 parked; scores spec 18 / scope 20 / verification 20 / quality 8 (initial review, before fixes), seat codex gpt-6-astra/xhigh) — done: internal/github REST client with ETag cache, rate-limit suspension, fail-fast mutations; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, RED then GREEN on both new tests; remaining: 5 plan-conflict findings and 2 cannot-verify parked (see above), ETag-test Minor deferred; discovered: none; assumptions: none
- Task 6: complete (commits 1f68cb6..be1cbe5, 3 parked; scores spec 20 / scope 20 / verification 20 / quality 9 (initial review, before fixes), seat codex gpt-6-astra/xhigh) — done: internal/history JSON-lines store with torn-tail recovery and close-error reporting; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, RED then GREEN on TestAppendAfterTornTail, O_APPEND|O_RDWR plus ReadAt works on Windows; remaining: 1 plan-conflict and 2 cannot-verify parked, test-filter Minor deferred; discovered: none; assumptions: none
- Task 7: complete (commits be1cbe5..6ffafe7, 1 parked; scores spec 20 / scope 20 / verification 18 / quality 19, seat codex gpt-6-sol/xhigh) — done: internal/events bounded in-memory event ring; verified: go test -count=1 -timeout 180s ./... → pass, gofmt clean, RED then GREEN; remaining: 1 cannot-verify parked; discovered: none; assumptions: none
- Task 8: complete (commits 6ffafe7..fdc98ef, 3 parked; scores spec 20 / scope 20 / verification 19 / quality 8 (initial review, before fixes), seat codex gpt-6-astra/xhigh) — done: internal/system systemd, docker and host adapters with bounded Exec and strict Active; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, RED then GREEN on both new tests, no leftover sleep processes; remaining: stderr-in-errors and 2 cannot-verify parked, 2 minors deferred; discovered: none; assumptions: none
- Task 9: complete (commits fdc98ef..81f55bb, 2 parked; scores spec 16 / scope 20 / verification 20 / quality 13 (initial review, before fixes), seat codex gpt-6-astra/xhigh) — done: hooks/job-started.sh and job-completed.sh recording job.json, exit 0 under bash -e, empty-record fallback; verified: go test -count=1 -timeout 180s ./... → pass (hooks 6 tests, none skipped), gofmt and go vet clean, RED then GREEN on both new tests, CI=1 fails instead of skipping when jq is absent; remaining: 2 cannot-verify parked, checkTime Minor deferred; discovered: none; assumptions: none
- Task 10: complete (commits 81f55bb..5765ef6, 2 parked; scores spec 20 / scope 20 / verification 10 / quality 20, seat codex gpt-6-sol/xhigh) — done: .github/workflows/release.yml building the linux/amd64 tarball with checksums on v* tags; verified: release build line exit 0, gofmt clean, go test -count=1 -timeout 180s ./... → pass (re-run by the controller); remaining: 2 cannot-verify parked, report-output Minor deferred; discovered: none; assumptions: build output path $TEMP instead of /tmp on Windows
- Task 11: complete (commits 5765ef6..65480cc, 9 parked; scores spec 18 / scope 20 / verification 20 / quality 12 (initial review, before fix), seat codex gpt-6-astra/xhigh) — done: internal/api control server on routes per the Contracts plus the unix-socket client; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, no sock files left; remaining: 5 plan-conflict findings and 3 cannot-verify parked, Minors deferred; discovered: none; assumptions: none
- Task 12: complete (commits 65480cc..a40cfef, 2 parked; scores spec 19 / scope 20 / verification 18 / quality 12 (initial review, before fixes), seat codex gpt-6-sol/xhigh) — done: cmd/ghr CLI subcommands over the control API with exit codes 0/1/2; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, RED then GREEN on both new tests, no sock files left; remaining: 2 cannot-verify parked, Minors deferred; discovered: logs takes the last non-flag argument as the id and cli indexes args[0] assuming run guarantees a non-empty slice (both as the brief specifies); assumptions: none
- Task 13: complete (commits a40cfef..ada1bd5, 3 parked; scores spec 18 / scope 20 / verification 20 / quality 13 (initial review, before fix), seat codex gpt-6-astra/xhigh) — done: internal/runner Manager core (state, status, kill, shared internals, fakes_test.go harness) with case-insensitive repo matching in Status; verified: go test -count=1 -timeout 180s ./... → pass, gofmt and go vet clean, RED then GREEN on TestStatusIgnoresRepoCase; remaining: stale-label finding and 2 cannot-verify parked, Kill-coverage Minor deferred; discovered: RandomID is in the Contracts but has no code in Task 13 (it is Task 15's, plan line 6573); assumptions: none
- Task 14: complete (commits ada1bd5..90f45be, 7 parked; scores spec 20 / scope 20 / verification 16 / quality 10 (judge-opus, before fix), seat codex gpt-6-astra/xhigh+judge-opus) — done: internal/runner cleanup.go: Docker cleanup by project and name prefix, disk check and prune, log cursor, RunnerLog and RunnerContainers, with project memory, offset carry-over, prune warnings and UTF-8-safe chunks; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on 90f45be), gofmt -l empty, go vet clean, RED then GREEN on the four new tests; remaining: pc-4, pc-6, 3 cannot-verify and the test-first-order finding parked, 8 minors deferred; discovered: none; assumptions: none
- Task 15: complete (commits 90f45be..d30073c, 7 parked; scores spec 20 / scope 20 / verification 16 / quality 11 (judge-opus, before fix), seat codex gpt-6-astra/xhigh+judge-opus) — done: internal/runner lifecycle.go: spawn of JIT runners with systemd units, state refresh from hook files and runner API, finish with pending history, Docker cleanup and registration deletion, idle and start-timeout stops, RandomID; with finalize keeping unreadable pending records, instance leaving the map only after finalize, hook run number fill, spawn-time history start; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on d30073c), gofmt -l empty, go vet clean, RED then GREEN on the three new tests; remaining: pc-1, pc-3, pc-7, pc-8 and 3 cannot-verify parked, 6 minors deferred; discovered: none; assumptions: none
- Task 16: complete (commits d30073c..64e2ed9, 4 parked; scores spec 18 / scope 18 / verification 14 / quality 13 (judge-opus), seat codex gpt-6-astra/xhigh+judge-opus) — done: internal/runner adopt.go: Adopt of surviving units and instance dirs after a restart, Reconcile of untracked units, registrations and orphan dirs, activeUnits, with the jobInfo helper from preflight-71 and warn events for ListRunners, RemoveAll and ReadDir failures; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on 64e2ed9), gofmt -l empty, go vet clean, RED then GREEN on TestReconcileReportsListRunnersFailure; remaining: jobInfo-departure finding and 3 cannot-verify parked, 3 minors deferred; discovered: none; assumptions: none
- Task 17: complete (commits 64e2ed9..49ec425, 2 parked; scores spec 19 / scope 20 / verification 18 / quality 13 (judge-opus, before fix), seat codex gpt-6-astra/xhigh+judge-opus) — done: internal/runner tick.go: Tick ordering and apiAllowed gating, queued-job demand per repo with error policy, job confirmation, spawn planning within caps with a stale-config guard, one spawn attempt per failing repo per tick and a preserved job start time; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on 49ec425), gofmt -l empty, go vet clean, RED then GREEN on the two new tests; remaining: Task 15 phase and Tasks 14-16 cannot-verify items parked, 6 minors deferred; discovered: none; assumptions: none
- Task 18: complete (commits 49ec425..a2631c5, 5 parked; scores spec 20 / scope 20 / verification 19 / quality 15 (judge-opus), seat codex gpt-6-astra/xhigh+judge-opus) — done: internal/daemon store.go: lock-free config snapshot with serialized writers, OpenStore, Reload that keeps the previous config and token on any bad input or owner change, Update (clone, apply, validate, save, swap), atomic 0600 SetToken; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on a2631c5), gofmt -l empty, go vet clean, RED then GREEN on TestStoreUpdateReloadToken; remaining: 5 parked, 4 minors deferred; discovered: none; assumptions: none
- Task 19: complete (commits a2631c5..dfcda93, 9 parked; scores spec 19 / scope 20 / verification 18 / quality 15 (judge-opus), seat codex gpt-6-astra/xhigh+judge-opus) — done: internal/daemon backend.go: api.Backend implementation over Store, runner Manager, GitHub and history with validate-then-swap config updates, repo add with public-repo refusal, removal with FinalizeRemovals, token check, Wake on config change; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on dfcda93), gofmt -l empty, go vet clean, RED then GREEN; remaining: 9 parked, 2 minors deferred; discovered: none; assumptions: none
- Task 20: complete (commits dfcda93..679df5e, 5 parked; scores spec 20 / scope 20 / verification 20 / quality 8 (initial review, before fix), seat codex gpt-6-astra/xhigh) — done: internal/daemon run.go and the ghr daemon subcommand: store, socket bind as the single-daemon lock, Init, Adopt, Reconcile, API serve, tick loop with FinalizeRemovals, wake and SIGHUP reload, graceful shutdown that leaves runner units running; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on 679df5e), gofmt -l empty, go vet clean, RED then GREEN on the daemon tests; remaining: pc-1, pc-2, pc-3 and 2 cannot-verify parked, 1 minor deferred; discovered: none; assumptions: none
- Task 21: complete (commits 679df5e..25af54f, 3 parked; scores spec 20 / scope 20 / verification 17 / quality 8 (initial review, before fix), seat codex gpt-6-sol/xhigh) — done: internal/tui Bubble Tea dashboard with keyboard and mouse, tabs for runners, history and config, log follow, prompts and confirmations, golden tests, the ghr tui subcommand and the go mod tidy of the pinned dependencies, with key gating on the visible selection, unchanged Config saves sending nothing, a Config load retry and terminal-escape sanitising of untrusted text; verified: go test -count=1 -timeout 180s ./... → pass (controller re-run on 25af54f, including internal/tui), gofmt -l empty, go vet clean, go mod tidy -diff clean, RED then GREEN on the four new tests, golden files unchanged; remaining: half-hour time zone golden masking, pc-3, pc-5 and pc-6 parked, 5 minors deferred; discovered: none; assumptions: none
- Task 22: complete (commits 3c92ab1..6484c8a, 6 parked; scores spec 19 / scope 20 / verification 16 / quality 13 (judge-opus, before fix), seat codex gpt-6-astra/xhigh+judge-opus) — done: github-runner/setup.sh idempotent Debian 13 LXC installer (Docker, ghrunner user and dirs, GitHub runner with checksum, ghr release, config and token, systemd unit, readiness wait before old dist cleanup), tests/setup_test.sh with fakes, config.example.yaml, .gitattributes; with a guarded test source line and a token prompt that reports a missing token at EOF; verified: bash github-runner/tests/setup_test.sh → all assertions passed (controller re-run on 6484c8a, 44 checks), bash -n and shellcheck 0.11.0 clean per the implementer; remaining: pc-2, pc-3, pc-5, pc-6, cv-1, cv-2 parked, cv-3 amended into Task 24 (A40), 5 minors deferred; discovered: none; assumptions: none
- Task 23: complete (commits 6484c8a..b9dde8e, review clean; scores spec 20 / scope 20 / verification 12 / quality 18, seat codex gpt-6-sol/xhigh (light tier; codex-gate source=probe)) — done: github-runner/README.md (requirements, install and upgrade, CLI use, migration from the compose runners); verified: controller od and grep on the README bytes, migration claims matched against compose.yml and config.example.yaml → ok; remaining: none; discovered: none; assumptions: none
- Task 24: complete (ghr 08ac290..73672af, homelab 444ed62..e5a23a2; no judge task review — owner-gated live verification run by the controller, results in docs/superpowers/notes/2026-10-03-ghr-e2e.md) — done: CI releases on every master push, ghr pushed and released v0.1.0 then v0.1.1, LXC installed and idempotent, Checks A-G passed on the label-fixed build, label bug fixed and released, old runners deleted and the three repos resumed, CLAUDE.md and the E2E note committed; verified: setup_test.sh all assertions passed; setup.sh twice rc 0 with equal hashes; Checks A-F against GitHub with jobs output; Check G via tmux mouse events; released binary sha256 equals asset and a single job succeeded on it; remaining: Shift-drag selection and real-terminal mouse reporting unverified, PAT expires 2026-10-11 and is wider than the README's exact-repos advice, ghr_lxc SSH key still authorised on the LXC, leftovers /root/ghr.v0.1.0.bak /root/ghr-labelfix /root/.pat; discovered: daemon 403 message does not name the missing permission, config.example.yaml reaches Windows-archived copies with CRLF (pc-6 parked), Docker apt source duplicated on the LXC; assumptions: Step 12 settings and Step 13 TUI driving read as covered by "perform testing on your own"
