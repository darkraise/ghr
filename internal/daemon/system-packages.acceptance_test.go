package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/jobs"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/packages"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/storage"
	"github.com/darkraise/ghr/internal/toolchain"
)

// ---------------------------------------------------------------------------
// A fake apt world. The Manager under test is packages.New(world.run), so
// every command line ghr builds goes through it; the world answers like apt,
// dpkg and apt-mark do and records what it was asked to do.
// ---------------------------------------------------------------------------

type accPkg struct {
	version, priority, section, desc string
	sizeKiB                          int
	status                           string // "installed" or "config-files"
	manual                           bool
	deps                             []string
}

type accGate struct {
	match   func(line string) bool
	entered chan struct{}
	open    chan struct{}
	once    sync.Once
}

func (g *accGate) Open() { g.once.Do(func() { close(g.open) }) }

func (g *accGate) WaitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the gated command to start")
	}
}

type accApt struct {
	mu       sync.Mutex
	pkgs     map[string]*accPkg
	cands    map[string]string   // apt-cache policy candidates
	noCand   map[string]bool     // known to apt-cache policy, with Candidate: (none)
	repo     map[string]string   // names apt-cache search can find, with descriptions
	simUp    map[string][]string // installing the key also upgrades these installed packages
	simNew   map[string][]string // installing the key also installs these new packages
	conflict map[string][]string // installing or upgrading the key needs these removed
	fail     map[string]string   // a command line containing the key fails with this stderr
	gates    []*accGate
	log      []string // command lines, after the env prefix
	badEnv   []string
	updates  int
	removed  []string // packages apt itself removed to satisfy a request
	banned   []string // apt-get remove, purge, autoremove and the like
}

func newAccApt() *accApt {
	a := &accApt{
		pkgs: map[string]*accPkg{}, cands: map[string]string{}, noCand: map[string]bool{}, repo: map[string]string{},
		simUp: map[string][]string{}, simNew: map[string][]string{}, conflict: map[string][]string{}, fail: map[string]string{},
	}
	for _, n := range packages.Base("debian", "13") {
		a.add(n, "1.0-1", "optional", "misc")
	}
	return a
}

// add installs name, marked manual.
func (a *accApt) add(name, version, priority, section string) *accPkg {
	p := &accPkg{version: version, priority: priority, section: section, desc: name + " summary", sizeKiB: 1000, status: "installed", manual: true}
	a.pkgs[name] = p
	return p
}

func (a *accApt) gate(match func(string) bool) *accGate {
	g := &accGate{match: match, entered: make(chan struct{}, 16), open: make(chan struct{})}
	a.mu.Lock()
	a.gates = append(a.gates, g)
	a.mu.Unlock()
	return g
}

func (a *accApt) lines() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.log)
}

func accSim(l string) bool { return strings.HasPrefix(l, "apt-get ") && strings.Contains(l, " -s ") }
func accRealInstall(l string) bool {
	return strings.HasPrefix(l, "apt-get install") && !accSim(l)
}
func accUpdate(l string) bool { return strings.HasPrefix(l, "apt-get update") }
func accDpkgRemove(l string) bool {
	return strings.HasPrefix(l, "dpkg --remove")
}

func accReadOnly(l string) bool {
	return strings.HasPrefix(l, "dpkg-query ") || strings.HasPrefix(l, "apt-cache ") ||
		strings.HasPrefix(l, "apt-mark showmanual") || strings.HasPrefix(l, "dpkg --compare-versions") || accSim(l)
}

// mutations are the commands that can change the host: everything but queries
// and apt-get simulations.
func (a *accApt) mutations() []string {
	var out []string
	for _, l := range a.lines() {
		if !accReadOnly(l) {
			out = append(out, l)
		}
	}
	return out
}

func (a *accApt) count(pred func(string) bool) int {
	n := 0
	for _, l := range a.lines() {
		if pred(l) {
			n++
		}
	}
	return n
}

func (a *accApt) installed(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pkgs[name]
	return p != nil && p.status == "installed"
}

func (a *accApt) version(name string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.pkgs[name]; p != nil {
		return p.version
	}
	return ""
}

func accExit(code int, stderr string) error {
	err := exec.Command("/bin/sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	return fmt.Errorf("env LC_ALL=C: %w: %s", err, stderr)
}

func accIsDigit(c byte) bool { return c >= '0' && c <= '9' }
func accIsAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func accOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case accIsDigit(c):
		return 0
	case accIsAlpha(c):
		return int(c)
	}
	return int(c) + 256
}

func accVerrevcmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		first := 0
		for (i < len(a) && !accIsDigit(a[i])) || (j < len(b) && !accIsDigit(b[j])) {
			ac, bc := 0, 0
			if i < len(a) {
				ac = accOrder(a[i])
			}
			if j < len(b) {
				bc = accOrder(b[j])
			}
			if ac != bc {
				return ac - bc
			}
			i++
			j++
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		for i < len(a) && accIsDigit(a[i]) && j < len(b) && accIsDigit(b[j]) {
			if first == 0 {
				first = int(a[i]) - int(b[j])
			}
			i++
			j++
		}
		if i < len(a) && accIsDigit(a[i]) {
			return 1
		}
		if j < len(b) && accIsDigit(b[j]) {
			return -1
		}
		if first != 0 {
			return first
		}
	}
	return 0
}

// accCompare orders two Debian versions, epoch first.
func accCompare(a, b string) int {
	epoch := func(v string) (int, string) {
		if e, rest, ok := strings.Cut(v, ":"); ok {
			n, _ := strconv.Atoi(e)
			return n, rest
		}
		return 0, v
	}
	ea, ra := epoch(a)
	eb, rb := epoch(b)
	if ea != eb {
		return ea - eb
	}
	return accVerrevcmp(ra, rb)
}

var accVar = regexp.MustCompile(`\$\{([^}]+)\}`)

