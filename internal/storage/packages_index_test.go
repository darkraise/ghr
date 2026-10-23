package storage

import (
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/jobs"
	"github.com/darkraise/ghr/internal/model"
)

func indexJob(t *testing.T, r *jobs.Registry) model.JobStatus {
	t.Helper()
	for _, j := range r.Snapshot() {
		if j.Name == config.JobPackageIndex {
			return j
		}
	}
	t.Fatal("no package_index task")
	return model.JobStatus{}
}

// runIndexNow runs the package_index task and waits for it to finish.
func runIndexNow(t *testing.T, reg *jobs.Registry) model.JobStatus {
	t.Helper()
	if err := reg.RunNow(config.JobPackageIndex); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the package_index run", func() bool {
		j := indexJob(t, reg)
		return j.LastFinish != nil && !j.Running && !j.Pending
	})
	return indexJob(t, reg)
}

// gateWorker parks the queue worker in an operation until the test ends or
// the returned gate is closed.
func gateWorker(t *testing.T, s *Service) chan struct{} {
	t.Helper()
	gate := make(chan struct{})
	if err := s.enqueue(testOp("gate", "clear", "nuget", "ok", gate)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the gated operation to run", func() bool { return s.Snapshot().Operations.Current != nil })
	return gate
}

func TestReconcileAtStartQueuesOneInstallPerMissingName(t *testing.T) {
	apt := newFakeApt(
		installed("ripgrep", "14.1.0-1", "14.1.0-1"),
		notInstalled("shellcheck", "0.10.0-1"),
		notInstalled("yq", "4.44.3-1"),
		notInstalled("zz-gone", ""),
		notInstalled("jq", "1.7.1-6"),
	)
	s, m, _ := pkgService(t, apt, "ripgrep", "shellcheck", "zz-gone", "yq", "jq")
	startService(t, s)
	waitFor(t, "two installs", func() bool { return len(s.Snapshot().Operations.Recent) == 2 })
	if got := recentOutcomes(s); !slices.Equal(got, []string{"install apt yq ok", "install apt shellcheck ok"}) {
		t.Fatalf("recent %q: one install per missing name with a candidate, none for the base name jq", got)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"others shellcheck", "install shellcheck", "others yq", "install yq"}) {
		t.Fatalf("apt calls %q", got)
	}
	if n := eventCount(s, "packages: no installation candidate in the configured apt sources for zz-gone"); n != 1 {
		t.Fatalf("%d warnings for the name without a candidate", n)
	}
	list, err := s.PackageList(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Managed {
		want := ""
		if p.Name == "zz-gone" {
			want = noCandidate
		}
		if p.Error != want {
			t.Errorf("%s error %q, want %q", p.Name, p.Error, want)
		}
	}
	if !slices.Equal(m.List(), []string{"ripgrep", "shellcheck", "zz-gone", "yq", "jq"}) {
		t.Fatalf("managed %q", m.List())
	}
}

func TestReconcileMovesThePackagesTopic(t *testing.T) {
	s, _, _ := pkgService(t, newFakeApt())
	startService(t, s)
	before := s.PackagesVersion()
	s.ReconcilePackages(t.Context())
	if s.PackagesVersion() <= before {
		t.Fatal("a reconcile, which follows every config reload, must move the packages topic")
	}
}

func TestAFailedReconcileInstallKeepsTheNameWithItsError(t *testing.T) {
	apt := newFakeApt(notInstalled("yq", "4.44.3-1"))
	apt.errs["install"] = errors.New("E: Unable to fetch some archives")
	s, m, _ := pkgService(t, apt, "yq")
	startService(t, s)
	if r := waitRecent(t, s, 1)[0]; r.Outcome != "failed" {
		t.Fatalf("operation %+v", r)
	}
	if s.pkgErr("yq") != "E: Unable to fetch some archives" || !slices.Equal(m.List(), []string{"yq"}) {
		t.Fatalf("error %q, managed %q", s.pkgErr("yq"), m.List())
	}
	apt.set(func(f *fakeApt) { delete(f.errs, "install") })
	s.ReconcilePackages(t.Context())
	waitFor(t, "the retried install", func() bool { return len(s.Snapshot().Operations.Recent) == 2 })
	if r := s.Snapshot().Operations.Recent[0]; r.Outcome != "ok" || s.pkgErr("yq") != "" {
		t.Fatalf("retry %+v, error %q", r, s.pkgErr("yq"))
	}
}

func TestReconcileClearsTheErrorOfANameInstalledByHand(t *testing.T) {
	apt := newFakeApt(notInstalled("yq", ""))
	s, _, _ := pkgService(t, apt, "yq")
	startService(t, s)
	waitFor(t, "the reconcile at start", func() bool { return s.pkgErr("yq") == noCandidate })
	apt.set(func(f *fakeApt) { f.pkgs["yq"] = installed("yq", "4.44.3-1", "4.44.3-1") })
	s.ReconcilePackages(t.Context())
	if got := s.pkgErr("yq"); got != "" {
		t.Fatalf("error %q for a name that is installed now", got)
	}
}

func TestReconcileSkipsNamesWithAnOperationQueuedOrRunning(t *testing.T) {
	apt := newFakeApt(notInstalled("shellcheck", "0.10.0-1"), notInstalled("yq", "4.44.3-1"))
	apt.off = true
	s, _, _ := pkgService(t, apt, "shellcheck", "yq")
	startService(t, s)
	// The start reconcile must have run while apt was off, or it could read
	// the queue before the enqueue below and apt after it is back on.
	waitFor(t, "the start reconcile", func() bool {
		return eventCount(s, "packages: apt is not available on this host; not checking shellcheck, yq") == 1
	})
	gate := make(chan struct{})
	if err := s.enqueue(testOp("a", "install", "apt shellcheck", "ok", gate), testOp("b", "install", "apt yq", "ok", nil)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the gated operation to run", func() bool { return s.Snapshot().Operations.Current != nil })
	apt.set(func(f *fakeApt) { f.off = false })
	s.ReconcilePackages(t.Context())
	if q := s.Snapshot().Operations.Queued; q != 1 {
		t.Fatalf("%d queued, want only the waiting yq operation", q)
	}
	if got := apt.infoList(); len(got) != 0 {
		t.Fatalf("info calls %q: busy names are not even read", got)
	}
	close(gate)
}

func TestAReconcileInstallOfANameDroppedWhileItWaitedIsSkipped(t *testing.T) {
	apt := newFakeApt(notInstalled("yq", "4.44.3-1"))
	apt.off = true
	s, m, _ := pkgService(t, apt, "yq")
	startService(t, s)
	waitFor(t, "the start reconcile", func() bool {
		return eventCount(s, "packages: apt is not available on this host; not checking yq") == 1
	})
	gate := gateWorker(t, s)
	apt.set(func(f *fakeApt) { f.off = false })
	s.ReconcilePackages(t.Context())
	if q := s.Snapshot().Operations.Queued; q != 1 {
		t.Fatalf("%d queued, want the reinstall", q)
	}
	// An edit of config.yaml, not a Forget, which would drop the reinstall.
	if err := m.Drop("yq"); err != nil {
		t.Fatal(err)
	}
	before := s.PackagesVersion()
	close(gate)
	r := waitRecent(t, s, 2)[0]
	if r.Kind != "install" || r.Target != "apt yq" || r.Outcome != "skipped" || r.Message != "yq is no longer managed" {
		t.Fatalf("operation %+v", r)
	}
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("apt calls %q", got)
	}
	if s.PackagesVersion() <= before {
		t.Fatal("the skipped reinstall did not move the packages topic")
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("managed %q", got)
	}
}

func TestAReconcileRefusedByAFullQueueStillChecksEveryName(t *testing.T) {
	apt := newFakeApt(notInstalled("shellcheck", "0.10.0-1"), notInstalled("yq", "4.44.3-1"), notInstalled("zz-gone", ""))
	apt.off = true
	s, _, _ := pkgService(t, apt, "shellcheck", "yq", "zz-gone")
	startService(t, s)
	waitFor(t, "the start reconcile", func() bool {
		return eventCount(s, "packages: apt is not available on this host; not checking shellcheck, yq, zz-gone") == 1
	})
	gate := gateWorker(t, s)
	defer close(gate)
	for i := range maxQueued {
		if err := s.enqueue(testOp(strconv.Itoa(i), "install", "apt p"+strconv.Itoa(i), "ok", nil)); err != nil {
			t.Fatal(err)
		}
	}
	apt.set(func(f *fakeApt) { f.off = false })
	s.ReconcilePackages(t.Context())
	if n := eventCount(s, "packages: reinstalling shellcheck, yq: "+ErrQueueFull.Error()); n != 1 {
		t.Fatalf("%d warnings naming the refused reinstalls", n)
	}
	if got := s.pkgErr("zz-gone"); got != noCandidate {
		t.Fatalf("error %q for the name after the refusal", got)
	}
	if n := eventCount(s, "packages: no installation candidate in the configured apt sources for zz-gone"); n != 1 {
		t.Fatalf("%d warnings for the name without a candidate", n)
	}
	if q := s.Snapshot().Operations.Queued; q != maxQueued {
		t.Fatalf("%d queued", q)
	}
}

func TestReconcileWithoutAptOnlyWarns(t *testing.T) {
	apt := newFakeApt(notInstalled("ripgrep", "14.1.0-1"))
	apt.off = true
	s, _, _ := pkgService(t, apt, "ripgrep")
	startService(t, s)
	waitFor(t, "the warning", func() bool {
		return eventCount(s, "packages: apt is not available on this host; not checking ripgrep") == 1
	})
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("apt calls %q", got)
	}
}

