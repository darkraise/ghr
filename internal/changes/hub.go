// Package changes tells subscribers which topics of the daemon's state
// changed. A watcher compares every topic's fingerprint on each tick and soon
// after a wake, and sends the topics that differ as one set.
package changes

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"slices"
	"sync"
	"time"
)

// MaxSubscribers bounds the open subscriptions, one per open browser stream.
const MaxSubscribers = 32

var (
	ErrNotReady = errors.New("changes: no sources yet")
	ErrFull     = errors.New("changes: too many subscribers")
	ErrClosed   = errors.New("changes: hub closed")
)

type Options struct {
	// Tick is how often the watcher compares fingerprints; zero means 250 ms.
	Tick time.Duration
	// WakeWindow is how long a wake waits for further wakes before its one
	// check; zero means 50 ms.
	WakeWindow time.Duration
	// Logf reports a topic whose fingerprint could not be computed; nil means log.Printf.
	Logf func(format string, args ...any)
}

type Hub struct {
	tick   time.Duration
	window time.Duration
	logf   func(format string, args ...any)
	wake   chan struct{}
	wg     sync.WaitGroup

	mu      sync.Mutex
	sources func() map[string]uint64
	before  func(topics []string)
	subs    map[*Sub]struct{}
	closed  bool
	// stop is closed to end the running watcher, nil while none runs; prev
	// and missing belong to that watcher.
	stop    chan struct{}
	prev    map[string]uint64
	missing map[string]bool
}

// Sub is one subscription. C receives sorted, de-duplicated topic sets and is
// closed when the subscription ends. A set is shared between subscribers, so
// a receiver must not modify it.
type Sub struct {
	C  <-chan []string
	ch chan []string
	h  *Hub
}

func New(o Options) *Hub {
	if o.Tick <= 0 {
		o.Tick = 250 * time.Millisecond
	}
	if o.WakeWindow <= 0 {
		o.WakeWindow = 50 * time.Millisecond
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	return &Hub{tick: o.Tick, window: o.WakeWindow, logf: o.Logf, wake: make(chan struct{}, 1), subs: map[*Sub]struct{}{}}
}

// Fingerprint is FNV-64a of v's JSON.
func Fingerprint(v any) (uint64, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64(), nil
}

// SetSources installs the function that computes every topic's fingerprint,
// and before, which runs ahead of sending a set that holds status, config,
// history or metrics. before runs with the hub's lock held, so it must not
// call the hub.
func (h *Hub) SetSources(sources func() map[string]uint64, before func(topics []string)) {
	h.mu.Lock()
	h.sources, h.before = sources, before
	h.mu.Unlock()
}

// Wake asks for a check before the next tick. It never blocks, so the event
// ring and the history store call it on every write.
func (h *Hub) Wake() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Subscribe opens a subscription. The first one takes the baseline
// fingerprints before it returns, so a change made right after is published.
func (h *Hub) Subscribe() (*Sub, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closed:
		return nil, ErrClosed
	case h.sources == nil:
		return nil, ErrNotReady
	case len(h.subs) >= MaxSubscribers:
		return nil, ErrFull
	}
	if h.stop == nil {
		h.prev = h.sources()
		if h.prev == nil {
			h.prev = map[string]uint64{}
		}
		h.missing = map[string]bool{}
		h.stop = make(chan struct{})
		h.wg.Add(1)
		go h.watch(h.stop)
	}
	ch := make(chan []string, 1)
	s := &Sub{C: ch, ch: ch, h: h}
	h.subs[s] = struct{}{}
	return s, nil
}

// Close ends the subscription; the last one to leave stops the watcher and
// drops its fingerprints, so the next subscriber takes a fresh baseline.
func (s *Sub) Close() {
	h := s.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	close(s.ch)
	if len(h.subs) == 0 && h.stop != nil {
		close(h.stop)
		h.stop, h.prev, h.missing = nil, nil, nil
	}
}

// Close ends every subscription and waits for the watcher, so no source runs
// after it returns.
func (h *Hub) Close() {
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		for s := range h.subs {
			close(s.ch)
			delete(h.subs, s)
		}
		if h.stop != nil {
			close(h.stop)
			h.stop, h.prev, h.missing = nil, nil, nil
		}
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *Hub) watch(stop chan struct{}) {
	defer h.wg.Done()
	t := time.NewTicker(h.tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		case <-h.wake:
			// Let a burst of wakes finish, then answer all of them with one check.
			select {
			case <-stop:
				return
			case <-time.After(h.window):
			}
			select {
			case <-h.wake:
			default:
			}
		}
		h.check(stop)
	}
}

func (h *Hub) check(stop chan struct{}) {
	h.mu.Lock()
	sources := h.sources
	h.mu.Unlock()
	cur := sources()
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-stop:
		return
	default:
	}
	var topics []string
	for k, old := range h.prev {
		v, ok := cur[k]
		if !ok {
			if !h.missing[k] {
				h.missing[k] = true
				h.logf("changes: cannot fingerprint %s; skipping it until it can", k)
			}
			continue
		}
		delete(h.missing, k)
		if v != old {
			topics = append(topics, k)
		}
	}
	for k, v := range cur {
		if _, seen := h.prev[k]; !seen {
			topics = append(topics, k)
		}
		h.prev[k] = v
	}
	if len(topics) == 0 {
		return
	}
	slices.Sort(topics)
	if h.before != nil && needsBefore(topics) {
		h.before(topics)
	}
	for s := range h.subs {
		s.send(topics)
	}
}

// send merges topics with a set the subscriber has not read yet, so a slow
// reader loses no topic. h.mu is held; the hub is the only sender, so the
// send cannot block once the buffer is empty.
func (s *Sub) send(topics []string) {
	select {
	case pending := <-s.ch:
		topics = union(pending, topics)
	default:
	}
	s.ch <- topics
}

func union(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}

func needsBefore(topics []string) bool {
	for _, t := range topics {
		switch t {
		case "status", "config", "history", "metrics":
			return true
		}
	}
	return false
}
