package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Control is the contract every form control meets.
type Control interface {
	Update(msg tea.Msg) (Control, tea.Cmd)
	View(focused bool, width int) string
	Focusable() bool // false when disabled
}

// Widget is a Control as a page holds it: addressable by ID, focusable and
// hit-testable. The controls in this package are pointers that update in
// place, so Update returns the receiver.
type Widget interface {
	Control
	ID() string
	// TakesKey reports whether the focused widget consumes k. Keys it does not
	// take fall through to the page keys and then the global keys.
	TakesKey(k tea.KeyMsg) bool
	// Capturing is true while the widget owns every click (an open dropdown).
	Capturing() bool
	// Hit reports whether a mouse event falls on one of the widget's zones.
	Hit(msg tea.MouseMsg) bool
	// Blur is called when focus leaves the widget: stop editing, close a dropdown.
	Blur()
	SetDisabled(disabled bool)
}

// Value is a form field's value. Which members are used depends on the
// field's Kind (see form.go).
type Value struct {
	Text string
	Num  int
	Set  bool // a number field: false means unset, so the default applies
	List []string
}

// Input is a Widget that holds a form value.
type Input interface {
	Widget
	Value() Value
	SetValue(v Value)
}

// Pressed is sent when a button is activated.
type Pressed struct{ ID string }

// Advance asks the page to move focus past the widget (enter in a text field).
type Advance struct{ ID string }

// ZoneID joins ID parts with "/", for example ZoneID("settings", "mode", "opt-1").
func ZoneID(parts ...string) string { return strings.Join(parts, "/") }

// glyph is the two-column focus marker every control view starts with.
func glyph(focused bool) string {
	if focused {
		return Accent.Render("›") + " "
	}
	return "  "
}

// clicked reports a left-button release, the event the TUI treats as a click.
func clicked(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft
}

// printable reports whether k is text a field should take.
func printable(k tea.KeyMsg) bool {
	return (k.Type == tea.KeyRunes && !k.Alt) || k.Type == tea.KeySpace
}

// editorKey reports whether a field that is being edited takes k: everything
// except the keys that move focus, scroll, save or quit.
func editorKey(k tea.KeyMsg) bool {
	switch k.String() {
	case "tab", "shift+tab", "up", "down", "pgup", "pgdown", "ctrl+s", "ctrl+c":
		return false
	}
	return true
}

func send(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }
