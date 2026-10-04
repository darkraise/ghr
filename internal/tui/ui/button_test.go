package ui

import "testing"

func TestButtonPressAndDisabled(t *testing.T) {
	b := NewButton("t/save", "Save changes", Primary)
	if got := render(b.View(true, 40)); got != "› [ Save changes ]" {
		t.Fatalf("view %q", got)
	}
	if got := render(NewButton("t/x", "Discard", Secondary).View(false, 40)); got != "  ( Discard )" {
		t.Fatalf("secondary view %q", got)
	}
	for _, k := range []string{"enter", " "} {
		if _, cmd := b.Update(key(k)); msgOf(cmd) != (Pressed{"t/save"}) {
			t.Fatalf("%q did not press", k)
		}
	}
	if _, cmd := b.Update(clickAt(t, b.View(false, 40), "t/save")); msgOf(cmd) != (Pressed{"t/save"}) {
		t.Fatal("click did not press")
	}
	if _, cmd := b.Update(outside); cmd != nil {
		t.Fatal("a click elsewhere pressed")
	}
	b.SetDisabled(true)
	if _, cmd := b.Update(key("enter")); cmd != nil || b.Focusable() {
		t.Fatal("a disabled button pressed or stayed focusable")
	}
}
