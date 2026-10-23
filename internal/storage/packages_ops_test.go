package storage

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/packages"
)

// fakeApt is a Manager over a table of packages. calls records every
// command that changes the host or the index, infos every Info call. A verb
// in block ("install", "remove", ...) waits for a send on its channel or the
// end of its context first.
type fakeApt struct {
	mu      sync.Mutex
	off     bool
	pkgs    map[string]packages.Info
	manual  []string
	others  bool
	errs    map[string]error
	block   map[string]chan struct{}
	calls   []string
	infos   []string
	exclude []string
}

func newFakeApt(pkgs ...packages.Info) *fakeApt {
	f := &fakeApt{pkgs: map[string]packages.Info{}, errs: map[string]error{}, block: map[string]chan struct{}{}}
	for _, p := range pkgs {
		f.pkgs[p.Name] = p
	}
	return f
}

func installed(name, version, candidate string) packages.Info {
	return packages.Info{Name: name, Version: version, Candidate: candidate, Installed: true,
		Upgradable: candidate != version, Priority: "optional", Section: "utils", Bytes: 1024}
}

func notInstalled(name, candidate string) packages.Info {
	return packages.Info{Name: name, Candidate: candidate, Priority: "optional", Section: "utils"}
}

func (f *fakeApt) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.off
}

// do records a changing command, waits when its verb is blocked, and
// returns the verb's error.
func (f *fakeApt) do(ctx context.Context, verb string, names ...string) error {
	f.mu.Lock()
	f.calls = append(f.calls, strings.TrimSpace(verb+" "+strings.Join(names, " ")))
	gate, err := f.block[verb], f.errs[verb]
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (f *fakeApt) Info(_ context.Context, names []string, candidates bool) ([]packages.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := "info " + strings.Join(names, ",")
	if candidates {
		call += " candidates"
	}
	f.infos = append(f.infos, call)
	if err := f.errs["info"]; err != nil {
		return nil, err
	}
	var out []packages.Info
	for _, n := range names {
		p, ok := f.pkgs[n]
		if !ok {
			continue
		}
		if !candidates {
			p.Candidate, p.Upgradable = "", false
		}
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeApt) Search(_ context.Context, term string) ([]packages.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "search "+term)
	var out []packages.Info
	for _, n := range slices.Sorted(maps.Keys(f.pkgs)) {
		if strings.Contains(n, term) {
			p := f.pkgs[n]
			p.Candidate, p.Upgradable = "", false
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeApt) Detected(_ context.Context, exclude []string) ([]packages.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exclude = slices.Clone(exclude)
	var out []packages.Info
	for _, n := range f.manual {
		if p := f.pkgs[n]; packages.Adoptable(p) && !slices.Contains(exclude, n) {
			p.Candidate, p.Upgradable = "", false
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeApt) Refresh(ctx context.Context) error { return f.do(ctx, "refresh") }

func (f *fakeApt) UpgradesOthers(ctx context.Context, names []string) (bool, error) {
	if err := f.do(ctx, "others", names...); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.others, nil
}

func (f *fakeApt) Install(ctx context.Context, names []string) error {
	if err := f.do(ctx, "install", names...); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		p := f.pkgs[n]
		p.Name, p.Installed, p.Version, p.Upgradable = n, true, p.Candidate, false
		f.pkgs[n] = p
	}
	return nil
}

func (f *fakeApt) Upgrade(ctx context.Context, names []string) error {
	if err := f.do(ctx, "upgrade", names...); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		p := f.pkgs[n]
		p.Version, p.Upgradable = p.Candidate, false
		f.pkgs[n] = p
	}
	return nil
}

func (f *fakeApt) MarkManual(ctx context.Context, name string) error { return f.do(ctx, "mark", name) }

func (f *fakeApt) Remove(ctx context.Context, name string) error {
	if err := f.do(ctx, "remove", name); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pkgs[name]
	p.Installed, p.Version, p.Upgradable = false, "", false
	f.pkgs[name] = p
	return nil
}

func (f *fakeApt) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeApt) infoList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.infos)
}

func (f *fakeApt) set(fn func(f *fakeApt)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// fakeManaged is the managed list; with addErr or dropErr set, saving fails.
type fakeManaged struct {
	mu      sync.Mutex
	names   []string
	addErr  error
	dropErr error
}

func (m *fakeManaged) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.names)
}

func (m *fakeManaged) Add(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	if !slices.Contains(m.names, name) {
		m.names = append(m.names, name)
	}
	return nil
}

func (m *fakeManaged) Drop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dropErr != nil {
		return m.dropErr
	}
	m.names = slices.DeleteFunc(m.names, func(n string) bool { return n == name })
	return nil
}

// pkgService is an unstarted service over apt with curl and jq as the base
// set, the given managed names, and an index refreshed a minute ago.
func pkgService(t *testing.T, apt *fakeApt, managed ...string) (*Service, *fakeManaged, *atomic.Int32) {
	t.Helper()
	var busy atomic.Int32
	s := newService(t, &fakeTools{}, &fakeDisk{})
	s.Hold = busyHold(&busy, nil)
	m := &fakeManaged{names: managed}
	dir := t.TempDir()
	s.Apt, s.Managed, s.Base = apt, m, []string{"curl", "jq"}
	s.AptLists, s.IndexStamp = filepath.Join(dir, "lists"), filepath.Join(dir, "packages-index")
	stampAt(t, s.IndexStamp, time.Now().Add(-time.Minute))
	return s, m, &busy
}

