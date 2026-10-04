package ui

import (
	"testing"

	"github.com/darkraise/ghr/internal/config"
)

func durationField() *TextField {
	f := NewTextField("t/poll", 8)
	f.Check = func(s string) error { _, err := config.ParseDuration(s); return err }
	f.SetValue(Value{Text: "10s"})
	return f
}

func TestTextFieldEditing(t *testing.T) {
	f := durationField()
	if got := render(f.View(true, 40)); got != "› [ 10s      ]" {
		t.Fatalf("view %q", got)
	}
	if !f.TakesKey(key("q")) || !f.TakesKey(key("enter")) || f.TakesKey(key("down")) || f.TakesKey(key("tab")) {
		t.Fatal("TakesKey while not editing")
	}
	typeText(f, "30s") // a printable key starts editing and replaces the value
	if !f.Editing() || f.Value().Text != "30s" {
		t.Fatalf("editing %v value %q", f.Editing(), f.Value().Text)
	}
	if got := render(f.View(true, 40)); got != "› [ 30s      ]" {
		t.Fatalf("editing view %q", got)
	}
	if f.TakesKey(key("tab")) || f.TakesKey(key("ctrl+s")) || !f.TakesKey(key("q")) || !f.TakesKey(key("left")) {
		t.Fatal("TakesKey while editing")
	}
	_, cmd := f.Update(key("enter"))
	if f.Editing() || msgOf(cmd) != (Advance{"t/poll"}) {
		t.Fatalf("enter: editing %v msg %v", f.Editing(), msgOf(cmd))
	}
	f.Update(key("enter")) // enter starts editing and keeps the value
	f.Update(key("backspace"))
	f.Update(key("m"))
	f.Update(key("esc"))
	if f.Editing() || f.Value().Text != "30m" {
		t.Fatalf("esc keeps the typed value: editing %v value %q", f.Editing(), f.Value().Text)
	}
	f.Update(key("enter"))
	f.Blur()
	if f.Editing() {
		t.Fatal("blur did not stop editing")
	}
}

func TestTextFieldInlineError(t *testing.T) {
	f := durationField()
	typeText(f, "soon")
	want := "  [ soon     ]\n  ✖ invalid duration \"soon\""
	if got := render(f.View(false, 40)); got != want {
		t.Fatalf("view:\n%s\nwant:\n%s", got, want)
	}
	if f.Err() == nil {
		t.Fatal("Err is nil")
	}
}

func TestTextFieldClickAndDisable(t *testing.T) {
	f := durationField()
	f.Update(clickAt(t, f.View(false, 40), "t/poll"))
	if !f.Editing() {
		t.Fatal("click did not start editing")
	}
	f.SetDisabled(true)
	if f.Editing() || f.Focusable() {
		t.Fatal("disabling left it editing or focusable")
	}
	f.Update(key("x"))
	if f.Value().Text != "10s" {
		t.Fatal("a disabled field changed")
	}
}
