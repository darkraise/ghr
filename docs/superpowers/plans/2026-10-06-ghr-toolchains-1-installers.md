# ghr toolchains 1: installers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `internal/toolchain` package: five installers (Node, Go, Python, Java, .NET) that list, resolve, install and remove toolchains in the shared tool cache, in exactly the layout each `setup-*` action reads, plus the `Set` registry and `popular` preset the daemon will use.

**Architecture:** One `Env` value carries the tool cache root, the runner user, the upstream URLs and every host seam (HTTP get, download, command runner, extractor), so tests run on Windows with an `httptest` server and a fake command runner. A shared archive routine (download → checksum → extract → chown → rename → marker last) serves Node, Go and Java; Python runs the archive's own `setup.sh` as the runner user; .NET runs `dotnet-install.sh`. Tests read every install back with code ported from the actions (`@actions/tool-cache` `find()`, setup-java `findInToolcache`, setup-dotnet's local SDK scan). Nothing outside the package changes; plan 2 wires it into the daemon.

**Tech Stack:** Go 1.26 (go.mod), standard library only (`archive/tar` and `compress/gzip` in tests); `internal/github.ParseVersion`; `internal/system.Download` and `system.Runner`.

**Spec:** docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md

**Execution:** inline — `claude --model sonnet --effort medium` — no task is heavy (highest total 4, no risk 3), so the session implements all nine itself; each task's `**Implementer:**` line records its score and names the agent only if that task is ever delegated. Effort follows the highest self-implemented total (4 → impl-sonnet-medium).

**Plan review:** 2026-10-06 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 17 / assumptions 17 (round 2)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr), branch `feat/toolchains` from master `9216e09`. The plan, spec and register live in `D:/Repositories/Personal/homelab`.
- This plan is part 1 of 3 for the spec: 1 installers (this plan), 2 daemon, API, CLI and `setup.sh`, 3 TUI, release and LXC acceptance. This plan changes nothing outside `internal/toolchain`.
- Every command runs from the ghr repository root in Git Bash with an explicit `timeout` (git and gh commands included). Package tests: `timeout 300 go test ./internal/toolchain/`. Before each commit also run `timeout 120 gofmt -l internal/toolchain` (must print nothing) and `timeout 300 go vet ./internal/toolchain/`. CI (`.github/workflows/ci.yml`) runs `go test -race -count=1 -timeout 180s ./...` on ubuntu, triggered only by pushes to `master` and by pull requests, so this branch gets CI through a draft pull request (Task 9). `-race` cannot run on this Windows box (no cgo), so CI is its only run.
- Tests that need Unix file modes or a real `tar` skip on Windows (`runtime.GOOS == "windows"`); CI (ubuntu) is their run of record.
- Edit Go files with the Edit or Write tools. Bash heredocs and inline `python -` lose backslashes.
- Comments: none unless the why is non-obvious; never reference this plan, a task or a review.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period, scope `toolchain`. One commit per task.
- Tool names ghr uses (API and CLI): `node`, `go`, `python`, `java`, `dotnet`. Tool cache folders the actions read: `node`, `go`, `Python`, `Java_Temurin-Hotspot_jdk`, `dotnet`. Architecture folder: `x64`; marker: `<version>/x64.complete`.
- The `popular` preset, in order: node `22`, node `24`, dotnet `8.0`, dotnet `10.0`, python `3.13`, python `3.14`, go `latest`, java `21`, java `25`.
- `Available` results are cached per tool for 1 hour; errors are not cached.
- Upstream URLs (spec §1): Node manifest `https://raw.githubusercontent.com/actions/node-versions/main/versions-manifest.json`; Go manifest `https://raw.githubusercontent.com/actions/go-versions/main/versions-manifest.json`; Python manifest `https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json`; Go releases `https://go.dev/dl/?mode=json&include=all`; Go archives `https://go.dev/dl/go<ver>.linux-amd64.tar.gz`; Adoptium `https://api.adoptium.net`; .NET index `https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json`; .NET script `https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh`.
- `<Root>/.tmp` is mode 0711, each operation directory under it 0700, both root-owned; everything installed is chowned to the runner user with `chown -h` (never following a symlink).
- Python's `setup.sh` runs as the runner user with `RUNNER_TOOL_CACHE=<Root>`, `LD_LIBRARY_PATH=<extracted>/lib`, `HOME=<runner home>`, and `AGENT_TOOLSDIRECTORY` unset. `dotnet-install.sh` runs as the runner user with `--version <sdk> --install-dir <Root>/dotnet --skip-non-versioned-files` and `HOME=<runner home>`.

## Contracts

**C1 Package API (`internal/toolchain/toolchain.go`, Task 1)** — consumed by every later task and by plan 2:

```go
var (
	ErrUnknownTool      = errors.New("unknown toolchain")
	ErrNotInstalled     = errors.New("toolchain version not installed")
	ErrAlreadyInstalled = errors.New("toolchain version already installed")
)

type Sources struct {
	NodeManifest, GoManifest, PythonManifest string
	GoReleases   string // go.dev JSON release list with SHA-256s
	GoDownload   string // prefix of go.dev archive URLs, ending in "/"
	Adoptium     string // API base without /v3
	DotnetIndex  string
	DotnetScript string
}

type Env struct {
	Root    string // the tool cache (RUNNER_TOOL_CACHE)
	Home    string // the runner user's home
	User    string // the runner user
	Sources Sources
	Get     func(ctx context.Context, url string) ([]byte, error)
	Fetch   func(ctx context.Context, url, dst string) error
	Run     func(ctx context.Context, name string, args ...string) ([]byte, error)
	Extract func(ctx context.Context, archive, dir string) error
	OpID    func() string
}

type Release struct{ Tool, Version, Folder, URL, SHA256 string }
type Choice struct {
	Spec    string `json:"spec"`
	Version string `json:"version"`
	LTS     bool   `json:"lts,omitempty"`
}
type Installed struct {
	Tool, Version, Arch, Path string
	InstalledAt               time.Time
}
type Installer interface {
	Available(ctx context.Context) ([]Choice, error)
	Resolve(ctx context.Context, spec string) (Release, error)
	Install(ctx context.Context, rel Release, progress func(step string)) error // ErrAlreadyInstalled when complete
	Installed() ([]Installed, error)
	Remove(version string) error // ErrNotInstalled when absent
}
```

**C2 Unexported helpers (Tasks 1, 2, 5)** — used by the installer tasks:

```go
const arch = "x64"
func pick(vs []string, spec string) (string, bool)  // "latest" or leading-parts match, newest wins (Task 1)
func newestFirst(vs []string)                         // Task 1
func looseCompare(a, b string) int                    // Task 1
func makeSemver(v string) string                      // "1.25" → "1.25.0" (Task 1)
func validFolder(name string) bool                    // Task 1
func identity(s string) string                        // Task 1
func marker(dir string) string                        // dir + ".complete" (Task 1)
func exists(p string) bool                            // Task 1
func (e *Env) versionDir(toolDir, folder string) string // <Root>/<toolDir>/<folder>/x64 (Task 1)
func (e *Env) has(toolDir, folder string) bool        // Task 1
func (e *Env) listLayout(tool, toolDir string, show func(folder string) string) ([]Installed, error) // Task 1
func (e *Env) removeLayout(toolDir, folder string) error // Task 1
func (e *Env) opDir() (string, error)                 // Task 2
func (e *Env) CleanTmp() error                        // exported, Task 2
func (e *Env) chownTree(ctx context.Context, path string) error // Task 2
func (e *Env) mkdirOwned(ctx context.Context, dir string) error  // Task 2
func (e *Env) installArchive(ctx context.Context, toolDir string, rel Release, progress func(string)) error // Task 2
func removeIncomplete(dir string)                     // x64 folder and marker (Task 2)
func dropFailed(dir string)                           // removeIncomplete plus the version folder when empty (Task 2)
func (e *Env) runAs(ctx context.Context, dir string, env []string, script string, args ...string) ([]byte, error) // Task 5
```

**C3 Installer constructors (Tasks 3–7), consumed by Task 8:** `const javaDir = "Java_Temurin-Hotspot_jdk"` (Task 6); `newNode(e *Env) Installer`, `newGo(e *Env) Installer`, `newPython(e *Env) Installer`, `newJava(e *Env) Installer`, `newDotnet(e *Env) Installer`. Shared manifest code `manifestTool` (Task 3) is embedded by node, golang and python.

**C4 Set (`set.go`, Task 8)** — consumed by plan 2:

```go
type Entry struct{ Tool, Spec string }
var Popular []Entry
func New(e *Env) *Set
func (s *Set) Tools() []string                 // sorted
func (s *Set) Get(tool string) (Installer, error) // wraps ErrUnknownTool
func (s *Set) Available(ctx context.Context, tool string) ([]Choice, error)
func (s *Set) Installed() ([]Installed, error)
func (s *Set) Other() ([]string, error)        // unowned top-level tool-cache folder names, sorted
func (s *Set) Root() string
func (s *Set) CleanTmp() error
```

**C5 Production Env (`env.go`, Task 9)** — consumed by plan 2: `func DefaultSources() Sources`, `func NewEnv(root, home, user string, run system.Runner) *Env`.

**C6 Test helpers (`helpers_test.go`, Tasks 1–2)** — used by every test task: `mkInstall`, `toolCacheFind`, `javaFind`, `dotnetFind` (Task 1); `fixture`, `newFixture`, `(*fixture).serve`, `(*fixture).hitCount`, `host`, `(*host).commands`, `tarGz`, `extractTarGz`, `sum256`, `testGet`, `tmpEntries` (Task 2).

## Assumptions (evidence)

- `@actions/tool-cache` `find()` accepts `<cache>/<tool>/<ver>/<arch>` only with the sibling `<arch>.complete` and a `semver.valid(semver.clean(<ver>))` folder; a non-explicit spec picks the highest satisfying cached version — `packages/tool-cache/src/tool-cache.ts`, read 2026-10-06 (Fable review, docs/superpowers/notes/2026-10-06-ghr-toolchains-caches-fable-review.md "Claims verified correct").
- setup-java caches under `Java_Temurin-Hotspot_jdk/<version_data.semver with + → ->/<arch>` and reads back with `findInToolcache`, converting `-` back to `+` before `semver.satisfies` — `src/distributions/temurin/installer.ts`, `src/util.ts`, Fable review finding 1, 2026-10-06.
- setup-dotnet finds local SDKs by reading `DOTNET_INSTALL_DIR/sdk/*` and checking each holds `dotnet.dll` — `src/installer.ts` `getInstalledSdkVersions`, Fable review finding 2. The version-matching rule ported in `dotnetFind` (x-range prefix match) is inferred.
- `dotnet-install.sh --skip-non-versioned-files` skips non-versioned files only when they already exist — the script's help text, Fable review finding 2.
- python-versions archives hold `setup.sh` at their root, need `LD_LIBRARY_PATH=<extracted>/lib`, and the script prefers `AGENT_TOOLSDIRECTORY` over `RUNNER_TOOL_CACHE` — setup-python `src/install-python.ts`, python-versions `installers/nix-setup-template.sh`, Fable review finding 3.
- setup-go resolves `stable` from the go-versions manifest before looking in the cache, and names folders with `makeSemver` — setup-go `src/installer.ts`, Fable review finding 7.
- Archive top folders: setup-node, setup-go and setup-java cache the archive's single top folder's contents (setup-node `--strip 1`, setup-go `path.join(extPath, 'go')`, setup-java `readdirSync(extracted)[0]`); the plan's `singleRoot` descends into a lone top directory, which covers those archives and leaves a root without one as is.
- Node, Go and Python manifest field names (`version`, `stable`, `files[].filename|arch|platform|platform_version|download_url`) — unverified for Node; Task 3 verifies with a live `curl`. Python and Go were read live in the Fable review.
- Adoptium `feature_releases/<major>/ga` returns `version_data.semver` and `binaries[].package.link|checksum`, and `/v3/info/available_releases` returns `available_releases` and `available_lts_releases` — the first verified live in the Fable review; the `page_size`/`sort_order` parameters and `available_releases` are unverified — Task 6 verifies them with a live `curl`.
- `releases-index.json` fields `channel-version`, `latest-sdk`, `support-phase`, `release-type` — verified live in the Fable review.
- `internal/github.ParseVersion` accepts only dot-separated digits (with an optional leading `v`) — `internal/github/releases.go:59-76`.
- `system.Download(ctx, url, dst)` fetches without credentials and errors on a non-200 — `internal/system/system.go` (`Download`).
- `/var/lib/ghr/toolcache` is created owned by `ghrunner`, mode 0755 — `github-runner/setup.sh:73` (`install -d -m 0755 -o "$RUNNER_USER" …/toolcache`). Python's `setup.sh` and `dotnet-install.sh` run as `ghrunner` and create their version folders inside it; the installers still create `<Root>/Python` and `<Root>/dotnet` with `mkdirOwned` first, so they do not depend on that.
- .NET `Installed()` and `Remove` list and remove only SDK folders named as a three-part version; a preview SDK a job installed (`11.0.100-rc.2…`) is neither listed nor removable here (accepted limitation, plan review round 2).
- No task carries an `**Items:**` line: register rows 1–4 (docs/superpowers/registers/2026-10-06-ghr-toolchains-caches.md) are discharged by plan 3's release and LXC acceptance task, which is where their acceptance criteria can be observed; this plan builds the library they rest on.
- `system.Runner` is `func(ctx, name string, args ...string) ([]byte, error)`, and `system.ExecGroup` kills the whole process group on cancel and puts stderr in the error — `internal/system/system.go:21,36-60`.

