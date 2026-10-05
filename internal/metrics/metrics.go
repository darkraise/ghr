// Package metrics keeps an hour of one-minute samples of the runner counts
// and the container's CPU and memory use.
package metrics

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// keep is how many samples the ring holds: an hour at one a minute.
const keep = 60

// Snapshot is the runner counts at one moment, as the Dashboard tiles show them.
type Snapshot struct{ Live, Queued int }

type Sampler struct {
	Now     func() time.Time
	CPUStat string
	CPUMax  string
	MemInfo string
	NumCPU  func() int
	Counts  func() Snapshot
	Disk    func() int

	mu      sync.Mutex
	samples []model.MetricSample
	cur     model.Metrics
	lastCPU int64
	lastAt  time.Time
	haveCPU bool
}

func NewSampler(counts func() Snapshot, disk func() int) *Sampler {
	return &Sampler{Now: time.Now, CPUStat: "/sys/fs/cgroup/cpu.stat", CPUMax: "/sys/fs/cgroup/cpu.max",
		MemInfo: "/proc/meminfo", NumCPU: runtime.NumCPU, Counts: counts, Disk: disk}
}

// Run samples at once and then every interval until ctx ends.
func (s *Sampler) Run(ctx context.Context, every time.Duration) {
	s.Sample()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sample()
		}
	}
}

// Sample records one sample now. CPU is the cgroup's usage growth since the
// previous sample over elapsed time and CPUs; the first sample, a failed
// read and a counter that went backwards give none.
func (s *Sampler) Sample() {
	now := s.Now()
	counts := s.Counts()
	usage, usageOK := readUsage(s.CPUStat)
	cpus, cpusOK := s.cpus()
	used, total, memOK := readMem(s.MemInfo)
	disk := s.Disk()

	smp := model.MetricSample{At: now, Live: counts.Live, Queued: counts.Queued}
	s.mu.Lock()
	defer s.mu.Unlock()
	if usageOK && cpusOK && s.haveCPU && usage >= s.lastCPU && now.After(s.lastAt) {
		pct := float64(usage-s.lastCPU) / (float64(now.Sub(s.lastAt).Microseconds()) * cpus) * 100
		pct = min(max(pct, 0), 100)
		smp.CPU = &pct
	}
	s.lastCPU, s.lastAt, s.haveCPU = usage, now, usageOK
	s.cur = model.Metrics{CPU: smp.CPU, DiskPct: disk}
	if memOK {
		smp.Mem = &used
		s.cur.MemUsed, s.cur.MemTotal = &used, &total
	}
	s.samples = append(s.samples, smp)
	if len(s.samples) > keep {
		s.samples = s.samples[len(s.samples)-keep:]
	}
}

// Metrics returns a copy of the samples and the current figures.
func (s *Sampler) Metrics() model.Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.cur
	m.Samples = append([]model.MetricSample{}, s.samples...)
	return m
}

// cpus is the cgroup's CPU quota, or the CPU count when cpu.max says "max".
// A failed or unparseable read reports false: guessing the CPU count would
// give a wrong percentage on a quota-limited host.
func (s *Sampler) cpus() (float64, bool) {
	data, err := os.ReadFile(s.CPUMax)
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(data))
	if len(f) != 2 {
		return 0, false
	}
	if f[0] == "max" {
		return float64(s.NumCPU()), true
	}
	q, err1 := strconv.ParseFloat(f[0], 64)
	p, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || q <= 0 || p <= 0 {
		return 0, false
	}
	return q / p, true
}

func readUsage(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "usage_usec" {
			n, err := strconv.ParseInt(f[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// readMem reads used (MemTotal − MemAvailable) and total memory in bytes.
// lxcfs reports the container's own figures in /proc/meminfo; the cgroup's
// memory.current would count page cache as used.
func readMem(path string) (used, total int64, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	tot, avail := int64(-1), int64(-1)
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			tot = n * 1024
		case "MemAvailable:":
			avail = n * 1024
		}
	}
	if tot < 0 || avail < 0 {
		return 0, 0, false
	}
	return tot - avail, tot, true
}
