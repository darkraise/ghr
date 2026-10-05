package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func repoPicker() *Picker {
	p := NewPicker("t/pick", 3)
	p.SetOptions([]PickOption{
		{Label: "booklore", Badge: "[PUBLIC]"},
		{Label: "darkcloud", Badge: "[PRIVATE]", Disabled: true, Note: "added"},
		{Label: "darkmem", Badge: "[PRIVATE]"},
		{Label: "immich", Badge: "[PRIVATE]"},
		{Label: "Kavita", Badge: "[PRIVATE]"},
	})
	return p
}

func picked(p *Picker) string {
	o, _ := p.Picked()
	return o.Label
}

func TestPickerView(t *testing.T) {
	p := repoPicker()
	want := strings.Join([]string{
		"› [ type to filter                       ]",
		"  ▸ booklore  [PUBLIC]                    ",
		"    darkcloud  [PRIVATE]  added           ",
		"    darkmem  [PRIVATE]                    ",
		"  2 more, ↑↓ to scroll                    ",
	}, "\n")
	if got := render(p.View(true, 42)); got != want {
		t.Fatalf("view:\n%s\nwant:\n%s", got, want)
	}
	narrow := render(p.View(false, 20))
	for i, l := range strings.Split(narrow, "\n") {
		if ansi.StringWidth(l) > 20 {
			t.Errorf("line %d is %d wide: %q", i, ansi.StringWidth(l), l)
		}
	}
	if !strings.Contains(narrow, "darkcloud  [PRI…") {
		t.Fatalf("a long row is not cut with an ellipsis:\n%s", narrow)
	}
	if got := render(NewPicker("t/empty", 3).View(false, 30)); !strings.Contains(got, "nothing to pick") {
		t.Fatalf("empty picker:\n%s", got)
	}
}

func TestPickerKeys(t *testing.T) {
	p := repoPicker()
	for _, k := range []string{"a", " ", "backspace", "up", "down", "enter"} {
		if !p.TakesKey(key(k)) {
			t.Errorf("%s not taken", k)
		}
	}
	for _, k := range []string{"tab", "shift+tab", "esc", "ctrl+s"} {
		if p.TakesKey(key(k)) {
			t.Errorf("%s taken; it belongs to the dialog", k)
		}
	}
	p.Update(key("down"))
	p.Update(key("enter"))
	if picked(p) != "" || p.takeAdvance() {
		t.Fatalf("a disabled option was picked: %q", picked(p))
	}
	p.Update(key("down"))
	p.Update(key("enter"))
	if picked(p) != "darkmem" || !p.takeAdvance() {
		t.Fatalf("enter picked %q", picked(p))
	}
	for range 5 {
		p.Update(key("down"))
	}
	v := render(p.View(true, 42))
	if !strings.Contains(v, "▸ Kavita") || strings.Contains(v, "booklore") || !strings.Contains(v, "2 more") {
		t.Fatalf("the window did not follow the highlight:\n%s", v)
	}
	p.Update(key("up"))
	p.Update(key("up"))
	p.Update(key("up"))
	if v := render(p.View(true, 42)); !strings.Contains(v, "▸ darkcloud") || strings.Contains(v, "booklore") {
		t.Fatalf("up scrolled wrong:\n%s", v)
	}
}

func TestPickerFilter(t *testing.T) {
	p := repoPicker()
	typeText(p, "DARK")
	v := render(p.View(true, 42))
	if p.Filter() != "DARK" || !strings.Contains(v, "[ DARK") || !strings.Contains(v, "darkcloud") || !strings.Contains(v, "darkmem") ||
		strings.Contains(v, "booklore") || strings.Contains(v, "more") {
		t.Fatalf("filter ignores case:\n%s", v)
	}
	p.Update(key("down"))
	p.Update(key("enter"))
	if picked(p) != "darkmem" {
		t.Fatalf("picked %q", picked(p))
	}
	typeText(p, "x")
	if v := render(p.View(true, 42)); !strings.Contains(v, "no match") {
		t.Fatalf("no match:\n%s", v)
	}
	p.Update(key("enter"))
	if picked(p) != "darkmem" {
		t.Fatal("enter on an empty list changed the pick")
	}
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	if p.Filter() != "DAR" {
		t.Fatalf("backspace: %q", p.Filter())
	}
}

func TestPickerMouse(t *testing.T) {
	p := repoPicker()
	view := p.View(false, 42)
	if !p.Hit(clickAt(t, view, "t/pick/row-2")) || p.Hit(outside) {
		t.Fatal("hit testing")
	}
	p.Update(clickAt(t, view, "t/pick/row-1"))
	if picked(p) != "" {
		t.Fatal("a click picked a disabled option")
	}
	p.Update(clickAt(t, view, "t/pick/row-2"))
	if picked(p) != "darkmem" {
		t.Fatalf("click picked %q", picked(p))
	}
	wheel := func(b tea.MouseButton) tea.MouseMsg {
		return tea.MouseMsg{X: 3, Y: 1, Action: tea.MouseActionPress, Button: b}
	}
	for range 4 {
		p.Update(wheel(tea.MouseButtonWheelDown))
	}
	v := render(p.View(false, 42))
	if !strings.Contains(v, "darkmem") || !strings.Contains(v, "Kavita") || strings.Contains(v, "darkcloud") {
		t.Fatalf("wheel down stops at the last full window:\n%s", v)
	}
	p.Update(wheel(tea.MouseButtonWheelUp))
	if v := render(p.View(false, 42)); !strings.Contains(v, "darkcloud") || strings.Contains(v, "Kavita") {
		t.Fatalf("wheel up:\n%s", v)
	}
}

func TestPickerKeepsPickAcrossOptions(t *testing.T) {
	p := repoPicker()
	p.Update(key("enter"))
	p.SetOptions([]PickOption{{Label: "zeta"}, {Label: "booklore"}})
	if picked(p) != "booklore" {
		t.Fatalf("pick lost: %q", picked(p))
	}
	p.SetOptions([]PickOption{{Label: "booklore", Disabled: true}})
	if picked(p) != "" {
		t.Fatal("a pick survived becoming disabled")
	}
	p.SetDisabled(true)
	p.Update(key("a"))
	if p.Focusable() || p.Filter() != "" {
		t.Fatal("a disabled picker took a key")
	}
}

// A window that shrinks or grows keeps the highlight in view, so enter picks
// the row the user sees.
func TestPickerResizeKeepsTheHighlightShown(t *testing.T) {
	p := NewPicker("t/resize", 8)
	var opts []PickOption
	for i := range 10 {
		opts = append(opts, PickOption{Label: fmt.Sprintf("r%02d", i)})
	}
	p.SetOptions(opts)
	for range 7 {
		p.Update(key("down"))
	}
	p.Rows = 3
	if v := render(p.View(true, 30)); !strings.Contains(v, "▸ r07") || strings.Contains(v, "r04") {
		t.Fatalf("shrunk:\n%s", v)
	}
	p.Update(key("enter"))
	if picked(p) != "r07" {
		t.Fatalf("enter picked %q", picked(p))
	}
	p.Rows = 8
	if v := render(p.View(true, 30)); !strings.Contains(v, "▸ r07") || !strings.Contains(v, "r02") || !strings.Contains(v, "r09") {
		t.Fatalf("grown:\n%s", v)
	}
}