## Task index

1. Package types, version helpers and layout listing
2. Archive install routine
3. Node installer
4. Go installer
5. Python installer
6. Java installer
7. .NET installer
8. Set registry and popular preset
9. Production environment

---

### Task 1: Package types, version helpers and layout listing

**Files:**
- Create: `internal/toolchain/toolchain.go`
- Create: `internal/toolchain/layout.go`
- Create: `internal/toolchain/helpers_test.go`
- Create: `internal/toolchain/toolchain_test.go`

**Interfaces:**
- Consumes: `github.ParseVersion`, `github.Version.Compare`.
- Produces: C1, the Task 1 entries of C2, the Task 1 entries of C6.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Create the branch**

```bash
cd D:/Repositories/Personal/ghr && timeout 60 git switch master && timeout 120 git pull --ff-only && timeout 60 git switch -c feat/toolchains
```

Expected: `Switched to a new branch 'feat/toolchains'`, HEAD at or after `9216e09`.

- [ ] **Step 2: Write the test helpers**

Create `internal/toolchain/helpers_test.go`:

```go
package toolchain

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// mkInstall creates <root>/<toolDir>/<folder>/x64 holding one file, with the
// .complete marker when complete is true.
func mkInstall(t *testing.T, root, toolDir, folder string, complete bool) string {
	t.Helper()
	dir := filepath.Join(root, toolDir, folder, "x64")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if complete {
		if err := os.WriteFile(dir+".complete", nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// semver is the part of node-semver the ported finders need.
type semver struct {
	nums       [3]int
	pre, build string
}

// parseSemver accepts what node-semver's valid(clean(s)) accepts for the
// folder names in question: three numeric parts without leading zeros, an
// optional -prerelease and an optional +build.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var v semver
	if i := strings.IndexByte(s, '+'); i >= 0 {
		v.build, s = s[i+1:], s[:i]
		if v.build == "" {
			return semver{}, false
		}
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre, s = s[i+1:], s[:i]
		if v.pre == "" {
			return semver{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || p == "" || (len(p) > 1 && p[0] == '0') || n < 0 {
			return semver{}, false
		}
		v.nums[i] = n
	}
	return v, true
}

// satisfies is node-semver's satisfies(v, spec) for an x-range spec such as
// "22", "3.13", "8.0.x" or "1.25.1": a prerelease never satisfies it and
// build metadata is ignored.
func satisfies(v semver, spec string) bool {
	if v.pre != "" {
		return false
	}
	for i, p := range strings.Split(spec, ".") {
		if p == "x" || p == "X" || p == "*" {
			return true
		}
		n, err := strconv.Atoi(p)
		if err != nil || i > 2 || v.nums[i] != n {
			return false
		}
	}
	return true
}

func semverLess(a, b semver) bool {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			return a.nums[i] < b.nums[i]
		}
	}
	return a.pre != "" && b.pre == ""
}

func dirNames(dir string) []string {
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// toolCacheFind ports @actions/tool-cache find(tool, spec, "x64"): an
// explicit version is looked up directly; otherwise the highest complete
// cached version satisfying spec wins. It returns "" when nothing matches.
func toolCacheFind(root, tool, spec string) string {
	if _, ok := parseSemver(spec); ok {
		p := filepath.Join(root, tool, spec, "x64")
		if exists(p) && exists(p+".complete") {
			return p
		}
		return ""
	}
	best, bestName := semver{}, ""
	for _, name := range dirNames(filepath.Join(root, tool)) {
		v, ok := parseSemver(name)
		p := filepath.Join(root, tool, name, "x64")
		if !ok || !exists(p) || !exists(p+".complete") || !satisfies(v, spec) {
			continue
		}
		if bestName == "" || semverLess(best, v) {
			best, bestName = v, name
		}
	}
	if bestName == "" {
		return ""
	}
	return filepath.Join(root, tool, bestName, "x64")
}

// javaFind ports setup-java's findInToolcache for Temurin: tool-cache's
// findAllVersions, then the folder's first "-" turned back into "+" before
// isVersionSatisfies.
func javaFind(root, spec string) string {
	tool := "Java_Temurin-Hotspot_jdk"
	best, bestName := semver{}, ""
	for _, name := range dirNames(filepath.Join(root, tool)) {
		p := filepath.Join(root, tool, name, "x64")
		if _, ok := parseSemver(name); !ok || !exists(p) || !exists(p+".complete") {
			continue
		}
		v, ok := parseSemver(strings.Replace(name, "-", "+", 1))
		if !ok || !satisfies(v, spec) {
			continue
		}
		if bestName == "" || semverLess(best, v) {
			best, bestName = v, name
		}
	}
	if bestName == "" {
		return ""
	}
	return filepath.Join(root, tool, bestName, "x64")
}

// dotnetFind ports setup-dotnet's local SDK scan: an SDK under
// <dotnetDir>/sdk counts when it holds dotnet.dll and satisfies spec.
func dotnetFind(dotnetDir, spec string) string {
	for _, name := range dirNames(filepath.Join(dotnetDir, "sdk")) {
		v, ok := parseSemver(name)
		if ok && satisfies(v, spec) && exists(filepath.Join(dotnetDir, "sdk", name, "dotnet.dll")) {
			return filepath.Join(dotnetDir, "sdk", name)
		}
	}
	return ""
}
```

- [ ] **Step 3: Write the failing tests**

Create `internal/toolchain/toolchain_test.go`:

```go
package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPortedFinders(t *testing.T) {
	root := t.TempDir()
	mkInstall(t, root, "node", "22.11.0", true)
	mkInstall(t, root, "node", "22.12.0", false)
	mkInstall(t, root, "node", "24.1.0", true)
	if got := toolCacheFind(root, "node", "22"); got != filepath.Join(root, "node", "22.11.0", "x64") {
		t.Fatalf("22 found %q; a folder without a marker must not count", got)
	}
	if got := toolCacheFind(root, "node", "24.1.0"); got == "" {
		t.Fatal("explicit 24.1.0 not found")
	}
	mkInstall(t, root, "Java_Temurin-Hotspot_jdk", "21.0.8-9", true)
	mkInstall(t, root, "Java_Temurin-Hotspot_jdk", "21.0.12.1-1", true)
	if got := toolCacheFind(root, "Java_Temurin-Hotspot_jdk", "21"); got != "" {
		t.Fatalf("tool-cache find accepted a prerelease folder: %q", got)
	}
	if got := javaFind(root, "21"); got != filepath.Join(root, "Java_Temurin-Hotspot_jdk", "21.0.8-9", "x64") {
		t.Fatalf("javaFind 21 found %q; a four-part folder is not semver", got)
	}
	dn := filepath.Join(root, "dotnet")
	if err := os.MkdirAll(filepath.Join(dn, "sdk", "8.0.100"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := dotnetFind(dn, "8.0.x"); got != "" {
		t.Fatalf("an SDK folder without dotnet.dll counted: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dn, "sdk", "8.0.100", "dotnet.dll"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := dotnetFind(dn, "8.0.x"); got == "" {
		t.Fatal("8.0.x not found")
	}
}

func TestPick(t *testing.T) {
	vs := []string{"22.11.0", "24.9.0", "22.9.1", "1.25", "3.13.7", "3.14.0"}
	cases := []struct{ spec, want string }{
		{"latest", "24.9.0"},
		{"22", "22.11.0"},
		{"22.9", "22.9.1"},
		{"22.9.1", "22.9.1"},
		{"3.13", "3.13.7"},
		{"1.25", "1.25"},
		{"20", ""},
		{"x", ""},
	}
	for _, c := range cases {
		got, ok := pick(vs, c.spec)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("pick(%q) = %q, %v; want %q", c.spec, got, ok, c.want)
		}
	}
}

func TestNewestFirstAndLooseCompare(t *testing.T) {
	vs := []string{"21.0.8+9", "21.0.12+101.0.LTS", "8.0.414", "10.0.105", "9.0.311"}
	newestFirst(vs)
	want := []string{"21.0.12+101.0.LTS", "21.0.8+9", "10.0.105", "9.0.311", "8.0.414"}
	if !reflect.DeepEqual(vs, want) {
		t.Fatalf("newestFirst = %v", vs)
	}
}

func TestMakeSemver(t *testing.T) {
	for in, want := range map[string]string{"1.25": "1.25.0", "1.25.1": "1.25.1", "1": "1.0.0"} {
		if got := makeSemver(in); got != want {
			t.Errorf("makeSemver(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidFolder(t *testing.T) {
	for name, want := range map[string]bool{"22.11.0": true, "": false, ".": false, "..": false, "a/b": false, `a\b`: false} {
		if got := validFolder(name); got != want {
			t.Errorf("validFolder(%q) = %v", name, got)
		}
	}
}

func TestListLayout(t *testing.T) {
	e := &Env{Root: t.TempDir()}
	if got, err := e.listLayout("node", "node", identity); err != nil || got != nil {
		t.Fatalf("missing tool folder: %v, %v", got, err)
	}
	mkInstall(t, e.Root, "node", "22.11.0", true)
	mkInstall(t, e.Root, "node", "24.9.0", true)
	mkInstall(t, e.Root, "node", "23.0.0", false)
	if err := os.WriteFile(filepath.Join(e.Root, "node", "stray"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := e.listLayout("node", "node", identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Version != "24.9.0" || got[1].Version != "22.11.0" {
		t.Fatalf("listed %+v", got)
	}
	g := got[0]
	if g.Tool != "node" || g.Arch != "x64" || g.Path != filepath.Join(e.Root, "node", "24.9.0", "x64") || g.InstalledAt.IsZero() {
		t.Fatalf("entry %+v", g)
	}
}

func TestRemoveLayout(t *testing.T) {
	e := &Env{Root: t.TempDir()}
	dir := mkInstall(t, e.Root, "node", "22.11.0", true)
	if err := e.removeLayout("node", "22.11.0"); err != nil {
		t.Fatal(err)
	}
	if exists(dir) || exists(dir+".complete") || exists(filepath.Dir(dir)) {
		t.Fatal("version left behind")
	}
	if err := e.removeLayout("node", "22.11.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
	if err := e.removeLayout("node", ".."); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("remove ..: %v", err)
	}
	dir = mkInstall(t, e.Root, "node", "24.9.0", true)
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "x86.complete"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.removeLayout("node", "24.9.0"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(filepath.Dir(dir), "x86.complete")) {
		t.Fatal("a version folder holding other files must be kept")
	}
}

func TestRemoveLayoutDeletesTheMarkerFirst(t *testing.T) {
	e := &Env{Root: t.TempDir()}
	dir := mkInstall(t, e.Root, "node", "22.11.0", false)
	// A non-empty directory in place of the marker makes deleting it fail,
	// which shows whether the version was touched before the marker.
	if err := os.MkdirAll(filepath.Join(dir+".complete", "stuck"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.removeLayout("node", "22.11.0"); err == nil {
		t.Fatal("no error deleting a marker that cannot be deleted")
	}
	if !exists(filepath.Join(dir, "file")) {
		t.Fatal("the version was deleted before its marker")
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/`
Expected: FAIL to compile — `undefined: Env`, `undefined: pick`, and the other names the next step adds.

- [ ] **Step 5: Write the package types and helpers**

Create `internal/toolchain/toolchain.go`:

