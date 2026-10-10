package events

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRingAfterAndCapacity(t *testing.T) {
	r := New()
	for i := 0; i < Capacity+5; i++ {
		r.Add("info", "a", "event %d", i)
	}
	all := r.After(0)
	if len(all) != Capacity || all[0].Seq != 6 {
		t.Fatalf("len=%d first=%d", len(all), all[0].Seq)
	}
	tail := r.After(int64(Capacity + 3))
	if len(tail) != 2 || tail[1].Msg != "event 1004" {
		t.Fatalf("tail = %+v", tail)
	}
}

func TestRingLastAndOnChange(t *testing.T) {
	r := New()
	if r.Last() != 0 {
		t.Fatalf("empty ring: Last = %d", r.Last())
	}
	r.Add("info", "", "before any hook")
	var seen []int64
	// The hook reads the ring, which deadlocks unless Add released its lock first.
	r.SetOnChange(func() { seen = append(seen, r.Last()) })
	r.Add("info", "a", "one")
	r.Add("warn", "a", "two")
	if r.Last() != 3 || !slices.Equal(seen, []int64{2, 3}) {
		t.Fatalf("Last = %d, hook saw %v", r.Last(), seen)
	}
	r.SetOnChange(nil)
	r.Add("info", "a", "three")
	if len(seen) != 2 {
		t.Fatalf("a removed hook still ran: %v", seen)
	}
}

func TestRingHookUnderConcurrency(t *testing.T) {
	r := New()
	var calls atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.Add("info", "", "event %d", j)
			}
		}()
		go func() {
			defer wg.Done()
			r.SetOnChange(func() { calls.Add(1) })
			r.Last()
		}()
	}
	wg.Wait()
	if r.Last() != 200 {
		t.Fatalf("Last = %d, want 200", r.Last())
	}
}
