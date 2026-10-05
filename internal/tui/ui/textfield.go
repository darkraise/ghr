package ui

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// TextField is a one-line input. Editing starts on enter (keeping the value),
// on a printable key (replacing it) or on a click; enter commits and asks to
// advance focus, esc stops editing and keeps what was typed.
type TextField struct {
	id       string
	Width    int                // columns inside the brackets
	Check    func(string) error // inline error shown under the field while it fails
	Disabled bool
	Mask     bool // shows • for every character in every state; Err returns ErrMaskedInvalid
	in       textinput.Model
	editing  bool
	advance  bool // enter committed; Group.Key moves focus on
}

func NewTextField(id string, width int) *TextField {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 200
	in.Cursor.SetMode(cursor.CursorStatic)
	return &TextField{id: id, Width: width, in: in}
}

func (f *TextField) ID() string                { return f.id }
func (f *TextField) Focusable() bool           { return !f.Disabled }
func (f *TextField) Capturing() bool           { return false }
func (f *TextField) Editing() bool             { return f.editing }
func (f *TextField) Hit(msg tea.MouseMsg) bool { return zone.Get(f.id).InBounds(msg) }
func (f *TextField) Value() Value              { return Value{Text: strings.TrimSpace(f.in.Value())} }

func (f *TextField) SetValue(v Value) {
	f.in.SetValue(v.Text)
	f.in.CursorEnd()
}

func (f *TextField) SetDisabled(d bool) {
	f.Disabled = d
	if d {
		f.Blur()
	}
}

func (f *TextField) Blur() {
	f.editing = false
	f.in.Blur()
}

// ErrMaskedInvalid is a masked field's check error. Check's own error may
// quote, escape or transform the value, so none of its text is shown.
var ErrMaskedInvalid = errors.New("not a valid value")

// Err is the inline error for the current value, or nil.
func (f *TextField) Err() error {
	if f.Check == nil {
		return nil
	}
	err := f.Check(f.Value().Text)
	if err != nil && f.Mask {
		return ErrMaskedInvalid
	}
	return err
}

func (f *TextField) TakesKey(k tea.KeyMsg) bool {
	if f.editing {
		return editorKey(k)
	}
	return printable(k) || k.String() == "enter"
}

func (f *TextField) startEditing() {
	f.editing = true
	f.in.Focus()
	f.in.CursorEnd()
}

func (f *TextField) takeAdvance() bool {
	a := f.advance
	f.advance = false
	return a
}

func (f *TextField) Update(msg tea.Msg) (Control, tea.Cmd) {
	f.advance = false
	if f.Disabled {
		return f, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !f.editing {
			switch {
			case msg.String() == "enter":
				f.startEditing()
			case printable(msg):
				f.startEditing()
				f.in.SetValue("")
				f.in, _ = f.in.Update(msg)
			}
			return f, nil
		}
		switch msg.String() {
		case "enter":
			f.Blur()
			f.advance = true
			return f, nil
		case "esc":
			f.Blur()
			return f, nil
		}
		var cmd tea.Cmd
		f.in, cmd = f.in.Update(msg)
		return f, cmd
	case tea.MouseMsg:
		if clicked(msg) && f.Hit(msg) && !f.editing {
			f.startEditing()
		}
	}
	return f, nil
}

// View renders [ 5m         ] and, while the value fails Check, a line with the error.
func (f *TextField) View(focused bool, _ int) string {
	var text string
	if f.Mask {
		f.in.EchoMode, f.in.EchoCharacter = textinput.EchoPassword, '•'
	}
	switch {
	case f.editing:
		f.in.Width = f.Width - 1
		text = f.in.View()
	case f.Mask:
		text = strings.Repeat("•", utf8.RuneCountInString(f.in.Value()))
	default:
		text = f.in.Value()
	}
	style := Bold
	if f.Disabled {
		style = Dim
	}
	out := glyph(focused) + zone.Mark(f.id, style.Render("[ ")+Cell(text, f.Width)+style.Render(" ]"))
	if err := f.Err(); err != nil {
		out += "\n  " + Red.Render("✖ "+err.Error())
	}
	return out
}
