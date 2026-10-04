package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

type ButtonKind int

const (
	Primary ButtonKind = iota
	Secondary
	Danger
)

// Button sends Pressed{ID} on enter, space or a click.
type Button struct {
	id       string
	Label    string
	Kind     ButtonKind
	Disabled bool
}

func NewButton(id, label string, kind ButtonKind) *Button {
	return &Button{id: id, Label: label, Kind: kind}
}

func (b *Button) ID() string                 { return b.id }
func (b *Button) Focusable() bool            { return !b.Disabled }
func (b *Button) Capturing() bool            { return false }
func (b *Button) Blur()                      {}
func (b *Button) SetDisabled(d bool)         { b.Disabled = d }
func (b *Button) Hit(msg tea.MouseMsg) bool  { return zone.Get(b.id).InBounds(msg) }
func (b *Button) TakesKey(k tea.KeyMsg) bool { return k.String() == "enter" || k.String() == " " }

func (b *Button) Update(msg tea.Msg) (Control, tea.Cmd) {
	if b.Disabled {
		return b, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if b.TakesKey(msg) {
			return b, send(Pressed{b.id})
		}
	case tea.MouseMsg:
		if clicked(msg) && b.Hit(msg) {
			return b, send(Pressed{b.id})
		}
	}
	return b, nil
}

// View renders [ Label ] (primary and danger) or ( Label ) (secondary).
func (b *Button) View(focused bool, _ int) string {
	text, style := "[ "+b.Label+" ]", Accent
	switch b.Kind {
	case Secondary:
		text, style = "( "+b.Label+" )", Bold
	case Danger:
		style = Red.Bold(true)
	}
	if b.Disabled {
		style = Dim
	}
	return glyph(focused) + zone.Mark(b.id, style.Render(text))
}
