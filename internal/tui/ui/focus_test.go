package ui

import (
	"strings"
	"testing"
)

func threeButtons() (*Button, *Button, *Button, *Group) {
	a, b, c := NewButton("g/a", "A", Primary), NewButton("g/b", "B", Primary), NewButton("g/c", "C", Primary)
	g := &Group{}
	g.Set([]Widget{a, b, c})
	return a, b, c, g
}

func TestGroupStartsOnFirstFocusableAndWraps(t *testing.T) {
	a, _, _, g := threeButtons()
	a.Disabled = true
	g.Set(g.Items())
	if g.FocusedID() != "g/b" {
		t.Fatalf("initial focus %q", g.FocusedID())
	}
	g.Next()
	g.Next() // skips the disabled a and wraps to b
	if g.FocusedID() != "g/b" {
		t.Fatalf("next wrap %q", g.FocusedID())
	}
	g.Prev()
	if g.FocusedID() != "g/c" {
		t.Fatalf("prev wrap %q", g.FocusedID())
	}
}

func TestGroupKeepsFocusByIDAndMovesOffDisabled(t *testing.T) {
	a, b, c, g := threeButtons()
	g.Focus("g/b")
	g.Set([]Widget{c, b, a}) // rebuilt in another order
	if g.FocusedID() != "g/b" {
		t.Fatalf("focus lost on rebuild: %q", g.FocusedID())
	}
	b.Disabled = true
	g.Set([]Widget{c, b, a})
	if g.FocusedID() != "g/a" {
		t.Fatalf("focus not moved to the next enabled widget: %q", g.FocusedID())
	}
	g.Set([]Widget{c, b})
	if g.FocusedID() != "g/c" {
		t.Fatalf("focus not moved when its widget vanished: %q", g.FocusedID())
	}
	g.Set(nil)
	if g.Focused() != nil {
		t.Fatal("an empty group has focus")
	}
}

func TestGroupBlursOnFocusChange(t *testing.T) {
	f := NewTextField("g/name", 10)
	b := NewButton("g/ok", "OK", Primary)
	g := &Group{}
	g.Set([]Widget{f, b})
	g.Key(key("enter"))
	if !f.Editing() {
		t.Fatal("enter did not reach the text field")
	}
	g.Next()
	if f.Editing() {
		t.Fatal("moving focus did not stop editing")
	}
}

func TestGroupKeyRouting(t *testing.T) {
	_, _, _, g := threeButtons()
	if ok, cmd := g.Key(key("enter")); !ok || msgOf(cmd) != (Pressed{"g/a"}) {
		t.Fatalf("enter: handled %v", ok)
	}
	if ok, _ := g.Key(key("q")); ok {
		t.Fatal("a button took q")
	}
}

func TestGroupMouseFocusesAndCaptures(t *testing.T) {
	s := modeSelect()
	b := NewButton("g/ok", "OK", Primary)
	g := &Group{}
	g.Set([]Widget{s, b})
	view := strings.Join([]string{s.View(false, 40), b.View(false, 40)}, "\n")
	ok, cmd := g.Mouse(clickAt(t, view, "g/ok"))
	if !ok || g.FocusedID() != "g/ok" || msgOf(cmd) != (Pressed{"g/ok"}) {
		t.Fatalf("click on the button: handled %v focus %q", ok, g.FocusedID())
	}
	g.Mouse(clickAt(t, view, "t/mode"))
	if !s.Open() || g.FocusedID() != "t/mode" {
		t.Fatal("click on the select did not focus and open it")
	}
	view = strings.Join([]string{s.View(false, 40), b.View(false, 40)}, "\n")
	ok, cmd = g.Mouse(clickAt(t, view, "g/ok"))
	if !ok || cmd != nil || s.Open() || g.FocusedID() != "t/mode" {
		t.Fatalf("an open dropdown did not swallow the click: handled %v open %v focus %q", ok, s.Open(), g.FocusedID())
	}
	if ok, _ := g.Mouse(outside); ok {
		t.Fatal("a click on nothing was handled")
	}
}