```go
// Package toolchain installs and lists toolchains in the shared GitHub
// Actions tool cache, in the layout each setup-* action reads before it
// downloads anything.
package toolchain

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/darkraise/ghr/internal/github"
)

const arch = "x64"

var (
	ErrUnknownTool      = errors.New("unknown toolchain")
	ErrNotInstalled     = errors.New("toolchain version not installed")
	ErrAlreadyInstalled = errors.New("toolchain version already installed")
)

// Sources are the upstream URLs the installers read.
type Sources struct {
	NodeManifest   string
	GoManifest     string
	PythonManifest string
	GoReleases     string // go.dev's JSON release list, with SHA-256s
	GoDownload     string // prefix of go.dev archive URLs, ending in "/"
	Adoptium       string // API base, without /v3
	DotnetIndex    string
	DotnetScript   string
}

// Env is what the installers need from the host.
type Env struct {
	Root    string // the tool cache (RUNNER_TOOL_CACHE)
	Home    string // the runner user's home
	User    string // the runner user, owner of everything installed
	Sources Sources
	Get     func(ctx context.Context, url string) ([]byte, error)
	Fetch   func(ctx context.Context, url, dst string) error
	Run     func(ctx context.Context, name string, args ...string) ([]byte, error)
	Extract func(ctx context.Context, archive, dir string) error
	OpID    func() string
}

// Release is one resolved toolchain version, ready to install.
type Release struct {
	Tool    string
	Version string // as shown
	Folder  string // the version folder in the tool cache
	URL     string
	SHA256  string // empty when the source publishes none
}

// Choice is one entry the Install dialog offers: Spec is what an install
// request takes, Version what the picker shows.
type Choice struct {
	Spec    string `json:"spec"`
	Version string `json:"version"`
	LTS     bool   `json:"lts,omitempty"`
}

type Installed struct {
	Tool        string
	Version     string
	Arch        string
	Path        string
	InstalledAt time.Time
}

type Installer interface {
	Available(ctx context.Context) ([]Choice, error)
	Resolve(ctx context.Context, spec string) (Release, error)
	// Install returns ErrAlreadyInstalled when rel is already complete.
	Install(ctx context.Context, rel Release, progress func(step string)) error
	Installed() ([]Installed, error)
	// Remove returns ErrNotInstalled for a version that is not installed.
	Remove(version string) error
}

func prefixOf(p, v github.Version) bool {
	if len(p) > len(v) {
		return false
	}
	for i := range p {
		if p[i] != v[i] {
			return false
		}
	}
	return true
}

// pick returns the newest of vs matching spec: "latest", or a version whose
// leading parts equal spec's ("22" matches 22.11.0, "3.13" matches 3.13.7).
func pick(vs []string, spec string) (string, bool) {
	want, ok := github.ParseVersion(spec)
	if spec != "latest" && !ok {
		return "", false
	}
	best, bestV := "", github.Version(nil)
	for _, v := range vs {
		pv, ok := github.ParseVersion(v)
		if !ok || (spec != "latest" && !prefixOf(want, pv)) {
			continue
		}
		if best == "" || pv.Compare(bestV) > 0 {
			best, bestV = v, pv
		}
	}
	return best, best != ""
}

func newestFirst(vs []string) {
	sort.SliceStable(vs, func(i, j int) bool { return looseCompare(vs[i], vs[j]) > 0 })
}

// looseCompare orders by the runs of digits in a and b, so Java's
// 21.0.12+101.0.LTS sorts after 21.0.8+9.
func looseCompare(a, b string) int {
	na, nb := numbers(a), numbers(b)
	for i := range max(len(na), len(nb)) {
		x, y := 0, 0
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(a, b)
}

func numbers(s string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// makeSemver pads a stable Go version to three parts, as setup-go's
// makeSemver does (1.25 → 1.25.0).
func makeSemver(v string) string {
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

// validFolder reports whether name is one path element ghr may use as a
// version folder.
func validFolder(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

func identity(s string) string { return s }
```

Create `internal/toolchain/layout.go`:

```go
package toolchain

import (
	"os"
	"path/filepath"
	"sort"
)

func marker(dir string) string { return dir + ".complete" }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (e *Env) versionDir(toolDir, folder string) string {
	return filepath.Join(e.Root, toolDir, folder, arch)
}

// has reports whether folder is a complete install: its x64 folder and its
// marker both exist.
func (e *Env) has(toolDir, folder string) bool {
	dir := e.versionDir(toolDir, folder)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	_, err = os.Stat(marker(dir))
	return err == nil
}

// listLayout lists the complete installs under <Root>/<toolDir>, newest
// first, showing each folder through show.
func (e *Env) listLayout(tool, toolDir string, show func(folder string) string) ([]Installed, error) {
	es, err := os.ReadDir(filepath.Join(e.Root, toolDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, v := range es {
		if !v.IsDir() || !e.has(toolDir, v.Name()) {
			continue
		}
		dir := e.versionDir(toolDir, v.Name())
		fi, err := os.Stat(marker(dir))
		if err != nil {
			continue
		}
		out = append(out, Installed{Tool: tool, Version: show(v.Name()), Arch: arch, Path: dir, InstalledAt: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return looseCompare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// removeLayout deletes the marker first, so a job starting meanwhile no
// longer finds the version, then the version itself.
func (e *Env) removeLayout(toolDir, folder string) error {
	if !validFolder(folder) || !e.has(toolDir, folder) {
		return ErrNotInstalled
	}
	dir := e.versionDir(toolDir, folder)
	if err := os.Remove(marker(dir)); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Fails, and keeps the folder, while it holds anything else.
	os.Remove(filepath.Dir(dir))
	return nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, then no gofmt output and no vet output.

- [ ] **Step 7: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add tool cache layout helpers"
```

### Task 2: Archive install routine

**Files:**
- Modify: `internal/toolchain/layout.go` (append)
- Modify: `internal/toolchain/helpers_test.go` (append)
- Create: `internal/toolchain/install_test.go`

**Interfaces:**
- Consumes: C1, the Task 1 entries of C2 and C6.
- Produces: the Task 2 entries of C2 (`opDir`, `CleanTmp`, `chownTree`, `mkdirOwned`, `installArchive`, `removeIncomplete`, `dropFailed`) and of C6 (`fixture`, `newFixture`, `serve`, `hitCount`, `host`, `commands`, `tarGz`, `extractTarGz`, `sum256`, `testGet`, `tmpEntries`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Add the fixture helpers**

Append to `internal/toolchain/helpers_test.go`, and add `"archive/tar"`, `"bytes"`, `"compress/gzip"`, `"context"`, `"crypto/sha256"`, `"encoding/hex"`, `"fmt"`, `"io"`, `"net/http"`, `"net/http/httptest"`, `"slices"`, `"sort"`, `"sync"` and `"github.com/darkraise/ghr/internal/system"` to its imports:

```go
// host fakes the commands an Env runs: chown is recorded, runuser runs the
// fake registered for the script it names.
type host struct {
	mu          sync.Mutex
	calls       []string
	scripts     map[string]func(dir string, env map[string]string, args []string) error
	failExtract error
}

func (h *host) run(_ context.Context, name string, args ...string) ([]byte, error) {
	h.mu.Lock()
	h.calls = append(h.calls, name+" "+strings.Join(args, " "))
	h.mu.Unlock()
	switch name {
	case "chown":
		return nil, nil
	case "runuser":
		return nil, h.runAs(args)
	}
	return nil, fmt.Errorf("unexpected command %s", name)
}

// runAs reads the command line Env.runAs builds:
// runuser -u USER -- env -u AGENT_TOOLSDIRECTORY K=V... bash -c CMD DIR SCRIPT ARGS...
func (h *host) runAs(args []string) error {
	e, b := slices.Index(args, "env"), slices.Index(args, "bash")
	if e < 0 || b < e+3 || len(args) < b+5 {
		return fmt.Errorf("unexpected runuser arguments %q", args)
	}
	env := map[string]string{}
	for _, kv := range args[e+3 : b] {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	f, ok := h.scripts[args[b+4]]
	if !ok {
		return fmt.Errorf("no fake for %s", args[b+4])
	}
	return f(args[b+3], env, args[b+5:])
}

func (h *host) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

// fixture is an Env whose sources point at a local HTTP server and whose
// commands go to a fake host.
type fixture struct {
	*Env
	host  *host
	url   string
	mu    sync.Mutex
	files map[string][]byte // by request URI
	hits  map[string]int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{host: &host{scripts: map[string]func(string, map[string]string, []string) error{}},
		files: map[string][]byte{}, hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		b, ok := f.files[r.URL.RequestURI()]
		f.hits[r.URL.RequestURI()]++
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	n := 0
	f.Env = &Env{
		Root: t.TempDir(),
		Home: "/home/ghrunner",
		User: "ghrunner",
		Sources: Sources{
			NodeManifest:   srv.URL + "/node.json",
			GoManifest:     srv.URL + "/go.json",
			PythonManifest: srv.URL + "/python.json",
			GoReleases:     srv.URL + "/godl.json",
			GoDownload:     srv.URL + "/dl/",
			Adoptium:       srv.URL,
			DotnetIndex:    srv.URL + "/dotnet-index.json",
			DotnetScript:   srv.URL + "/dotnet-install.sh",
		},
		Get:   testGet,
		Fetch: system.Download,
		Run:   f.host.run,
		Extract: func(_ context.Context, archive, dir string) error {
			if f.host.failExtract != nil {
				return f.host.failExtract
			}
			return extractTarGz(archive, dir)
		},
		OpID: func() string { n++; return fmt.Sprintf("op%d", n) },
	}
	return f
}

func (f *fixture) serve(uri string, body []byte) {
	f.mu.Lock()
	f.files[uri] = body
	f.mu.Unlock()
}

func (f *fixture) hitCount(uri string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[uri]
}

func testGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// tarGz builds a .tar.gz holding files (path → content), parents implied.
func tarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(files[n])), Typeflag: tar.TypeReg})
		tw.Write([]byte(files[n]))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func extractTarGz(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(h.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o755); err != nil {
			return err
		}
	}
}

func sum256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func tmpEntries(t *testing.T, root string) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/toolchain/install_test.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func demoRelease(f *fixture, body []byte) Release {
	f.serve("/demo.tar.gz", body)
	return Release{Tool: "demo", Version: "1.2.3", Folder: "1.2.3", URL: f.url + "/demo.tar.gz", SHA256: sum256(body)}
}

func TestInstallArchiveWritesTheToolCacheLayout(t *testing.T) {
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"demo-1.2.3/bin/demo": "bin"}))
	var steps []string
	if err := f.installArchive(context.Background(), "demo", rel, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.Root, "demo", "1.2.3", "x64")
	if got := toolCacheFind(f.Root, "demo", "1.2"); got != dir {
		t.Fatalf("tool-cache find returned %q", got)
	}
	if !exists(filepath.Join(dir, "bin", "demo")) {
		t.Fatal("the archive's lone top folder must become x64")
	}
	if !reflect.DeepEqual(steps, []string{"downloading", "extracting"}) {
		t.Fatalf("progress %v", steps)
	}
	if got := tmpEntries(t, f.Root); len(got) != 0 {
		t.Fatalf(".tmp holds %v", got)
	}
	want := []string{
		"chown -R -h ghrunner:ghrunner " + filepath.Join(f.Root, ".tmp", "op1", "x", "demo-1.2.3"),
		"chown -h ghrunner:ghrunner " + filepath.Join(f.Root, "demo"),
		"chown -h ghrunner:ghrunner " + filepath.Join(f.Root, "demo", "1.2.3"),
		"chown -h ghrunner:ghrunner " + dir + ".complete",
	}
	if got := f.host.commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands\n got %q\nwant %q", got, want)
	}
}

func TestInstallArchiveKeepsARootWithSeveralEntries(t *testing.T) {
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin", "LICENSE": "l"}))
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(f.Root, "demo", "1.2.3", "x64", "bin", "demo")) {
		t.Fatal("a root with several entries must be installed as is")
	}
}

func TestInstallArchiveSkipsACompleteVersion(t *testing.T) {
	f := newFixture(t)
	mkInstall(t, f.Root, "demo", "1.2.3", true)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("got %v", err)
	}
	if f.hitCount("/demo.tar.gz") != 0 {
		t.Fatal("a complete version must not be downloaded")
	}
}

func TestInstallArchiveReplacesAFolderWithoutMarker(t *testing.T) {
	f := newFixture(t)
	stale := mkInstall(t, f.Root, "demo", "1.2.3", false)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(stale, "file")) {
		t.Fatal("the marker-less folder's content survived")
	}
	if !exists(stale + ".complete") {
		t.Fatal("no marker")
	}
}

func TestInstallArchiveFailuresLeaveNothing(t *testing.T) {
	cases := map[string]func(f *fixture, rel *Release){
		"checksum": func(f *fixture, rel *Release) { rel.SHA256 = strings.Repeat("0", 64) },
		"download": func(f *fixture, rel *Release) { rel.URL = f.url + "/missing.tar.gz" },
		"extract":  func(f *fixture, rel *Release) { f.host.failExtract = errors.New("bad archive") },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
			breakIt(f, &rel)
			err := f.installArchive(context.Background(), "demo", rel, func(string) {})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error %v does not name %s", err, name)
			}
			dir := filepath.Join(f.Root, "demo", "1.2.3", "x64")
			if exists(dir) || exists(dir+".complete") {
				t.Fatal("a failed install left the version behind")
			}
			if got := tmpEntries(t, f.Root); len(got) != 0 {
				t.Fatalf(".tmp holds %v", got)
			}
		})
	}
}

func TestInstallArchiveCancelledLateLeavesNoVersionFolder(t *testing.T) {
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	ctx, cancel := context.WithCancel(context.Background())
	extract := f.Extract
	f.Extract = func(c context.Context, archive, dir string) error {
		defer cancel()
		return extract(c, archive, dir)
	}
	if err := f.installArchive(ctx, "demo", rel, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if exists(filepath.Join(f.Root, "demo", "1.2.3")) {
		t.Fatal("the version folder this install created survived")
	}
}

func TestInstallArchiveTmpModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes")
	}
	f := newFixture(t)
	rel := demoRelease(f, tarGz(map[string]string{"bin/demo": "bin"}))
	fetch := f.Fetch
	f.Fetch = func(ctx context.Context, url, dst string) error {
		for path, want := range map[string]os.FileMode{filepath.Join(f.Root, ".tmp"): 0o711, filepath.Dir(dst): 0o700} {
			st, err := os.Stat(path)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				continue
			}
			if st.Mode().Perm() != want {
				t.Errorf("%s: mode %v, want %v", path, st.Mode().Perm(), want)
			}
		}
		return fetch(ctx, url, dst)
	}
	if err := f.installArchive(context.Background(), "demo", rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanTmpKeepsMarkerlessVersions(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.Root, ".tmp", "op9", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	inProgress := mkInstall(t, f.Root, "node", "22.11.0", false)
	if err := f.CleanTmp(); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(f.Root, ".tmp")) {
		t.Fatal(".tmp survived")
	}
	if !exists(inProgress) {
		t.Fatal("a marker-less version folder may be a job's own download and must stay")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/`
Expected: FAIL to compile — `f.installArchive undefined`, `f.CleanTmp undefined`.

- [ ] **Step 4: Write the install routine**

Append to `internal/toolchain/layout.go`, and add `"context"`, `"crypto/sha256"`, `"encoding/hex"`, `"fmt"`, `"io"` and `"strings"` to its imports:

```go
// opDir makes a fresh directory for one operation under <Root>/.tmp. Both
// are root-owned; .tmp allows only traversal, so jobs cannot reach into a
// download.
func (e *Env) opDir() (string, error) {
	tmp := filepath.Join(e.Root, ".tmp")
	if err := os.MkdirAll(tmp, 0o711); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o711); err != nil {
		return "", err
	}
	dir := filepath.Join(tmp, e.OpID())
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// CleanTmp removes what unfinished operations left in <Root>/.tmp. Version
// folders without a marker are left alone: one may be a job's own setup-*
// download in progress.
func (e *Env) CleanTmp() error { return os.RemoveAll(filepath.Join(e.Root, ".tmp")) }

func (e *Env) owner() string { return e.User + ":" + e.User }

func (e *Env) chownTree(ctx context.Context, path string) error {
	_, err := e.Run(ctx, "chown", "-R", "-h", e.owner(), path)
	return err
}

// mkdirOwned creates dir, when missing, owned by the runner user, so jobs'
// setup-* steps can still add versions beside ghr's.
func (e *Env) mkdirOwned(ctx context.Context, dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	_, err := e.Run(ctx, "chown", "-h", e.owner(), dir)
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verify(path, want string) error {
	if want == "" {
		return nil
	}
	got, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum: SHA-256 mismatch: the source says %s, the download is %s", want, got)
	}
	return nil
}

// singleRoot returns dir's lone subdirectory when dir holds nothing else,
// as the actions cache an archive's single top folder.
func singleRoot(dir string) (string, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(es) == 1 && es[0].IsDir() {
		return filepath.Join(dir, es[0].Name()), nil
	}
	return dir, nil
}

// removeIncomplete deletes an x64 folder and its marker.
func removeIncomplete(dir string) {
	os.Remove(marker(dir))
	os.RemoveAll(dir)
}

// dropFailed undoes a failed install: the x64 folder, its marker, and the
// version folder when nothing else is left in it.
func dropFailed(dir string) {
	removeIncomplete(dir)
	os.Remove(filepath.Dir(dir))
}

// installArchive installs rel's archive at <Root>/<toolDir>/<rel.Folder>/x64,
// the layout @actions/tool-cache find() reads, writing the marker last.
func (e *Env) installArchive(ctx context.Context, toolDir string, rel Release, progress func(string)) error {
	if !validFolder(rel.Folder) {
		return fmt.Errorf("invalid version folder %q", rel.Folder)
	}
	if e.has(toolDir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	op, err := e.opDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(op)
	progress("downloading")
	archive := filepath.Join(op, "archive.tar.gz")
	if err := e.Fetch(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := verify(archive, rel.SHA256); err != nil {
		return err
	}
	progress("extracting")
	out := filepath.Join(op, "x")
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	if err := e.Extract(ctx, archive, out); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	src, err := singleRoot(out)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := e.chownTree(ctx, src); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	toolPath := filepath.Join(e.Root, toolDir)
	verPath := filepath.Join(toolPath, rel.Folder)
	for _, d := range []string{toolPath, verPath} {
		if err := e.mkdirOwned(ctx, d); err != nil {
			return err
		}
	}
	target := filepath.Join(verPath, arch)
	if err := ctx.Err(); err != nil {
		dropFailed(target)
		return err
	}
	// A target without a marker is a killed install; replace it, as
	// @actions/tool-cache's _createToolPath does.
	removeIncomplete(target)
	if err := os.Rename(src, target); err != nil {
		dropFailed(target)
		return err
	}
	if err := os.WriteFile(marker(target), nil, 0o644); err != nil {
		dropFailed(target)
		return err
	}
	if _, err := e.Run(ctx, "chown", "-h", e.owner(), marker(target)); err != nil {
		dropFailed(target)
		return fmt.Errorf("chown: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain` (`TestInstallArchiveTmpModes` reports SKIP on Windows), no gofmt or vet output.

- [ ] **Step 6: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): install archives into the tool cache"
```

### Task 3: Node installer

**Files:**
- Create: `internal/toolchain/manifest.go`
- Create: `internal/toolchain/node.go`
- Create: `internal/toolchain/node_test.go`

**Interfaces:**
- Consumes: C1; C2 `pick`, `newestFirst`, `identity`, `listLayout`, `removeLayout`, `installArchive`; C6 `newFixture`, `serve`, `tarGz`, `toolCacheFind`.
- Produces: `manifestTool`, `manifestFile`, `manifestEntry` (embedded by Tasks 4 and 5); C3 `newNode`.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Check the live Node manifest shape**

Run: `timeout 60 curl -sf https://raw.githubusercontent.com/actions/node-versions/main/versions-manifest.json | head -c 700`
Expected: a JSON array whose entries carry `"version"`, `"stable"` and `"files"`, each file with `"filename"`, `"arch"`, `"platform"` and `"download_url"`, and a linux x64 file per version. If the field names differ, stop and report them instead of continuing.

