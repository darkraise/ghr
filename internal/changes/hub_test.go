package changes

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"
)

// source is a fake fingerprint function whose values tests change.
type source struct {
	mu    sync.Mutex
	fp    map[string]uint64
	calls int
	gate  chan struct{} // when set, get waits for it to close
}

func newSource(fp map[string]uint64) *source { return &source{fp: fp} }

func (s *source) get() map[string]uint64 {
	s.mu.Lock()
	s.calls++
	gate := s.gate
	out := maps.Clone(s.fp)
	s.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return out
}

// set applies every change in one step, so one check sees them together.
func (s *source) set(kv map[string]uint64) {
	s.mu.Lock()
	maps.Copy(s.fp, kv)
	s.mu.Unlock()
}

func (s *source) drop(topic string) {
	s.mu.Lock()
	delete(s.fp, topic)
	s.mu.Unlock()
}

func (s *source) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// block makes later reads wait until the returned channel is closed.
func (s *source) block() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gate = make(chan struct{})
	return s.gate
}

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logs) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

const fast = 5 * time.Millisecond

func newHub(t *testing.T, tick, window time.Duration, src *source) (*Hub, *logs) {
	t.Helper()
	l := &logs{}
	h := New(Options{Tick: tick, WakeWindow: window, Logf: l.logf})
	h.SetSources(src.get, nil)
	t.Cleanup(h.Close)
	return h, l
}

func subscribe(t *testing.T, h *Hub) *Sub {
	t.Helper()
	s, err := h.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func recv(t *testing.T, s *Sub) []string {
	t.Helper()
	select {
	case got, ok := <-s.C:
		if !ok {
			t.Fatal("the channel closed")
		}
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("no topic set within 2 s")
	}
	return nil
}

func quiet(t *testing.T, s *Sub, d time.Duration) {
	t.Helper()
	select {
	case got := <-s.C:
		t.Fatalf("unexpected set %v", got)
	case <-time.After(d):
	}
}

// waitChecks waits until src has been read n more times.
func waitChecks(t *testing.T, src *source, n int) {
	t.Helper()
	target := src.n() + n
	deadline := time.Now().Add(2 * time.Second)
	for src.n() < target {
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d checks within 2 s", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitClosed(t *testing.T, s *Sub) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-s.C:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the channel was not closed")
		}
	}
}

func TestPublishesOnlyChangedTopics(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"status": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
	quiet(t, s, 50*time.Millisecond)
}

