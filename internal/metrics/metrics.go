// Package metrics keeps three hours of one-minute samples of the runner
// counts and the container's CPU and memory use, and 30 days of hourly rollups.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

const (
	keepMinutes = 180 // three hours at one a minute
	showMinutes = 60  // what GET /metrics serves
	keepHours   = 30 * 24
)

// saveEvery bounds what a crash loses.
const saveEvery = 15 * time.Minute

// state is the persisted form of the rings.
type state struct {
	Minutes []model.MetricSample `json:"minutes"`
	Hours   []model.MetricRollup `json:"hours"`
	Open    *openHour            `json:"open,omitempty"`
}

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
	// Path is where Save and Load keep the rings; empty keeps them in memory only.
	Path string

	mu       sync.Mutex
	samples  []model.MetricSample
	hours    []model.MetricRollup
	open     *openHour
	closed   bool // an hour closed since the last save
	lastSave time.Time
	cur      model.Metrics
	lastCPU  int64
	lastAt   time.Time
	haveCPU  bool
}

func NewSampler(counts func() Snapshot, disk func() int) *Sampler {
	return &Sampler{Now: time.Now, CPUStat: "/sys/fs/cgroup/cpu.stat", CPUMax: "/sys/fs/cgroup/cpu.max",
		MemInfo: "/proc/meminfo", NumCPU: runtime.NumCPU, Counts: counts, Disk: disk}
}

// Run samples at once and then every interval until ctx ends. It saves when
// an hour closes, every saveEvery, and on the way out.
func (s *Sampler) Run(ctx context.Context, every time.Duration) {
	s.Sample()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.save()
			return
		case <-t.C:
			s.Sample()
			if s.saveDue() {
				s.save()
			}
		}
	}
}

// saveDue reports whether an hour closed or saveEvery passed since the last save.
func (s *Sampler) saveDue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.Now().Sub(s.lastSave) >= saveEvery
}

func (s *Sampler) save() {
	if err := s.Save(); err != nil {
		log.Printf("metrics: save %s: %v", s.Path, err)
	}
	s.mu.Lock()
	s.lastSave, s.closed = s.Now(), false
	s.mu.Unlock()
}

// Save writes the rings and the open hour to Path, atomically and readable
// by root only. An empty Path saves nothing.
func (s *Sampler) Save() error {
	if s.Path == "" {
		return nil
	}
	s.mu.Lock()
	data, err := json.Marshal(state{Minutes: s.samples, Hours: s.hours, Open: s.open})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".metrics-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Load restores what Save wrote, dropping minutes older than three hours and
// rollups older than 30 days. A saved open hour resumes when it is the
// current UTC hour and is closed as it stands otherwise. A missing file is an
// empty start; a malformed one returns an error and changes nothing.
func (s *Sampler) Load() error {
	if s.Path == "" {
		return nil
	}
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("%s: %w", s.Path, err)
	}
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = nil
	for _, m := range st.Minutes {
		if now.Sub(m.At) <= keepMinutes*time.Minute {
			s.samples = append(s.samples, m)
		}
	}
	if len(s.samples) > keepMinutes {
		s.samples = s.samples[len(s.samples)-keepMinutes:]
	}
	s.hours = nil
	for _, r := range st.Hours {
		if now.Sub(r.At) <= keepHours*time.Hour {
			s.hours = append(s.hours, r)
		}
	}
	s.open = nil
	if o := st.Open; o != nil {
		current := now.UTC().Truncate(time.Hour)
		switch {
		case o.At.Equal(current):
			s.open = o
		case o.At.Before(current) && now.Sub(o.At) <= keepHours*time.Hour:
			s.hours = append(s.hours, o.rollup())
		}
	}
	if len(s.hours) > keepHours {
		s.hours = s.hours[len(s.hours)-keepHours:]
	}
	return nil
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
	if usageOK && cpusOK && s.haveCPU && usage >= s.lastCPU && now.Sub(s.lastAt) >= time.Microsecond {
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
	if len(s.samples) > keepMinutes {
		s.samples = s.samples[len(s.samples)-keepMinutes:]
	}
	s.addToHour(smp)
}

// Metrics returns a copy of the last hour of samples and the current figures.
func (s *Sampler) Metrics() model.Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.cur
	start := max(len(s.samples)-showMinutes, 0)
	m.Samples = append([]model.MetricSample{}, s.samples[start:]...)
	return m
}

// Minutes returns a copy of every kept minute sample, oldest first.
func (s *Sampler) Minutes() []model.MetricSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.MetricSample{}, s.samples...)
}

// Hours returns a copy of the closed hourly rollups, oldest first, followed
// by the hour in progress.
func (s *Sampler) Hours() []model.MetricRollup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]model.MetricRollup{}, s.hours...)
	if s.open != nil {
		out = append(out, s.open.rollup())
	}
	return out
}

// openHour accumulates the samples of the UTC hour in progress.
type openHour struct {
	At        time.Time `json:"at"`
	Samples   int       `json:"samples"`
	QueuedMax int       `json:"queued_max"`
	CPUSum    float64   `json:"cpu_sum"`
	CPUN      int       `json:"cpu_n"`
	MemSum    float64   `json:"mem_sum"`
	MemN      int       `json:"mem_n"`
}

func (o *openHour) add(smp model.MetricSample) {
	o.Samples++
	o.QueuedMax = max(o.QueuedMax, smp.Queued)
	if smp.CPU != nil {
		o.CPUSum += *smp.CPU
		o.CPUN++
	}
	if smp.Mem != nil {
		o.MemSum += float64(*smp.Mem)
		o.MemN++
	}
}

func (o *openHour) rollup() model.MetricRollup {
	r := model.MetricRollup{At: o.At, Samples: o.Samples, QueuedMax: o.QueuedMax}
	if o.CPUN > 0 {
		v := o.CPUSum / float64(o.CPUN)
		r.CPUAvg = &v
	}
	if o.MemN > 0 {
		v := int64(o.MemSum / float64(o.MemN))
		r.MemAvg = &v
	}
	return r
}

// addToHour folds smp into the open UTC hour, closing it when smp belongs to
// a later one. Rollups are keyed on UTC so every one spans 60 minutes; a
// sample from before the open hour (a backwards clock step) is left out.
// The caller holds s.mu.
func (s *Sampler) addToHour(smp model.MetricSample) {
	hour := smp.At.UTC().Truncate(time.Hour)
	if s.open != nil && hour.Before(s.open.At) {
		return
	}
	if s.open != nil && hour.After(s.open.At) {
		s.closeHour()
	}
	if s.open == nil {
		s.open = &openHour{At: hour}
	}
	s.open.add(smp)
}

// closeHour moves the open hour into the rollup ring. The caller holds s.mu.
func (s *Sampler) closeHour() {
	s.hours = append(s.hours, s.open.rollup())
	if len(s.hours) > keepHours {
		s.hours = s.hours[len(s.hours)-keepHours:]
	}
	s.open = nil
	s.closed = true
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
