package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Toggle flips On with enter, space or a click.
type Toggle struct {
	id       string
	On       bool
	Disabled bool
}

func NewToggle(id string, on bool) *Toggle { return &Toggle{id: id, On: on} }

func (t *Toggle) ID() string                 { return t.id }
func (t *Toggle) Focusable() bool            { return !t.Disabled }
func (t *Toggle) Capturing() bool            { return false }
func (t *Toggle) Blur()                      {}
func (t *Toggle) SetDisabled(d bool)         { t.Disabled = d }
func (t *Toggle) Hit(msg tea.MouseMsg) bool  { return zone.Get(t.id).InBounds(msg) }
func (t *Toggle) TakesKey(k tea.KeyMsg) bool { return k.String() == "enter" || k.String() == " " }

func (t *Toggle) Update(msg tea.Msg) (Control, tea.Cmd) {
	if t.Disabled {
		return t, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if t.TakesKey(msg) {
			t.On = !t.On
		}
	case tea.MouseMsg:
		if clicked(msg) && t.Hit(msg) {
			t.On = !t.On
		}
	}
	return t, nil
}

// View renders [●━] On or [━○] Off.
func (t *Toggle) View(focused bool, _ int) string {
	text, style := "[━○] Off", Dim
	if t.On {
		text, style = "[●━] On", Green
	}
	if t.Disabled {
		style = Dim
	}
	return glyph(focused) + zone.Mark(t.id, style.Render(text))
}