func TestTopicsChangedInOneCheckFormOneSet(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"status": 2, "config": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"config", "status"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSubscribeTakesTheBaselineBeforeReturning(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	if src.n() < 1 {
		t.Fatal("Subscribe returned before reading the sources")
	}
	src.set(map[string]uint64{"status": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFreshBaselineAfterTheLastSubscriberLeaves(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	subscribe(t, h).Close()
	src.set(map[string]uint64{"status": 2})
	s := subscribe(t, h)
	quiet(t, s, 50*time.Millisecond)
	src.set(map[string]uint64{"status": 3})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
}

func TestWakeChecksBeforeTheTick(t *testing.T) {
	src := newSource(map[string]uint64{"events": 1})
	h, _ := newHub(t, time.Hour, 10*time.Millisecond, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"events": 2})
	h.Wake()
	if got := recv(t, s); !slices.Equal(got, []string{"events"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTopicMissingFromTheBaselinePublishesWhenItAppears(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, time.Hour, 10*time.Millisecond, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"storage": 1})
	h.Wake()
	if got := recv(t, s); !slices.Equal(got, []string{"storage"}) {
		t.Fatalf("got %v", got)
	}
}

func TestBurstOfWakesCausesOneCheck(t *testing.T) {
	src := newSource(map[string]uint64{"events": 1})
	h, _ := newHub(t, time.Hour, 50*time.Millisecond, src)
	subscribe(t, h)
	before := src.n()
	for range 10 {
		h.Wake()
	}
	time.Sleep(200 * time.Millisecond)
	if got := src.n() - before; got != 1 {
		t.Fatalf("%d checks for one burst, want 1", got)
	}
}

func TestMissingTopicIsSkippedAndLoggedOnce(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, l := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.drop("config")
	src.set(map[string]uint64{"status": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
	waitChecks(t, src, 3)
	if l.count() != 1 {
		t.Fatalf("%d log lines for one missing topic, want 1", l.count())
	}
	// The topic's last fingerprint was kept, so its return with a new value publishes.
	src.set(map[string]uint64{"config": 5})
	if got := recv(t, s); !slices.Equal(got, []string{"config"}) {
		t.Fatalf("got %v", got)
	}
	src.drop("config")
	waitChecks(t, src, 3)
	if l.count() != 2 {
		t.Fatalf("%d log lines after the topic went missing again, want 2", l.count())
	}
}

func TestSubscribeRefusals(t *testing.T) {
	h := New(Options{})
	if _, err := h.Subscribe(); !errors.Is(err, ErrNotReady) {
		t.Fatalf("before SetSources: %v", err)
	}
	src := newSource(map[string]uint64{"status": 1})
	h.SetSources(src.get, nil)
	for i := range MaxSubscribers {
		if _, err := h.Subscribe(); err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
	}
	if _, err := h.Subscribe(); !errors.Is(err, ErrFull) {
		t.Fatalf("subscriber 33: %v", err)
	}
	h.Close()
	if _, err := h.Subscribe(); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestFullChannelMergesSets(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"status": 2})
	waitChecks(t, src, 3)
	src.set(map[string]uint64{"config": 2})
	waitChecks(t, src, 3)
	if got := recv(t, s); !slices.Equal(got, []string{"config", "status"}) {
		t.Fatalf("got %v", got)
	}
	quiet(t, s, 30*time.Millisecond)
}

func TestCloseDuringPublishAndWakeDuringClose(t *testing.T) {
	var mu sync.Mutex
	var n uint64
	h := New(Options{Tick: time.Millisecond, WakeWindow: time.Millisecond})
	h.SetSources(func() map[string]uint64 {
		mu.Lock()
		defer mu.Unlock()
		n++
		return map[string]uint64{"status": n}
	}, func([]string) {})
	var subs []*Sub
	for range 5 {
		subs = append(subs, subscribe(t, h))
	}
	stopWake := make(chan struct{})
	wakeDone := make(chan struct{})
	go func() {
		defer close(wakeDone)
		for {
			select {
			case <-stopWake:
				return
			default:
				h.Wake()
			}
		}
	}()
	time.Sleep(30 * time.Millisecond)
	closed := make(chan struct{})
	go func() {
		h.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked")
	}
	close(stopWake)
	<-wakeDone
	for _, s := range subs {
		waitClosed(t, s)
	}
}

func TestCloseWaitsForTheWatcherAndIsIdempotent(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	gate := src.block()
	waitChecks(t, src, 1)
	closed := make(chan struct{})
	go func() {
		h.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a check was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the check ended")
	}
	waitClosed(t, s)
	after := src.n()
	time.Sleep(30 * time.Millisecond)
	if src.n() != after {
		t.Fatal("a source ran after Close returned")
	}
	h.Close()
	s.Close()
}

func TestSubCloseTwice(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	s.Close()
	s.Close()
	waitClosed(t, s)
}

func TestBeforePublishRunsForStateTopics(t *testing.T) {
	src := newSource(map[string]uint64{"gauges": 1, "history": 1, "status": 1})
	var mu sync.Mutex
	var ran [][]string
	h := New(Options{Tick: fast, WakeWindow: fast})
	h.SetSources(src.get, func(topics []string) {
		mu.Lock()
		ran = append(ran, topics)
		mu.Unlock()
	})
	t.Cleanup(h.Close)
	s := subscribe(t, h)
	for _, topic := range []string{"gauges", "status", "history"} {
		src.set(map[string]uint64{topic: 2})
		if got := recv(t, s); !slices.Equal(got, []string{topic}) {
			t.Fatalf("got %v, want [%s]", got, topic)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || !slices.Equal(ran[0], []string{"status"}) || !slices.Equal(ran[1], []string{"history"}) {
		t.Fatalf("before ran for %v", ran)
	}
}

func TestNeedsBefore(t *testing.T) {
	for _, tc := range []struct {
		topics []string
		want   bool
	}{
		{[]string{"status"}, true},
		{[]string{"config"}, true},
		{[]string{"history"}, true},
		{[]string{"metrics"}, true},
		{[]string{"events", "metrics"}, true},
		{[]string{"gauges"}, false},
		{[]string{"events", "gauges", "storage", "token"}, false},
	} {
		if got := needsBefore(tc.topics); got != tc.want {
			t.Errorf("needsBefore(%v) = %v", tc.topics, got)
		}
	}
}

func TestFingerprint(t *testing.T) {
	a, err := Fingerprint(map[string]int{"x": 1})
	b, _ := Fingerprint(map[string]int{"x": 1})
	c, _ := Fingerprint(map[string]int{"x": 2})
	if err != nil || a != b || a == c {
		t.Fatalf("a=%d b=%d c=%d err=%v", a, b, c, err)
	}
	if _, err := Fingerprint(make(chan int)); err == nil {
		t.Fatal("a value JSON cannot encode gave no error")
	}
}