func TestPackageIndexTaskRefreshesThenReconciles(t *testing.T) {
	apt := newFakeApt(notInstalled("yq", ""))
	s, _, _ := pkgService(t, apt, "yq")
	reg, _ := jobsRig()
	s.Jobs = reg
	startService(t, s)
	waitFor(t, "the reconcile at start", func() bool { return s.pkgErr("yq") == noCandidate })
	if j := indexJob(t, reg); j.Every != "1d" || !j.Enabled || j.LastStart != nil {
		t.Fatalf("task %+v: on, daily, and not run at start", j)
	}
	// The refresh brings a candidate the reconcile then installs.
	apt.set(func(f *fakeApt) { f.pkgs["yq"] = notInstalled("yq", "4.44.3-1") })
	if j := runIndexNow(t, reg); j.LastError != "" {
		t.Fatalf("task %+v", j)
	}
	waitFor(t, "the reinstall", func() bool { return len(s.Snapshot().Operations.Recent) == 1 })
	if got := recentOutcomes(s); !slices.Equal(got, []string{"install apt yq ok"}) {
		t.Fatalf("recent %q: a successful task refresh stays out of recent", got)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"refresh", "others yq", "install yq"}) {
		t.Fatalf("apt calls %q", got)
	}
}

func TestAFailedIndexRefreshIsTheTaskResult(t *testing.T) {
	apt := newFakeApt()
	apt.errs["refresh"] = errors.New("E: Failed to fetch http://deb.debian.org/debian/dists/trixie/InRelease")
	s, _, _ := pkgService(t, apt)
	reg, _ := jobsRig()
	s.Jobs = reg
	startService(t, s)
	if j := runIndexNow(t, reg); j.LastError != "E: Failed to fetch http://deb.debian.org/debian/dists/trixie/InRelease" {
		t.Fatalf("task %+v", j)
	}
	if got := recentOutcomes(s); !slices.Equal(got, []string{"refresh apt index failed"}) {
		t.Fatalf("recent %q: a failed task refresh is kept", got)
	}
}