func stampAt(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func waitRecent(t *testing.T, s *Service, n int) []model.Operation {
	t.Helper()
	waitFor(t, "finished operations", func() bool { return len(s.Snapshot().Operations.Recent) >= n })
	return s.Snapshot().Operations.Recent
}

func TestAddPackagesInstallsANameNotInstalled(t *testing.T) {
	apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"))
	s, m, _ := pkgService(t, apt)
	startService(t, s)
	res, err := s.AddPackages(t.Context(), []string{"ripgrep"})
	if err != nil || !slices.Equal(res.Install, []string{"ripgrep"}) || len(res.Adopt) != 0 {
		t.Fatalf("AddPackages %+v, %v", res, err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Kind != "install" || r.Target != "apt ripgrep" || r.Outcome != "ok" || r.Message != "installed ripgrep" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"others ripgrep", "install ripgrep"}) {
		t.Fatalf("apt calls %q", got)
	}
	if got := m.List(); !slices.Equal(got, []string{"ripgrep"}) {
		t.Fatalf("managed %q", got)
	}
}

func TestAddPackagesAdoptsAnInstalledName(t *testing.T) {
	apt := newFakeApt(installed("vim", "2:9.1-2", "2:9.1-2"))
	s, m, busy := pkgService(t, apt)
	busy.Store(1)
	startService(t, s)
	res, err := s.AddPackages(t.Context(), []string{"vim"})
	if err != nil || !slices.Equal(res.Adopt, []string{"vim"}) || len(res.Install) != 0 {
		t.Fatalf("AddPackages %+v, %v", res, err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Kind != "adopt" || r.Target != "apt vim" || r.Outcome != "ok" || r.Message != "adopted vim" {
		t.Fatalf("operation %+v: an adopt never holds the runners", r)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"mark vim"}) {
		t.Fatalf("apt calls %q", got)
	}
	if got := m.List(); !slices.Equal(got, []string{"vim"}) {
		t.Fatalf("managed %q", got)
	}
}

func TestAddPackagesRefusesAndQueuesNothing(t *testing.T) {
	libc := installed("libc6", "2.41-1", "2.41-1")
	libc.Priority, libc.Section = "required", "libs"
	libfoo := installed("libfoo1", "1.0", "1.0")
	libfoo.Section = "universe/libs"
	oldlib := installed("libold", "1.0", "1.0")
	oldlib.Section = "oldlibs"
	std := installed("less", "668-1", "668-1")
	std.Priority = "standard"
	apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"), installed("git", "1:2.47", "1:2.47"), libc, libfoo, oldlib, std)
	s, _, _ := pkgService(t, apt, "git")
	startService(t, s)
	for _, c := range []struct {
		names []string
		want  error
	}{
		{nil, packages.ErrNoNames},
		{[]string{"-o"}, packages.ErrInvalidName},
		{[]string{"jq;rm"}, packages.ErrInvalidName},
		{[]string{"ripgrep", "jq;rm"}, packages.ErrInvalidName},
		{[]string{"ripgrep", "ripgrep"}, packages.ErrInvalidName},
		{[]string{"ripgrep", "curl"}, packages.ErrBase},
		{[]string{"git"}, packages.ErrAlreadyManaged},
		{[]string{"ripgrep", "libc6"}, packages.ErrNotAdoptable},
		{[]string{"libfoo1"}, packages.ErrNotAdoptable},
		{[]string{"libold"}, packages.ErrNotAdoptable},
		{[]string{"less"}, packages.ErrNotAdoptable},
	} {
		if _, err := s.AddPackages(t.Context(), c.names); !errors.Is(err, c.want) {
			t.Errorf("AddPackages(%q): %v, want %v", c.names, err, c.want)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if st := s.Snapshot().Operations; st.Queued != 0 || st.Current != nil || len(st.Recent) != 0 {
		t.Fatalf("a refused request queued work: %+v", st)
	}
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("a refused request ran %q", got)
	}
}

func TestAFailedInstallLeavesConfigUnchanged(t *testing.T) {
	apt := newFakeApt(notInstalled("chrony", "4.6-1"))
	apt.errs["install"] = errors.New("E: Packages need to be removed but remove is disabled.")
	s, m, _ := pkgService(t, apt)
	startService(t, s)
	if _, err := s.AddPackages(t.Context(), []string{"chrony"}); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Outcome != "failed" || r.Message != "E: Packages need to be removed but remove is disabled." {
		t.Fatalf("operation %+v", r)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("managed %q after a failed install", got)
	}
}

func TestAnInstallOfANameWithoutAnExactPackageRunsNothing(t *testing.T) {
	// apt-get would read "ripgre." as a regular expression and "ripgrep-" as
	// a request to remove ripgrep.
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1"))
	s, m, _ := pkgService(t, apt)
	startService(t, s)
	if _, err := s.AddPackages(t.Context(), []string{"ripgre.", "ripgrep-"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range waitRecent(t, s, 2) {
		if r.Outcome != "failed" || r.Message != noCandidate {
			t.Fatalf("operation %+v", r)
		}
	}
	if got := apt.callList(); slices.ContainsFunc(got, func(c string) bool {
		return strings.HasPrefix(c, "others ") || strings.HasPrefix(c, "install ")
	}) {
		t.Fatalf("apt calls %q: no apt-get install may run", got)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("managed %q", got)
	}
}

func TestAnInstallWhoseSaveFailsFailsTheOperation(t *testing.T) {
	apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"))
	s, m, _ := pkgService(t, apt)
	m.addErr = errors.New("config.yaml: permission denied")
	startService(t, s)
	if _, err := s.AddPackages(t.Context(), []string{"ripgrep"}); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Outcome != "failed" || r.Message != "installed ripgrep but could not add it to config.yaml: config.yaml: permission denied" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); !slices.Contains(got, "install ripgrep") {
		t.Fatalf("apt calls %q", got)
	}
}

func TestAnInstallHoldsTheRunnersOnlyWhenItUpgradesOthers(t *testing.T) {
	for _, others := range []bool{false, true} {
		apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"))
		apt.others = others
		s, m, busy := pkgService(t, apt)
		busy.Store(1)
		startService(t, s)
		if _, err := s.AddPackages(t.Context(), []string{"ripgrep"}); err != nil {
			t.Fatal(err)
		}
		r := waitRecent(t, s, 1)[0]
		if !others {
			if r.Outcome != "ok" || !slices.Equal(m.List(), []string{"ripgrep"}) {
				t.Fatalf("an install adding only new packages, runners busy: %+v", r)
			}
			continue
		}
		if r.Outcome != "refused" || r.Message != (model.BusyError{N: 1}).Error() {
			t.Fatalf("an install that upgrades others, runners busy: %+v", r)
		}
		if slices.Contains(apt.callList(), "install ripgrep") || len(m.List()) != 0 {
			t.Fatalf("a refused install ran: %q, managed %q", apt.callList(), m.List())
		}
	}
}

func TestAnInstallRefreshesTheIndexOnlyWhenItIsAnHourOld(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		name             string
		stamp, inRelease time.Duration // ages; 0 leaves the file out
		refresh          bool
	}{
		{"stamp 30m old", 30 * time.Minute, 0, false},
		{"stamp 2h old", 2 * time.Hour, 0, true},
		{"no stamp, InRelease 30m old", 0, 30 * time.Minute, false},
		{"no stamp, InRelease 2h old", 0, 2 * time.Hour, true},
		{"neither", 0, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"))
			s, _, _ := pkgService(t, apt)
			os.Remove(s.IndexStamp)
			if c.stamp > 0 {
				stampAt(t, s.IndexStamp, now.Add(-c.stamp))
			}
			if c.inRelease > 0 {
				stampAt(t, filepath.Join(s.AptLists, "deb.debian.org_debian_dists_trixie_InRelease"), now.Add(-c.inRelease))
			}
			startService(t, s)
			if _, err := s.AddPackages(t.Context(), []string{"ripgrep"}); err != nil {
				t.Fatal(err)
			}
			if r := waitRecent(t, s, 1)[0]; r.Outcome != "ok" {
				t.Fatalf("operation %+v", r)
			}
			if got := slices.Contains(apt.callList(), "refresh"); got != c.refresh {
				t.Fatalf("refreshed %v, want %v: calls %q", got, c.refresh, apt.callList())
			}
			if !c.refresh {
				return
			}
			fi, err := os.Stat(s.IndexStamp)
			if err != nil || time.Since(fi.ModTime()) > time.Minute {
				t.Fatalf("the stamp was not touched: %v, %v", fi, err)
			}
		})
	}
}

func TestAnInstallWhoseRefreshFailsInstallsNothing(t *testing.T) {
	apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"))
	apt.errs["refresh"] = errors.New("E: Could not get lock /var/lib/apt/lists/lock")
	s, m, _ := pkgService(t, apt)
	stampAt(t, s.IndexStamp, time.Now().Add(-2*time.Hour))
	startService(t, s)
	if _, err := s.AddPackages(t.Context(), []string{"ripgrep"}); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Outcome != "failed" || r.Message != "E: Could not get lock /var/lib/apt/lists/lock" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"refresh"}) || len(m.List()) != 0 {
		t.Fatalf("apt calls %q, managed %q", got, m.List())
	}
}