- [ ] **Step 2: Write the failing tests**

Create `internal/toolchain/node_test.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nodeManifest(url string) []byte {
	return []byte(fmt.Sprintf(`[
 {"version":"24.9.0","stable":true,"files":[
   {"filename":"node-24.9.0-win32-x64","arch":"x64","platform":"win32","download_url":"%[1]s/win.zip"},
   {"filename":"node-24.9.0-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/node-24.9.0.tar.gz"}]},
 {"version":"25.0.0-rc.1","stable":false,"files":[
   {"filename":"node-25.0.0-rc.1-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/rc.tar.gz"}]},
 {"version":"22.12.0","stable":false,"files":[
   {"filename":"node-22.12.0-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/unstable.tar.gz"}]},
 {"version":"22.11.0","stable":true,"files":[
   {"filename":"node-22.11.0-linux-arm64","arch":"arm64","platform":"linux","download_url":"%[1]s/arm.tar.gz"},
   {"filename":"node-22.11.0-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/node-22.11.0.tar.gz"}]},
 {"version":"22.10.0","stable":true,"files":[
   {"filename":"node-22.10.0-win32-x64","arch":"x64","platform":"win32","download_url":"%[1]s/win.zip"}]}
]`, url))
}

func TestNodeAvailableAndResolve(t *testing.T) {
	f := newFixture(t)
	f.serve("/node.json", nodeManifest(f.url))
	n := newNode(f.Env)
	cs, err := n.Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []Choice{{Spec: "24.9.0", Version: "24.9.0"}, {Spec: "22.11.0", Version: "22.11.0"}}; !reflect.DeepEqual(cs, want) {
		t.Fatalf("available %+v", cs)
	}
	rel, err := n.Resolve(context.Background(), "22")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Release{Tool: "node", Version: "22.11.0", Folder: "22.11.0", URL: f.url + "/node-22.11.0.tar.gz"}); rel != want {
		t.Fatalf("resolved %+v", rel)
	}
	if rel, _ := n.Resolve(context.Background(), "latest"); rel.Version != "24.9.0" {
		t.Fatalf("latest resolved %+v", rel)
	}
	if _, err := n.Resolve(context.Background(), "20"); err == nil {
		t.Fatal("20 resolved")
	}
}

func TestNodeInstallIsFoundBySetupNode(t *testing.T) {
	f := newFixture(t)
	f.serve("/node.json", nodeManifest(f.url))
	f.serve("/node-22.11.0.tar.gz", tarGz(map[string]string{"node-22.11.0-linux-x64/bin/node": "node"}))
	n := newNode(f.Env)
	rel, err := n.Resolve(context.Background(), "22")
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Install(context.Background(), rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	dir := toolCacheFind(f.Root, "node", "22")
	if dir != filepath.Join(f.Root, "node", "22.11.0", "x64") || !exists(filepath.Join(dir, "bin", "node")) {
		t.Fatalf("setup-node would find %q", dir)
	}
	got, err := n.Installed()
	if err != nil || len(got) != 1 || got[0].Version != "22.11.0" || got[0].Tool != "node" {
		t.Fatalf("installed %+v, %v", got, err)
	}
	if err := n.Remove("22.11.0"); err != nil {
		t.Fatal(err)
	}
	if toolCacheFind(f.Root, "node", "22") != "" {
		t.Fatal("removed version still found")
	}
	if err := n.Remove("22.11.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestNodeManifestErrorsNameTheTool(t *testing.T) {
	f := newFixture(t)
	if _, err := newNode(f.Env).Available(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "Node versions: ") {
		t.Fatalf("error %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run Node`
Expected: FAIL to compile — `undefined: newNode`.

- [ ] **Step 4: Write the manifest code and the Node installer**

Create `internal/toolchain/manifest.go`:

```go
package toolchain

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/darkraise/ghr/internal/github"
)

type manifestFile struct {
	Filename        string `json:"filename"`
	Arch            string `json:"arch"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	DownloadURL     string `json:"download_url"`
}

type manifestEntry struct {
	Version string         `json:"version"`
	Stable  bool           `json:"stable"`
	Files   []manifestFile `json:"files"`
}

// manifestTool is a tool whose versions come from an actions/*-versions
// manifest and which installs into the tool-cache layout.
type manifestTool struct {
	e      *Env
	tool   string // the name ghr uses
	dir    string // the tool cache folder the action reads
	label  string
	url    string
	fileOK func(manifestFile) bool
}

// files maps each stable version to its first file fileOK accepts.
func (m manifestTool) files(ctx context.Context) (map[string]manifestFile, error) {
	b, err := m.e.Get(ctx, m.url)
	if err != nil {
		return nil, fmt.Errorf("%s versions: %w", m.label, err)
	}
	var es []manifestEntry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("%s versions: %w", m.label, err)
	}
	out := map[string]manifestFile{}
	for _, en := range es {
		if _, ok := github.ParseVersion(en.Version); !ok || !en.Stable {
			continue
		}
		for _, f := range en.Files {
			if m.fileOK(f) {
				out[en.Version] = f
				break
			}
		}
	}
	return out, nil
}

func versions(fs map[string]manifestFile) []string {
	vs := make([]string, 0, len(fs))
	for v := range fs {
		vs = append(vs, v)
	}
	newestFirst(vs)
	return vs
}

func (m manifestTool) Available(ctx context.Context) ([]Choice, error) {
	fs, err := m.files(ctx)
	if err != nil {
		return nil, err
	}
	out := []Choice{}
	for _, v := range versions(fs) {
		out = append(out, Choice{Spec: v, Version: v})
	}
	return out, nil
}

func (m manifestTool) pick(ctx context.Context, spec string) (string, manifestFile, error) {
	fs, err := m.files(ctx)
	if err != nil {
		return "", manifestFile{}, err
	}
	v, ok := pick(versions(fs), spec)
	if !ok {
		return "", manifestFile{}, fmt.Errorf("no stable %s release matches %q", m.label, spec)
	}
	return v, fs[v], nil
}

