package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

// TagList edits a list of short strings: ✕ removes a tag, "+ add" (or enter)
// opens an inline input where enter adds the draft and backspace on an empty
// input removes the last tag. A draft is never hidden: Value includes it, and
// esc, tab or any other focus change commits it. Enter on an empty input
// closes it and asks to advance focus.
type TagList struct {
	id       string
	Disabled bool
	tags     []string
	adding   bool
	advance  bool // enter on an empty input; Group.Key moves focus on
	in       textinput.Model
}

func NewTagList(id string) *TagList {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 100
	in.Width = 16
	in.Cursor.SetMode(cursor.CursorStatic)
	return &TagList{id: id, in: in}
}

func (l *TagList) ID() string      { return l.id }
func (l *TagList) Focusable() bool { return !l.Disabled }
func (l *TagList) Capturing() bool { return false }
func (l *TagList) Adding() bool    { return l.adding }

// Value is the tags plus the draft being typed, if it is a new tag.
func (l *TagList) Value() Value {
	out := append([]string{}, l.tags...)
	if d := l.draft(); d != "" && !l.has(d) {
		out = append(out, d)
	}
	return Value{List: out}
}

// SetValue replaces the tags and drops any draft.
func (l *TagList) SetValue(v Value) {
	l.tags = append([]string{}, v.List...)
	l.adding = false
	l.in.SetValue("")
	l.in.Blur()
}

func (l *TagList) SetDisabled(d bool) {
	l.Disabled = d
	if d {
		l.Blur()
	}
}

// Blur commits the draft and closes the input.
func (l *TagList) Blur() {
	l.commit()
	l.adding = false
	l.in.Blur()
}

func (l *TagList) Hit(msg tea.MouseMsg) bool {
	ids := []string{ZoneID(l.id, "add"), ZoneID(l.id, "input")}
	for i := range l.tags {
		ids = append(ids, l.xZone(i), l.tagZone(i))
	}
	for _, id := range ids {
		if zone.Get(id).InBounds(msg) {
			return true
		}
	}
	return false
}

func (l *TagList) TakesKey(k tea.KeyMsg) bool {
	if l.adding {
		return editorKey(k)
	}
	return k.String() == "enter" || k.String() == " "
}

func (l *TagList) draft() string { return strings.TrimSpace(l.in.Value()) }

func (l *TagList) has(tag string) bool {
	for _, t := range l.tags {
		if t == tag {
			return true
		}
	}
	return false
}

func (l *TagList) commit() {
	if d := l.draft(); d != "" && !l.has(d) {
		l.tags = append(l.tags, d)
	}
	l.in.SetValue("")
}

func (l *TagList) startAdding() {
	l.adding = true
	l.in.SetValue("")
	l.in.Focus()
}

func (l *TagList) xZone(i int) string   { return ZoneID(l.id, fmt.Sprintf("x-%d", i)) }
func (l *TagList) tagZone(i int) string { return ZoneID(l.id, fmt.Sprintf("tag-%d", i)) }

func (l *TagList) takeAdvance() bool {
	a := l.advance
	l.advance = false
	return a
}

func (l *TagList) Update(msg tea.Msg) (Control, tea.Cmd) {
	l.advance = false
	if l.Disabled {
		return l, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !l.adding {
			if l.TakesKey(msg) {
				l.startAdding()
			}
			return l, nil
		}
		switch msg.String() {
		case "enter":
			if l.draft() == "" {
				l.Blur()
				l.advance = true
				return l, nil
			}
			l.commit()
			return l, nil
		case "esc":
			l.Blur()
			return l, nil
		case "backspace":
			if l.in.Value() == "" {
				if n := len(l.tags); n > 0 {
					l.tags = l.tags[:n-1]
				}
				return l, nil
			}
		}
		var cmd tea.Cmd
		l.in, cmd = l.in.Update(msg)
		return l, cmd
	case tea.MouseMsg:
		if !clicked(msg) {
			return l, nil
		}
		for i := range l.tags {
			if zone.Get(l.xZone(i)).InBounds(msg) {
				l.tags = append(l.tags[:i:i], l.tags[i+1:]...)
				return l, nil
			}
		}
		if zone.Get(ZoneID(l.id, "add")).InBounds(msg) && !l.adding {
			l.startAdding()
		}
	}
	return l, nil
}

// View renders tag ✕  tag ✕  + add, wrapping onto further lines (indented
// past the focus marker) so every tag, ✕ and the input stay within width.
func (l *TagList) View(focused bool, width int) string {
	style := Bold
	if l.Disabled {
		style = Dim
	}
	var parts []string
	for i, t := range l.tags {
		p := zone.Mark(l.tagZone(i), style.Render(t))
		if !l.Disabled {
			p += " " + zone.Mark(l.xZone(i), Dim.Render("✕"))
		}
		parts = append(parts, p)
	}
	switch {
	case l.adding:
		parts = append(parts, zone.Mark(ZoneID(l.id, "input"), style.Render("[ ")+Cell(l.in.View(), 16)+style.Render(" ]")))
	case !l.Disabled:
		parts = append(parts, zone.Mark(ZoneID(l.id, "add"), Accent.Render("+ add")))
	case len(l.tags) == 0:
		parts = append(parts, Dim.Render("none"))
	}
	avail := max(width-2, 10)
	var lines []string
	line := ""
	for _, p := range parts {
		switch {
		case line == "":
			line = p
		case ansi.StringWidth(line)+2+ansi.StringWidth(p) <= avail:
			line += "  " + p
		default:
			lines = append(lines, line)
			line = p
		}
	}
	lines = append(lines, line)
	return glyph(focused) + strings.Join(lines, "\n  ")
}