func TestPackageList(t *testing.T) {
	vim := installed("vim", "2:9.1-2", "2:9.1-2")
	vimRuntime := installed("libvim1", "2:9.1-2", "2:9.1-2")
	vimRuntime.Section = "libs"
	apt := newFakeApt(
		installed("ripgrep", "14.1.0-1", "14.1.1-1"),
		installed("shellcheck", "0.10.0-1", "0.10.0-1"),
		notInstalled("yq", ""),
		installed("curl", "8.14.1-2", "8.14.1-2"),
		installed("jq", "1.7.1-6", "1.7.1-6"),
		vim, vimRuntime,
	)
	apt.manual = []string{"ripgrep", "vim", "libvim1", "jq"}
	s, _, _ := pkgService(t, apt, "yq", "shellcheck", "ripgrep", "jq")
	stamp := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	stampAt(t, s.IndexStamp, stamp)
	startService(t, s)
	s.setPkgErr("shellcheck", "E: Unable to fetch some archives")
	got, err := s.PackageList(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got.Managed {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"ripgrep", "shellcheck", "yq"}) {
		t.Fatalf("managed %q: sorted, without the base name jq", names)
	}
	if rg := got.Managed[0]; !rg.Installed || !rg.Upgradable || rg.Candidate != "14.1.1-1" || rg.Version != "14.1.0-1" || !rg.Managed || rg.Bytes != 1024 {
		t.Fatalf("ripgrep %+v", rg)
	}
	if sc := got.Managed[1]; sc.Upgradable || sc.Error != "E: Unable to fetch some archives" {
		t.Fatalf("shellcheck %+v", sc)
	}
	if yq := got.Managed[2]; yq.Installed || yq.Version != "" {
		t.Fatalf("yq %+v", yq)
	}
	if len(got.Base) != 2 || got.Base[0].Name != "curl" || got.Base[0].Managed || got.Base[1].Name != "jq" || !got.Base[1].Managed || got.Base[1].Candidate != "" {
		t.Fatalf("base %+v: installed base packages, jq marked managed, no candidates", got.Base)
	}
	if len(got.Detected) != 1 || got.Detected[0].Name != "vim" {
		t.Fatalf("detected %+v", got.Detected)
	}
	if got.IndexUpdatedAt == nil || !got.IndexUpdatedAt.Equal(stamp) {
		t.Fatalf("index_updated_at %v, want the stamp's %v", got.IndexUpdatedAt, stamp)
	}
	infos := apt.infoList()
	if !slices.Contains(infos, "info ripgrep,shellcheck,yq candidates") || !slices.Contains(infos, "info curl,jq") {
		t.Fatalf("info calls %q: candidates for the managed names only", infos)
	}
	apt.mu.Lock()
	exclude := slices.Clone(apt.exclude)
	apt.mu.Unlock()
	for _, n := range []string{"ripgrep", "jq", "curl", "yq"} {
		if !slices.Contains(exclude, n) {
			t.Errorf("Detected's exclude %q lacks %s", exclude, n)
		}
	}
}