func (m manifestTool) Installed() ([]Installed, error) { return m.e.listLayout(m.tool, m.dir, identity) }

func (m manifestTool) Remove(version string) error { return m.e.removeLayout(m.dir, version) }

func linuxX64(f manifestFile) bool { return f.Platform == "linux" && f.Arch == arch }
```

Create `internal/toolchain/node.go`:

```go
package toolchain

import "context"

type node struct{ manifestTool }

func newNode(e *Env) Installer {
	return node{manifestTool{e: e, tool: "node", dir: "node", label: "Node", url: e.Sources.NodeManifest, fileOK: linuxX64}}
}

func (n node) Resolve(ctx context.Context, spec string) (Release, error) {
	v, f, err := n.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	return Release{Tool: "node", Version: v, Folder: v, URL: f.DownloadURL}, nil
}

func (n node) Install(ctx context.Context, rel Release, progress func(string)) error {
	return n.e.installArchive(ctx, n.dir, rel, progress)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, no gofmt or vet output.

- [ ] **Step 6: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the Node installer"
```

### Task 4: Go installer

**Files:**
- Create: `internal/toolchain/golang.go`
- Create: `internal/toolchain/golang_test.go`

**Interfaces:**
- Consumes: C1; `manifestTool`, `linuxX64` (Task 3); C2 `makeSemver`, `installArchive`; C6 `newFixture`, `serve`, `tarGz`, `sum256`, `toolCacheFind`.
- Produces: C3 `newGo`.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

Create `internal/toolchain/golang_test.go`:

```go
package toolchain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const goManifest = `[
 {"version":"1.25.1","stable":true,"files":[{"filename":"go-1.25.1-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/a"}]},
 {"version":"1.24.7","stable":true,"files":[{"filename":"go-1.24.7-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/b"}]},
 {"version":"1.20","stable":true,"files":[{"filename":"go-1.20-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/c"}]}
]`

func goReleases(sums map[string]string) []byte {
	var files []string
	for name, sum := range sums {
		files = append(files, fmt.Sprintf(`{"filename":%q,"os":"linux","arch":"amd64","kind":"archive","sha256":%q}`, name, sum))
	}
	return []byte(`[{"version":"go1.25.2","stable":true,"files":[` + strings.Join(files, ",") + `]}]`)
}

func TestGoResolvesFromTheManifestAndGoDev(t *testing.T) {
	f := newFixture(t)
	f.serve("/go.json", []byte(goManifest))
	f.serve("/godl.json", goReleases(map[string]string{
		"go1.25.2.linux-amd64.tar.gz": "22",
		"go1.25.1.linux-amd64.tar.gz": "11",
		"go1.20.linux-amd64.tar.gz":   "20",
	}))
	g := newGo(f.Env)
	rel, err := g.Resolve(context.Background(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	want := Release{Tool: "go", Version: "1.25.1", Folder: "1.25.1", URL: f.url + "/dl/go1.25.1.linux-amd64.tar.gz", SHA256: "11"}
	if rel != want {
		t.Fatalf("latest resolved %+v; setup-go's stable comes from the go-versions manifest, not go.dev", rel)
	}
	rel, err = g.Resolve(context.Background(), "1.20")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Folder != "1.20.0" || rel.Version != "1.20.0" || rel.URL != f.url+"/dl/go1.20.linux-amd64.tar.gz" {
		t.Fatalf("1.20 resolved %+v", rel)
	}
	if _, err := g.Resolve(context.Background(), "1.24"); err == nil || !strings.Contains(err.Error(), "go1.24.7.linux-amd64.tar.gz") {
		t.Fatalf("a release without a go.dev checksum: %v", err)
	}
}

func TestGoInstallIsFoundBySetupGo(t *testing.T) {
	f := newFixture(t)
	archive := tarGz(map[string]string{"go/bin/go": "go"})
	f.serve("/go.json", []byte(goManifest))
	f.serve("/godl.json", goReleases(map[string]string{"go1.25.1.linux-amd64.tar.gz": sum256(archive)}))
	f.serve("/dl/go1.25.1.linux-amd64.tar.gz", archive)
	g := newGo(f.Env)
	rel, err := g.Resolve(context.Background(), "1.25")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Install(context.Background(), rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	dir := toolCacheFind(f.Root, "go", "1.25.1")
	if dir != filepath.Join(f.Root, "go", "1.25.1", "x64") || !exists(filepath.Join(dir, "bin", "go")) {
		t.Fatalf("setup-go would find %q", dir)
	}
	if got := toolCacheFind(f.Root, "go", "1.25"); got != dir {
		t.Fatalf("setup-go with 1.25 would find %q", got)
	}
	if got, err := g.Installed(); err != nil || len(got) != 1 || got[0].Version != "1.25.1" {
		t.Fatalf("installed %+v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run Go`
Expected: FAIL to compile — `undefined: newGo`.

- [ ] **Step 3: Write the Go installer**

Create `internal/toolchain/golang.go`:

```go
package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
)

type golang struct{ manifestTool }

func newGo(e *Env) Installer {
	return golang{manifestTool{e: e, tool: "go", dir: "go", label: "Go", url: e.Sources.GoManifest, fileOK: linuxX64}}
}

// Resolve picks the version from the go-versions manifest, which setup-go
// resolves "stable" from, and downloads the official go.dev archive, which
// publishes a SHA-256.
func (g golang) Resolve(ctx context.Context, spec string) (Release, error) {
	v, _, err := g.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	name := "go" + v + ".linux-amd64.tar.gz"
	sum, err := g.sha256(ctx, name)
	if err != nil {
		return Release{}, err
	}
	sv := makeSemver(v)
	return Release{Tool: "go", Version: sv, Folder: sv, URL: g.e.Sources.GoDownload + name, SHA256: sum}, nil
}

func (g golang) sha256(ctx context.Context, name string) (string, error) {
	b, err := g.e.Get(ctx, g.e.Sources.GoReleases)
	if err != nil {
		return "", fmt.Errorf("go.dev releases: %w", err)
	}
	var rs []struct {
		Files []struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return "", fmt.Errorf("go.dev releases: %w", err)
	}
	for _, r := range rs {
		for _, f := range r.Files {
			if f.Filename == name && f.SHA256 != "" {
				return f.SHA256, nil
			}
		}
	}
	return "", fmt.Errorf("go.dev lists no SHA-256 for %s", name)
}

func (g golang) Install(ctx context.Context, rel Release, progress func(string)) error {
	return g.e.installArchive(ctx, g.dir, rel, progress)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, no gofmt or vet output.

- [ ] **Step 5: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the Go installer"
```

### Task 5: Python installer

**Files:**
- Create: `internal/toolchain/python.go`
- Modify: `internal/toolchain/layout.go` (append `runAs`)
- Create: `internal/toolchain/python_test.go`

**Interfaces:**
- Consumes: C1; `manifestTool` (Task 3); C2 `has`, `versionDir`, `validFolder`, `opDir`, `chownTree`, `mkdirOwned`, `dropFailed`; C6 `newFixture`, `serve`, `tarGz`, `host`, `commands`, `toolCacheFind`, `tmpEntries`.
- Produces: C2 `runAs` (used by Task 7); C3 `newPython`.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

Create `internal/toolchain/python_test.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func pythonManifest(url string) []byte {
	return []byte(fmt.Sprintf(`[
 {"version":"3.14.0","stable":true,"files":[
   {"filename":"python-3.14.0-linux-22.04-x64.tar.gz","arch":"x64","platform":"linux","platform_version":"22.04","download_url":"%[1]s/old.tar.gz"}]},
 {"version":"3.13.7","stable":true,"files":[
   {"filename":"python-3.13.7-linux-24.04-x64-freethreaded.tar.gz","arch":"x64-freethreaded","platform":"linux","platform_version":"24.04","download_url":"%[1]s/ft.tar.gz"},
   {"filename":"python-3.13.7-linux-24.04-x64.tar.gz","arch":"x64","platform":"linux","platform_version":"24.04","download_url":"%[1]s/py-3.13.7.tar.gz"}]}
]`, url))
}

// fakeSetup acts like python-versions' setup.sh: it installs into
// $RUNNER_TOOL_CACHE/Python/<ver>/x64 and writes the marker last.
func fakeSetup(ver string, seen *map[string]string, fail bool) func(string, map[string]string, []string) error {
	return func(dir string, env map[string]string, _ []string) error {
		*seen = env
		if !exists(filepath.Join(dir, "setup.sh")) {
			return errors.New("setup.sh not in the working directory")
		}
		x64 := filepath.Join(env["RUNNER_TOOL_CACHE"], "Python", ver, "x64")
		if err := os.MkdirAll(filepath.Join(x64, "bin"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(x64, "bin", "python3"), nil, 0o755); err != nil {
			return err
		}
		if fail {
			return errors.New("ensurepip failed")
		}
		return os.WriteFile(x64+".complete", nil, 0o644)
	}
}

func pythonFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.serve("/python.json", pythonManifest(f.url))
	f.serve("/py-3.13.7.tar.gz", tarGz(map[string]string{"setup.sh": "#!/bin/bash", "python": "", "lib/libpython3.13.so.1.0": ""}))
	return f
}

func TestPythonResolveUsesUbuntu2404X64Files(t *testing.T) {
	f := pythonFixture(t)
	p := newPython(f.Env)
	rel, err := p.Resolve(context.Background(), "3")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Release{Tool: "python", Version: "3.13.7", Folder: "3.13.7", URL: f.url + "/py-3.13.7.tar.gz"}); rel != want {
		t.Fatalf("resolved %+v", rel)
	}
	if _, err := p.Resolve(context.Background(), "3.14"); err == nil {
		t.Fatal("3.14 has no 24.04 build here and must not resolve")
	}
}

func TestPythonInstallRunsSetupShAsTheRunnerUser(t *testing.T) {
	f := pythonFixture(t)
	var env map[string]string
	f.host.scripts["setup.sh"] = fakeSetup("3.13.7", &env, false)
	p := newPython(f.Env)
	rel, _ := p.Resolve(context.Background(), "3.13")
	var steps []string
	if err := p.Install(context.Background(), rel, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if got := toolCacheFind(f.Root, "Python", "3.13"); got != filepath.Join(f.Root, "Python", "3.13.7", "x64") {
		t.Fatalf("setup-python would find %q", got)
	}
	if got := toolCacheFind(f.Root, "Python", "3.13.7"); got == "" {
		t.Fatal("setup-python with 3.13.7 would find nothing")
	}
	extracted := filepath.Join(f.Root, ".tmp", "op1", "x")
	want := map[string]string{"RUNNER_TOOL_CACHE": f.Root, "LD_LIBRARY_PATH": filepath.Join(extracted, "lib"), "HOME": "/home/ghrunner"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	cmds := f.host.commands()
	chown := slices.Index(cmds, "chown -R -h ghrunner:ghrunner "+filepath.Join(f.Root, ".tmp", "op1"))
	run := slices.IndexFunc(cmds, func(c string) bool { return strings.HasPrefix(c, "runuser -u ghrunner -- env -u AGENT_TOOLSDIRECTORY ") })
	if chown < 0 || run < 0 || chown > run {
		t.Fatalf("commands %q: the operation directory must go to ghrunner before setup.sh runs without AGENT_TOOLSDIRECTORY", cmds)
	}
	if mk := slices.Index(cmds, "chown -h ghrunner:ghrunner "+filepath.Join(f.Root, "Python")); mk < 0 || mk > run {
		t.Fatalf("commands %q: <Root>/Python must exist, owned by ghrunner, before setup.sh runs", cmds)
	}
	if !slices.Equal(steps, []string{"downloading", "extracting", "running setup.sh"}) {
		t.Fatalf("progress %v", steps)
	}
	if err := p.Install(context.Background(), rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install: %v", err)
	}
}

func TestPythonFailedSetupLeavesNoVersion(t *testing.T) {
	for name, fail := range map[string]bool{"script error": true, "no marker": false} {
		t.Run(name, func(t *testing.T) {
			f := pythonFixture(t)
			var env map[string]string
			setup := fakeSetup("3.13.7", &env, fail)
			if !fail {
				setup = func(dir string, e map[string]string, args []string) error {
					return os.MkdirAll(filepath.Join(e["RUNNER_TOOL_CACHE"], "Python", "3.13.7", "x64"), 0o755)
				}
			}
			f.host.scripts["setup.sh"] = setup
			p := newPython(f.Env)
			rel, _ := p.Resolve(context.Background(), "3.13")
			if err := p.Install(context.Background(), rel, func(string) {}); err == nil {
				t.Fatal("no error")
			}
			if exists(filepath.Join(f.Root, "Python", "3.13.7")) {
				t.Fatal("a failed install left its version folder behind")
			}
			if got := tmpEntries(t, f.Root); len(got) != 0 {
				t.Fatalf(".tmp holds %v", got)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run Python`
Expected: FAIL to compile — `undefined: newPython`.

- [ ] **Step 3: Write `runAs` and the Python installer**

Append to `internal/toolchain/layout.go`:

```go
// runAs runs script, relative to dir, as the runner user with env added and
// AGENT_TOOLSDIRECTORY removed (python-versions' setup.sh prefers it over
// RUNNER_TOOL_CACHE).
func (e *Env) runAs(ctx context.Context, dir string, env []string, script string, args ...string) ([]byte, error) {
	a := []string{"-u", e.User, "--", "env", "-u", "AGENT_TOOLSDIRECTORY"}
	a = append(a, env...)
	a = append(a, "bash", "-c", `cd -- "$0" && exec bash "$@"`, dir, script)
	a = append(a, args...)
	return e.Run(ctx, "runuser", a...)
}
```

Create `internal/toolchain/python.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type python struct{ manifestTool }

func newPython(e *Env) Installer {
	return python{manifestTool{e: e, tool: "python", dir: "Python", label: "Python", url: e.Sources.PythonManifest,
		fileOK: func(f manifestFile) bool {
			return f.Platform == "linux" && f.PlatformVersion == "24.04" && f.Arch == arch
		}}}
}

func (p python) Resolve(ctx context.Context, spec string) (Release, error) {
	v, f, err := p.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	return Release{Tool: "python", Version: v, Folder: v, URL: f.DownloadURL}, nil
}

// Install runs the archive's own setup.sh, as setup-python does: the builds
// are shared-library builds whose rpath names the build machine's tool
// cache, so the script needs LD_LIBRARY_PATH to start python.
func (p python) Install(ctx context.Context, rel Release, progress func(string)) error {
	e := p.e
	if !validFolder(rel.Folder) {
		return fmt.Errorf("invalid version folder %q", rel.Folder)
	}
	if e.has(p.dir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	op, err := e.opDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(op)
	progress("downloading")
	archive := filepath.Join(op, "python.tar.gz")
	if err := e.Fetch(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	progress("extracting")
	out := filepath.Join(op, "x")
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	if err := e.Extract(ctx, archive, out); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := e.chownTree(ctx, op); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	if err := e.mkdirOwned(ctx, filepath.Join(e.Root, p.dir)); err != nil {
		return err
	}
	progress("running setup.sh")
	target := e.versionDir(p.dir, rel.Folder)
	env := []string{"RUNNER_TOOL_CACHE=" + e.Root, "LD_LIBRARY_PATH=" + filepath.Join(out, "lib"), "HOME=" + e.Home}
	if _, err := e.runAs(ctx, out, env, "setup.sh"); err != nil {
		dropFailed(target)
		return fmt.Errorf("setup.sh: %w", err)
	}
	if !e.has(p.dir, rel.Folder) {
		dropFailed(target)
		return errors.New("setup.sh finished without writing the .complete marker")
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, no gofmt or vet output.

- [ ] **Step 5: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the Python installer"
```

### Task 6: Java installer

**Files:**
- Create: `internal/toolchain/java.go`
- Create: `internal/toolchain/java_test.go`

**Interfaces:**
- Consumes: C1; C2 `installArchive`, `listLayout`, `removeLayout`; C6 `newFixture`, `serve`, `tarGz`, `sum256`, `javaFind`.
- Produces: C3 `newJava`.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Check the live Adoptium answers**

Run: `timeout 60 curl -sf "https://api.adoptium.net/v3/info/available_releases" | head -c 400; echo; timeout 60 curl -sf "https://api.adoptium.net/v3/assets/feature_releases/21/ga?architecture=x64&os=linux&image_type=jdk&jvm_impl=hotspot&vendor=eclipse&page_size=1&sort_order=DESC" | head -c 1500`
Expected: the first prints a JSON object with `available_releases` and `available_lts_releases` arrays of integers; the second a JSON array of exactly one release whose `version_data.semver` starts with `21.` and whose `binaries[0].package` has `link` and `checksum`. If either differs, stop and report it instead of continuing.

- [ ] **Step 2: Write the failing tests**

Create `internal/toolchain/java_test.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

const javaQuery = "/v3/assets/feature_releases/21/ga?architecture=x64&os=linux&image_type=jdk&jvm_impl=hotspot&vendor=eclipse&page_size=1&sort_order=DESC"

func javaFixture(t *testing.T) (*fixture, []byte) {
	f := newFixture(t)
	archive := tarGz(map[string]string{"jdk-21.0.12.1+1/bin/java": "java"})
	f.serve("/v3/info/available_releases", []byte(`{"available_lts_releases":[8,11,17,21,25],"available_releases":[8,11,17,21,24,25],"most_recent_feature_release":25}`))
	f.serve(javaQuery, []byte(fmt.Sprintf(`[{"release_name":"jdk-21.0.12.1+1","version_data":{"semver":"21.0.12+101.0.LTS"},
	  "binaries":[{"package":{"name":"OpenJDK21U-jdk_x64_linux_hotspot.tar.gz","link":%q,"checksum":%q}}]}]`, f.url+"/jdk.tar.gz", sum256(archive))))
	f.serve("/jdk.tar.gz", archive)
	return f, archive
}

func TestJavaAvailableListsMajors(t *testing.T) {
	f, _ := javaFixture(t)
	cs, err := newJava(f.Env).Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Choice{{"25", "25", true}, {"24", "24", false}, {"21", "21", true}, {"17", "17", true}, {"11", "11", true}, {"8", "8", true}}
	if !reflect.DeepEqual(cs, want) {
		t.Fatalf("available %+v", cs)
	}
}

func TestJavaResolveUsesTheSemverFolder(t *testing.T) {
	f, archive := javaFixture(t)
	rel, err := newJava(f.Env).Resolve(context.Background(), "21")
	if err != nil {
		t.Fatal(err)
	}
	want := Release{Tool: "java", Version: "21.0.12+101.0.LTS", Folder: "21.0.12-101.0.LTS", URL: f.url + "/jdk.tar.gz", SHA256: sum256(archive)}
	if rel != want {
		t.Fatalf("resolved %+v", rel)
	}
	for _, bad := range []string{"21.0.8", "latest", "0"} {
		if _, err := newJava(f.Env).Resolve(context.Background(), bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
	if _, err := newJava(f.Env).Resolve(context.Background(), "17"); err == nil {
		t.Error("a major the source lists nothing for resolved")
	}
}

func TestJavaInstallIsFoundBySetupJava(t *testing.T) {
	f, _ := javaFixture(t)
	j := newJava(f.Env)
	rel, err := j.Resolve(context.Background(), "21")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Install(context.Background(), rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	dir := javaFind(f.Root, "21")
	if dir != filepath.Join(f.Root, "Java_Temurin-Hotspot_jdk", "21.0.12-101.0.LTS", "x64") || !exists(filepath.Join(dir, "bin", "java")) {
		t.Fatalf("setup-java would find %q", dir)
	}
	got, err := j.Installed()
	if err != nil || len(got) != 1 || got[0].Version != "21.0.12+101.0.LTS" || got[0].Tool != "java" {
		t.Fatalf("installed %+v, %v", got, err)
	}
	if err := j.Remove("21.0.12+101.0.LTS"); err != nil {
		t.Fatal(err)
	}
	if javaFind(f.Root, "21") != "" {
		t.Fatal("removed version still found")
	}
	if err := j.Remove("21.0.12+101.0.LTS"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run Java`
Expected: FAIL to compile — `undefined: newJava`.

- [ ] **Step 4: Write the Java installer**

Create `internal/toolchain/java.go`:

```go
package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const javaDir = "Java_Temurin-Hotspot_jdk"

type java struct{ e *Env }

func newJava(e *Env) Installer { return java{e} }

func (j java) Available(ctx context.Context) ([]Choice, error) {
	b, err := j.e.Get(ctx, j.e.Sources.Adoptium+"/v3/info/available_releases")
	if err != nil {
		return nil, fmt.Errorf("Java versions: %w", err)
	}
	var r struct {
		Releases []int `json:"available_releases"`
		LTS      []int `json:"available_lts_releases"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("Java versions: %w", err)
	}
	lts := map[int]bool{}
	for _, v := range r.LTS {
		lts[v] = true
	}
	sort.Sort(sort.Reverse(sort.IntSlice(r.Releases)))
	out := []Choice{}
	for _, v := range r.Releases {
		s := strconv.Itoa(v)
		out = append(out, Choice{Spec: s, Version: s, LTS: lts[v]})
	}
	return out, nil
}

// Resolve takes a major version only and returns its newest GA Temurin JDK.
// The folder is version_data.semver with its first "+" turned into "-", as
// setup-java names it; the release name can have four parts, which is not
// semver.
func (j java) Resolve(ctx context.Context, spec string) (Release, error) {
	major, err := strconv.Atoi(spec)
	if err != nil || major <= 0 {
		return Release{}, fmt.Errorf("Java installs by major version, such as 21; got %q", spec)
	}
	url := fmt.Sprintf("%s/v3/assets/feature_releases/%d/ga?architecture=x64&os=linux&image_type=jdk&jvm_impl=hotspot&vendor=eclipse&page_size=1&sort_order=DESC", j.e.Sources.Adoptium, major)
	b, err := j.e.Get(ctx, url)
	if err != nil {
		return Release{}, fmt.Errorf("Java %d: %w", major, err)
	}
	var rs []struct {
		VersionData struct {
			Semver string `json:"semver"`
		} `json:"version_data"`
		Binaries []struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
			} `json:"package"`
		} `json:"binaries"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return Release{}, fmt.Errorf("Java %d: %w", major, err)
	}
	if len(rs) == 0 || len(rs[0].Binaries) == 0 || rs[0].VersionData.Semver == "" {
		return Release{}, fmt.Errorf("Adoptium lists no GA JDK %d for linux x64", major)
	}
	sv, pkg := rs[0].VersionData.Semver, rs[0].Binaries[0].Package
	return Release{Tool: "java", Version: sv, Folder: strings.Replace(sv, "+", "-", 1), URL: pkg.Link, SHA256: pkg.Checksum}, nil
}

func (j java) Install(ctx context.Context, rel Release, progress func(string)) error {
	return j.e.installArchive(ctx, javaDir, rel, progress)
}

func (j java) Installed() ([]Installed, error) {
	return j.e.listLayout("java", javaDir, func(folder string) string { return strings.Replace(folder, "-", "+", 1) })
}

func (j java) Remove(version string) error {
	return j.e.removeLayout(javaDir, strings.Replace(version, "+", "-", 1))
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, no gofmt or vet output.

- [ ] **Step 6: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the Java installer"
```

### Task 7: .NET installer

**Files:**
- Create: `internal/toolchain/dotnet.go`
- Create: `internal/toolchain/dotnet_test.go`

**Interfaces:**
- Consumes: C1; C2 `newestFirst`, `looseCompare`, `validFolder`, `exists`, `opDir`, `chownTree`, `mkdirOwned`, `runAs`; C6 `newFixture`, `serve`, `host`, `commands`, `dotnetFind`, `tmpEntries`.
- Produces: C3 `newDotnet`.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/toolchain/dotnet_test.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

const dotnetIndex = `{"releases-index":[
 {"channel-version":"11.0","latest-sdk":"11.0.100-rc.2.25502.107","support-phase":"go-live","release-type":"sts"},
 {"channel-version":"10.0","latest-sdk":"10.0.105","support-phase":"active","release-type":"lts"},
 {"channel-version":"9.0","latest-sdk":"9.0.311","support-phase":"maintenance","release-type":"sts"},
 {"channel-version":"8.0","latest-sdk":"8.0.414","support-phase":"maintenance","release-type":"lts"},
 {"channel-version":"7.0","latest-sdk":"7.0.410","support-phase":"eol","release-type":"sts"}
]}`

func dotnetFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.serve("/dotnet-index.json", []byte(dotnetIndex))
	f.serve("/dotnet-install.sh", []byte("#!/bin/bash"))
	return f
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDotnetAvailableAndResolve(t *testing.T) {
	f := dotnetFixture(t)
	d := newDotnet(f.Env)
	cs, err := d.Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Choice{{"10.0", "10.0.105", true}, {"9.0", "9.0.311", false}, {"8.0", "8.0.414", true}}
	if !reflect.DeepEqual(cs, want) {
		t.Fatalf("available %+v", cs)
	}
	cases := map[string]string{"8.0": "8.0.414", "10.0": "10.0.105", "8.0.400": "8.0.400"}
	for spec, ver := range cases {
		rel, err := d.Resolve(context.Background(), spec)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		if want := (Release{Tool: "dotnet", Version: ver, Folder: ver, URL: f.url + "/dotnet-install.sh"}); rel != want {
			t.Fatalf("%s resolved %+v", spec, rel)
		}
	}
	for _, bad := range []string{"7.0", "11.0", "8", "latest"} {
		if _, err := d.Resolve(context.Background(), bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
}

func TestDotnetInstallRunsTheInstallScript(t *testing.T) {
	f := dotnetFixture(t)
	var gotArgs []string
	var gotEnv map[string]string
	f.host.scripts["dotnet-install.sh"] = func(dir string, env map[string]string, args []string) error {
		gotArgs, gotEnv = args, env
		if !exists(filepath.Join(dir, "dotnet-install.sh")) {
			return errors.New("script not in the working directory")
		}
		root := filepath.Join(f.Root, "dotnet")
		touch(t, filepath.Join(root, "dotnet"))
		touch(t, filepath.Join(root, "sdk", "8.0.414", "dotnet.dll"))
		touch(t, filepath.Join(root, "shared", "Microsoft.NETCore.App", "8.0.20", "x"))
		return nil
	}
	d := newDotnet(f.Env)
	rel, _ := d.Resolve(context.Background(), "8.0")
	var steps []string
	if err := d.Install(context.Background(), rel, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"--version", "8.0.414", "--install-dir", filepath.Join(f.Root, "dotnet"), "--skip-non-versioned-files"}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("args %q", gotArgs)
	}
	if gotEnv["HOME"] != "/home/ghrunner" {
		t.Fatalf("env %v", gotEnv)
	}
	if dotnetFind(filepath.Join(f.Root, "dotnet"), "8.0.x") == "" {
		t.Fatal("setup-dotnet would not find SDK 8.0.414")
	}
	if !slices.Equal(steps, []string{"downloading", "running dotnet-install.sh"}) {
		t.Fatalf("progress %v", steps)
	}
	if !slices.Contains(f.host.commands(), "chown -h ghrunner:ghrunner "+filepath.Join(f.Root, "dotnet")) {
		t.Fatalf("commands %q: <Root>/dotnet must be created owned by ghrunner", f.host.commands())
	}
	if got := tmpEntries(t, f.Root); len(got) != 0 {
		t.Fatalf(".tmp holds %v", got)
	}
	if err := d.Install(context.Background(), rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install: %v", err)
	}
	got, err := d.Installed()
	if err != nil || len(got) != 1 || got[0].Version != "8.0.414" || got[0].Path != filepath.Join(f.Root, "dotnet", "sdk", "8.0.414") {
		t.Fatalf("installed %+v, %v", got, err)
	}
}

func TestDotnetFailedInstallRemovesOnlyWhatItCreated(t *testing.T) {
	f := dotnetFixture(t)
	f.host.scripts["dotnet-install.sh"] = func(dir string, env map[string]string, args []string) error {
		touch(t, filepath.Join(f.Root, "dotnet", "sdk", args[1], "partial"))
		return errors.New("download failed")
	}
	d := newDotnet(f.Env)
	rel, _ := d.Resolve(context.Background(), "8.0")
	if err := d.Install(context.Background(), rel, func(string) {}); err == nil {
		t.Fatal("no error")
	}
	if exists(filepath.Join(f.Root, "dotnet", "sdk", "8.0.414")) {
		t.Fatal("the SDK folder this install created survived")
	}
	touch(t, filepath.Join(f.Root, "dotnet", "sdk", "10.0.105", "old"))
	rel, _ = d.Resolve(context.Background(), "10.0")
	if err := d.Install(context.Background(), rel, func(string) {}); err == nil {
		t.Fatal("no error")
	}
	if !exists(filepath.Join(f.Root, "dotnet", "sdk", "10.0.105", "old")) {
		t.Fatal("an SDK folder that existed before the install was removed")
	}
}

func dotnetTree(t *testing.T, root string, sdks ...string) {
	touch(t, filepath.Join(root, "dotnet"))
	for _, s := range sdks {
		touch(t, filepath.Join(root, "sdk", s, "dotnet.dll"))
	}
	for _, major := range []string{"8", "10"} {
		for _, p := range []string{"shared/Microsoft.NETCore.App/%s.0.20", "shared/Microsoft.AspNetCore.App/%s.0.20", "packs/Microsoft.NETCore.App.Ref/%s.0.20",
			"host/fxr/%s.0.20", "templates/%s.0.20", "sdk-manifests/%s.0.100", "metadata/workloads/%s.0.100", "library-packs/%s.0.100"} {
			touch(t, filepath.Join(root, filepath.FromSlash(fmt.Sprintf(p, major)), "x"))
		}
	}
}

func TestDotnetRemoveDropsTheMajorWithItsLastSDK(t *testing.T) {
	f := dotnetFixture(t)
	root := filepath.Join(f.Root, "dotnet")
	dotnetTree(t, root, "8.0.414", "10.0.105")
	if err := newDotnet(f.Env).Remove("8.0.414"); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"sdk/8.0.414", "shared/Microsoft.NETCore.App/8.0.20", "shared/Microsoft.AspNetCore.App/8.0.20",
		"packs/Microsoft.NETCore.App.Ref/8.0.20", "host/fxr/8.0.20", "templates/8.0.20", "sdk-manifests/8.0.100", "metadata/workloads/8.0.100", "library-packs/8.0.100"} {
		if exists(filepath.Join(root, filepath.FromSlash(gone))) {
			t.Errorf("%s survived", gone)
		}
	}
	for _, kept := range []string{"dotnet", "sdk/10.0.105", "shared/Microsoft.NETCore.App/10.0.20", "host/fxr/10.0.20"} {
		if !exists(filepath.Join(root, filepath.FromSlash(kept))) {
			t.Errorf("%s was removed", kept)
		}
	}
}

func TestDotnetRemoveKeepsTheMajorWhileAnotherSDKUsesIt(t *testing.T) {
	f := dotnetFixture(t)
	root := filepath.Join(f.Root, "dotnet")
	dotnetTree(t, root, "8.0.414", "8.0.311")
	d := newDotnet(f.Env)
	if err := d.Remove("8.0.414"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "sdk", "8.0.414")) || !exists(filepath.Join(root, "shared", "Microsoft.NETCore.App", "8.0.20")) {
		t.Fatal("only the SDK itself may go while 8.0.311 remains")
	}
	touch(t, filepath.Join(root, "sdk", "preview", "dotnet.dll"))
	for _, bad := range []string{"9.0.100", "..", "8.0.414", "preview"} {
		if err := d.Remove(bad); !errors.Is(err, ErrNotInstalled) {
			t.Errorf("remove %q: %v", bad, err)
		}
	}
	if !exists(filepath.Join(root, "shared", "Microsoft.NETCore.App", "10.0.20")) {
		t.Error("removing a folder that is not a version took the runtimes with it")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run Dotnet`
Expected: FAIL to compile — `undefined: newDotnet`.

- [ ] **Step 3: Write the .NET installer**

Create `internal/toolchain/dotnet.go`:

```go
package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkraise/ghr/internal/github"
)

type dotnet struct{ e *Env }

func newDotnet(e *Env) Installer { return dotnet{e} }

type dotnetChannel struct {
	Channel   string `json:"channel-version"`
	LatestSDK string `json:"latest-sdk"`
	Phase     string `json:"support-phase"`
	Type      string `json:"release-type"`
}

func (d dotnet) root() string { return filepath.Join(d.e.Root, "dotnet") }

func (d dotnet) sdkDir(ver string) string { return filepath.Join(d.root(), "sdk", ver) }

func (d dotnet) hasSDK(ver string) bool {
	return validFolder(ver) && exists(filepath.Join(d.sdkDir(ver), "dotnet.dll"))
}

// channels are the supported ones: support-phase active or maintenance.
func (d dotnet) channels(ctx context.Context) ([]dotnetChannel, error) {
	b, err := d.e.Get(ctx, d.e.Sources.DotnetIndex)
	if err != nil {
		return nil, fmt.Errorf(".NET versions: %w", err)
	}
	var r struct {
		Index []dotnetChannel `json:"releases-index"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf(".NET versions: %w", err)
	}
	var out []dotnetChannel
	for _, c := range r.Index {
		if c.Phase == "active" || c.Phase == "maintenance" {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d dotnet) Available(ctx context.Context) ([]Choice, error) {
	cs, err := d.channels(ctx)
	if err != nil {
		return nil, err
	}
	specs := make([]string, 0, len(cs))
	by := map[string]dotnetChannel{}
	for _, c := range cs {
		specs = append(specs, c.Channel)
		by[c.Channel] = c
	}
	newestFirst(specs)
	out := []Choice{}
	for _, s := range specs {
		out = append(out, Choice{Spec: s, Version: by[s].LatestSDK, LTS: by[s].Type == "lts"})
	}
	return out, nil
}

// Resolve takes a channel ("8.0", its latest SDK) or a full SDK version.
func (d dotnet) Resolve(ctx context.Context, spec string) (Release, error) {
	rel := func(v string) Release {
		return Release{Tool: "dotnet", Version: v, Folder: v, URL: d.e.Sources.DotnetScript}
	}
	if v, ok := github.ParseVersion(spec); ok && len(v) == 3 {
		return rel(spec), nil
	}
	cs, err := d.channels(ctx)
	if err != nil {
		return Release{}, err
	}
	for _, c := range cs {
		if c.Channel == spec {
			return rel(c.LatestSDK), nil
		}
	}
	return Release{}, fmt.Errorf("no supported .NET channel %q; use a channel such as 8.0 or a full SDK version", spec)
}

// Install runs dotnet-install.sh with --skip-non-versioned-files, as
// setup-dotnet does: the first install still writes the dotnet host, and
// later ones never overwrite it while a job may be running it.
func (d dotnet) Install(ctx context.Context, rel Release, progress func(string)) error {
	e := d.e
	if !validFolder(rel.Version) {
		return fmt.Errorf("invalid SDK version %q", rel.Version)
	}
	if d.hasSDK(rel.Version) {
		return ErrAlreadyInstalled
	}
	existed := exists(d.sdkDir(rel.Version))
	fail := func(err error) error {
		if !existed {
			os.RemoveAll(d.sdkDir(rel.Version))
		}
		return err
	}
	op, err := e.opDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(op)
	progress("downloading")
	if err := e.Fetch(ctx, rel.URL, filepath.Join(op, "dotnet-install.sh")); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := e.chownTree(ctx, op); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	if err := e.mkdirOwned(ctx, d.root()); err != nil {
		return err
	}
	progress("running dotnet-install.sh")
	if _, err := e.runAs(ctx, op, []string{"HOME=" + e.Home}, "dotnet-install.sh",
		"--version", rel.Version, "--install-dir", d.root(), "--skip-non-versioned-files"); err != nil {
		return fail(fmt.Errorf("dotnet-install.sh: %w", err))
	}
	if !d.hasSDK(rel.Version) {
		return fail(fmt.Errorf("dotnet-install.sh finished without SDK %s", rel.Version))
	}
	return nil
}

func (d dotnet) Installed() ([]Installed, error) {
	es, err := os.ReadDir(filepath.Join(d.root(), "sdk"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, en := range es {
		if v, ok := github.ParseVersion(en.Name()); !ok || len(v) != 3 || !en.IsDir() || !d.hasSDK(en.Name()) {
			continue
		}
		fi, err := os.Stat(d.sdkDir(en.Name()))
		if err != nil {
			continue
		}
		out = append(out, Installed{Tool: "dotnet", Version: en.Name(), Arch: arch, Path: d.sdkDir(en.Name()), InstalledAt: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return looseCompare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// dotnetMajorDirs hold one folder per version, named <major>.<…>, that the
// last SDK of a major leaves behind.
var dotnetMajorDirs = []string{"shared/*", "packs/*", "host/fxr", "templates", "sdk-manifests", "metadata/workloads", "library-packs"}

// Remove deletes one SDK. When it was the last SDK of its major, that
// major's runtimes and packs go too; the dotnet host stays.
func (d dotnet) Remove(version string) error {
	if v, ok := github.ParseVersion(version); !ok || len(v) != 3 || !d.hasSDK(version) {
		return ErrNotInstalled
	}
	if err := os.RemoveAll(d.sdkDir(version)); err != nil {
		return err
	}
	major := version[:strings.IndexByte(version, '.')+1]
	others, err := filepath.Glob(filepath.Join(d.root(), "sdk", major+"*", "dotnet.dll"))
	if err != nil || len(others) > 0 {
		return err
	}
	var errs []error
	for _, pattern := range dotnetMajorDirs {
		matches, err := filepath.Glob(filepath.Join(d.root(), filepath.FromSlash(pattern), major+"*"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, no gofmt or vet output.

- [ ] **Step 5: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the .NET installer"
```

### Task 8: Set registry and popular preset

**Files:**
- Create: `internal/toolchain/set.go`
- Create: `internal/toolchain/set_test.go`

**Interfaces:**
- Consumes: C1; C3 constructors; `javaDir` (Task 6); C2 `CleanTmp`, `exists`; C6 `newFixture`, `serve`, `hitCount`, `mkInstall`; `nodeManifest` (Task 3 test).
- Produces: C4.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/toolchain/set_test.go`:

```go
package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSetTools(t *testing.T) {
	s := New(newFixture(t).Env)
	if got := s.Tools(); !reflect.DeepEqual(got, []string{"dotnet", "go", "java", "node", "python"}) {
		t.Fatalf("tools %v", got)
	}
	if _, err := s.Get("ruby"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ruby: %v", err)
	}
}

func TestPopularPreset(t *testing.T) {
	want := []Entry{{"node", "22"}, {"node", "24"}, {"dotnet", "8.0"}, {"dotnet", "10.0"}, {"python", "3.13"}, {"python", "3.14"}, {"go", "latest"}, {"java", "21"}, {"java", "25"}}
	if !reflect.DeepEqual(Popular, want) {
		t.Fatalf("popular %v", Popular)
	}
	s := New(newFixture(t).Env)
	for _, e := range Popular {
		if _, err := s.Get(e.Tool); err != nil {
			t.Errorf("%v: %v", e, err)
		}
	}
}

func TestSetAvailableIsCachedForAnHour(t *testing.T) {
	f := newFixture(t)
	s := New(f.Env)
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if _, err := s.Available(context.Background(), "node"); err == nil {
		t.Fatal("a missing manifest answered")
	}
	f.serve("/node.json", nodeManifest(f.url))
	for range 2 {
		if cs, err := s.Available(context.Background(), "node"); err != nil || len(cs) != 2 {
			t.Fatalf("available %v, %v", cs, err)
		}
	}
	if got := f.hitCount("/node.json"); got != 2 {
		t.Fatalf("%d manifest reads; the error must not be cached and the success must be", got)
	}
	now = now.Add(61 * time.Minute)
	if _, err := s.Available(context.Background(), "node"); err != nil {
		t.Fatal(err)
	}
	if got := f.hitCount("/node.json"); got != 3 {
		t.Fatalf("%d manifest reads after an hour", got)
	}
	if _, err := s.Available(context.Background(), "ruby"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ruby: %v", err)
	}
}

func TestSetInstalledAndOther(t *testing.T) {
	f := newFixture(t)
	s := New(f.Env)
	mkInstall(t, f.Root, "node", "22.11.0", true)
	mkInstall(t, f.Root, "Java_Temurin-Hotspot_jdk", "21.0.8-9", true)
	mkInstall(t, f.Root, "PyPy", "7.3.17", true)
	mkInstall(t, f.Root, "Ruby", "3.3.5", true)
	if err := os.MkdirAll(filepath.Join(f.Root, ".tmp", "op1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.Root, "stray"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Tool != "java" || got[0].Version != "21.0.8+9" || got[1].Tool != "node" {
		t.Fatalf("installed %+v", got)
	}
	other, err := s.Other()
	if err != nil || !reflect.DeepEqual(other, []string{"PyPy", "Ruby"}) {
		t.Fatalf("other %v, %v", other, err)
	}
	if s.Root() != f.Root {
		t.Fatalf("root %q", s.Root())
	}
	if err := s.CleanTmp(); err != nil || exists(filepath.Join(f.Root, ".tmp")) {
		t.Fatalf("clean tmp: %v", err)
	}
}

func TestSetOtherWithoutToolCache(t *testing.T) {
	e := &Env{Root: filepath.Join(t.TempDir(), "missing")}
	if got, err := New(e).Other(); err != nil || got != nil {
		t.Fatalf("other %v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run 'Set|Popular'`
Expected: FAIL to compile — `undefined: New`, `undefined: Popular`, `undefined: Entry`.

- [ ] **Step 3: Write the Set**

Create `internal/toolchain/set.go`:

```go
package toolchain

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Entry is one preset item: a tool and the spec resolved when it installs.
type Entry struct{ Tool, Spec string }

// Popular is the preset setup.sh installs on a first install.
var Popular = []Entry{
	{"node", "22"}, {"node", "24"},
	{"dotnet", "8.0"}, {"dotnet", "10.0"},
	{"python", "3.13"}, {"python", "3.14"},
	{"go", "latest"},
	{"java", "21"}, {"java", "25"},
}

const availableTTL = time.Hour

// ownedDirs are the tool cache folders an installer owns, plus .tmp.
var ownedDirs = map[string]bool{"node": true, "go": true, "Python": true, javaDir: true, "dotnet": true, ".tmp": true}

type cachedChoices struct {
	at time.Time
	cs []Choice
}

// Set is every installer, keyed by the tool name ghr uses.
type Set struct {
	e     *Env
	tools map[string]Installer
	now   func() time.Time

	mu    sync.Mutex
	avail map[string]cachedChoices
}

func New(e *Env) *Set {
	return &Set{
		e:   e,
		now: time.Now,
		tools: map[string]Installer{
			"node": newNode(e), "go": newGo(e), "python": newPython(e), "java": newJava(e), "dotnet": newDotnet(e),
		},
		avail: map[string]cachedChoices{},
	}
}

func (s *Set) Tools() []string {
	out := make([]string, 0, len(s.tools))
	for t := range s.tools {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func (s *Set) Get(tool string) (Installer, error) {
	i, ok := s.tools[tool]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownTool, tool)
	}
	return i, nil
}

// Available is the installer's list, kept for an hour; errors are not kept.
func (s *Set) Available(ctx context.Context, tool string) ([]Choice, error) {
	i, err := s.Get(tool)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	c, ok := s.avail[tool]
	s.mu.Unlock()
	if ok && s.now().Sub(c.at) < availableTTL {
		return c.cs, nil
	}
	cs, err := i.Available(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.avail[tool] = cachedChoices{s.now(), cs}
	s.mu.Unlock()
	return cs, nil
}

// Installed lists every installer's versions, tools in name order.
func (s *Set) Installed() ([]Installed, error) {
	var out []Installed
	for _, t := range s.Tools() {
		got, err := s.tools[t].Installed()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		out = append(out, got...)
	}
	return out, nil
}

// Other names the tool cache folders no installer owns, such as PyPy or
// Ruby that jobs' own setup-* steps added.
func (s *Set) Other() ([]string, error) {
	es, err := os.ReadDir(s.e.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if e.IsDir() && !ownedDirs[e.Name()] {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Set) Root() string { return s.e.Root }

func (s *Set) CleanTmp() error { return s.e.CleanTmp() }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 120 gofmt -l internal/toolchain && timeout 300 go vet ./internal/toolchain/`
Expected: `ok  	github.com/darkraise/ghr/internal/toolchain`, no gofmt or vet output.

- [ ] **Step 5: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the installer set and preset"
```

### Task 9: Production environment

**Files:**
- Create: `internal/toolchain/env.go`
- Create: `internal/toolchain/env_test.go`

**Interfaces:**
- Consumes: C1; `system.Download`, `system.Runner`, `system.Exec`; C6 `tarGz`.
- Produces: C5.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

Create `internal/toolchain/env_test.go`:

```go
package toolchain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/system"
)

func TestDefaultSources(t *testing.T) {
	want := Sources{
		NodeManifest:   "https://raw.githubusercontent.com/actions/node-versions/main/versions-manifest.json",
		GoManifest:     "https://raw.githubusercontent.com/actions/go-versions/main/versions-manifest.json",
		PythonManifest: "https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json",
		GoReleases:     "https://go.dev/dl/?mode=json&include=all",
		GoDownload:     "https://go.dev/dl/",
		Adoptium:       "https://api.adoptium.net",
		DotnetIndex:    "https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json",
		DotnetScript:   "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh",
	}
	if got := DefaultSources(); got != want {
		t.Fatalf("sources %+v", got)
	}
}

func TestHTTPGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("body"))
	}))
	defer srv.Close()
	if b, err := httpGet(context.Background(), srv.URL+"/ok"); err != nil || string(b) != "body" {
		t.Fatalf("got %q, %v", b, err)
	}
	if _, err := httpGet(context.Background(), srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}

func TestNewEnvWiresTheRunner(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	e := NewEnv("/var/lib/ghr/toolcache", "/home/ghrunner", "ghrunner", run)
	if e.Root != "/var/lib/ghr/toolcache" || e.Home != "/home/ghrunner" || e.User != "ghrunner" || e.Sources != DefaultSources() {
		t.Fatalf("env %+v", e)
	}
	if err := e.Extract(context.Background(), "a.tar.gz", "dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), "chown", "x"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tar -xzf a.tar.gz -C dir", "chown x"}; !slices.Equal(calls, want) {
		t.Fatalf("calls %q", calls)
	}
	if a, b := e.OpID(), e.OpID(); a == b || a == "" {
		t.Fatalf("op ids %q, %q", a, b)
	}
}

func TestNewEnvExtractsWithRealTar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("GNU tar on Linux is the target")
	}
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not on PATH")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(archive, tarGz(map[string]string{"node-22/bin/node": "n"}), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "x")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewEnv(dir, "", "", system.Exec).Extract(context.Background(), archive, out); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(out, "node-22", "bin", "node")) {
		t.Fatal("not extracted")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/toolchain/ -run 'DefaultSources|HTTPGet|NewEnv'`
Expected: FAIL to compile — `undefined: DefaultSources`, `undefined: httpGet`, `undefined: NewEnv`.

- [ ] **Step 3: Write the production environment**

Create `internal/toolchain/env.go`:

```go
package toolchain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/darkraise/ghr/internal/system"
)

func DefaultSources() Sources {
	return Sources{
		NodeManifest:   "https://raw.githubusercontent.com/actions/node-versions/main/versions-manifest.json",
		GoManifest:     "https://raw.githubusercontent.com/actions/go-versions/main/versions-manifest.json",
		PythonManifest: "https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json",
		GoReleases:     "https://go.dev/dl/?mode=json&include=all",
		GoDownload:     "https://go.dev/dl/",
		Adoptium:       "https://api.adoptium.net",
		DotnetIndex:    "https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json",
		DotnetScript:   "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh",
	}
}

// NewEnv is the daemon's Env for the tool cache at root. run should kill a
// command's whole process group on cancel (system.ExecGroup), because the
// install scripts start children of their own.
func NewEnv(root, home, user string, run system.Runner) *Env {
	return &Env{
		Root:    root,
		Home:    home,
		User:    user,
		Sources: DefaultSources(),
		Get:     httpGet,
		Fetch:   system.Download,
		Run:     run,
		Extract: func(ctx context.Context, archive, dir string) error {
			_, err := run(ctx, "tar", "-xzf", archive, "-C", dir)
			return err
		},
		OpID: newOpID,
	}
}

// GetTimeout bounds one read of a version list.
var GetTimeout = time.Minute

// httpGet reads a public version list; go.dev's full list is several MB,
// hence the generous cap.
func httpGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, GetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func newOpID() string {
	var b [6]byte
	rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Run the whole package and the full suite**

Run: `timeout 300 go test ./internal/toolchain/ && timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: `ok` for `internal/toolchain` and every other package (no other package changed), no gofmt or vet output.

- [ ] **Step 5: Commit**

```bash
timeout 60 git add internal/toolchain
timeout 60 git commit -m "feat(toolchain): add the production environment"
```

- [ ] **Step 6: Run CI through a draft pull request**

CI runs only for pushes to `master` and for pull requests, so open a draft pull request:

```bash
timeout 120 git push -u origin feat/toolchains
timeout 120 gh pr create --draft --base master --head feat/toolchains --title "feat(toolchain): toolchain installers" --body "Part 1 of 3 of docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md (homelab repo): the internal/toolchain package. Draft until parts 2 and 3 land on this branch."
id=""
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  id=$(timeout 60 gh run list --branch feat/toolchains --event pull_request --limit 1 --json databaseId -q '.[0].databaseId')
  [ -n "$id" ] && break
  sleep 10
done
echo "run: ${id:-none}"
[ -n "$id" ] && timeout 900 gh run watch --exit-status "$id"
```

If it prints `run: none`, GitHub has not started the pull request's run within two minutes; check `timeout 60 gh pr checks` and rerun the loop.

Expected: the pull request's CI run succeeds, which runs `-race` and the Unix-only tests (`TestInstallArchiveTmpModes`, `TestNewEnvExtractsWithRealTar`). If it fails, read the log with `timeout 120 gh run view --log-failed <id>`, fix, commit and push; the pull request reruns CI. The branch and pull request stay open: plan 2 continues on them.
