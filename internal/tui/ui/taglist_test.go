package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTagListAddAndRemove(t *testing.T) {
	l := NewTagList("t/labels")
	l.SetValue(Value{List: []string{"homelab"}})
	if got := render(l.View(true, 60)); got != "› homelab ✕  + add" {
		t.Fatalf("view %q", got)
	}
	if !l.TakesKey(key("enter")) || l.TakesKey(key("q")) {
		t.Fatal("TakesKey while not adding")
	}
	l.Update(key("enter"))
	if !l.Adding() || !l.TakesKey(key("q")) {
		t.Fatal("enter did not open the input")
	}
	typeText(l, "gpu")
	l.Update(key("enter"))
	typeText(l, "homelab") // duplicates are dropped
	l.Update(key("enter"))
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"homelab", "gpu"}) {
		t.Fatalf("tags %v", got)
	}
	l.Update(key("backspace")) // empty input: removes the last tag
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"homelab"}) {
		t.Fatalf("backspace: %v", got)
	}
	g := &Group{}
	g.Set([]Widget{l, NewButton("t/next", "Next", Primary)})
	g.Key(key("enter")) // enter on an empty input closes it and advances
	if l.Adding() || g.FocusedID() != "t/next" {
		t.Fatalf("empty enter: adding %v focus %q", l.Adding(), g.FocusedID())
	}
}

// A draft is part of the value while typed, and esc, tab or blur commit it.
func TestTagListDraftIsNeverHidden(t *testing.T) {
	l := NewTagList("t/labels")
	l.SetValue(Value{List: []string{"a"}})
	l.Update(key("enter"))
	typeText(l, "b")
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("draft not in the value: %v", got)
	}
	l.Update(key("esc"))
	if got := l.Value().List; l.Adding() || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("esc: adding %v tags %v", l.Adding(), got)
	}
	l.Update(key("enter"))
	typeText(l, "c")
	l.Blur() // tab or a click elsewhere
	if got := l.Value().List; l.Adding() || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("blur: adding %v tags %v", l.Adding(), got)
	}
	l.Update(key("enter"))
	typeText(l, "d")
	l.SetValue(Value{List: []string{"a"}}) // Discard
	if got := l.Value().List; l.Adding() || !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("SetValue kept the draft: adding %v tags %v", l.Adding(), got)
	}
}

func TestTagListMouse(t *testing.T) {
	l := NewTagList("t/labels")
	l.SetValue(Value{List: []string{"a", "b", "c"}})
	l.Update(clickAt(t, l.View(false, 60), "t/labels/x-1"))
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("✕ click: %v", got)
	}
	l.Update(clickAt(t, l.View(false, 60), "t/labels/add"))
	if !l.Adding() {
		t.Fatal("+ add click did not open the input")
	}
	if !l.Hit(clickAt(t, l.View(false, 60), "t/labels/tag-0")) || !l.Hit(clickAt(t, l.View(false, 60), "t/labels/input")) || l.Hit(outside) {
		t.Fatal("Hit")
	}
	l.Update(key("esc"))
	if l.Adding() {
		t.Fatal("esc left the input open")
	}
}

// Long lists wrap within the width, and every ✕ and "+ add" stays clickable.
func TestTagListWrapsToWidth(t *testing.T) {
	l := NewTagList("t/prefixes")
	var tags []string
	for i := 0; i < 6; i++ {
		tags = append(tags, fmt.Sprintf("cleanup-prefix-%d-", i))
	}
	l.SetValue(Value{List: tags})
	view := render(l.View(true, 40))
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("did not wrap:\n%s", view)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("line wider than 40: %q", line)
		}
	}
	l.Update(clickAt(t, l.View(true, 40), "t/prefixes/x-5"))
	l.Update(clickAt(t, l.View(true, 40), "t/prefixes/add"))
	if got := l.Value().List; len(got) != 5 || !l.Adding() {
		t.Fatalf("clicks on wrapped lines: tags %v adding %v", got, l.Adding())
	}
}

func TestTagListValueIsACopy(t *testing.T) {
	src := []string{"a"}
	l := NewTagList("t/labels")
	l.SetValue(Value{List: src})
	l.Value().List[0] = "z"
	src[0] = "y"
	if got := l.Value().List; got[0] != "a" {
		t.Fatalf("tags share memory with a caller: %v", got)
	}
	l.SetDisabled(true)
	if got := render(l.View(false, 60)); got != "  a" {
		t.Fatalf("disabled view %q", got)
	}
}