func TestPackageListDatesTheIndexByInReleaseWithoutAStamp(t *testing.T) {
	s, _, _ := pkgService(t, newFakeApt())
	os.Remove(s.IndexStamp)
	old, newer := time.Now().Add(-5*time.Hour).Truncate(time.Second), time.Now().Add(-2*time.Hour).Truncate(time.Second)
	stampAt(t, filepath.Join(s.AptLists, "a_InRelease"), old)
	stampAt(t, filepath.Join(s.AptLists, "b_InRelease"), newer)
	stampAt(t, filepath.Join(s.AptLists, "c_Packages"), time.Now())
	startService(t, s)
	got, err := s.PackageList(t.Context())
	if err != nil || got.IndexUpdatedAt == nil || !got.IndexUpdatedAt.Equal(newer) {
		t.Fatalf("index_updated_at %v, %v; want %v", got.IndexUpdatedAt, err, newer)
	}
	if got.Managed == nil || got.Base == nil || got.Detected == nil {
		t.Fatalf("empty lists must not be nil: %+v", got)
	}
}

func TestSearchPackagesMarksManagedNames(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1"), notInstalled("ripgrep-all", "0.10.6-1"))
	s, _, _ := pkgService(t, apt, "ripgrep")
	startService(t, s)
	got, err := s.SearchPackages(t.Context(), "rip")
	if err != nil || len(got) != 2 {
		t.Fatalf("search %+v, %v", got, err)
	}
	if !got[0].Managed || !got[0].Installed || got[1].Managed || got[1].Installed {
		t.Fatalf("search %+v", got)
	}
	for _, term := range []string{"r", "rip grep", "RIP", "^rip"} {
		if _, err := s.SearchPackages(t.Context(), term); !errors.Is(err, packages.ErrInvalidTerm) {
			t.Errorf("term %q: %v, want ErrInvalidTerm", term, err)
		}
	}
	if got := apt.callList(); !slices.Equal(got, []string{"search rip"}) {
		t.Fatalf("apt calls %q: an invalid term must not reach apt", got)
	}
}

func TestUpgradeRefusesNamesThatAreNotManagedOrBase(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.1-1"), installed("jq", "1.7.1-6", "1.7.1-6"))
	s, _, _ := pkgService(t, apt, "ripgrep", "jq")
	startService(t, s)
	for _, c := range []struct {
		names []string
		want  error
	}{
		{nil, packages.ErrNoNames},
		{[]string{"vim"}, packages.ErrNotManaged},
		{[]string{"ripgrep", "jq"}, packages.ErrBase},
	} {
		if err := s.UpgradePackages(c.names, false); !errors.Is(err, c.want) {
			t.Errorf("UpgradePackages(%q): %v, want %v", c.names, err, c.want)
		}
	}
	if q := s.Snapshot().Operations.Queued; q != 0 {
		t.Fatalf("%d queued", q)
	}
}

