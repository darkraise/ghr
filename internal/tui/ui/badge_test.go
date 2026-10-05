package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func withProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestBadgeProfiles(t *testing.T) {
	withProfile(t, termenv.Ascii)
	if got := Badge("active", BadgeOK); got != "[ACTIVE]" {
		t.Fatalf("ascii badge %q", got)
	}
	withProfile(t, termenv.ANSI256)
	got := Badge("busy", BadgeBusy)
	if !strings.Contains(got, "\x1b[") || ansi.Strip(got) != " BUSY " || ansi.StringWidth(got) != len("[BUSY]") {
		t.Fatalf("colour badge %q", got)
	}
}

func TestSparkline(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	vals := []*float64{f(0), f(1), nil, f(4), f(8)}
	if got := Sparkline(vals, 8, 7); got != "  ▁▂ ▅█" {
		t.Fatalf("sparkline %q", got)
	}
	if got := Sparkline(vals, 8, 3); got != " ▅█" {
		t.Fatalf("cut to the newest %q", got)
	}
	if got := Sparkline(vals, 0, 2); got != "▁▁" {
		t.Fatalf("zero max %q", got)
	}
	if got := Gauge(5, 10, 4); got != "▕██░░▏" {
		t.Fatalf("gauge %q", got)
	}
}

func TestBadgeRowKeepsBadgeColoursWhenSelected(t *testing.T) {
	withProfile(t, termenv.ANSI256)
	badge := Badge("busy", BadgeBusy)
	got := BadgeRow(Sel, true, "▸ a3f9c1 ", badge, " CI / e2e", 30)
	if !strings.Contains(got, badge) || ansi.StringWidth(got) != 30 {
		t.Fatalf("selected row %q (width %d)", got, ansi.StringWidth(got))
	}
	if plain := BadgeRow(Sel, false, "  a3f9c1 ", badge, " CI", 30); !strings.Contains(plain, badge) || ansi.StringWidth(plain) != 30 {
		t.Fatalf("plain row %q", plain)
	}
	// The text before the badge gives way so the badge keeps its place and colours.
	for _, sel := range []bool{true, false} {
		over := BadgeRow(Sel, sel, strings.Repeat("x", 28), badge, "tail", 30)
		if ansi.StringWidth(over) != 30 || !strings.Contains(over, badge) || !strings.Contains(over, "…") {
			t.Fatalf("overflowing row (selected %v) %q, %d wide", sel, over, ansi.StringWidth(over))
		}
	}
	// Narrower than the badge itself: the badge is cut, still in colour.
	if tiny := BadgeRow(Sel, true, "ab", badge, "", 3); ansi.StringWidth(tiny) != 3 || !strings.Contains(tiny, "\x1b[") {
		t.Fatalf("tiny row %q", tiny)
	}
}
