package events

import "testing"

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