func (a *accApt) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	a.mu.Lock()
	if name != "env" || len(args) < 3 || args[0] != "LC_ALL=C" || args[1] != "DEBIAN_FRONTEND=noninteractive" {
		a.badEnv = append(a.badEnv, strings.Join(append([]string{name}, args...), " "))
		a.mu.Unlock()
		return nil, errors.New("fake: command not run through env LC_ALL=C DEBIAN_FRONTEND=noninteractive")
	}
	cmd, rest := args[2], args[3:]
	line := strings.Join(append([]string{cmd}, rest...), " ")
	a.log = append(a.log, line)
	var gate *accGate
	for _, g := range a.gates {
		if g.match(line) {
			gate = g
		}
	}
	a.mu.Unlock()
	if gate != nil {
		select {
		case gate.entered <- struct{}{}:
		default:
		}
		select {
		case <-gate.open:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for sub, msg := range a.fail {
		if strings.Contains(line, sub) {
			return nil, accExit(100, msg)
		}
	}
	switch cmd {
	case "dpkg-query":
		return a.dpkgQuery(rest)
	case "apt-cache":
		return a.aptCache(rest)
	case "apt-mark":
		return a.aptMark(rest)
	case "apt-get":
		return a.aptGet(rest)
	case "dpkg":
		return a.dpkg(rest)
	}
	return nil, accExit(127, cmd+": command not found")
}

func accAfterDashes(args []string) []string {
	if i := slices.Index(args, "--"); i >= 0 {
		return args[i+1:]
	}
	return nil
}

func (a *accApt) dpkgQuery(rest []string) ([]byte, error) {
	format := ""
	for i := 0; i < len(rest); i++ {
		switch {
		case strings.HasPrefix(rest[i], "-f="):
			format = rest[i][3:]
		case strings.HasPrefix(rest[i], "--showformat="):
			format = strings.TrimPrefix(rest[i], "--showformat=")
		case (rest[i] == "-f" || rest[i] == "--showformat") && i+1 < len(rest):
			format = rest[i+1]
			i++
		}
	}
	format = strings.NewReplacer(`\t`, "\t", `\n`, "\n").Replace(format)
	var out strings.Builder
	missing := false
	for _, n := range accAfterDashes(rest) {
		p := a.pkgs[n]
		if p == nil {
			missing = true
			continue
		}
		abbrev, status, dbstatus := "ii ", "install ok installed", "installed"
		if p.status == "config-files" {
			abbrev, status, dbstatus = "rc ", "deinstall ok config-files", "config-files"
		}
		vals := map[string]string{
			"Package": n, "db:Status-Status": dbstatus, "db:Status-Abbrev": abbrev, "Status": status,
			"Version": p.version, "Installed-Size": strconv.Itoa(p.sizeKiB), "Priority": p.priority,
			"Section": p.section, "binary:Summary": p.desc, "Architecture": "amd64",
		}
		out.WriteString(accVar.ReplaceAllStringFunc(format, func(m string) string { return vals[m[2:len(m)-1]] }))
	}
	if missing {
		return []byte(out.String()), accExit(1, "dpkg-query: no packages found matching")
	}
	return []byte(out.String()), nil
}

func (a *accApt) aptCache(rest []string) ([]byte, error) {
	if len(rest) == 0 {
		return nil, accExit(100, "E: no operation")
	}
	switch rest[0] {
	case "policy":
		var out strings.Builder
		for _, n := range accAfterDashes(rest) {
			cand, hasCand := a.cands[n]
			p := a.pkgs[n]
			if !hasCand && !a.noCand[n] && p == nil {
				continue
			}
			inst := "(none)"
			if p != nil && p.status == "installed" {
				inst = p.version
			}
			if !hasCand {
				cand = "(none)"
			}
			fmt.Fprintf(&out, "%s:\n  Installed: %s\n  Candidate: %s\n  Version table:\n", n, inst, cand)
			if hasCand {
				fmt.Fprintf(&out, "     %s 500\n        500 http://deb.debian.org/debian trixie/main amd64 Packages\n", cand)
			}
		}
		return []byte(out.String()), nil
	case "search":
		pats := accAfterDashes(rest)
		if len(pats) != 1 {
			return nil, accExit(100, "E: one pattern expected")
		}
		re, err := regexp.Compile(pats[0])
		if err != nil {
			return nil, accExit(100, "E: Regex compilation error - "+err.Error())
		}
		names := make([]string, 0, len(a.repo))
		for n := range a.repo {
			if re.MatchString(n) {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		var out strings.Builder
		for _, n := range names {
			fmt.Fprintf(&out, "%s - %s\n", n, a.repo[n])
		}
		return []byte(out.String()), nil
	}
	return nil, accExit(100, "E: invalid operation "+rest[0])
}

func (a *accApt) aptMark(rest []string) ([]byte, error) {
	if len(rest) == 0 {
		return nil, accExit(100, "E: no operation")
	}
	switch rest[0] {
	case "showmanual":
		var names []string
		for n, p := range a.pkgs {
			if p.status == "installed" && p.manual {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		return []byte(strings.Join(names, "\n") + "\n"), nil
	case "manual", "auto":
		for _, n := range accAfterDashes(rest) {
			p := a.pkgs[n]
			if p == nil || p.status != "installed" {
				return nil, accExit(100, "E: Can't select versions from package '"+n+"' as it is purely virtual")
			}
			p.manual = rest[0] == "manual"
		}
		return nil, nil
	}
	return nil, accExit(100, "E: invalid operation "+rest[0])
}

func accHas(args []string, flag string) bool { return slices.Contains(args, flag) }

func (a *accApt) aptGet(rest []string) ([]byte, error) {
	if len(rest) == 0 {
		return nil, accExit(100, "E: no operation")
	}
	switch rest[0] {
	case "update":
		a.updates++
		return []byte("Reading package lists...\n"), nil
	case "install":
		return a.install(rest)
	}
	a.banned = append(a.banned, "apt-get "+strings.Join(rest, " "))
	return nil, nil
}

func (a *accApt) install(rest []string) ([]byte, error) {
	names := accAfterDashes(rest)
	noRemove, only, sim := accHas(rest, "--no-remove"), accHas(rest, "--only-upgrade"), accHas(rest, "-s")
	for _, n := range names {
		if _, ok := a.cands[n]; !ok {
			return nil, accExit(100, "E: Unable to locate package "+n)
		}
		if gone := a.conflict[n]; len(gone) > 0 {
			if noRemove {
				return nil, accExit(100, "E: Packages need to be removed but remove is disabled.")
			}
			if !sim {
				for _, g := range gone {
					if p := a.pkgs[g]; p != nil {
						p.status = "config-files"
						a.removed = append(a.removed, g)
					}
				}
			}
		}
	}
	if sim {
		var out strings.Builder
		out.WriteString("NOTE: This is only a simulation!\nReading package lists...\n")
		for _, n := range names {
			p := a.pkgs[n]
			switch {
			case p != nil && p.status == "installed" && only && accCompare(a.cands[n], p.version) > 0:
				fmt.Fprintf(&out, "Inst %s [%s] (%s Debian:13/stable [amd64])\n", n, p.version, a.cands[n])
			case p != nil && p.status == "installed":
				continue
			default:
				fmt.Fprintf(&out, "Inst %s (%s Debian:13/stable [amd64])\n", n, a.cands[n])
			}
			for _, d := range a.simNew[n] {
				fmt.Fprintf(&out, "Inst %s (1.0-1 Debian:13/stable [amd64])\n", d)
			}
			for _, u := range a.simUp[n] {
				if up := a.pkgs[u]; up != nil {
					fmt.Fprintf(&out, "Inst %s [%s] (%s Debian:13/stable [amd64])\n", u, up.version, a.cands[u])
				}
			}
		}
		return []byte(out.String()), nil
	}
	for _, n := range names {
		p := a.pkgs[n]
		switch {
		case p != nil && p.status == "installed" && only:
			p.version = a.cands[n]
			continue
		case only:
			continue
		case p == nil || p.status != "installed":
			p = a.add(n, a.cands[n], "optional", "utils")
		default:
			p.version = a.cands[n]
		}
		for _, d := range a.simNew[n] {
			if a.pkgs[d] == nil {
				q := a.add(d, "1.0-1", "optional", "libs")
				q.manual = false
			}
		}
		for _, u := range a.simUp[n] {
			if up := a.pkgs[u]; up != nil {
				up.version = a.cands[u]
			}
		}
	}
	return []byte("Setting up...\n"), nil
}

func (a *accApt) dpkg(rest []string) ([]byte, error) {
	if len(rest) == 0 {
		return nil, accExit(2, "dpkg: need an action option")
	}
	switch rest[0] {
	case "--compare-versions":
		if len(rest) != 4 || rest[2] != "gt" {
			return nil, accExit(2, "dpkg: unsupported comparison")
		}
		if accCompare(rest[1], rest[3]) > 0 {
			return nil, nil
		}
		return nil, accExit(1, "")
	case "--remove":
		for _, n := range accAfterDashes(rest) {
			p := a.pkgs[n]
			if p == nil || p.status != "installed" {
				continue
			}
			for other, o := range a.pkgs {
				if o.status == "installed" && slices.Contains(o.deps, n) {
					return nil, accExit(2, fmt.Sprintf("dpkg: dependency problems prevent removal of %s:\n %s depends on %s.\n\ndpkg: error processing package %s (--remove):\n dependency problems - not removing", n, other, n, n))
				}
			}
			p.status, p.manual = "config-files", false
		}
		return nil, nil
	}
	return nil, accExit(2, "dpkg: unsupported action "+rest[0])
}

// ---------------------------------------------------------------------------
// The stack under test: config.yaml in a temp dir, a daemon Store, the real
// storage service over packages.New(world.run), the real API handler.
// ---------------------------------------------------------------------------

type accHold struct {
	mu       sync.Mutex
	err      error
	calls    int
	releases int
}

func (h *accHold) fn(context.Context) (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.err != nil {
		return nil, h.err
	}
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.releases++
	}, nil
}

func (h *accHold) set(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.err = err
}

func (h *accHold) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

type accTools struct{ root string }

func (accTools) Get(tool string) (toolchain.Installer, error) {
	return nil, fmt.Errorf("%w %q", toolchain.ErrUnknownTool, tool)
}
func (accTools) Available(context.Context, string) ([]toolchain.Choice, error) { return nil, nil }
func (accTools) Installed() ([]toolchain.Installed, error)                     { return nil, nil }
func (accTools) Other() ([]string, error)                                      { return nil, nil }
func (t accTools) Root() string                                                { return t.root }
func (accTools) CleanTmp() error                                               { return nil }

// accPATH puts executables named like the apt tools on PATH, and nothing else,
// so packages.New's Available finds them.
func accPATH(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

var accAptTools = []string{"apt-get", "apt-cache", "apt-mark", "dpkg", "dpkg-query"}

// accConfig is the test config.yaml with a packages list.
func accConfig(pkgs ...string) string {
	if len(pkgs) == 0 {
		return cfgYAML
	}
	var b strings.Builder
	b.WriteString(cfgYAML + "packages:\n")
	for _, p := range pkgs {
		b.WriteString("  - " + strconv.Quote(p) + "\n")
	}
	return b.String()
}

type accOpts struct {
	packages []string         // config.yaml's packages list
	world    func(w *accApt)  // set up before the service starts
	nilApt   bool             // a Service with no Manager
	pathMiss string           // leave this tool off PATH
	stampAge time.Duration    // age of the index stamp; 0 is fresh, negative leaves no stamp
	noStart  bool             // do not start the service
	lists    func(dir string) // fill apt's lists directory
}

type accStack struct {
	t     *testing.T
	apt   *accApt
	store *Store
	svc   *storage.Service
	b     *Backend
	reg   *jobs.Registry
	hold  *accHold
	srv   *httptest.Server
	stamp string
	lists string
}

func newAcc(t *testing.T, o accOpts) *accStack {
	t.Helper()
	var tools []string
	for _, n := range accAptTools {
		if n != o.pathMiss {
			tools = append(tools, n)
		}
	}
	accPATH(t, tools...)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(accConfig(o.packages...)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("tok1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _, err := OpenStore(cfgPath, filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	s := &accStack{t: t, apt: newAccApt(), store: store, hold: &accHold{}, stamp: filepath.Join(dir, "packages-index"), lists: filepath.Join(dir, "lists")}
	if err := os.MkdirAll(s.lists, 0o755); err != nil {
		t.Fatal(err)
	}
	if o.lists != nil {
		o.lists(s.lists)
	}
	if o.stampAge >= 0 {
		if err := os.WriteFile(s.stamp, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-o.stampAge).Truncate(time.Second)
		if err := os.Chtimes(s.stamp, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if o.world != nil {
		o.world(s.apt)
	}
	s.reg = jobs.New(func() config.Jobs { return store.Config().Jobs }, time.Now, nil)
	svc := &storage.Service{
		Tools: accTools{root: t.TempDir()}, Docker: nopDisk{}, Home: t.TempDir(), Hold: s.hold.fn, Events: events.New(), Jobs: s.reg,
		Managed: managedList{store: store}, Base: packages.Base("debian", "13"), AptLists: s.lists, IndexStamp: s.stamp,
	}
	if !o.nilApt {
		svc.Apt = packages.New(s.apt.run)
	}
	s.svc = svc
	b, _, _ := newBackend(t)
	b.Store, b.Space, b.Pkgs, b.Jobs = store, svc, svc, s.reg
	s.b = b
	s.srv = httptest.NewServer(api.NewServer(b))
	t.Cleanup(s.srv.Close)
	if !o.noStart {
		s.start()
	}
	return s
}

func (s *accStack) start() {
	s.t.Helper()
	s.svc.Start()
	s.t.Cleanup(func() {
		s.apt.mu.Lock()
		gates := slices.Clone(s.apt.gates)
		s.apt.mu.Unlock()
		for _, g := range gates {
			g.Open()
		}
		s.svc.Close()
		s.svc.Wait()
		s.b.StopBackground()
		s.b.Wait()
	})
	// The start-up reconcile runs in the background; until it returns it may
	// pick up a name a test lists after start, or bump the packages topic.
	select {
	case <-s.svc.Reconciled():
	case <-time.After(10 * time.Second):
		s.t.Fatal("the start-up reconcile did not return")
	}
}

func (s *accStack) do(method, path, body string) (int, string) {
	s.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	c := s.srv.Client()
	c.Timeout = 15 * time.Second
	resp, err := c.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (s *accStack) mustDo(method, path, body string, want int) string {
	s.t.Helper()
	st, out := s.do(method, path, body)
	if st != want {
		s.t.Fatalf("%s %s %s: status %d (%s), want %d", method, path, body, st, strings.TrimSpace(out), want)
	}
	return out
}

func (s *accStack) listing() model.Packages {
	s.t.Helper()
	var p model.Packages
	if err := json.Unmarshal([]byte(s.mustDo("GET", "/packages", "", 200)), &p); err != nil {
		s.t.Fatal(err)
	}
	return p
}

func (s *accStack) idle() bool {
	o := s.svc.Snapshot().Operations
	return o.Current == nil && o.Queued == 0
}

// waitDone waits until the queue is empty and at least n operations ended.
func (s *accStack) waitDone(n int) []model.Operation {
	s.t.Helper()
	waitFor(s.t, fmt.Sprintf("%d operations to end", n), func() bool {
		return s.idle() && len(s.svc.Snapshot().Operations.Recent) >= n
	})
	return s.svc.Snapshot().Operations.Recent
}

func (s *accStack) waitQueued(n int) {
	s.t.Helper()
	waitFor(s.t, fmt.Sprintf("%d queued operations", n), func() bool { return s.svc.Snapshot().Operations.Queued == n })
}

// recent finds the newest ended operation of kind on target.
func (s *accStack) recent(kind, target string) model.Operation {
	s.t.Helper()
	for _, o := range s.svc.Snapshot().Operations.Recent {
		if o.Kind == kind && o.Target == target {
			return o
		}
	}
	s.t.Fatalf("no ended %s operation on %q in %+v", kind, target, s.svc.Snapshot().Operations.Recent)
	return model.Operation{}
}

func (s *accStack) cfgRaw() string {
	s.t.Helper()
	b, err := os.ReadFile(s.store.ConfigPath)
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}

// cfgPackages is what config.yaml lists on disk now.
func (s *accStack) cfgPackages() []string {
	s.t.Helper()
	c, _, err := config.Parse([]byte(s.cfgRaw()))
	if err != nil {
		s.t.Fatal(err)
	}
	return c.Packages
}

func (s *accStack) job(name string) model.JobStatus {
	for _, j := range s.reg.Snapshot() {
		if j.Name == name {
			return j
		}
	}
	s.t.Fatalf("no task %s", name)
	return model.JobStatus{}
}

func accNames(infos []model.PackageInfo) []string {
	var out []string
	for _, i := range infos {
		out = append(out, i.Name)
	}
	sort.Strings(out)
	return out
}

func accByName(infos []model.PackageInfo) map[string]model.PackageInfo {
	out := map[string]model.PackageInfo{}
	for _, i := range infos {
		out[i.Name] = i
	}
	return out
}

// accAfterSep returns the arguments after "--" on a command line.
func accAfterSep(line string) []string {
	f := strings.Fields(line)
	if i := slices.Index(f, "--"); i >= 0 {
		return f[i+1:]
	}
	return nil
}

// accInOrder reports whether want appears in fields as a subsequence.
func accInOrder(fields []string, want ...string) bool {
	i := 0
	for _, f := range fields {
		if i < len(want) && f == want[i] {
			i++
		}
	}
	return i == len(want)
}

func accRealInstalls(a *accApt) []string {
	var out []string
	for _, l := range a.lines() {
		if accRealInstall(l) {
			out = append(out, l)
		}
	}
	return out
}

var (
	accBusy     = model.BusyError{N: 1}
	accStarting = fmt.Errorf("%w: a runner is starting", runner.ErrHoldRefused)
)

// ---------------------------------------------------------------------------
// A1 to A26
// ---------------------------------------------------------------------------

func TestA1_InstallQueuesOneOperationAndListsTheNameOnSuccess(t *testing.T) {
	s := newAcc(t, accOpts{world: func(w *accApt) { w.cands["ripgrep"] = "14.1.0-1" }})
	gate := s.apt.gate(accRealInstall)
	var res model.AddPackagesResult
	if err := json.Unmarshal([]byte(s.mustDo("POST", "/packages", `{"names":["ripgrep"]}`, 202)), &res); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Install, []string{"ripgrep"}) || len(res.Adopt) != 0 {
		t.Fatalf("answer %+v", res)
	}
	gate.WaitEntered(t)
	if slices.Contains(s.cfgPackages(), "ripgrep") {
		t.Fatal("config.yaml listed ripgrep before apt-get finished")
	}
	gate.Open()
	ops := s.waitDone(1)
	if len(ops) != 1 || ops[0].Kind != "install" || ops[0].Target != "apt ripgrep" || ops[0].Outcome != "ok" {
		t.Fatalf("operations %+v", ops)
	}
	installs := accRealInstalls(s.apt)
	if len(installs) != 1 {
		t.Fatalf("apt-get install ran %d times: %q", len(installs), installs)
	}
	f := strings.Fields(installs[0])
	if !accInOrder(f, "apt-get", "install", "-y", "--no-install-recommends", "--no-remove", "--", "ripgrep") || !slices.Equal(accAfterSep(installs[0]), []string{"ripgrep"}) {
		t.Fatalf("install command %q", installs[0])
	}
	if len(s.apt.badEnv) != 0 {
		t.Fatalf("commands run without env LC_ALL=C DEBIAN_FRONTEND=noninteractive: %q", s.apt.badEnv)
	}
	if !slices.Contains(s.cfgPackages(), "ripgrep") {
		t.Fatalf("config.yaml lists %q after a successful install", s.cfgPackages())
	}
	if !s.apt.installed("ripgrep") {
		t.Fatal("ripgrep is not installed")
	}
}

func TestA2_AddingAnInstalledNameAdoptsIt(t *testing.T) {
	s := newAcc(t, accOpts{world: func(w *accApt) {
		w.add("htop", "3.4.1-5", "optional", "admin").manual = false
		w.cands["htop"] = "3.4.1-5"
	}})
	var res model.AddPackagesResult
	if err := json.Unmarshal([]byte(s.mustDo("POST", "/packages", `{"names":["htop"]}`, 202)), &res); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Adopt, []string{"htop"}) || len(res.Install) != 0 {
		t.Fatalf("answer %+v", res)
	}
	ops := s.waitDone(1)
	if len(ops) != 1 || ops[0].Kind != "adopt" || ops[0].Target != "apt htop" || ops[0].Outcome != "ok" {
		t.Fatalf("operations %+v", ops)
	}
	if got := s.apt.mutations(); !slices.Equal(got, []string{"apt-mark manual -- htop"}) {
		t.Fatalf("mutating commands %q", got)
	}
	if n := s.apt.count(func(l string) bool { return strings.HasPrefix(l, "apt-get") }); n != 0 {
		t.Fatalf("apt-get ran %d times for an adopt", n)
	}
	if !slices.Contains(s.cfgPackages(), "htop") {
		t.Fatalf("config.yaml lists %q", s.cfgPackages())
	}
	s.apt.mu.Lock()
	manual := s.apt.pkgs["htop"].manual
	s.apt.mu.Unlock()
	if !manual {
		t.Fatal("htop is not marked manual")
	}
}

func TestA3_InvalidNamesAreRefusedAndNoCommandRuns(t *testing.T) {
	s := newAcc(t, accOpts{world: func(w *accApt) { w.cands["ripgrep"] = "14.1.0-1" }})
	for _, bad := range []string{"-o", "jq;rm", "--force-yes", "Jq", "a", "", "$(id)", "a b", "../etc", "-", "+ab", ".ab", "rip\ngrep", strings.Repeat("a", 64)} {
		body, _ := json.Marshal(map[string][]string{"names": {bad}})
		if st, out := s.do("POST", "/packages", string(body)); st != 400 {
			t.Errorf("name %q: status %d (%s), want 400", bad, st, strings.TrimSpace(out))
		}
	}
	// One bad name among good ones queues none of them.
	if st, out := s.do("POST", "/packages", `{"names":["ripgrep","-o"]}`); st != 400 {
		t.Errorf("mixed names: status %d (%s), want 400", st, strings.TrimSpace(out))
	}
	if st, _ := s.do("POST", "/packages", `{"names":[]}`); st != 400 {
		t.Errorf("no names: status %d, want 400", st)
	}
	time.Sleep(200 * time.Millisecond)
	if lines := s.apt.lines(); len(lines) != 0 {
		t.Fatalf("commands ran for refused names: %q", lines)
	}
	o := s.svc.Snapshot().Operations
	if o.Queued != 0 || o.Current != nil || len(o.Recent) != 0 {
		t.Fatalf("operations %+v", o)
	}
	if len(s.cfgPackages()) != 0 {
		t.Fatalf("config.yaml lists %q", s.cfgPackages())
	}
	// The same names are refused by upgrade and uninstall.
	for _, bad := range []string{"-o", "jq;rm"} {
		if st, _ := s.do("DELETE", "/packages/"+url.PathEscape(bad)+"?uninstall=1", ""); st < 400 {
			t.Errorf("uninstall %q: status %d", bad, st)
		}
	}
	if lines := s.apt.lines(); len(lines) != 0 {
		t.Fatalf("commands ran for refused names: %q", lines)
	}
}

func TestA4_UninstallRunsOnlyDpkgRemoveAndKeepsTheNameWhenDpkgRefuses(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find"}, world: func(w *accApt) {
		w.add("ripgrep", "14.1.0-1", "optional", "utils")
		w.add("fd-find", "10.2.0-1", "optional", "utils")
		w.add("libpcre2-8-0", "10.42-4", "optional", "libs").manual = false
		w.add("ripgrep-all", "0.10-1", "optional", "utils").deps = []string{"fd-find"}
		w.pkgs["ripgrep"].deps = []string{"libpcre2-8-0"}
	}})
	s.mustDo("DELETE", "/packages/ripgrep?uninstall=1", "", 202)
	ops := s.waitDone(1)
	if ops[0].Kind != "remove" || ops[0].Outcome != "ok" {
		t.Fatalf("operations %+v", ops)
	}
	if got := s.apt.mutations(); !slices.Equal(got, []string{"dpkg --remove -- ripgrep"}) {
		t.Fatalf("mutating commands %q", got)
	}
	if n := s.apt.count(func(l string) bool { return strings.HasPrefix(l, "apt-get") }); n != 0 {
		t.Fatalf("apt-get ran %d times for an uninstall", n)
	}
	if len(s.apt.banned) != 0 || len(s.apt.removed) != 0 {
		t.Fatalf("apt removed packages: %q %q", s.apt.banned, s.apt.removed)
	}
	if s.apt.installed("ripgrep") || !s.apt.installed("libpcre2-8-0") || !s.apt.installed("ripgrep-all") || !s.apt.installed("fd-find") {
		t.Fatal("the uninstall changed more than ripgrep")
	}
	if slices.Contains(s.cfgPackages(), "ripgrep") || !slices.Contains(s.cfgPackages(), "fd-find") {
		t.Fatalf("config.yaml lists %q", s.cfgPackages())
	}

	// ripgrep-all depends on fd-find, so dpkg refuses and the name stays.
	before := s.cfgRaw()
	s.mustDo("DELETE", "/packages/fd-find?uninstall=1", "", 202)
	ops = s.waitDone(2)
	op := s.recent("remove", "apt fd-find")
	if op.Outcome != "failed" || !strings.Contains(op.Message, "dependency problems") {
		t.Fatalf("operation %+v of %+v", op, ops)
	}
	if !s.apt.installed("fd-find") || !s.apt.installed("ripgrep-all") {
		t.Fatal("a refused uninstall removed a package")
	}
	if !slices.Contains(s.cfgPackages(), "fd-find") || s.cfgRaw() != before {
		t.Fatalf("config.yaml changed after dpkg refused: %q", s.cfgPackages())
	}
	want := []string{"dpkg --remove -- ripgrep", "dpkg --remove -- fd-find"}
	if got := s.apt.mutations(); !slices.Equal(got, want) {
		t.Fatalf("mutating commands %q, want %q", got, want)
	}
}

func TestA5_ChangingAptCallsPassNoRemoveAndAnAbortLeavesConfigAlone(t *testing.T) {
	conflicted := func(w *accApt) {
		w.add("systemd-timesyncd", "257-1", "optional", "admin")
		w.cands["conflicty"] = "2.0-1"
		w.conflict["conflicty"] = []string{"systemd-timesyncd"}
	}
	checkAll := func(t *testing.T, s *accStack) {
		t.Helper()
		var installs int
		for _, l := range s.apt.lines() {
			if strings.HasPrefix(l, "apt-get install") {
				installs++
				if !slices.Contains(strings.Fields(l), "--no-remove") {
					t.Errorf("apt-get install without --no-remove: %q", l)
				}
			}
		}
		if installs == 0 {
			t.Error("no apt-get install ran")
		}
		if len(s.apt.removed) != 0 || len(s.apt.banned) != 0 || !s.apt.installed("systemd-timesyncd") {
			t.Errorf("apt removed packages: %q %q", s.apt.removed, s.apt.banned)
		}
	}
	t.Run("install", func(t *testing.T) {
		s := newAcc(t, accOpts{world: conflicted})
		before := s.cfgRaw()
		s.mustDo("POST", "/packages", `{"names":["conflicty"]}`, 202)
		s.waitDone(1)
		op := s.recent("install", "apt conflicty")
		if op.Outcome != "failed" || !strings.Contains(op.Message, "remove is disabled") {
			t.Fatalf("operation %+v", op)
		}
		if s.cfgRaw() != before {
			t.Fatalf("config.yaml changed:\n%s", s.cfgRaw())
		}
		checkAll(t, s)
	})
	t.Run("upgrade", func(t *testing.T) {
		s := newAcc(t, accOpts{packages: []string{"conflicty"}, world: func(w *accApt) {
			conflicted(w)
			w.add("conflicty", "1.0-1", "optional", "utils")
		}})
		before := s.cfgRaw()
		s.mustDo("POST", "/packages/upgrade", `{"names":["conflicty"]}`, 202)
		s.waitDone(1)
		op := s.recent("upgrade", "apt conflicty")
		if op.Outcome != "failed" || !strings.Contains(op.Message, "remove is disabled") {
			t.Fatalf("operation %+v", op)
		}
		if s.cfgRaw() != before || s.apt.version("conflicty") != "1.0-1" {
			t.Fatalf("config.yaml or the package changed:\n%s", s.cfgRaw())
		}
		checkAll(t, s)
	})
	t.Run("upgrade all", func(t *testing.T) {
		s := newAcc(t, accOpts{packages: []string{"conflicty"}, world: func(w *accApt) {
			conflicted(w)
			w.add("conflicty", "1.0-1", "optional", "utils")
		}})
		before := s.cfgRaw()
		s.mustDo("POST", "/packages/upgrade", `{"all":true}`, 202)
		s.waitDone(1)
		if ops := s.svc.Snapshot().Operations.Recent; ops[0].Outcome != "failed" {
			t.Fatalf("operations %+v", ops)
		}
		if s.cfgRaw() != before {
			t.Fatalf("config.yaml changed:\n%s", s.cfgRaw())
		}
		checkAll(t, s)
	})
	t.Run("reconcile", func(t *testing.T) {
		s := newAcc(t, accOpts{packages: []string{"conflicty"}, world: conflicted})
		before := s.cfgRaw()
		s.waitDone(1)
		op := s.recent("install", "apt conflicty")
		if op.Outcome != "failed" || !strings.Contains(op.Message, "remove is disabled") {
			t.Fatalf("operation %+v", op)
		}
		if s.cfgRaw() != before {
			t.Fatalf("config.yaml changed:\n%s", s.cfgRaw())
		}
		checkAll(t, s)
	})
}

func TestA6_ForgetDropsTheNameAndItsQueuedOperationsAndRunsNothing(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find"}, world: func(w *accApt) {
		w.add("ripgrep", "1.0-1", "optional", "utils")
		w.add("fd-find", "1.0-1", "optional", "utils")
		w.cands["ripgrep"], w.cands["fd-find"] = "2.0-1", "2.0-1"
	}})
	gate := s.apt.gate(accUpdate)
	s.mustDo("POST", "/packages/refresh", "", 202)
	gate.WaitEntered(t)
	s.mustDo("POST", "/packages/upgrade", `{"names":["ripgrep"]}`, 202)
	s.mustDo("DELETE", "/packages/ripgrep?uninstall=1", "", 202)
	s.mustDo("POST", "/packages/upgrade", `{"names":["fd-find"]}`, 202)
	s.waitQueued(3)
	ran := len(s.apt.lines())
	s.mustDo("DELETE", "/packages/ripgrep", "", 204)
	if got := len(s.apt.lines()); got != ran {
		t.Fatalf("forget ran %d commands: %q", got-ran, s.apt.lines()[ran:])
	}
	if got := s.svc.Snapshot().Operations.Queued; got != 1 {
		t.Fatalf("%d operations still queued, want only fd-find's", got)
	}
	if p := s.cfgPackages(); slices.Contains(p, "ripgrep") || !slices.Contains(p, "fd-find") {
		t.Fatalf("config.yaml lists %q", p)
	}
	gate.Open()
	s.waitDone(2)
	for _, l := range s.apt.mutations() {
		if strings.Contains(l, "ripgrep") {
			t.Errorf("a command named the forgotten package: %q", l)
		}
	}
	if !s.apt.installed("ripgrep") || s.apt.version("ripgrep") != "1.0-1" {
		t.Fatal("forget changed the installed package")
	}
	if s.apt.version("fd-find") != "2.0-1" {
		t.Fatalf("the other queued upgrade did not run: fd-find %s", s.apt.version("fd-find"))
	}
}

func TestA7_BasePackagesCannotBeAddedUpgradedOrUninstalled(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"curl", "ripgrep"}, world: func(w *accApt) {
		w.add("ripgrep", "1.0-1", "optional", "utils")
		w.cands["ripgrep"] = "2.0-1"
		w.pkgs["curl"].version = "1.0-1"
		w.cands["curl"], w.cands["git"], w.cands["jq"] = "1.1-1", "1.1-1", "1.1-1"
	}})
	base := packages.Base("debian", "13")
	for _, n := range []string{"git", "jq", "curl"} {
		if !slices.Contains(base, n) {
			t.Fatalf("%s is not in packages.Base", n)
		}
	}
	for _, n := range []string{"git", "jq", "curl"} {
		body := fmt.Sprintf(`{"names":[%q]}`, n)
		if st, out := s.do("POST", "/packages", body); st != 400 {
			t.Errorf("add %s: status %d (%s), want 400", n, st, strings.TrimSpace(out))
		}
		if st, out := s.do("POST", "/packages/upgrade", body); st != 400 {
			t.Errorf("upgrade %s: status %d (%s), want 400", n, st, strings.TrimSpace(out))
		}
	}
	if st, out := s.do("DELETE", "/packages/curl?uninstall=1", ""); st != 400 {
		t.Errorf("uninstall curl (base and listed): status %d (%s), want 400", st, strings.TrimSpace(out))
	}
	if st, _ := s.do("DELETE", "/packages/git?uninstall=1", ""); st < 400 {
		t.Errorf("uninstall git (base): status %d", st)
	}
	time.Sleep(200 * time.Millisecond)
	if got := s.apt.mutations(); len(got) != 0 {
		t.Fatalf("a refused request ran %q", got)
	}
	if o := s.svc.Snapshot().Operations; o.Queued != 0 || len(o.Recent) != 0 {
		t.Fatalf("operations %+v", o)
	}

	p := s.listing()
	if !slices.Contains(accNames(p.Base), "curl") {
		t.Errorf("curl is not listed under base: %q", accNames(p.Base))
	}
	if got := accNames(p.Managed); !slices.Equal(got, []string{"ripgrep"}) {
		t.Errorf("managed lists %q, want only ripgrep", got)
	}

	// upgrade-all leaves the base name out.
	s.mustDo("POST", "/packages/upgrade", `{"all":true}`, 202)
	s.waitDone(1)
	var upgraded [][]string
	for _, l := range accRealInstalls(s.apt) {
		upgraded = append(upgraded, accAfterSep(l))
	}
	if len(upgraded) != 1 || !slices.Equal(upgraded[0], []string{"ripgrep"}) {
		t.Fatalf("upgrade-all installed %q, want only ripgrep", upgraded)
	}
	if s.apt.version("curl") != "1.0-1" {
		t.Fatalf("curl was upgraded to %s", s.apt.version("curl"))
	}

	// A base name in config.yaml can be forgotten.
	s.mustDo("DELETE", "/packages/curl", "", 204)
	if got := s.cfgPackages(); !slices.Equal(got, []string{"ripgrep"}) {
		t.Fatalf("config.yaml lists %q", got)
	}
	if !s.apt.installed("curl") {
		t.Fatal("forget removed curl")
	}
}

func TestA8_UpgradableOnlyWhenTheCandidateIsGreater(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"newer", "same", "epoch", "older", "missing"}, world: func(w *accApt) {
		w.add("newer", "1.0-1", "optional", "utils")
		w.cands["newer"] = "1.0-2"
		w.add("same", "2.0-1", "optional", "utils")
		w.cands["same"] = "2.0-1"
		w.add("epoch", "1:1.0-1", "optional", "utils")
		w.cands["epoch"] = "2.0-1"
		w.add("older", "3.0-1", "optional", "utils")
		w.cands["older"] = "2.9-1"
		w.cands["missing"] = "0.24-1"
	}})
	got := accByName(s.listing().Managed)
	for name, want := range map[string]bool{"newer": true, "same": false, "epoch": false, "older": false, "missing": false} {
		i, ok := got[name]
		if !ok {
			t.Fatalf("%s is not listed: %+v", name, got)
		}
		if i.Upgradable != want {
			t.Errorf("%s: upgradable %v, want %v (%+v)", name, i.Upgradable, want, i)
		}
	}
	if got["newer"].Candidate != "1.0-2" || got["newer"].Version != "1.0-1" {
		t.Errorf("newer: %+v", got["newer"])
	}
}

func TestA9_UpgradeAndUninstallAreRefusedWhileRunnersAreBusy(t *testing.T) {
	for _, busy := range []error{accBusy, accStarting} {
		t.Run(busy.Error(), func(t *testing.T) {
			s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find"}, world: func(w *accApt) {
				w.add("ripgrep", "1.0-1", "optional", "utils")
				w.add("fd-find", "1.0-1", "optional", "utils")
				w.cands["ripgrep"], w.cands["fd-find"] = "2.0-1", "2.0-1"
			}})
			s.hold.set(busy)
			s.mustDo("POST", "/packages/upgrade", `{"names":["ripgrep"]}`, 202)
			s.waitDone(1)
			op := s.recent("upgrade", "apt ripgrep")
			if op.Outcome != "refused" || !strings.Contains(op.Message, busy.Error()) {
				t.Fatalf("upgrade %+v", op)
			}
			s.mustDo("DELETE", "/packages/fd-find?uninstall=1", "", 202)
			s.waitDone(2)
			op = s.recent("remove", "apt fd-find")
			if op.Outcome != "refused" || !strings.Contains(op.Message, busy.Error()) {
				t.Fatalf("uninstall %+v", op)
			}
			if n := len(accRealInstalls(s.apt)); n != 0 {
				t.Fatalf("a refused upgrade ran apt-get install %d times", n)
			}
			if n := s.apt.count(accDpkgRemove); n != 0 {
				t.Fatalf("a refused uninstall ran dpkg --remove %d times", n)
			}
			if s.apt.version("ripgrep") != "1.0-1" || !s.apt.installed("fd-find") || !slices.Contains(s.cfgPackages(), "fd-find") {
				t.Fatal("a refused operation changed the host or config.yaml")
			}

			// Once the runners are idle the same requests go through.
			s.hold.set(nil)
			s.mustDo("POST", "/packages/upgrade", `{"names":["ripgrep"]}`, 202)
			s.mustDo("DELETE", "/packages/fd-find?uninstall=1", "", 202)
			s.waitDone(4)
			if s.apt.version("ripgrep") != "2.0-1" || s.apt.installed("fd-find") {
				t.Fatalf("operations did not run after the hold: ripgrep %s, fd-find installed %v", s.apt.version("ripgrep"), s.apt.installed("fd-find"))
			}
		})
	}
}

