# ghr TUI Revamp, Plan 1: Settings API and Bare `ghr` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the daemon's config patch API change every `config.yaml` setting except `owner`, and make bare `ghr` open the TUI when run in a terminal.

**Architecture:** `model.ConfigPatch` gains optional pointer fields that `daemon.Backend.PatchConfig` applies inside the existing store `update` closure, so the existing `Config.Validate` and atomic save cover them. Duration parse errors gain the field name. `cmd/ghr` routes a no-argument run to the TUI through two replaceable package variables (`runTUI`, `isTerminal`) and keeps today's usage-and-exit-2 behaviour when not on a terminal.

**Tech Stack:** Go 1.26, Bubble Tea v1 (unchanged), `github.com/charmbracelet/x/term` v0.2.2 (already in the module graph).

**Spec:** docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md

**Execution:** inline — `claude --model sonnet --effort medium` — three small tasks, none heavy (highest total 4); the session implements them and runs the whole-branch final review.

**Plan review:** 2026-10-04 — codex gpt-6-astra / xhigh — executability 16 / coherence 20 / coverage 18 / assumptions 19 (round 1)

## Global Constraints

- All code tasks run in `D:/Repositories/Personal/ghr` (module `github.com/darkraise/ghr`, `go 1.26`, branch `master`, remote `https://github.com/darkraise/ghr.git`). Work on a feature branch `feat/settings-api-launch`; never commit to `master` directly.
- Every push to ghr's `master` publishes a release. Do not push, merge to `master`, tag or deploy without your human partner's explicit approval.
- Run every shell command in Git Bash (the Bash tool), never PowerShell: commands use POSIX redirection and `$?`.
- Copy every code block verbatim, including comments. Create or replace files with the Write or Edit tools, never shell heredocs.
- Line endings are LF (`.gitattributes`: `* text=auto eol=lf`).
- Every `go test` carries `-count=1 -timeout 180s`. `-race` is unavailable on this Windows machine (no cgo).
- Before each commit: `gofmt -l cmd internal` prints nothing and `go vet ./...` passes.
- Commits: `<type>(<scope>): <subject>`, type one of feat|fix|docs|style|refactor|test|chore|perf, the whole first line ≤ 50 characters, imperative, English. Use each task's commit command as written.
- Do not upgrade Bubble Tea, lipgloss, bubbles or bubblezone.
- No change to the CLI's `ghr set` subcommands (spec §4: out of scope).

## Contracts

**ConfigPatch** (`internal/model/model.go`) — after Task 1, exactly:

