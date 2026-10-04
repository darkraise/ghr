package ui

import "testing"

func TestToggleFlips(t *testing.T) {
	tg := NewToggle("t/public", false)
	if got := render(tg.View(false, 40)); got != "  [━○] Off" {
		t.Fatalf("view %q", got)
	}
	tg.Update(key(" "))
	if !tg.On || render(tg.View(true, 40)) != "› [●━] On" {
		t.Fatalf("space: on %v", tg.On)
	}
	tg.Update(clickAt(t, tg.View(false, 40), "t/public"))
	if tg.On {
		t.Fatal("click did not flip")
	}
	tg.Update(key("enter"))
	if !tg.On {
		t.Fatal("enter did not flip")
	}
}