func TestA10_AnInstallThatUpgradesOthersHoldsTheRunners(t *testing.T) {
	world := func(w *accApt) {
		w.add("libshared1", "1.0-1", "optional", "libs").manual = false
		w.cands["libshared1"] = "1.1-1"
		w.cands["addon"], w.cands["plain"], w.cands["plus-lib"] = "1.0-1", "1.0-1", "1.0-1"
		w.simUp["addon"] = []string{"libshared1"}
		w.simNew["plus-lib"] = []string{"libnew1"}
	}
	s := newAcc(t, accOpts{world: world})
	s.mustDo("POST", "/packages", `{"names":["plain"]}`, 202)
	s.mustDo("POST", "/packages", `{"names":["plus-lib"]}`, 202)
	s.waitDone(2)
	if n := s.hold.callCount(); n != 0 {
		t.Fatalf("installs that only add packages held the runners %d times", n)
	}
	if !s.apt.installed("plain") || !s.apt.installed("plus-lib") || !s.apt.installed("libnew1") {
		t.Fatal("the installs did not run")
	}
	s.mustDo("POST", "/packages", `{"names":["addon"]}`, 202)
	s.waitDone(3)
	if n := s.hold.callCount(); n != 1 {
		t.Fatalf("an install that upgrades libshared1 held the runners %d times, want 1", n)
	}
	if s.apt.version("libshared1") != "1.1-1" || !s.apt.installed("addon") {
		t.Fatal("the upgrading install did not run")
	}
	s.hold.mu.Lock()
	releases := s.hold.releases
	s.hold.mu.Unlock()
	if releases != 1 {
		t.Fatalf("the hold was released %d times", releases)
	}

	// With the runners busy, the upgrading install is refused and installs nothing.
	r := newAcc(t, accOpts{world: world})
	r.hold.set(accBusy)
	r.mustDo("POST", "/packages", `{"names":["addon"]}`, 202)
	r.waitDone(1)
	op := r.recent("install", "apt addon")
	if op.Outcome != "refused" {
		t.Fatalf("operation %+v", op)
	}
	if n := len(accRealInstalls(r.apt)); n != 0 || r.apt.version("libshared1") != "1.0-1" || slices.Contains(r.cfgPackages(), "addon") {
		t.Fatal("a refused install changed the host or config.yaml")
	}
}