```go
type RunnerLimitsPatch struct {
	MemoryMax *string `json:"memory_max,omitempty"`
	CPUQuota  *string `json:"cpu_quota,omitempty"`
}

type ConfigPatch struct {
	Mode             *string              `json:"mode,omitempty"`
	GlobalMax        *int                 `json:"global_max,omitempty"`
	PollInterval     *string              `json:"poll_interval,omitempty"`
	StartTimeout     *string              `json:"start_timeout,omitempty"`
	IdleTimeout      *string              `json:"idle_timeout,omitempty"`
	HistoryRetention *string              `json:"history_retention,omitempty"`
	DiskHighWater    *int                 `json:"disk_high_water,omitempty"`
	BuildCacheKeep   *string              `json:"build_cache_keep,omitempty"`
	Labels           *[]string            `json:"labels,omitempty"`
	RunnerLimits     *RunnerLimitsPatch   `json:"runner_limits,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
}
```

A nil field means "unchanged". Plan 2's Settings form sends these fields.

**PatchConfig errors** (`internal/daemon/backend.go`) — an unparseable duration returns `api.BadRequest("<json field>: invalid duration \"<value>\"")`, for example `poll_interval: invalid duration "soon"`; fields are checked in the order poll_interval, start_timeout, idle_timeout, history_retention. Any value that `Config.Validate` rejects returns a 400 carrying Validate's message. Either way `config.yaml` is unchanged.

**CLI entry** (`cmd/ghr/main.go`) — `var runTUI = tui.Run` (type `func(tui.Client) error`); `var fdIsTerminal = term.IsTerminal` (type `func(uintptr) bool`); `func stdioIsTerminal() bool` (true only when `fdIsTerminal` is true for both `os.Stdin.Fd()` and `os.Stdout.Fd()`); `var isTerminal = stdioIsTerminal`. Tests replace `runTUI`, `isTerminal` and `fdIsTerminal`. `run(nil, …)`: if `isTerminal()` then open the TUI (exit 0, or 1 with `ghr: <err>` on stderr), else print `usage` to stderr and exit 2. `run([]string{"tui"}, …)` opens the TUI the same way.

## Assumptions (evidence)

- Every new setting is read from the store at use, so a patch takes effect without a restart: `internal/daemon/run.go:185` (poll interval), `internal/runner/cleanup.go:174-188,220` (disk, build cache, retention), `internal/runner/lifecycle.go:113-114` (runner limits, at runner start). Verified by reading, 2026-10-04.
- `Store.Update` clones, applies, validates, saves, then swaps, so a rejected patch writes nothing: `internal/daemon/store.go:68-84`; `Backend.update` turns a non-API error into a 400: `internal/daemon/backend.go:110-118`. Verified 2026-10-04.
- `Validate` rejects `disk_high_water` outside 1..100, a size not matching `^[0-9]+(B|KB|MB|GB|TB)$`, a memory value not matching `^([1-9][0-9]*[KMGT]?|[1-9][0-9]?%|100%|infinity)$`, a CPU quota not matching `^[1-9][0-9]*%$`, and a repo with no custom label: `internal/config/config.go:119-121,279-289,322`. Verified 2026-10-04.
- `api.Error.Error()` returns the bare message: `internal/api/server.go:22`. Verified 2026-10-04.
- The daemon test store's config (`internal/daemon/store_test.go:12-21`) has `labels: [homelab]` and repos `darkcloud` and `darkmem` with no repo labels, so an empty global label list fails validation. Verified 2026-10-04.
- `config.Duration.String()` prints `30s` for 30 seconds and `14d` for 14 days: `internal/config/config.go:84-90`. Verified 2026-10-04.
- `github.com/charmbracelet/x/term v0.2.2` is in `go.mod` as indirect and in the module cache, and exports `IsTerminal(fd uintptr) bool`: `go.mod:22`, `~/go/pkg/mod/github.com/charmbracelet/x/term@v0.2.2/term.go:11`. Verified 2026-10-04.
- No existing test asserts the unprefixed `invalid duration` message: `Grep "invalid duration" --glob *_test.go` over the ghr repo returned nothing, 2026-10-04.
- `tui.Run(c Client) error` and `*api.Client` satisfies `tui.Client`: `internal/tui/model.go:21-37,172`, `cmd/ghr/main.go:55`. Verified 2026-10-04.
- `PATCH /config` decodes the body straight into `model.ConfigPatch` with `encoding/json` and calls `Backend.PatchConfig`; success is 204, an `api.Error` is its status with body `{"error": "<msg>"}`: `internal/api/server.go:87-93,134-139,146-160`. So a `json.Unmarshal` of a literal body into `ConfigPatch` followed by `PatchConfig` (Task 1) exercises the JSON keys the HTTP path uses; Task 3 checks the real socket. Verified 2026-10-04.
- The LXC `gh-runner` (192.168.0.99, key `~/.ssh/ghr_lxc`) runs ghr v0.1.2 from `/root/github-runner/setup.sh`, has `curl` and `tmux`, and its `/etc/ghr/config.yaml` holds `poll_interval: 10s` and `disk_high_water: 80`: checked over SSH, 2026-10-04.
- The real `term.IsTerminal` can only be exercised with a real terminal, which a Windows `go test` run does not have; Task 2 tests the both-descriptors rule through `fdIsTerminal`, and Task 3 runs bare `ghr` under tmux on the LXC.

## Task index

1. Settings fields in the config patch API
2. Bare `ghr` opens the TUI
3. Release and LXC acceptance

---

### Task 1: Settings fields in the config patch API

**Files:**
- Modify: `internal/model/model.go` (the `ConfigPatch` struct, currently lines 101-107)
- Modify: `internal/daemon/backend.go` (`PatchConfig`, the duration loop currently at lines 139-148)
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Consumes: none
- Produces: **ConfigPatch** and **PatchConfig errors** (see Contracts)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, replace the import block with:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
)
```

