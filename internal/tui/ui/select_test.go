package ui

import (
	"strings"
	"testing"
)

func modeSelect() *Select {
	s := NewSelect("t/mode", []Option{
		{Value: "queue", Label: "queue", Desc: "jobs first"},
		{Value: "all", Label: "all", Desc: "warm runners"},
	})
	s.SetValue(Value{Text: "queue"})
	return s
}

func TestSelectKeys(t *testing.T) {
	s := modeSelect()
	if got := render(s.View(true, 40)); got != "› [ queue ▾ ]" {
		t.Fatalf("closed view %q", got)
	}
	s.Update(key("right"))
	if s.Value().Text != "all" {
		t.Fatalf("right: %q", s.Value().Text)
	}
	s.Update(key("right")) // wraps
	if s.Value().Text != "queue" {
		t.Fatalf("right wrap: %q", s.Value().Text)
	}
	s.Update(key("left"))
	if s.Value().Text != "all" {
		t.Fatalf("left wrap: %q", s.Value().Text)
	}
	s.Update(key("enter"))
	if !s.Open() || !s.Capturing() {
		t.Fatal("enter did not open")
	}
	want := strings.Join([]string{"  [ all ▾ ]", "      queue  jobs first", "    ▸ all  warm runners"}, "\n")
	if got := render(s.View(false, 40)); got != want {
		t.Fatalf("open view:\n%s\nwant:\n%s", got, want)
	}
	if !s.TakesKey(key("q")) || !s.TakesKey(key("1")) {
		t.Fatal("an open dropdown let a key through")
	}
	for _, k := range []string{"q", "j", "k", " "} { // held and ignored while open
		s.Update(key(k))
	}
	if !s.Open() || s.Value().Text != "all" || !strings.Contains(render(s.View(false, 40)), "▸ all") {
		t.Fatalf("an ignored key changed the open dropdown: open %v value %q", s.Open(), s.Value().Text)
	}
	s.Update(key("up"))
	s.Update(key("enter"))
	if s.Open() || s.Value().Text != "queue" {
		t.Fatalf("pick: open %v value %q", s.Open(), s.Value().Text)
	}
	s.Update(key(" "))
	s.Update(key("down"))
	s.Update(key("esc"))
	if s.Open() || s.Value().Text != "queue" {
		t.Fatalf("esc: open %v value %q", s.Open(), s.Value().Text)
	}
	if s.TakesKey(key("q")) || s.TakesKey(key("down")) {
		t.Fatal("a closed select took a page key")
	}
}

func TestSelectMouse(t *testing.T) {
	s := modeSelect()
	s.Update(clickAt(t, s.View(false, 40), "t/mode"))
	if !s.Open() {
		t.Fatal("click did not open")
	}
	s.Update(clickAt(t, s.View(false, 40), "t/mode/opt-1"))
	if s.Open() || s.Value().Text != "all" {
		t.Fatalf("option click: open %v value %q", s.Open(), s.Value().Text)
	}
	s.Update(clickAt(t, s.View(false, 40), "t/mode"))
	if !s.Hit(clickAt(t, s.View(false, 40), "t/mode/opt-0")) {
		t.Fatal("an open option is not part of the select")
	}
	if s.Hit(outside) {
		t.Fatal("a click elsewhere hit the select")
	}
	s.Update(outside)
	if s.Open() || s.Value().Text != "all" {
		t.Fatalf("outside click: open %v value %q", s.Open(), s.Value().Text)
	}
}

func TestSelectBlurAndDisable(t *testing.T) {
	s := modeSelect()
	s.Update(key("enter"))
	s.Blur()
	if s.Open() {
		t.Fatal("blur left the dropdown open")
	}
	s.Update(key("enter"))
	s.SetDisabled(true)
	if s.Open() || s.Focusable() {
		t.Fatal("disabling left it open or focusable")
	}
	s.Update(key("right"))
	if s.Value().Text != "queue" {
		t.Fatal("a disabled select changed")
	}
}