func TestA11_ReconcileQueuesOneInstallPerMissingNameAndRecordsMissingCandidates(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find", "yq", "zzz-gone", "bat"}, world: func(w *accApt) {
		w.add("fd-find", "1.0-1", "optional", "utils")
		w.cands["fd-find"], w.cands["ripgrep"], w.cands["bat"] = "1.0-1", "14.1.0-1", "0.24-1"
		w.noCand["yq"] = true
	}})
	s.waitDone(2)
	var installs [][]string
	for _, l := range accRealInstalls(s.apt) {
		installs = append(installs, accAfterSep(l))
	}
	sort.Slice(installs, func(i, j int) bool { return installs[i][0] < installs[j][0] })
	if len(installs) != 2 || !slices.Equal(installs[0], []string{"bat"}) || !slices.Equal(installs[1], []string{"ripgrep"}) {
		t.Fatalf("installs %q, want one for bat and one for ripgrep", installs)
	}
	for _, n := range []string{"ripgrep", "bat"} {
		if !s.apt.installed(n) {
			t.Errorf("%s was not installed", n)
		}
	}
	if s.apt.installed("yq") || s.apt.installed("zzz-gone") {
		t.Error("a name without a candidate was installed")
	}
	got := accByName(s.listing().Managed)
	for _, n := range []string{"yq", "zzz-gone"} {
		if got[n].Error == "" {
			t.Errorf("%s has no error: %+v", n, got[n])
		}
	}
	for _, n := range []string{"ripgrep", "bat", "fd-find"} {
		if got[n].Error != "" {
			t.Errorf("%s has an error: %q", n, got[n].Error)
		}
	}
	if len(s.cfgPackages()) != 5 {
		t.Fatalf("config.yaml lists %q", s.cfgPackages())
	}
}

func TestA12_PackageIndexTaskRefreshesDailyReportsTheOutcomeThenReconciles(t *testing.T) {
	t.Run("default schedule", func(t *testing.T) {
		d := config.DefaultJobs().PackageIndex
		if !d.Enabled || d.Every.D() != 24*time.Hour {
			t.Fatalf("default %+v", d)
		}
		s := newAcc(t, accOpts{noStart: true})
		if j := s.store.Config().Jobs.PackageIndex; !j.Enabled || j.Every.D() != 24*time.Hour {
			t.Fatalf("config default %+v", j)
		}
		start := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
		reg := jobs.New(func() config.Jobs { return s.store.Config().Jobs }, func() time.Time { return start }, nil)
		if reg.Due(config.JobPackageIndex, start.Add(23*time.Hour)) || !reg.Due(config.JobPackageIndex, start.Add(24*time.Hour)) {
			t.Fatal("package_index is not due exactly 24h after start")
		}
	})
	t.Run("success", func(t *testing.T) {
		s := newAcc(t, accOpts{world: func(w *accApt) { w.cands["bat"] = "0.24-1" }})
		// Listed after the start, with no reload: only the task's reconcile can pick it up.
		if err := (managedList{store: s.store}).Add("bat"); err != nil {
			t.Fatal(err)
		}
		if s.apt.count(accRealInstall) != 0 {
			t.Fatal("bat was installed before the task ran")
		}
		if err := s.reg.RunNow(config.JobPackageIndex); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the package_index run", func() bool { return s.job(config.JobPackageIndex).LastFinish != nil })
		if j := s.job(config.JobPackageIndex); j.LastError != "" {
			t.Fatalf("task %+v", j)
		}
		s.waitDone(1)
		if !s.apt.installed("bat") {
			t.Fatal("the task did not reconcile")
		}
		lines := s.apt.lines()
		upd := slices.IndexFunc(lines, accUpdate)
		inst := slices.IndexFunc(lines, accRealInstall)
		if s.apt.count(accUpdate) != 1 || upd < 0 || inst < 0 || upd > inst {
			t.Fatalf("expected one apt-get update before the reinstall, got %q", lines)
		}
	})
	t.Run("failure", func(t *testing.T) {
		s := newAcc(t, accOpts{world: func(w *accApt) {
			w.fail["apt-get update"] = "E: Could not get lock /var/lib/apt/lists/lock. It is held by process 1"
		}})
		if err := s.reg.RunNow(config.JobPackageIndex); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the package_index run", func() bool { return s.job(config.JobPackageIndex).LastFinish != nil })
		if j := s.job(config.JobPackageIndex); !strings.Contains(j.LastError, "Could not get lock") {
			t.Fatalf("task %+v", j)
		}
	})
}

func TestA13_SearchReturnsAtMostFiftyWithInstalledAndManagedState(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"rip"}, world: func(w *accApt) {
		w.add("rip", "1.0-1", "optional", "utils")
		w.add("rip-installed", "1.0-1", "optional", "utils")
		w.repo["rip"], w.repo["rip-installed"] = "exact match", "installed, unmanaged"
		for i := 0; i < 70; i++ {
			w.repo[fmt.Sprintf("rip-tool-%02d", i)] = "a tool"
		}
		w.repo["grip"] = "contains rip"
		w.repo["unrelated"] = "no match"
	}})
	var found []model.PackageInfo
	if err := json.Unmarshal([]byte(s.mustDo("GET", "/packages/search?q=rip", "", 200)), &found); err != nil {
		t.Fatal(err)
	}
	if len(found) != 50 {
		t.Fatalf("%d results, want 50", len(found))
	}
	seen := map[string]bool{}
	for _, r := range found {
		if seen[r.Name] {
			t.Errorf("%s listed twice", r.Name)
		}
		seen[r.Name] = true
		if !strings.Contains(r.Name, "rip") {
			t.Errorf("%s does not match the term", r.Name)
		}
		wantInstalled := r.Name == "rip" || r.Name == "rip-installed"
		if r.Installed != wantInstalled || r.Managed != (r.Name == "rip") {
			t.Errorf("%s: installed %v managed %v", r.Name, r.Installed, r.Managed)
		}
	}
	if !seen["rip"] {
		t.Errorf("the exact name is missing: %v", accNames(found))
	}
	raw := s.mustDo("GET", "/packages/search?q=rip", "", 200)
	if !strings.Contains(raw, `"installed"`) {
		t.Errorf("results carry no installed state: %.200s", raw)
	}
}

func TestA14_DetectedListsOnlyAdoptableManualPackages(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"ripgrep"}, world: func(w *accApt) {
		w.add("ripgrep", "1.0-1", "optional", "utils")
		w.add("htop", "3.4-1", "optional", "admin")
		w.add("fancy-extra", "1.0-1", "extra", "utils")
		w.add("auto-pulled", "1.0-1", "optional", "utils").manual = false
		w.add("libfoo1", "1.0-1", "optional", "libs")
		w.add("libold1", "1.0-1", "optional", "oldlibs")
		w.add("bash", "5.2-1", "required", "shells")
		w.add("coreutils", "9.7-1", "important", "utils")
		w.add("less", "668-1", "standard", "text")
		w.add("unlisted-prio", "1.0-1", "", "utils")
	}})
	p := s.listing()
	want := []string{"fancy-extra", "htop"}
	if got := accNames(p.Detected); !slices.Equal(got, want) {
		t.Fatalf("detected %q, want %q", got, want)
	}
	for _, i := range p.Detected {
		if !i.Installed {
			t.Errorf("%s is not marked installed", i.Name)
		}
	}
	if slices.Contains(accNames(p.Detected), "jq") || !slices.Contains(accNames(p.Base), "jq") {
		t.Errorf("jq is base: detected %q base %q", accNames(p.Detected), accNames(p.Base))
	}
}

// accRoutes is every /packages route, each with a request that would be valid.
var accRoutes = [][3]string{
	{"GET", "/packages", ""},
	{"GET", "/packages/search?q=rip", ""},
	{"POST", "/packages", `{"names":["ripgrep"]}`},
	{"POST", "/packages/upgrade", `{"all":true}`},
	{"POST", "/packages/upgrade", `{"names":["ripgrep"]}`},
	{"DELETE", "/packages/ripgrep?uninstall=1", ""},
	{"DELETE", "/packages/ripgrep", ""},
	{"POST", "/packages/refresh", ""},
}

func TestA16_EveryPackagesRouteAnswers503WhenAptIsNotOnPATH(t *testing.T) {
	for _, missing := range accAptTools {
		s := newAcc(t, accOpts{pathMiss: missing, packages: []string{"ripgrep"}})
		for _, r := range accRoutes {
			st, out := s.do(r[0], r[1], r[2])
			if st != 503 || !strings.Contains(out, "apt is not available on this host") {
				t.Errorf("without %s: %s %s: status %d (%s)", missing, r[0], r[1], st, strings.TrimSpace(out))
			}
		}
		if lines := s.apt.lines(); len(lines) != 0 {
			t.Fatalf("without %s commands ran: %q", missing, lines)
		}
	}
}

