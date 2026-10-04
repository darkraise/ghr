package ui

import "strings"

// LabelWidth is the width of a form row's label column.
const LabelWidth = 18

// Row is one setting on a form card: a label, its widgets, a description.
type Row struct {
	Label string
	Items []Widget // left to right; only the first may span several lines
	Text  string   // shown read-only when Items is empty
	Desc  string   // after the control when wide, else on its own line below
	Err   string   // shown in red under the row
	Dirty bool     // adds a ● after the controls
}

// Section is a card of rows. An untitled section renders without a frame.
type Section struct {
	Title string
	Note  string // dim line at the top of the card
	Rows  []Row
}

// Range is the lines [Start, End) a widget occupies in a rendered form.
type Range struct{ Start, End int }

// Render lays sections out as cards of total width w and returns the lines
// with the line range of every widget, so a page can scroll a widget into
// view without waiting for the next frame's zones.
func Render(secs []Section, focused string, w int, wide bool) ([]string, map[string]Range) {
	var out []string
	ranges := map[string]Range{}
	for _, s := range secs {
		inner, top := w, len(out)
		if s.Title != "" {
			inner, top = w-4, top+1 // inside the frame, below its top border
		}
		var body []string
		if s.Note != "" {
			body = append(body, Dim.Render(s.Note))
		}
		for _, r := range s.Rows {
			start := len(body)
			body = append(body, renderRow(r, focused, inner, wide)...)
			for _, it := range r.Items {
				ranges[it.ID()] = Range{top + start, top + len(body)}
			}
		}
		if s.Title == "" {
			for _, l := range body {
				out = append(out, Cell(l, w))
			}
			continue
		}
		out = append(out, strings.Split(Box(s.Title, w, body), "\n")...)
	}
	return out, ranges
}

func renderRow(r Row, focused string, w int, wide bool) []string {
	pad := strings.Repeat(" ", LabelWidth+1)
	line := Cell(r.Label, LabelWidth) + " "
	var rest []string
	if len(r.Items) == 0 {
		line += "  " + r.Text
	}
	for i, it := range r.Items {
		lines := strings.Split(it.View(it.ID() == focused, w-LabelWidth-1), "\n")
		if i > 0 {
			line += " "
		} else {
			rest = lines[1:]
		}
		line += lines[0]
	}
	if r.Dirty {
		line += " " + Amber.Render("●")
	}
	if r.Desc != "" && wide {
		line += "  " + Dim.Render(r.Desc)
	}
	out := []string{line}
	for _, l := range rest {
		out = append(out, pad+l)
	}
	if r.Desc != "" && !wide {
		out = append(out, pad+"  "+Dim.Render(r.Desc))
	}
	if r.Err != "" {
		out = append(out, pad+"  "+Red.Render("✖ "+r.Err))
	}
	return out
}

// ScrollTo returns the scroll offset that brings r into a view of h lines,
// moving as little as possible from off but keeping one line of context (a
// card border or the neighbouring row) beyond it. A range taller than the
// view is aligned to its top. Callers clamp the result to the content.
func ScrollTo(off, h int, r Range) int {
	switch {
	case r.Start < off || r.End-r.Start >= h:
		return max(r.Start-1, 0)
	case r.End > off+h:
		return r.End + 1 - h
	}
	return off
}