func TestPackageIndexTaskWithoutApt(t *testing.T) {
	s, _, _ := pkgService(t, newFakeApt())
	s.Apt = nil
	reg, _ := jobsRig()
	s.Jobs = reg
	startService(t, s)
	if j := runIndexNow(t, reg); j.LastError != "apt is not available on this host" {
		t.Fatalf("task %+v", j)
	}
	if st := s.Snapshot().Operations; st.Queued != 0 || len(st.Recent) != 0 {
		t.Fatalf("operations %+v", st)
	}
}

func TestPackageIndexTaskWaitsOnARefreshAlreadyQueued(t *testing.T) {
	apt := newFakeApt()
	s, _, _ := pkgService(t, apt)
	reg, _ := jobsRig()
	s.Jobs = reg
	startService(t, s)
	gate := gateWorker(t, s)
	if err := s.RefreshPackageIndex(); err != nil {
		t.Fatal(err)
	}
	if err := reg.RunNow(config.JobPackageIndex); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the task to start", func() bool { return indexJob(t, reg).Running })
	time.Sleep(50 * time.Millisecond)
	if q := s.Snapshot().Operations.Queued; q != 1 {
		t.Fatalf("%d queued, want the one refresh", q)
	}
	close(gate)
	waitFor(t, "the task to finish", func() bool { j := indexJob(t, reg); return j.LastFinish != nil && !j.Running })
	if j := indexJob(t, reg); j.LastError != "" {
		t.Fatalf("task %+v", j)
	}
	if got := apt.callList(); !slices.Equal(got, []string{"refresh"}) {
		t.Fatalf("apt calls %q: one refresh", got)
	}
	if got := recentOutcomes(s); !slices.Equal(got, []string{"refresh apt index ok", "clear nuget ok"}) {
		t.Fatalf("recent %q: the API's refresh keeps its place in recent", got)
	}
}