Then add these three tests directly after `TestPatchConfig`:

```go
// Every setting except owner can be patched live, and the change is persisted.
func TestPatchConfigSettings(t *testing.T) {
	b, _, _ := newBackend(t)
	poll, retention, keep := "30s", "14d", "10GB"
	mem, cpu, disk := "4G", "150%", 70
	labels := []string{"homelab", "docker"}
	if err := b.PatchConfig(model.ConfigPatch{PollInterval: &poll, HistoryRetention: &retention, DiskHighWater: &disk,
		BuildCacheKeep: &keep, Labels: &labels, RunnerLimits: &model.RunnerLimitsPatch{MemoryMax: &mem, CPUQuota: &cpu}}); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.PollInterval.String() != "30s" || c.HistoryRetention.String() != "14d" || c.DiskHighWater != 70 || c.BuildCacheKeep != "10GB" ||
		!reflect.DeepEqual(c.Labels, labels) || c.RunnerLimits != (config.RunnerLimits{MemoryMax: "4G", CPUQuota: "150%"}) {
		t.Fatalf("cfg %+v", c)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PollInterval != c.PollInterval || saved.HistoryRetention != c.HistoryRetention || saved.DiskHighWater != 70 ||
		saved.BuildCacheKeep != "10GB" || !reflect.DeepEqual(saved.Labels, labels) || saved.RunnerLimits != c.RunnerLimits {
		t.Fatalf("not persisted: %+v", saved)
	}
	// A partial runner_limits patch leaves the other limit alone.
	mem = "8G"
	if err := b.PatchConfig(model.ConfigPatch{RunnerLimits: &model.RunnerLimitsPatch{MemoryMax: &mem}}); err != nil {
		t.Fatal(err)
	}
	if rl := b.Store.Config().RunnerLimits; rl != (config.RunnerLimits{MemoryMax: "8G", CPUQuota: "150%"}) {
		t.Fatalf("limits %+v", rl)
	}
}

// The JSON keys clients send (PATCH /config decodes straight into ConfigPatch)
// reach the new fields; a wrong or missing struct tag fails here.
func TestPatchConfigJSONKeys(t *testing.T) {
	b, _, _ := newBackend(t)
	body := `{"poll_interval":"20s","history_retention":"7d","disk_high_water":75,"build_cache_keep":"5GB",` +
		`"labels":["homelab","x"],"runner_limits":{"memory_max":"2G","cpu_quota":"100%"}}`
	var p model.ConfigPatch
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if err := b.PatchConfig(p); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.PollInterval.String() != "20s" || c.HistoryRetention.String() != "7d" || c.DiskHighWater != 75 || c.BuildCacheKeep != "5GB" ||
		!reflect.DeepEqual(c.Labels, []string{"homelab", "x"}) || c.RunnerLimits != (config.RunnerLimits{MemoryMax: "2G", CPUQuota: "100%"}) {
		t.Fatalf("cfg %+v", c)
	}
}

// An invalid setting is a 400 that names the problem and leaves config.yaml untouched.
func TestPatchConfigRejectsInvalidSettings(t *testing.T) {
	zero, size, cpu, mem, dur := 0, "lots", "fast", "6 gigs", "soon"
	empty := []string{}
	cases := []struct {
		name string
		p    model.ConfigPatch
		msg  string
	}{
		{"disk", model.ConfigPatch{DiskHighWater: &zero}, "disk_high_water must be 1..100"},
		{"size", model.ConfigPatch{BuildCacheKeep: &size}, "build_cache_keep must look like 20GB"},
		{"cpu", model.ConfigPatch{RunnerLimits: &model.RunnerLimitsPatch{CPUQuota: &cpu}}, "runner_limits.cpu_quota"},
		{"memory", model.ConfigPatch{RunnerLimits: &model.RunnerLimitsPatch{MemoryMax: &mem}}, "runner_limits.memory_max"},
		{"poll", model.ConfigPatch{PollInterval: &dur}, `poll_interval: invalid duration "soon"`},
		{"start", model.ConfigPatch{StartTimeout: &dur}, `start_timeout: invalid duration "soon"`},
		{"idle", model.ConfigPatch{IdleTimeout: &dur}, `idle_timeout: invalid duration "soon"`},
		{"retention", model.ConfigPatch{HistoryRetention: &dur}, `history_retention: invalid duration "soon"`},
		{"labels", model.ConfigPatch{Labels: &empty}, "needs at least one label"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, _ := newBackend(t)
			before, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			err = b.PatchConfig(tc.p)
			if apiStatus(err) != 400 || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("err %v, want 400 containing %q", err, tc.msg)
			}
			after, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("config.yaml changed:\n%s", after)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/daemon/ -run 'TestPatchConfig' -count=1 -timeout 180s`
