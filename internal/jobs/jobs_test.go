package jobs

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

var start = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// rig is a registry over a config and a clock the test changes.
type rig struct {
	mu      sync.Mutex
	cfg     config.Jobs
	now     time.Time
	changes int
	r       *Registry
}

func newRig() *rig {
	g := &rig{cfg: config.DefaultJobs(), now: start}
	g.r = New(func() config.Jobs {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.cfg
	}, func() time.Time {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.now
	}, func() {
		g.mu.Lock()
		g.changes++
		g.mu.Unlock()
	})
	return g
}

func (g *rig) set(fn func(c *config.Jobs)) {
	g.mu.Lock()
	fn(&g.cfg)
	g.mu.Unlock()
}

func (g *rig) job(t *testing.T, name string) model.JobStatus {
	t.Helper()
	for _, j := range g.r.Snapshot() {
		if j.Name == name {
			return j
		}
	}
	t.Fatalf("no task %s", name)
	return model.JobStatus{}
}

func TestReleaseChecksAreDueAtStartTheRestAfterAnInterval(t *testing.T) {
	g := newRig()
	for name, cases := range map[string][]struct {
		at  time.Duration
		due bool
	}{
		config.JobGhrUpdateCheck:    {{0, true}},
		config.JobRunnerUpdateCheck: {{0, true}},
		config.JobPrune:             {{0, false}, {24*time.Hour - time.Second, false}, {24 * time.Hour, true}},
		config.JobStorageMeasure:    {{29 * time.Minute, false}, {30 * time.Minute, true}},
	} {
		for _, c := range cases {
			if got := g.r.Due(name, start.Add(c.at)); got != c.due {
				t.Errorf("%s at +%s: due %v, want %v", name, c.at, got, c.due)
			}
		}
	}
}

func TestFinishedSchedulesTheNextRun(t *testing.T) {
	g := newRig()
	at := start.Add(time.Hour)
	g.r.Started(config.JobPrune, at)
	if g.r.Due(config.JobPrune, at.Add(48*time.Hour)) {
		t.Fatal("due while running")
	}
	if j := g.job(t, config.JobPrune); !j.Running || j.LastStart == nil || !j.LastStart.Equal(at) || j.Next != nil {
		t.Fatalf("running %+v", j)
	}
	g.r.Finished(config.JobPrune, at, nil, nil)
	if next, ok := g.r.Next(config.JobPrune); !ok || !next.Equal(at.Add(24*time.Hour)) {
		t.Fatalf("next %v ok %v", next, ok)
	}
	if g.r.Due(config.JobPrune, at.Add(24*time.Hour-time.Second)) || !g.r.Due(config.JobPrune, at.Add(24*time.Hour)) {
		t.Fatal("due at the wrong time")
	}
	if j := g.job(t, config.JobPrune); j.Running || j.LastFinish == nil || !j.LastFinish.Equal(at) || j.LastError != "" {
		t.Fatalf("finished %+v", j)
	}
}

func TestFinishedWithRetryAndError(t *testing.T) {
	g := newRig()
	retry := start.Add(time.Hour)
	g.r.Started(config.JobRunnerUpdateCheck, start)
	g.r.Finished(config.JobRunnerUpdateCheck, start, errors.New("github: 502"), &retry)
	if next, _ := g.r.Next(config.JobRunnerUpdateCheck); !next.Equal(retry) {
		t.Fatalf("next %v, want %v", next, retry)
	}
	if j := g.job(t, config.JobRunnerUpdateCheck); j.LastError != "github: 502" {
		t.Fatalf("error %q", j.LastError)
	}
	later := retry.Add(time.Minute)
	g.r.Started(config.JobRunnerUpdateCheck, later)
	g.r.Finished(config.JobRunnerUpdateCheck, later, nil, nil)
	if next, _ := g.r.Next(config.JobRunnerUpdateCheck); !next.Equal(later.Add(24 * time.Hour)) {
		t.Fatalf("a success did not clear the retry: %v", next)
	}
	if j := g.job(t, config.JobRunnerUpdateCheck); j.LastError != "" {
		t.Fatalf("a success kept the error %q", j.LastError)
	}
}