func TestPackageIndexTaskWithAFullQueue(t *testing.T) {
	apt := newFakeApt()
	s, _, _ := pkgService(t, apt)
	reg, _ := jobsRig()
	s.Jobs = reg
	startService(t, s)
	gate := gateWorker(t, s)
	defer close(gate)
	for i := range maxQueued {
		if err := s.enqueue(testOp(strconv.Itoa(i), "install", "apt p"+strconv.Itoa(i), "ok", nil)); err != nil {
			t.Fatal(err)
		}
	}
	if j := runIndexNow(t, reg); j.LastError != ErrQueueFull.Error() {
		t.Fatalf("task %+v", j)
	}
	if got := apt.callList(); len(got) != 0 {
		t.Fatalf("apt calls %q", got)
	}
}

func TestAPackageIndexRunRefusedByAFullQueueSkipsTheReconcile(t *testing.T) {
	apt := newFakeApt(notInstalled("yq", ""))
	s, _, _ := pkgService(t, apt, "yq")
	reg, _ := jobsRig()
	s.Jobs = reg
	startService(t, s)
	waitFor(t, "the reconcile at start", func() bool { return s.pkgErr("yq") == noCandidate })
	gate := gateWorker(t, s)
	defer close(gate)
	for i := range maxQueued {
		if err := s.enqueue(testOp(strconv.Itoa(i), "install", "apt p"+strconv.Itoa(i), "ok", nil)); err != nil {
			t.Fatal(err)
		}
	}
	if j := runIndexNow(t, reg); j.LastError != ErrQueueFull.Error() {
		t.Fatalf("task %+v", j)
	}
	time.Sleep(50 * time.Millisecond)
	if got := apt.infoList(); len(got) != 1 {
		t.Fatalf("info calls %q: only the reconcile at start reads the managed names", got)
	}
}

func TestCloseEndsAPackageIndexRunWaitingOnTheQueue(t *testing.T) {
	s, _, _ := pkgService(t, newFakeApt())
	reg, _ := jobsRig()
	s.Jobs = reg
	s.Start()
	gateWorker(t, s)
	if err := reg.RunNow(config.JobPackageIndex); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the task to start", func() bool { return indexJob(t, reg).Running })
	s.Close()
	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	recvOrFail(t, waited, "Wait after Close")
	if j := indexJob(t, reg); j.Running {
		t.Fatalf("task %+v still running", j)
	}
}