Expected: FAIL to compile with errors such as `unknown field PollInterval in struct literal of type model.ConfigPatch` and `undefined: model.RunnerLimitsPatch`.

- [ ] **Step 3: Add the patch fields**

In `internal/model/model.go`, replace the whole `ConfigPatch` struct:

```go
type ConfigPatch struct {
	Mode         *string              `json:"mode,omitempty"`
	GlobalMax    *int                 `json:"global_max,omitempty"`
	StartTimeout *string              `json:"start_timeout,omitempty"`
	IdleTimeout  *string              `json:"idle_timeout,omitempty"`
	Repos        map[string]RepoPatch `json:"repos,omitempty"`
}
```

with:

```go
type RunnerLimitsPatch struct {
	MemoryMax *string `json:"memory_max,omitempty"`
	CPUQuota  *string `json:"cpu_quota,omitempty"`
}

type ConfigPatch struct {
	Mode             *string              `json:"mode,omitempty"`
	GlobalMax        *int                 `json:"global_max,omitempty"`
	PollInterval     *string              `json:"poll_interval,omitempty"`
	StartTimeout     *string              `json:"start_timeout,omitempty"`
	IdleTimeout      *string              `json:"idle_timeout,omitempty"`
	HistoryRetention *string              `json:"history_retention,omitempty"`
	DiskHighWater    *int                 `json:"disk_high_water,omitempty"`
	BuildCacheKeep   *string              `json:"build_cache_keep,omitempty"`
	Labels           *[]string            `json:"labels,omitempty"`
	RunnerLimits     *RunnerLimitsPatch   `json:"runner_limits,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
}
```

- [ ] **Step 4: Apply the fields in PatchConfig**

In `internal/daemon/backend.go`, inside `PatchConfig`, replace this block:

```go
		for field, v := range map[*config.Duration]*string{&c.StartTimeout: p.StartTimeout, &c.IdleTimeout: p.IdleTimeout} {
			if v == nil {
				continue
			}
			d, err := config.ParseDuration(*v)
			if err != nil {
				return api.BadRequest(err.Error())
			}
			*field = d
		}
```

with:

```go
		// A slice, not a map, so the first bad duration reported is deterministic.
		for _, f := range []struct {
			name  string
			field *config.Duration
			v     *string
		}{
			{"poll_interval", &c.PollInterval, p.PollInterval},
			{"start_timeout", &c.StartTimeout, p.StartTimeout},
			{"idle_timeout", &c.IdleTimeout, p.IdleTimeout},
			{"history_retention", &c.HistoryRetention, p.HistoryRetention},
		} {
			if f.v == nil {
				continue
			}
			d, err := config.ParseDuration(*f.v)
			if err != nil {
				return api.BadRequest(f.name + ": " + err.Error())
			}
			*f.field = d
		}
		if p.DiskHighWater != nil {
			c.DiskHighWater = *p.DiskHighWater
		}
		if p.BuildCacheKeep != nil {
			c.BuildCacheKeep = *p.BuildCacheKeep
		}
		if p.Labels != nil {
			c.Labels = *p.Labels
		}
		if rl := p.RunnerLimits; rl != nil {
			if rl.MemoryMax != nil {
				c.RunnerLimits.MemoryMax = *rl.MemoryMax
			}
			if rl.CPUQuota != nil {
				c.RunnerLimits.CPUQuota = *rl.CPUQuota
			}
		}
