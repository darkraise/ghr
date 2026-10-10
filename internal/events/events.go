// Package events keeps the most recent daemon events in memory.
package events

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

const Capacity = 1000

type Ring struct {
	Now      func() time.Time
	mu       sync.Mutex
	seq      int64
	items    []model.Event
	onChange func()
}

func New() *Ring { return &Ring{Now: time.Now} }

// Add records an event and mirrors it to the process log (journald under systemd).
func (r *Ring) Add(level, repo, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.mu.Lock()
	r.seq++
	r.items = append(r.items, model.Event{Seq: r.seq, Time: r.Now(), Level: level, Repo: repo, Msg: msg})
	if len(r.items) > Capacity {
		r.items = r.items[len(r.items)-Capacity:]
	}
	fn := r.onChange
	r.mu.Unlock()
	log.Printf("%s %s %s", level, repo, msg)
	if fn != nil {
		fn()
	}
}

// SetOnChange installs fn, which Add calls after it releases the ring's lock;
// nil removes it.
func (r *Ring) SetOnChange(fn func()) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

// Last returns the newest event's sequence number, 0 before any event.
func (r *Ring) Last() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seq
}

// After returns events with Seq > seq, oldest first.
func (r *Ring) After(seq int64) []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.Event
	for _, e := range r.items {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}
