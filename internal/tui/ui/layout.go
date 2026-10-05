package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// LabelWidth is the width of a form row's label column.
const LabelWidth = 18

// StackBelow is the card inside width under which a row's label takes its
// own line and the controls start on the next one, indented two columns,
// so a control is never cut by the label column.
const StackBelow = 44

// Row is one setting on a form card: a label, its widgets, a description.
type Row struct {
	Label string
	Items []Widget // left to right; only the first may span several lines
	Text  string   // shown read-only when Items is empty
	Lines []string // read-only lines under the label, when Items and Text are empty
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
			for _, l := range WrapWords(s.Note, max(inner, 1)) {
				body = append(body, Dim.Render(l))
			}
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
	var head []string
	pad := strings.Repeat(" ", LabelWidth+1)
	line := Cell(r.Label, LabelWidth) + " "
	if w < StackBelow {
		head, pad, line = []string{Cell(r.Label, w)}, "  ", "  "
	}
	ctlW := w - ansi.StringWidth(pad)
	wrap := func(s string) []string { return WrapWords(s, max(ctlW-2, 1)) }
	var rest []string
	if len(r.Items) == 0 {
		if len(r.Lines) == 0 {
			line += "  " + r.Text
		} else {
			var all []string
			for _, l := range r.Lines {
				all = append(all, wrap(l)...)
			}
			line += "  " + all[0]
			for _, l := range all[1:] {
				rest = append(rest, "  "+l)
			}
		}
	}
	for i, it := range r.Items {
		lines := strings.Split(it.View(it.ID() == focused, ctlW), "\n")
		if i > 0 {
			line += " "
		} else {
			rest = append(rest, lines[1:]...)
		}
		line += lines[0]
	}
	if r.Dirty {
		line += " " + Amber.Render("●")
	}
	if r.Desc != "" && wide {
		line += "  " + Dim.Render(r.Desc)
	}
	out := append(head, line)
	for _, l := range rest {
		out = append(out, pad+l)
	}
	if r.Desc != "" && !wide {
		for _, l := range wrap(r.Desc) {
			out = append(out, pad+"  "+Dim.Render(l))
		}
	}
	if r.Err != "" {
		out = append(out, pad+"  "+Red.Render("✖ "+r.Err))
	}
	return out
}

// WrapWords breaks s into lines at most w wide at spaces only. ansi.Wrap
// always breaks after a hyphen, which would split a label such as
// self-hosted across lines; a word wider than w is hard-broken instead. A
// colour still open at a break is closed and reopened on the next line.
func WrapWords(s string, w int) []string {
	var out []string
	cur, curW := "", 0
	for _, word := range strings.Fields(s) {
		ww := ansi.StringWidth(word)
		if curW > 0 && curW+1+ww <= w {
			cur, curW = cur+" "+word, curW+1+ww
			continue
		}
		if curW > 0 {
			out = append(out, cur)
		}
		if ww > w {
			parts := strings.Split(ansi.Hardwrap(word, w, false), "\n")
			out = append(out, parts[:len(parts)-1]...)
			word = parts[len(parts)-1]
			ww = ansi.StringWidth(word)
		}
		cur, curW = word, ww
	}
	return carrySGR(append(out, cur))
}

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// carrySGR ends each line whose last SGR sequence is not a reset with a
// reset, and starts the next line with that sequence.
func carrySGR(lines []string) []string {
	open := ""
	for i, l := range lines {
		l, open = open+l, ""
		if ms := sgrRe.FindAllString(l, -1); len(ms) > 0 {
			if last := ms[len(ms)-1]; last != "\x1b[m" && last != "\x1b[0m" {
				open, l = last, l+"\x1b[m"
			}
		}
		lines[i] = l
	}
	return lines
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