func TestUpgradeAllRefreshesThenUpgradesWhatIsUpgradable(t *testing.T) {
	apt := newFakeApt(
		installed("ripgrep", "14.1.0-1", "14.1.1-1"),
		installed("shellcheck", "0.10.0-1", "0.10.0-1"),
		installed("jq", "1.7.1-6", "1.7.2-1"),
	)
	s, _, _ := pkgService(t, apt, "ripgrep", "shellcheck", "jq")
	startService(t, s)
	if err := s.UpgradePackages(nil, true); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Kind != "upgrade" || r.Target != "apt all managed packages" || r.Outcome != "ok" || r.Message != "upgraded ripgrep" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"refresh", "upgrade ripgrep"}) {
		t.Fatalf("apt calls %q: the base name jq is left out", got)
	}
}

func TestAnUpgradeWithNothingToUpgradeIsSkipped(t *testing.T) {
	apt := newFakeApt(installed("shellcheck", "0.10.0-1", "0.10.0-1"))
	s, _, busy := pkgService(t, apt, "shellcheck")
	busy.Store(1)
	startService(t, s)
	if err := s.UpgradePackages([]string{"shellcheck"}, false); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Outcome != "skipped" || r.Message != "nothing to upgrade" {
		t.Fatalf("operation %+v: nothing upgradable takes no hold", r)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"refresh"}) {
		t.Fatalf("apt calls %q", got)
	}
}

func TestAnUpgradeSkipsNamesForgottenWhileItWaited(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.1-1"), installed("shellcheck", "0.10.0-1", "0.10.1-1"))
	s, _, _ := pkgService(t, apt, "ripgrep", "shellcheck")
	startService(t, s)
	gate := make(chan struct{})
	if err := s.enqueue(testOp("run", "clear", "nuget", "ok", gate)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the gated operation to run", func() bool { return s.Snapshot().Operations.Current != nil })
	if err := s.UpgradePackages([]string{"ripgrep", "shellcheck"}, false); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"ripgrep", "shellcheck"} {
		if err := s.ForgetPackage(n); err != nil {
			t.Fatal(err)
		}
	}
	if q := s.Snapshot().Operations.Queued; q != 1 {
		t.Fatalf("%d queued: Forget drops only operations whose target is apt <name>", q)
	}
	close(gate)
	r := waitRecent(t, s, 2)[0]
	if r.Target != "apt ripgrep, shellcheck" || r.Outcome != "skipped" || r.Message != "nothing to upgrade" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("apt calls %q: no name left means no refresh", got)
	}
}

func TestUpgradeAndUninstallAreRefusedWhileRunnersAreBusy(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.1-1"))
	s, m, busy := pkgService(t, apt, "ripgrep")
	busy.Store(2)
	startService(t, s)
	if err := s.UpgradePackages([]string{"ripgrep"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UninstallPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	for _, r := range waitRecent(t, s, 2)[:2] {
		if r.Outcome != "refused" || r.Message != "refused: 2 jobs running" {
			t.Fatalf("while busy: %+v", r)
		}
	}
	if got := apt.callList(); !slices.Equal(got, []string{"refresh"}) || !slices.Equal(m.List(), []string{"ripgrep"}) {
		t.Fatalf("apt calls %q, managed %q", got, m.List())
	}
}

func TestUninstallRunsDpkgRemoveAndDropsTheName(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1"), installed("shellcheck", "0.10.0-1", "0.10.0-1"))
	s, m, _ := pkgService(t, apt, "ripgrep", "shellcheck")
	startService(t, s)
	if err := s.UninstallPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Kind != "remove" || r.Target != "apt ripgrep" || r.Outcome != "ok" || r.Message != "uninstalled ripgrep" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"remove ripgrep"}) || !slices.Equal(m.List(), []string{"shellcheck"}) {
		t.Fatalf("apt calls %q, managed %q", got, m.List())
	}
}