func TestRunNow(t *testing.T) {
	g := newRig()
	woke := 0
	g.r.Register(config.JobPrune, func() { woke++ })
	if err := g.r.RunNow(config.JobPrune); err != nil {
		t.Fatal(err)
	}
	if woke != 1 || !g.r.Due(config.JobPrune, start) || !g.job(t, config.JobPrune).Pending {
		t.Fatalf("woke %d due %v", woke, g.r.Due(config.JobPrune, start))
	}
	if err := g.r.RunNow(config.JobPrune); !errors.Is(err, ErrBusy) {
		t.Fatalf("second RunNow: %v", err)
	}
	g.r.Started(config.JobPrune, start)
	if g.job(t, config.JobPrune).Pending {
		t.Fatal("Started kept the Run now pending")
	}
	if err := g.r.RunNow(config.JobPrune); !errors.Is(err, ErrBusy) {
		t.Fatalf("RunNow while running: %v", err)
	}
	g.set(func(c *config.Jobs) { c.StorageMeasure.Enabled = false })
	if err := g.r.RunNow(config.JobStorageMeasure); !errors.Is(err, ErrOff) {
		t.Fatalf("RunNow while off: %v", err)
	}
	if err := g.r.RunNow("nightly"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("RunNow of an unknown task: %v", err)
	}
}

func TestConfigChangesTakeEffect(t *testing.T) {
	g := newRig()
	woke := map[string]int{}
	for _, name := range config.JobNames {
		g.r.Register(name, func() { woke[name]++ })
	}
	g.r.Finished(config.JobPrune, start, nil, nil)
	g.set(func(c *config.Jobs) { c.Prune.Every = config.Duration(12 * time.Hour) })
	if next, _ := g.r.Next(config.JobPrune); !next.Equal(start.Add(12 * time.Hour)) {
		t.Fatalf("next %v after a shorter interval", next)
	}
	before := g.changes
	g.r.Changed()
	for _, name := range config.JobNames {
		if woke[name] != 1 {
			t.Errorf("%s woken %d times", name, woke[name])
		}
	}
	if g.changes != before+1 {
		t.Fatal("Changed did not report a change")
	}
}

func TestATaskThatIsOff(t *testing.T) {
	g := newRig()
	if err := g.r.RunNow(config.JobPrune); err != nil {
		t.Fatal(err)
	}
	g.set(func(c *config.Jobs) { c.Prune.Enabled = false })
	g.r.Changed()
	if g.r.Due(config.JobPrune, start.Add(72*time.Hour)) {
		t.Fatal("due while off")
	}
	if _, ok := g.r.Next(config.JobPrune); ok {
		t.Fatal("a next run while off")
	}
	if d := g.r.Until(config.JobPrune, start); d != time.Hour {
		t.Fatalf("until %s while off", d)
	}
	g.r.Started(config.JobPrune, start)
	g.r.Finished(config.JobPrune, start, nil, nil)
	j := g.job(t, config.JobPrune)
	if j.Enabled || j.Pending || j.Next != nil || j.LastFinish == nil {
		t.Fatalf("off %+v", j)
	}
}

func TestUntil(t *testing.T) {
	g := newRig()
	if d := g.r.Until(config.JobGhrUpdateCheck, start.Add(time.Minute)); d != 0 {
		t.Fatalf("until %s for a due task", d)
	}
	if d := g.r.Until(config.JobStorageMeasure, start.Add(10*time.Minute)); d != 20*time.Minute {
		t.Fatalf("until %s", d)
	}
}

func TestSnapshot(t *testing.T) {
	g := newRig()
	snap := g.r.Snapshot()
	if len(snap) != len(config.JobNames) {
		t.Fatalf("%d tasks", len(snap))
	}
	for i, j := range snap {
		if j.Name != config.JobNames[i] || j.Title == "" || j.Description == "" || !j.Enabled {
			t.Errorf("task %d %+v", i, j)
		}
	}
	if snap[2].Every != "1d" || snap[3].Every != "30m0s" {
		t.Fatalf("every %q %q", snap[2].Every, snap[3].Every)
	}
	var none *Registry
	if none.Snapshot() != nil {
		t.Fatal("a nil registry has tasks")
	}
}

func TestConcurrentUse(t *testing.T) {
	g := newRig()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				_ = g.r.RunNow(config.JobPrune)
				g.r.Due(config.JobPrune, start)
				g.r.Started(config.JobPrune, start)
				g.r.Finished(config.JobPrune, start, nil, nil)
				g.r.Snapshot()
				g.r.Changed()
			}
		}()
	}
	wg.Wait()
}