func TestA16_EmptyPathDaemon(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	o := testOptions(t, accConfig("ripgrep"))
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the daemon", func() bool { _, err := c.Status(context.Background()); return err == nil })
	for _, r := range accRoutes {
		st, out, err := socketCall(o.Socket, r[0], r[1], r[2])
		if err != nil {
			t.Fatal(err)
		}
		if st != 503 || !strings.Contains(out, "apt is not available on this host") {
			t.Errorf("%s %s: status %d (%s)", r[0], r[1], st, strings.TrimSpace(out))
		}
	}
}

func TestA17_BadPackagesEntriesAreDroppedWithAWarningAndTheDaemonStarts(t *testing.T) {
	yaml := accConfig("ripgrep", "-o", "jq;rm", "ripgrep", "Bad_Name", "fd-find")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(yaml), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok1\n"), 0o600)
	store, warnings, err := OpenStore(cfgPath, filepath.Join(dir, "token"))
	if err != nil {
		t.Fatalf("the config was refused: %v", err)
	}
	if got := store.Config().Packages; !slices.Equal(got, []string{"ripgrep", "fd-find"}) {
		t.Fatalf("packages %q", got)
	}
	all := strings.Join(warnings, "\n")
	for _, want := range []string{"-o", "jq;rm", "Bad_Name", "ripgrep"} {
		if !strings.Contains(all, want) {
			t.Errorf("no warning names %q: %q", want, warnings)
		}
	}
	if len(warnings) < 4 {
		t.Errorf("%d warnings, want one per dropped entry: %q", len(warnings), warnings)
	}
}

func TestA17_DaemonStarts(t *testing.T) {
	// PATH is empty so the daemon runs no real apt.
	t.Setenv("PATH", t.TempDir())
	o := testOptions(t, accConfig("ripgrep", "-o", "jq;rm", "ripgrep", "Bad_Name", "fd-find"))
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the daemon", func() bool { _, err := c.Status(context.Background()); return err == nil })
}