func TestAnUninstallDpkgRefusesKeepsTheName(t *testing.T) {
	apt := newFakeApt(installed("libjq1", "1.7.1-6", "1.7.1-6"))
	apt.errs["remove"] = errors.New("dpkg: dependency problems prevent removal of libjq1:amd64: jq depends on libjq1 (= 1.7.1-6). dpkg: error processing package libjq1:amd64 (--remove): dependency problems - not removing")
	s, m, _ := pkgService(t, apt, "libjq1")
	startService(t, s)
	if err := s.UninstallPackage("libjq1"); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Outcome != "failed" || !strings.HasSuffix(r.Message, "dependency problems - not removing") {
		t.Fatalf("operation %+v", r)
	}
	if got := m.List(); !slices.Equal(got, []string{"libjq1"}) {
		t.Fatalf("managed %q", got)
	}
}

func TestUninstallRefusals(t *testing.T) {
	apt := newFakeApt(installed("jq", "1.7.1-6", "1.7.1-6"), installed("curl", "8.14.1-2", "8.14.1-2"))
	s, _, _ := pkgService(t, apt, "jq")
	startService(t, s)
	if err := s.UninstallPackage("curl"); !errors.Is(err, packages.ErrNotManaged) {
		t.Errorf("a base name that is not managed: %v, want ErrNotManaged first", err)
	}
	if err := s.UninstallPackage("jq;rm"); !errors.Is(err, packages.ErrNotManaged) {
		t.Errorf("an invalid name: %v", err)
	}
	if err := s.UninstallPackage("jq"); !errors.Is(err, packages.ErrBase) {
		t.Errorf("a managed base name: %v, want ErrBase", err)
	}
	if q := s.Snapshot().Operations.Queued; q != 0 {
		t.Fatalf("%d queued", q)
	}
}

