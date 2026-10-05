package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

func TestRowLines(t *testing.T) {
	lines, _ := Render([]Section{{Rows: []Row{{Label: "Runners get", Lines: []string{"self-hosted linux x64", "homelab (global)"}}}}}, "", 60, true)
	if len(lines) != 2 || !strings.Contains(lines[0], "Runners get") || !strings.Contains(lines[0], "self-hosted linux x64") ||
		!strings.HasPrefix(lines[1], strings.Repeat(" ", LabelWidth+1)) || !strings.Contains(lines[1], "homelab (global)") {
		t.Fatalf("lines %q", lines)
	}
}

// Long Lines and a narrow-form description wrap to the space beside the
// label instead of being cut at the card edge; every word stays visible.
func TestRowLinesAndDescWrap(t *testing.T) {
	long := "self-hosted linux x64 darkcloud-linux gpu cuda-12 homelab (global) arm64-builder"
	desc := "container name prefixes removed after each job, matched by their start"
	lines, _ := Render([]Section{{Title: "Labels", Rows: []Row{
		{Label: "Runners get", Lines: []string{long, "runners already running keep their labels"}},
		{Label: "Prefixes", Items: []Widget{NewTextField("p", 10)}, Desc: desc},
	}}}, "", 52, false)
	text := strings.Join(lines, "\n")
	for _, word := range append(strings.Fields(long), strings.Fields(desc)...) {
		if !strings.Contains(text, word) {
			t.Errorf("word %q lost:\n%s", word, text)
		}
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > 52 {
			t.Fatalf("line %d is %d wide: %q", i, ansi.StringWidth(l), l)
		}
	}
	if !strings.Contains(text, "keep their labels") {
		t.Fatalf("final note cut:\n%s", text)
	}
}

// Under StackBelow columns inside a card, the label takes its own line and
// the control gets the full width, so it is never cut.
func TestRowStacksWhenNarrow(t *testing.T) {
	s := NewStepper("m", 0, 99, 1)
	s.ZeroText = "∞"
	lines, _ := Render([]Section{{Title: "Capacity", Rows: []Row{{Label: "Max", Items: []Widget{s}, Desc: "once set, it stays explicit"}}}}, "", 40, false)
	if len(lines) < 4 || !strings.Contains(lines[1], "Max") || strings.Contains(lines[1], "[") {
		t.Fatalf("label not on its own line: %q", lines)
	}
	want := ansi.Strip(render(s.View(false, 34)))
	if !strings.Contains(ansi.Strip(render(lines[2])), strings.TrimSpace(want)) {
		t.Fatalf("control cut: %q, want %q", lines[2], want)
	}
	wide, _ := Render([]Section{{Title: "Capacity", Rows: []Row{{Label: "Max", Items: []Widget{s}}}}}, "", 60, false)
	if !strings.Contains(wide[1], "Max") || !strings.Contains(wide[1], "[") {
		t.Fatalf("a card 56 wide inside keeps the label column: %q", wide[1])
	}
}

// A colour opened before a break is closed at the line's end and reopened on
// the next line, so it never runs into what follows the line.
func TestWrapWordsCarriesColourAcrossBreaks(t *testing.T) {
	dim := "\x1b[2m"
	got := WrapWords("plain "+dim+"one two three\x1b[0m end", 12)
	want := []string{"plain " + dim + "one\x1b[m", dim + "two three\x1b[0m", "end"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}
