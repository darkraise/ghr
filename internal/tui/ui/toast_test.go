package ui

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func TestToastExpiry(t *testing.T) {
	var ts Toast
	ts.Show("Settings saved", false, t0)
	ts.Tick(t0.Add(3 * time.Second))
	if !ts.Active() {
		t.Fatal("success cleared before 4s")
	}
	ts.Tick(t0.Add(4 * time.Second))
	if ts.Active() {
		t.Fatal("success still shown at 4s")
	}
	ts.Show("boom", true, t0)
	ts.Tick(t0.Add(9 * time.Second))
	if !ts.Active() {
		t.Fatal("error cleared before 10s")
	}
	ts.Tick(t0.Add(10 * time.Second))
	if ts.Active() {
		t.Fatal("error still shown at 10s")
	}
}

func TestToastReplaceViewAndClose(t *testing.T) {
	var ts Toast
	if ts.View(30) != "" {
		t.Fatal("an inactive toast rendered")
	}
	ts.Show("first", false, t0)
	ts.Show("second failure", true, t0.Add(time.Second))
	if got := render(ts.View(30)); got != " ✖ second failure            ✕" {
		t.Fatalf("view %q", got)
	}
	ts.Tick(t0.Add(5 * time.Second)) // the replacement carries its own 10s
	if !ts.Active() {
		t.Fatal("the replacing error expired on the first toast's clock")
	}
	ts.Show("a very long message that will not fit", false, t0)
	if got := render(ts.View(20)); got != " ✔ a very long me… ✕" {
		t.Fatalf("truncated view %q", got)
	}
	click := clickAt(t, ts.View(20), ts.CloseZone())
	if click.X != 19 {
		t.Fatalf("✕ at column %d", click.X)
	}
	ts.Close()
	if ts.Active() {
		t.Fatal("close did not clear")
	}
}