func TestA18_PackagesTopicChangesWhenAnOperationEndsAndWhenTheListChanges(t *testing.T) {
	s := newAcc(t, accOpts{packages: []string{"ripgrep"}, world: func(w *accApt) {
		w.add("ripgrep", "1.0-1", "optional", "utils")
		w.add("fd-find", "1.0-1", "optional", "utils")
		w.cands["bat"] = "0.24-1"
	}})
	hub := changes.New(changes.Options{Tick: 20 * time.Millisecond, WakeWindow: 5 * time.Millisecond, Logf: func(string, ...any) {}})
	hub.SetSources(s.b.fingerprints, nil)
	t.Cleanup(hub.Close)
	s.b.Changes = hub
	sub, err := s.b.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	sawPackages := func(within time.Duration) bool {
		deadline := time.After(within)
		for {
			select {
			case topics, ok := <-sub.C:
				if !ok {
					t.Fatal("the subscription ended")
				}
				if slices.Contains(topics, "packages") {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	if sawPackages(300 * time.Millisecond) {
		t.Fatal("the packages topic changed with nothing happening")
	}

	// An operation ending.
	gate := s.apt.gate(accUpdate)
	s.mustDo("POST", "/packages/refresh", "", 202)
	gate.WaitEntered(t)
	if sawPackages(300 * time.Millisecond) {
		t.Fatal("the packages topic changed while the operation was still running")
	}
	gate.Open()
	if !sawPackages(5 * time.Second) {
		t.Fatal("the packages topic did not change when the operation ended")
	}
	s.waitDone(1)
	if sawPackages(300 * time.Millisecond) {
		t.Fatal("the packages topic kept changing with nothing happening")
	}

	// The managed list changing, with no operation involved.
	s.mustDo("DELETE", "/packages/ripgrep", "", 204)
	if !sawPackages(5 * time.Second) {
		t.Fatal("the packages topic did not change when the managed list changed")
	}

	// The list edited in config.yaml and reloaded.
	sawPackages(300 * time.Millisecond)
	if err := os.WriteFile(s.store.ConfigPath, []byte(accConfig("fd-find")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.b.Reload(); err != nil {
		t.Fatal(err)
	}
	if !sawPackages(5 * time.Second) {
		t.Fatal("the packages topic did not change when a reload changed the managed list")
	}
}

func TestA21_ForgetIsRefusedWhileAnOperationOnTheNameRunsAndAStaleRemoveRunsNoDpkg(t *testing.T) {
	world := func(w *accApt) {
		w.add("ripgrep", "1.0-1", "optional", "utils")
		w.add("fd-find", "1.0-1", "optional", "utils")
		w.cands["ripgrep"], w.cands["fd-find"] = "2.0-1", "2.0-1"
	}
	t.Run("running operation", func(t *testing.T) {
		s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find"}, world: world})
		gate := s.apt.gate(accRealInstall)
		s.mustDo("POST", "/packages/upgrade", `{"names":["ripgrep"]}`, 202)
		gate.WaitEntered(t)
		ran := len(s.apt.lines())
		if st, out := s.do("DELETE", "/packages/ripgrep", ""); st != 409 {
			t.Fatalf("forget during an operation on the name: status %d (%s), want 409", st, strings.TrimSpace(out))
		}
		if !slices.Contains(s.cfgPackages(), "ripgrep") || len(s.apt.lines()) != ran {
			t.Fatal("a refused forget changed config.yaml or ran a command")
		}
		// An operation on another name does not block it.
		s.mustDo("DELETE", "/packages/fd-find", "", 204)
		gate.Open()
		s.waitDone(1)
		s.mustDo("DELETE", "/packages/ripgrep", "", 204)
		if got := s.cfgPackages(); len(got) != 0 {
			t.Fatalf("config.yaml lists %q", got)
		}
	})
	t.Run("queued operations are dropped", func(t *testing.T) {
		s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find"}, world: world})
		gate := s.apt.gate(accUpdate)
		s.mustDo("POST", "/packages/refresh", "", 202)
		gate.WaitEntered(t)
		s.mustDo("POST", "/packages/upgrade", `{"names":["ripgrep"]}`, 202)
		s.mustDo("DELETE", "/packages/ripgrep?uninstall=1", "", 202)
		s.waitQueued(2)
		s.mustDo("DELETE", "/packages/ripgrep", "", 204)
		if got := s.svc.Snapshot().Operations.Queued; got != 0 {
			t.Fatalf("%d operations on the forgotten name are still queued", got)
		}
		gate.Open()
		s.waitDone(1)
		if n := s.apt.count(accDpkgRemove) + len(accRealInstalls(s.apt)); n != 0 {
			t.Fatalf("a dropped operation ran %d commands", n)
		}
	})
	t.Run("remove after the name left config.yaml", func(t *testing.T) {
		s := newAcc(t, accOpts{packages: []string{"ripgrep", "fd-find"}, world: world})
		gate := s.apt.gate(accUpdate)
		s.mustDo("POST", "/packages/refresh", "", 202)
		gate.WaitEntered(t)
		s.mustDo("DELETE", "/packages/ripgrep?uninstall=1", "", 202)
		s.waitQueued(1)
		if err := os.WriteFile(s.store.ConfigPath, []byte(accConfig("fd-find")), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.b.Reload(); err != nil {
			t.Fatal(err)
		}
		gate.Open()
		s.waitDone(2)
		op := s.recent("remove", "apt ripgrep")
		if op.Outcome != "failed" {
			t.Fatalf("operation %+v", op)
		}
		if n := s.apt.count(accDpkgRemove); n != 0 {
			t.Fatalf("dpkg --remove ran %d times", n)
		}
		if !s.apt.installed("ripgrep") {
			t.Fatal("ripgrep was removed")
		}
	})
}

func TestA22_NonAdoptableInstalledNamesAreRefusedAndNothingIsQueued(t *testing.T) {
	s := newAcc(t, accOpts{world: func(w *accApt) {
		w.add("htop", "3.4-1", "optional", "admin")
		w.add("bash", "5.2-1", "required", "shells")
		w.add("coreutils", "9.7-1", "important", "utils")
		w.add("less", "668-1", "standard", "text")
		w.add("libfoo1", "1.0-1", "optional", "libs")
		w.add("libold1", "1.0-1", "extra", "oldlibs")
	}})
	for _, n := range []string{"bash", "coreutils", "less", "libfoo1", "libold1"} {
		if st, out := s.do("POST", "/packages", fmt.Sprintf(`{"names":[%q]}`, n)); st != 400 {
			t.Errorf("%s: status %d (%s), want 400", n, st, strings.TrimSpace(out))
		}
	}
	// One non-adoptable name keeps the adoptable one from being queued too.
	if st, _ := s.do("POST", "/packages", `{"names":["htop","bash"]}`); st != 400 {
		t.Errorf("mixed: status %d, want 400", st)
	}
	time.Sleep(200 * time.Millisecond)
	if o := s.svc.Snapshot().Operations; o.Queued != 0 || o.Current != nil || len(o.Recent) != 0 {
		t.Fatalf("operations %+v", o)
	}
	if got := s.apt.mutations(); len(got) != 0 {
		t.Fatalf("mutating commands %q", got)
	}
	if len(s.cfgPackages()) != 0 {
		t.Fatalf("config.yaml lists %q", s.cfgPackages())
	}
	// The adoptable one is accepted on its own.
	s.mustDo("POST", "/packages", `{"names":["htop"]}`, 202)
}

func TestA23_ANilManagerAnswers503AndTheTaskSaysAptIsNotAvailable(t *testing.T) {
	s := newAcc(t, accOpts{nilApt: true, packages: []string{"ripgrep"}})
	for _, r := range accRoutes {
		st, out := s.do(r[0], r[1], r[2])
		if st != 503 || !strings.Contains(out, "apt is not available on this host") {
			t.Errorf("%s %s: status %d (%s)", r[0], r[1], st, strings.TrimSpace(out))
		}
	}
	if err := s.reg.RunNow(config.JobPackageIndex); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the package_index run", func() bool { return s.job(config.JobPackageIndex).LastFinish != nil })
	if j := s.job(config.JobPackageIndex); j.LastError != "apt is not available on this host" {
		t.Fatalf("task %+v", j)
	}
	if !slices.Contains(s.cfgPackages(), "ripgrep") {
		t.Fatal("config.yaml lost ripgrep")
	}
}

func TestA24_SearchRefusesATermThatFailsTheRule(t *testing.T) {
	s := newAcc(t, accOpts{world: func(w *accApt) { w.repo["ripgrep"] = "grep, but faster" }})
	for _, q := range []string{"a", "Rip", "rip ls", "rip;ls", "rip*", "rip(", "ri$", "rip_grep", strings.Repeat("a", 65), "."} {
		if st, out := s.do("GET", "/packages/search?q="+url.QueryEscape(q), ""); st != 400 {
			t.Errorf("q=%q: status %d (%s), want 400", q, st, strings.TrimSpace(out))
		}
	}
	if st, _ := s.do("GET", "/packages/search", ""); st != 400 {
		t.Errorf("no q: status %d, want 400", st)
	}
	if lines := s.apt.lines(); len(lines) != 0 {
		t.Fatalf("commands ran for refused terms: %q", lines)
	}
	for _, q := range []string{"ri", strings.Repeat("a", 64), "rip.grep", "g++"} {
		if st, out := s.do("GET", "/packages/search?q="+url.QueryEscape(q), ""); st != 200 {
			t.Errorf("q=%q: status %d (%s), want 200", q, st, strings.TrimSpace(out))
		}
	}
}

func TestA25_AnInstallRefreshesOnlyWhenTheLastRefreshIsOlderThanAnHour(t *testing.T) {
	world := func(w *accApt) {
		for _, n := range []string{"one", "two", "three"} {
			w.cands[n] = "1.0-1"
		}
	}
	install := func(s *accStack, name string, ended int) {
		s.mustDo("POST", "/packages", fmt.Sprintf(`{"names":[%q]}`, name), 202)
		s.waitDone(ended)
	}
	t.Run("fresh stamp", func(t *testing.T) {
		s := newAcc(t, accOpts{world: world, stampAge: 10 * time.Minute})
		install(s, "one", 1)
		if n := s.apt.count(accUpdate); n != 0 {
			t.Fatalf("apt-get update ran %d times with a 10 minute old stamp", n)
		}
	})
	t.Run("old stamp", func(t *testing.T) {
		s := newAcc(t, accOpts{world: world, stampAge: 2 * time.Hour})
		install(s, "one", 1)
		lines := s.apt.lines()
		upd, inst := slices.IndexFunc(lines, accUpdate), slices.IndexFunc(lines, accRealInstall)
		if s.apt.count(accUpdate) != 1 || upd < 0 || inst < 0 || upd > inst {
			t.Fatalf("expected one apt-get update before the install, got %q", lines)
		}
		fi, err := os.Stat(s.stamp)
		if err != nil || time.Since(fi.ModTime()) > time.Minute {
			t.Fatalf("the stamp was not renewed after a successful refresh: %v %v", fi, err)
		}
		install(s, "two", 2)
		if n := s.apt.count(accUpdate); n != 1 {
			t.Fatalf("a second install refreshed again: %d updates", n)
		}
	})
	t.Run("failed refresh keeps the old stamp", func(t *testing.T) {
		s := newAcc(t, accOpts{world: func(w *accApt) {
			world(w)
			w.fail["apt-get update"] = "E: Failed to fetch http://deb.debian.org/ Temporary failure"
		}, stampAge: 2 * time.Hour})
		before, _ := os.Stat(s.stamp)
		install(s, "one", 1)
		op := s.recent("install", "apt one")
		if op.Outcome != "failed" || s.apt.installed("one") || slices.Contains(s.cfgPackages(), "one") {
			t.Fatalf("operation %+v", op)
		}
		after, _ := os.Stat(s.stamp)
		if !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("a failed refresh moved the stamp from %v to %v", before.ModTime(), after.ModTime())
		}
	})
	t.Run("stamp dates the index", func(t *testing.T) {
		s := newAcc(t, accOpts{world: world, stampAge: 3 * time.Hour, lists: func(dir string) {
			at := time.Now().Add(-10 * time.Minute)
			os.WriteFile(filepath.Join(dir, "deb.debian.org_dists_trixie_InRelease"), nil, 0o644)
			os.Chtimes(filepath.Join(dir, "deb.debian.org_dists_trixie_InRelease"), at, at)
		}})
		fi, _ := os.Stat(s.stamp)
		got := s.listing().IndexUpdatedAt
		if got == nil || got.Unix() != fi.ModTime().Unix() {
			t.Fatalf("index_updated_at %v, want the stamp's %v", got, fi.ModTime())
		}
	})
	t.Run("no stamp, newest InRelease", func(t *testing.T) {
		old, newest := time.Now().Add(-5*time.Hour).Truncate(time.Second), time.Now().Add(-90*time.Minute).Truncate(time.Second)
		s := newAcc(t, accOpts{world: world, stampAge: -1, lists: func(dir string) {
			for name, at := range map[string]time.Time{
				"a.example_dists_x_InRelease":    old,
				"b.example_dists_x_InRelease":    newest,
				"c.example_dists_x_Release":      time.Now(),
				"d.example_dists_x_Packages.lz4": time.Now(),
			} {
				os.WriteFile(filepath.Join(dir, name), nil, 0o644)
				os.Chtimes(filepath.Join(dir, name), at, at)
			}
		}})
		got := s.listing().IndexUpdatedAt
		if got == nil || got.Unix() != newest.Unix() {
			t.Fatalf("index_updated_at %v, want the newest InRelease %v", got, newest)
		}
		install(s, "one", 1)
		if n := s.apt.count(accUpdate); n != 1 {
			t.Fatalf("with the index 90 minutes old, apt-get update ran %d times", n)
		}
	})
	t.Run("no stamp, recent InRelease", func(t *testing.T) {
		s := newAcc(t, accOpts{world: world, stampAge: -1, lists: func(dir string) {
			at := time.Now().Add(-5 * time.Minute)
			p := filepath.Join(dir, "a.example_dists_x_InRelease")
			os.WriteFile(p, nil, 0o644)
			os.Chtimes(p, at, at)
		}})
		install(s, "one", 1)
		if n := s.apt.count(accUpdate); n != 0 {
			t.Fatalf("with the index 5 minutes old, apt-get update ran %d times", n)
		}
	})
	t.Run("nothing dates the index", func(t *testing.T) {
		s := newAcc(t, accOpts{world: world, stampAge: -1})
		if got := s.listing().IndexUpdatedAt; got != nil {
			t.Fatalf("index_updated_at %v with no stamp and no lists", got)
		}
	})
}

func TestA26_AReloadReconcilesAndTheTaskReusesAQueuedRefresh(t *testing.T) {
	t.Run("reload reconciles", func(t *testing.T) {
		s := newAcc(t, accOpts{world: func(w *accApt) { w.cands["bat"] = "0.24-1" }})
		s.waitDone(0)
		if s.apt.count(accRealInstall) != 0 {
			t.Fatal("an install ran before the reload")
		}
		if err := os.WriteFile(s.store.ConfigPath, []byte(accConfig("bat")), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.b.Reload(); err != nil {
			t.Fatal(err)
		}
		s.waitDone(1)
		op := s.recent("install", "apt bat")
		if op.Outcome != "ok" || !s.apt.installed("bat") {
			t.Fatalf("operation %+v", op)
		}
	})
	t.Run("task reuses a queued refresh", func(t *testing.T) {
		s := newAcc(t, accOpts{world: func(w *accApt) { w.cands["bat"] = "0.24-1" }})
		gate := s.apt.gate(accRealInstall)
		s.mustDo("POST", "/packages", `{"names":["bat"]}`, 202)
		gate.WaitEntered(t)
		s.mustDo("POST", "/packages/refresh", "", 202)
		s.waitQueued(1)
		if err := s.reg.RunNow(config.JobPackageIndex); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the package_index run to start", func() bool { return s.job(config.JobPackageIndex).Running })
		time.Sleep(200 * time.Millisecond)
		if got := s.svc.Snapshot().Operations.Queued; got != 1 {
			t.Fatalf("%d operations queued, want the one refresh the API queued", got)
		}
		gate.Open()
		waitFor(t, "the package_index run to end", func() bool { return s.job(config.JobPackageIndex).LastFinish != nil })
		if j := s.job(config.JobPackageIndex); j.LastError != "" {
			t.Fatalf("task %+v", j)
		}
		s.waitDone(2)
		if n := s.apt.count(accUpdate); n != 1 {
			t.Fatalf("apt-get update ran %d times, want 1", n)
		}
	})
}