```

Leave the rest of `PatchConfig` (mode, global max, the repos loop, the cancelled-removal events) unchanged.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/daemon/ -run 'TestPatchConfig' -count=1 -timeout 180s -v`
Expected: PASS for `TestPatchConfig`, `TestPatchConfigSettings`, `TestPatchConfigJSONKeys` and all nine subtests of `TestPatchConfigRejectsInvalidSettings`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "feat(daemon): patch every setting except owner"
```

### Task 2: Bare `ghr` opens the TUI

**Files:**
- Modify: `cmd/ghr/main.go` (imports, the `run` function, the `usage` constant)
- Modify: `go.mod` (`github.com/charmbracelet/x/term` becomes a direct requirement)
- Test: `cmd/ghr/main_test.go`

**Interfaces:**
- Consumes: none
- Produces: **CLI entry** (see Contracts)

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing test**

In `cmd/ghr/main_test.go`, replace the import block with:

```go
import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/tui"
)
```

In `TestVersionAndUsage`, make the no-argument check independent of how the test binary's stdio is attached. Replace:

```go
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("no args: exit %d", code)
	}
```

with:

```go
	stubTerminal(t, false)
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("no args: exit %d", code)
	}
```

Append to the end of the file:

```go
// stubTerminal fixes isTerminal for one test and restores it afterwards.
func stubTerminal(t *testing.T, on bool) {
	t.Helper()
	old := isTerminal
	isTerminal = func() bool { return on }
	t.Cleanup(func() { isTerminal = old })
}

// stubTUI replaces runTUI with fn for one test and restores it afterwards.
func stubTUI(t *testing.T, fn func(tui.Client) error) {
	t.Helper()
	old := runTUI
	runTUI = fn
	t.Cleanup(func() { runTUI = old })
}

func TestBareGhrOpensTUIOnTerminal(t *testing.T) {
	calls := 0
	stubTUI(t, func(tui.Client) error { calls++; return nil })
	stubTerminal(t, true)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 0 || calls != 1 {
		t.Fatalf("bare ghr: exit %d, tui calls %d, stderr %q", code, calls, errb.String())
	}
	if code := run([]string{"tui"}, strings.NewReader(""), &out, &errb); code != 0 || calls != 2 {
		t.Fatalf("ghr tui: exit %d, tui calls %d", code, calls)
	}
	stubTUI(t, func(tui.Client) error { return errors.New("terminal gone") })
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "ghr: terminal gone") {
		t.Fatalf("tui error: exit %d, stderr %q", code, errb.String())
	}
}

// The production detector needs both stdin and stdout on a terminal; a
// detector that always says false would never open the TUI.
func TestStdioIsTerminalNeedsBoth(t *testing.T) {
	old := fdIsTerminal
	t.Cleanup(func() { fdIsTerminal = old })
	in, out := os.Stdin.Fd(), os.Stdout.Fd()
	for _, tc := range []struct{ stdin, stdout, want bool }{
		{true, true, true}, {true, false, false}, {false, true, false}, {false, false, false},
	} {
		fdIsTerminal = func(fd uintptr) bool {
			switch fd {
			case in:
				return tc.stdin
			case out:
				return tc.stdout
			}
			t.Fatalf("unexpected fd %d", fd)
			return false
		}
		if got := stdioIsTerminal(); got != tc.want {
			t.Fatalf("stdin terminal %v, stdout terminal %v: got %v, want %v", tc.stdin, tc.stdout, got, tc.want)
		}
	}
}