func TestForgetDropsQueuedOperationsAndRunsNoCommand(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.1-1"), installed("jq", "1.7.1-6", "1.7.1-6"))
	s, m, _ := pkgService(t, apt, "ripgrep", "jq")
	startService(t, s)
	gate := make(chan struct{})
	if err := s.enqueue(testOp("run", "clear", "nuget", "ok", gate)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the gated operation to run", func() bool { return s.Snapshot().Operations.Current != nil })
	if err := s.UpgradePackages([]string{"ripgrep"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UninstallPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	s.setPkgErr("ripgrep", "E: old error")
	before := s.PackagesVersion()
	if err := s.ForgetPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetPackage("jq"); err != nil {
		t.Fatalf("a managed base name can be forgotten: %v", err)
	}
	if q := s.Snapshot().Operations.Queued; q != 0 {
		t.Fatalf("%d queued after Forget", q)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("managed %q", got)
	}
	if s.PackagesVersion() <= before || s.pkgErr("ripgrep") != "" {
		t.Fatalf("version %d (was %d), error %q", s.PackagesVersion(), before, s.pkgErr("ripgrep"))
	}
	if err := s.ForgetPackage("ripgrep"); !errors.Is(err, packages.ErrNotManaged) {
		t.Fatalf("forgetting twice: %v", err)
	}
	close(gate)
	waitRecent(t, s, 1)
	time.Sleep(50 * time.Millisecond)
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("apt calls %q", got)
	}
}

func TestAForgetWhoseSaveFailsKeepsTheQueuedWork(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1"))
	s, m, _ := pkgService(t, apt, "ripgrep")
	startService(t, s)
	gate := make(chan struct{})
	if err := s.enqueue(testOp("run", "clear", "nuget", "ok", gate)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the gated operation to run", func() bool { return s.Snapshot().Operations.Current != nil })
	if err := s.UninstallPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	s.setPkgErr("ripgrep", "E: x")
	saveErr := errors.New("config.yaml: permission denied")
	m.mu.Lock()
	m.dropErr = saveErr
	m.mu.Unlock()
	if err := s.ForgetPackage("ripgrep"); !errors.Is(err, saveErr) {
		t.Fatalf("ForgetPackage: %v, want %v", err, saveErr)
	}
	if q := s.Snapshot().Operations.Queued; q != 1 {
		t.Fatalf("%d queued: a failed save must keep the removal", q)
	}
	if got := s.pkgErr("ripgrep"); got != "E: x" {
		t.Fatalf("error %q: a failed save must keep it", got)
	}
	if got := m.List(); !slices.Equal(got, []string{"ripgrep"}) {
		t.Fatalf("managed %q", got)
	}
	m.mu.Lock()
	m.dropErr = nil
	m.mu.Unlock()
	close(gate)
	r := waitRecent(t, s, 2)[0]
	if r.Kind != "remove" || r.Outcome != "ok" || r.Message != "uninstalled ripgrep" {
		t.Fatalf("operation %+v", r)
	}
}

func TestForgetIsRefusedWhileAnOperationOnTheNameRuns(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1"))
	apt.block["remove"] = make(chan struct{})
	s, m, _ := pkgService(t, apt, "ripgrep")
	startService(t, s)
	if err := s.UninstallPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the removal to run", func() bool { return slices.Contains(apt.callList(), "remove ripgrep") })
	if err := s.ForgetPackage("ripgrep"); !errors.Is(err, packages.ErrBusy) {
		t.Fatalf("Forget while its removal runs: %v, want ErrBusy", err)
	}
	if got := m.List(); !slices.Equal(got, []string{"ripgrep"}) {
		t.Fatalf("managed %q", got)
	}
	close(apt.block["remove"])
	if r := waitRecent(t, s, 1)[0]; r.Outcome != "ok" {
		t.Fatalf("operation %+v", r)
	}
}

func TestARemoveWhoseNameLeftTheListFailsWithoutDpkg(t *testing.T) {
	apt := newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1"))
	s, m, _ := pkgService(t, apt, "ripgrep")
	startService(t, s)
	gate := make(chan struct{})
	if err := s.enqueue(testOp("run", "clear", "nuget", "ok", gate)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the gated operation to run", func() bool { return s.Snapshot().Operations.Current != nil })
	if err := s.UninstallPackage("ripgrep"); err != nil {
		t.Fatal(err)
	}
	// An edit of config.yaml, not a Forget, which would drop the removal.
	if err := m.Drop("ripgrep"); err != nil {
		t.Fatal(err)
	}
	close(gate)
	r := waitRecent(t, s, 2)[0]
	if r.Outcome != "failed" || r.Message != "ripgrep is no longer managed" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("apt calls %q", got)
	}
}

func TestEveryPackageRequestNeedsApt(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(s *Service)
	}{
		{"no Manager", func(s *Service) { s.Apt = nil }},
		{"no managed list", func(s *Service) { s.Managed = nil }},
		{"apt not on PATH", func(s *Service) { s.Apt.(*fakeApt).off = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, _, _ := pkgService(t, newFakeApt(installed("ripgrep", "14.1.0-1", "14.1.0-1")), "ripgrep")
			c.set(s)
			startService(t, s)
			ctx := t.Context()
			_, listErr := s.PackageList(ctx)
			_, searchErr := s.SearchPackages(ctx, "rip")
			_, addErr := s.AddPackages(ctx, []string{"vim"})
			for i, err := range []error{
				listErr, searchErr, addErr,
				s.UpgradePackages(nil, true),
				s.UninstallPackage("ripgrep"),
				s.ForgetPackage("ripgrep"),
				s.RefreshPackageIndex(),
			} {
				if !errors.Is(err, packages.ErrUnavailable) {
					t.Errorf("request %d: %v, want ErrUnavailable", i, err)
				}
			}
		})
	}
}

func TestRefreshPackageIndexStampsTheIndex(t *testing.T) {
	apt := newFakeApt()
	s, _, _ := pkgService(t, apt)
	stampAt(t, s.IndexStamp, time.Now().Add(-48*time.Hour))
	startService(t, s)
	before := s.PackagesVersion()
	if err := s.RefreshPackageIndex(); err != nil {
		t.Fatal(err)
	}
	r := waitRecent(t, s, 1)[0]
	if r.Kind != "refresh" || r.Target != "apt index" || r.Outcome != "ok" {
		t.Fatalf("operation %+v", r)
	}
	if fi, err := os.Stat(s.IndexStamp); err != nil || time.Since(fi.ModTime()) > time.Minute {
		t.Fatalf("stamp %v, %v", fi, err)
	}
	if s.PackagesVersion() <= before {
		t.Fatal("the packages version did not move when the refresh ended")
	}
}
