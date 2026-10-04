package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

type Option struct {
	Value, Label, Desc string
}

// Select picks one of a fixed set of options. Its dropdown opens inline: the
// options render as extra lines below it and push the content below down.
type Select struct {
	id       string
	Options  []Option
	Disabled bool
	sel      int
	open     bool
	cursor   int
}

func NewSelect(id string, opts []Option) *Select { return &Select{id: id, Options: opts} }

func (s *Select) ID() string         { return s.id }
func (s *Select) Focusable() bool    { return !s.Disabled }
func (s *Select) Capturing() bool    { return s.open }
func (s *Select) Open() bool         { return s.open }
func (s *Select) Blur()              { s.open = false }
func (s *Select) SetDisabled(d bool) { s.Disabled = d; s.open = s.open && !d }
func (s *Select) Selected() Option   { return s.Options[s.sel] }
func (s *Select) Value() Value       { return Value{Text: s.Options[s.sel].Value} }

// SetValue selects the option whose Value is v.Text; an unknown value is ignored.
func (s *Select) SetValue(v Value) {
	for i, o := range s.Options {
		if o.Value == v.Text {
			s.sel = i
		}
	}
}

func (s *Select) optZone(i int) string { return ZoneID(s.id, fmt.Sprintf("opt-%d", i)) }

// TakesKey: an open dropdown takes every key; a closed one takes ←/→ to
// cycle and enter/space to open.
func (s *Select) TakesKey(k tea.KeyMsg) bool {
	if s.open {
		return true
	}
	switch k.String() {
	case "left", "right", "enter", " ":
		return true
	}
	return false
}

func (s *Select) Hit(msg tea.MouseMsg) bool {
	if zone.Get(s.id).InBounds(msg) {
		return true
	}
	for i := range s.Options {
		if s.open && zone.Get(s.optZone(i)).InBounds(msg) {
			return true
		}
	}
	return false
}

func (s *Select) Update(msg tea.Msg) (Control, tea.Cmd) {
	if s.Disabled {
		return s, nil
	}
	n := len(s.Options)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if s.open {
			// Every other key is held and ignored while the dropdown is open.
			switch k {
			case "up":
				s.cursor = max(s.cursor-1, 0)
			case "down":
				s.cursor = min(s.cursor+1, n-1)
			case "enter":
				s.sel, s.open = s.cursor, false
			case "esc":
				s.open = false
			}
			return s, nil
		}
		switch k {
		case "left":
			s.sel = (s.sel + n - 1) % n
		case "right":
			s.sel = (s.sel + 1) % n
		case "enter", " ":
			s.open, s.cursor = true, s.sel
		}
	case tea.MouseMsg:
		if !clicked(msg) {
			return s, nil
		}
		if s.open {
			// Any click closes the dropdown; one on an option also picks it.
			for i := range s.Options {
				if zone.Get(s.optZone(i)).InBounds(msg) {
					s.sel = i
				}
			}
			s.open = false
			return s, nil
		}
		if zone.Get(s.id).InBounds(msg) {
			s.open, s.cursor = true, s.sel
		}
	}
	return s, nil
}

// View renders [ queue ▾ ], followed by one line per option while open.
func (s *Select) View(focused bool, _ int) string {
	style := Bold
	if s.Disabled {
		style = Dim
	}
	lines := []string{glyph(focused) + zone.Mark(s.id, style.Render("[ "+s.Selected().Label+" ▾ ]"))}
	if s.open {
		for i, o := range s.Options {
			mark, label := "  ", o.Label
			if i == s.cursor {
				mark, label = Accent.Render("▸ "), Accent.Render(o.Label)
			}
			text := mark + label
			if o.Desc != "" {
				text += "  " + Dim.Render(o.Desc)
			}
			lines = append(lines, "    "+zone.Mark(s.optZone(i), text))
		}
	}
	return strings.Join(lines, "\n")
}