func TestBareGhrPrintsUsageWithoutTerminal(t *testing.T) {
	calls := 0
	stubTUI(t, func(tui.Client) error { calls++; return nil })
	stubTerminal(t, false)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 || calls != 0 || !strings.Contains(errb.String(), "usage: ghr") {
		t.Fatalf("exit %d, tui calls %d, stderr %q", code, calls, errb.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/ghr/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: isTerminal`, `undefined: runTUI`, `undefined: fdIsTerminal` and `undefined: stdioIsTerminal`.

- [ ] **Step 3: Implement the launch**

In `cmd/ghr/main.go`, replace the import block with:

```go
import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/daemon"
	"github.com/darkraise/ghr/internal/tui"
)
```

Directly below the existing `var newClient = …` line, add:

```go
// runTUI, isTerminal and fdIsTerminal are replaced in tests.
var (
	runTUI       = tui.Run
	isTerminal   = stdioIsTerminal
	fdIsTerminal = term.IsTerminal
)

// stdioIsTerminal reports whether both stdin and stdout are terminals.
func stdioIsTerminal() bool {
	return fdIsTerminal(os.Stdin.Fd()) && fdIsTerminal(os.Stdout.Fd())
}

// openTUI runs the dashboard and returns the process exit code.
func openTUI(stderr io.Writer) int {
	if err := runTUI(newClient()); err != nil {
		fmt.Fprintln(stderr, "ghr:", err)
		return 1
	}
	return 0
}
```

In `run`, replace:

```go
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
```

with:

```go
	if len(args) == 0 {
		// Scripts and pipes keep the old usage-and-exit-2 behaviour.
		if isTerminal() {
			return openTUI(stderr)
		}
		fmt.Fprint(stderr, usage)
		return 2
	}
```

and replace the `tui` case:

```go
	case "tui":
		if err := tui.Run(newClient()); err != nil {
			fmt.Fprintln(stderr, "ghr tui:", err)
			return 1
		}
		return 0
```

with:

```go
	case "tui":
		return openTUI(stderr)
```

In the `usage` constant, replace its first three lines:

```
usage: ghr <command>

  daemon                          run the supervisor (systemd runs this)
  tui                             interactive dashboard
```

with:

```
usage: ghr [command]

  (no command)                    open the interactive dashboard
  daemon                          run the supervisor (systemd runs this)
  tui                             same as no command
```

Leave every other usage line unchanged.

- [ ] **Step 4: Make `x/term` a direct requirement**

Run: `go mod tidy`
Expected: `go.mod` now lists `github.com/charmbracelet/x/term v0.2.2` in the direct `require` block (no `// indirect`), and no other requirement changes version. Check with `git diff go.mod go.sum`; if any other module's version changed, run `git checkout go.mod go.sum` and instead edit `go.mod` by hand: move the `github.com/charmbracelet/x/term v0.2.2 // indirect` line into the first `require` block and delete ` // indirect`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/ghr/ -count=1 -timeout 180s -v`
Expected: PASS for `TestVersionAndUsage`, `TestBareGhrOpensTUIOnTerminal`, `TestStdioIsTerminalNeedsBoth`, `TestBareGhrPrintsUsageWithoutTerminal` and the existing CLI tests.

- [ ] **Step 6: Run the whole suite, the checks, and a real-binary smoke test**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

Run (Git Bash): `go build -o ghr-smoke.exe ./cmd/ghr && ./ghr-smoke.exe < /dev/null; echo "exit=$?"; rm -f ghr-smoke.exe`
Expected: the usage text starting `usage: ghr [command]`, then `exit=2` (stdin is not a terminal, so the TUI is not opened), and no `ghr-smoke.exe` left behind.

- [ ] **Step 7: Commit**

```bash
git add cmd/ghr/main.go cmd/ghr/main_test.go go.mod go.sum
git commit -m "feat(cli): open the dashboard on bare ghr"
```

### Task 3: Release and LXC acceptance

**Files:**
- None in ghr. Modify: `D:/Repositories/Personal/homelab/docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md` (row 1 state)

**Interfaces:**
- Consumes: **ConfigPatch**, **PatchConfig errors** and **CLI entry** (see Contracts), as merged from Tasks 1 and 2
- Produces: none

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

- [ ] **Step 1: Ask for approval to release**

Every push to ghr's `master` publishes a release that the LXC then runs. Ask your human partner, in one message: "Plan 1 passes all tests on `feat/settings-api-launch`. May I fast-forward `master`, push (publishing a release), and deploy that release to the LXC with `GHR_VERSION` pinned?" Wait for an explicit yes. If the answer is no, stop here: leave register row 1 at `planned`, and report that Task 3 awaits approval.

- [ ] **Step 2: Merge, push and wait for the release**

Run in `D:/Repositories/Personal/ghr`:

```bash
git checkout master && git merge --ff-only feat/settings-api-launch && timeout 120 git push origin master
RUN=$(timeout 60 gh run list --branch master --limit 1 --json databaseId -q '.[0].databaseId')
timeout 600 gh run watch "$RUN" --exit-status --interval 15 > /dev/null; echo "ci exit=$?"
timeout 60 gh release list --limit 1
```

Expected: `ci exit=0`, and the newest release is a new tag (one patch above the previous `v0.1.2`, for example `v0.1.3`). Call it `TAG` below. If CI fails, stop and report the failing run; do not deploy.

- [ ] **Step 3: Deploy the pinned release**

Run, replacing `TAG`:

```bash
timeout 590 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'cd /root/github-runner && GHR_VERSION=TAG timeout 570 bash setup.sh > /root/setup-TAG.log 2>&1; echo rc=$?; tail -8 /root/setup-TAG.log; systemctl is-active ghr'
```

Expected: `rc=0`, a log line `installing ghr (TAG)`, a status table listing the repos, and `active`.

- [ ] **Step 4: Bare `ghr` on a real terminal and without one**

Run:

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
tmux new-session -d -s ghrcheck -x 120 -y 40 ghr
timeout 15 bash -c "until tmux capture-pane -p -t ghrcheck | grep -q Dashboard; do :; done"; echo found=$?
tmux capture-pane -p -t ghrcheck | head -6
tmux send-keys -t ghrcheck q
timeout 5 bash -c "while tmux has-session -t ghrcheck 2>/dev/null; do :; done"; echo quit=$?
tmux kill-session -t ghrcheck 2>/dev/null
ghr < /dev/null > /tmp/ghr-usage.txt 2>&1; echo bare=$?; head -1 /tmp/ghr-usage.txt; rm -f /tmp/ghr-usage.txt'
```

Expected: `found=0` (the dashboard rendered under tmux's real terminal), the first lines of the dashboard with the `ghr` header box and the `Dashboard` tab, `quit=0` (`q` closed it), then `bare=2` and `usage: ghr [command]` (no terminal on stdin, so usage instead of the TUI).

- [ ] **Step 5: Patch settings over the real socket, then restore them**

Run:

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
S="curl -s --unix-socket /run/ghr/ghr.sock"
grep -E "^(poll_interval|disk_high_water):" /etc/ghr/config.yaml
before=$(sha256sum /etc/ghr/config.yaml)
$S -o /dev/null -w "bad-disk=%{http_code}\n" -X PATCH http://ghr/config -d "{\"disk_high_water\":0}"
$S -w " bad-poll=%{http_code}\n" -X PATCH http://ghr/config -d "{\"poll_interval\":\"soon\"}"
[ "$(sha256sum /etc/ghr/config.yaml)" = "$before" ] && echo unchanged=yes || echo unchanged=NO
$S -o /dev/null -w "valid=%{http_code}\n" -X PATCH http://ghr/config -d "{\"poll_interval\":\"15s\",\"disk_high_water\":85}"
grep -E "^(poll_interval|disk_high_water):" /etc/ghr/config.yaml
$S -o /dev/null -w "restore=%{http_code}\n" -X PATCH http://ghr/config -d "{\"poll_interval\":\"10s\",\"disk_high_water\":80}"
grep -E "^(poll_interval|disk_high_water):" /etc/ghr/config.yaml'
```

Expected, in order:
- the original values `poll_interval: 10s` and `disk_high_water: 80` (if they differ, stop before the valid patch and use the values printed here in the restore line instead);
- `bad-disk=400`;
- `{"error":"poll_interval: invalid duration \"soon\""}` followed by ` bad-poll=400`;
- `unchanged=yes`;
- `valid=204`, then `poll_interval: 15s` and `disk_high_water: 85`;
- `restore=204`, then `poll_interval: 10s` and `disk_high_water: 80` again.

- [ ] **Step 6: Record the result**

Run in `D:/Repositories/Personal/homelab`, replacing `TAG`:

```bash
bash /d/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/scripts/register set docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md 1 done --note "ghr TAG on the LXC: bare ghr opens the dashboard under tmux, prints usage (exit 2) without a terminal; settings API verified over the socket and restored"
git add docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md
git commit -m "docs(github-runner): record ghr TAG acceptance"
```

Expected: `register` exits 0 and the commit succeeds. Do not push homelab.
