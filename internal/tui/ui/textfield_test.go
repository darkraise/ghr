package ui

import (
	"fmt"
	"strings"
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
	g := &Group{}
	g.Set([]Widget{f, NewButton("t/next", "Next", Primary)})
	g.Key(key("enter")) // enter commits and moves focus on at once, with no message in between
	if f.Editing() || g.FocusedID() != "t/next" {
		t.Fatalf("enter: editing %v focus %q", f.Editing(), g.FocusedID())
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

func TestMaskedFieldNeverShowsItsValue(t *testing.T) {
	f := NewTextField("t/token", 20)
	f.Mask = true
	f.Check = func(s string) error {
		if len(s) < 10 {
			return fmt.Errorf("token %q is too short", s)
		}
		return nil
	}
	typeText(f, "ghp_secret")
	views := []string{render(f.View(true, 40))}
	f.Update(key("enter")) // commit: no longer editing
	views = append(views, render(f.View(true, 40)), render(f.View(false, 40)))
	f.SetDisabled(true)
	views = append(views, render(f.View(false, 40)))
	f.SetDisabled(false)
	f.SetValue(Value{Text: "short"})
	views = append(views, render(f.View(false, 40)))
	for i, v := range views {
		if strings.Contains(v, "secret") || strings.Contains(v, "short") || strings.Contains(v, "ghp") {
			t.Fatalf("view %d shows the value: %q", i, v)
		}
	}
	if !strings.Contains(views[2], "••••••••••") {
		t.Fatalf("masked view %q", views[2])
	}
	if f.Value().Text != "short" {
		t.Fatal("masking changes only the view")
	}
}

// A masked field's check error never carries the value in any form: raw,
// quoted, escaped or transformed.
func TestMaskedFieldErrorIsGeneric(t *testing.T) {
	f := NewTextField("t/token", 20)
	f.Mask = true
	f.Check = func(s string) error {
		return fmt.Errorf("bad %s %q %s %s", s, s, strings.ToUpper(s), strings.ReplaceAll(s, "_", `\_`))
	}
	f.SetValue(Value{Text: "ghp_a\"b"})
	err := f.Err()
	if err == nil || err != ErrMaskedInvalid {
		t.Fatalf("Err() = %v", err)
	}
	v := render(f.View(false, 40))
	for _, s := range []string{err.Error(), v} {
		low := strings.ToLower(s)
		if strings.Contains(low, "ghp") || strings.Contains(s, `a\"b`) || strings.Contains(s, `a"b`) {
			t.Fatalf("value leaked: %q", s)
		}
	}
	if !strings.Contains(v, ErrMaskedInvalid.Error()) {
		t.Fatalf("view %q lacks the generic error", v)
	}
	f.Check = func(string) error { return nil }
	if f.Err() != nil {
		t.Fatal("a passing check gives no error")
	}
}
