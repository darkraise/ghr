package metrics

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

type host struct {
	dir string
	s   *Sampler
	now time.Time
}

func newHost(t *testing.T) *host {
	t.Helper()
	h := &host{dir: t.TempDir(), now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	h.s = NewSampler(func() Snapshot { return Snapshot{Live: 2, Queued: 3} }, func() int { return 11 })
	h.s.Now = func() time.Time { return h.now }
	h.s.CPUStat = filepath.Join(h.dir, "cpu.stat")
	h.s.CPUMax = filepath.Join(h.dir, "cpu.max")
	h.s.MemInfo = filepath.Join(h.dir, "meminfo")
	h.s.NumCPU = func() int { return 2 }
	h.write(t, "cpu.max", "max 100000\n")
	h.write(t, "meminfo", "MemTotal:        8388608 kB\nMemFree:  100 kB\nMemAvailable:    8204288 kB\n")
	return h
}

func (h *host) write(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSamplesCPUMemoryAndCounts(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 1000000\nuser_usec 1\n")
	h.s.Sample()
	m := h.s.Metrics()
	if len(m.Samples) != 1 || m.Samples[0].CPU != nil || m.Samples[0].Live != 2 || m.Samples[0].Queued != 3 {
		t.Fatalf("first sample %+v", m.Samples)
	}
	if *m.MemUsed != (8388608-8204288)*1024 || *m.MemTotal != 8388608*1024 || m.DiskPct != 11 {
		t.Fatalf("current %+v", m)
	}
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 61000000\n") // 60 s of CPU over 60 s on 2 CPUs
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[1].CPU == nil || *m.Samples[1].CPU != 50 || *m.CPU != 50 {
		t.Fatalf("second sample %+v", m.Samples[1])
	}
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 5\n") // the counter went backwards
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[2].CPU != nil {
		t.Fatal("a counter going backwards gives no CPU value")
	}
	os.Remove(filepath.Join(h.dir, "meminfo"))
	h.now = h.now.Add(time.Minute)
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[3].Mem != nil || m.MemUsed != nil {
		t.Fatal("a failed memory read gives no value")
	}
}

func TestQuotaAndRing(t *testing.T) {
	h := newHost(t)
	h.s.NumCPU = func() int { return 8 }
	h.write(t, "cpu.max", "200000 100000\n") // a quota of 2 CPUs
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Sample()
	for i := 1; i <= 64; i++ {
		h.now = h.now.Add(time.Minute)
		h.write(t, "cpu.stat", "usage_usec "+itoa(int64(i)*30000000)+"\n") // 30 s per minute on 2 CPUs
		h.s.Sample()
	}
	m := h.s.Metrics()
	if len(m.Samples) != 60 || *m.CPU != 25 {
		t.Fatalf("samples %d cpu %v", len(m.Samples), *m.CPU)
	}
	if want := time.Date(2026, 10, 5, 12, 5, 0, 0, time.UTC); !m.Samples[0].At.Equal(want) {
		t.Fatalf("oldest kept %v", m.Samples[0].At)
	}
}

// A failed quota read gives no CPU value rather than a percentage over the
// host's CPU count, and the next good read recovers.
func TestQuotaReadFailureAndRecovery(t *testing.T) {
	h := newHost(t)
	h.s.NumCPU = func() int { return 8 }
	h.write(t, "cpu.max", "200000 100000\n")
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Sample()
	os.Remove(filepath.Join(h.dir, "cpu.max"))
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 30000000\n")
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[1].CPU != nil || m.CPU != nil {
		t.Fatalf("missing cpu.max: %+v", m.Samples[1])
	}
	h.write(t, "cpu.max", "garbage\n")
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 60000000\n")
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[2].CPU != nil {
		t.Fatalf("unparseable cpu.max: %+v", m.Samples[2])
	}
	h.write(t, "cpu.max", "200000 100000\n")
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 90000000\n")
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[3].CPU == nil || *m.Samples[3].CPU != 25 {
		t.Fatalf("recovered: %+v", m.Samples[3])
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Changing the series Metrics returns leaves the sampler's own untouched.
func TestMetricsReturnsACopy(t *testing.T) {
	h := newHost(t)
	h.s.Sample()
	h.now = h.now.Add(time.Minute)
	h.s.Sample()
	m := h.s.Metrics()
	m.Samples[0].Live = 99
	m.Samples[1] = model.MetricSample{}
	_ = append(m.Samples[:1], model.MetricSample{Queued: 42})
	again := h.s.Metrics()
	if len(again.Samples) != 2 || again.Samples[0].Live != 2 || again.Samples[1].Queued != 3 {
		t.Fatalf("sampler state changed: %+v", again.Samples)
	}
}

// Run samples at once, then on every tick, and returns when ctx ends.
func TestRunSamplesUntilCancelled(t *testing.T) {
	h := newHost(t)
	var calls atomic.Int32
	h.s.Counts = func() Snapshot { calls.Add(1); return Snapshot{} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.s.Run(ctx, time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d samples", calls.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	n := calls.Load()
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != n {
		t.Fatal("sampling continued after Run returned")
	}
}
