package ui

import (
	"reflect"
	"strings"
	"testing"
)

func layoutSections() ([]Section, *Select, *Stepper, *Button, *Button) {
	mode := modeSelect()
	max := NewStepper("t/max", 1, 99, 1)
	max.SetValue(Value{Num: 2, Set: true})
	pause, remove := NewButton("t/pause", "Pause", Primary), NewButton("t/remove", "Remove", Danger)
	return []Section{
		{Title: "General", Rows: []Row{
			{Label: "Mode", Items: []Widget{mode}, Desc: "jobs first"},
			{Label: "Global max", Items: []Widget{max}, Desc: "queue mode", Dirty: true},
			{Label: "Owner", Text: "darkraise"},
		}},
		{Title: "darkmem", Note: "removing…", Rows: []Row{
			{Items: []Widget{pause, remove}, Err: "warm must be <= max"},
		}},
	}, mode, max, pause, remove
}

func TestRenderWide(t *testing.T) {
	secs, _, _, _, _ := layoutSections()
	lines, ranges := Render(secs, "t/max", 64, true)
	want := []string{
		"╭─ General ────────────────────────────────────────────────────╮",
		"│ Mode                 [ queue ▾ ]  jobs first                 │",
		"│ Global max         › [ − ] 2 [ + ] ●  queue mode             │",
		"│ Owner                darkraise                               │",
		"╰──────────────────────────────────────────────────────────────╯",
		"╭─ darkmem ────────────────────────────────────────────────────╮",
		"│ removing…                                                    │",
		"│                      [ Pause ]   [ Remove ]                  │",
		"│                      ✖ warm must be <= max                   │",
		"╰──────────────────────────────────────────────────────────────╯",
	}
	if got := strings.Split(render(strings.Join(lines, "\n")), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	wantRanges := map[string]Range{"t/mode": {1, 2}, "t/max": {2, 3}, "t/pause": {7, 9}, "t/remove": {7, 9}}
	if !reflect.DeepEqual(ranges, wantRanges) {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestRenderNarrowPutsDescriptionsBelowAndCountsOpenDropdown(t *testing.T) {
	secs, mode, _, _, _ := layoutSections()
	mode.Update(key("enter"))
	lines, ranges := Render(secs, "", 50, false)
	got := render(strings.Join(lines[:7], "\n"))
	want := strings.Join([]string{
		"╭─ General ──────────────────────────────────────╮",
		"│ Mode                 [ queue ▾ ]               │",
		"│                        ▸ queue  jobs first     │",
		"│                          all  warm runners     │",
		"│                      jobs first                │",
		"│ Global max           [ − ] 2 [ + ] ●           │",
		"│                      queue mode                │",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if ranges["t/mode"] != (Range{1, 5}) || ranges["t/max"] != (Range{5, 7}) {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestRenderUntitledSectionHasNoFrame(t *testing.T) {
	add := NewButton("t/add", "+ Add repository", Primary)
	lines, ranges := Render([]Section{{Rows: []Row{{Items: []Widget{add}}}}}, "", 44, true)
	if got := render(strings.Join(lines, "\n")); got != "                     [ + Add repository ]   " {
		t.Fatalf("got %q", got)
	}
	if ranges["t/add"] != (Range{0, 1}) {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestScrollTo(t *testing.T) {
	cases := []struct {
		off, h int
		r      Range
		want   int
	}{
		{0, 10, Range{3, 4}, 0},   // already visible
		{0, 10, Range{12, 14}, 5}, // below: bottom-align, one line of context
		{8, 10, Range{2, 3}, 1},   // above: top-align, one line of context
		{4, 10, Range{0, 1}, 0},   // above at the very top: no line before it
		{0, 3, Range{5, 10}, 4},   // taller than the view: top-align
	}
	for _, c := range cases {
		if got := ScrollTo(c.off, c.h, c.r); got != c.want {
			t.Errorf("ScrollTo(%d, %d, %v) = %d, want %d", c.off, c.h, c.r, got, c.want)
		}
	}
}
