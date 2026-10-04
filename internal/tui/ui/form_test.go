package ui

import (
	"reflect"
	"testing"
)

func TestEqualByKind(t *testing.T) {
	cases := []struct {
		k    Kind
		a, b Value
		want bool
	}{
		{KindDuration, Value{Text: "120s"}, Value{Text: "2m0s"}, true},
		{KindDuration, Value{Text: "1d"}, Value{Text: "24h"}, true},
		{KindDuration, Value{Text: "soon"}, Value{Text: "2m"}, false},
		{KindDuration, Value{Text: "soon"}, Value{Text: "soon"}, true},
		{KindText, Value{Text: "6G"}, Value{Text: "6g"}, false},
		{KindInt, Value{Num: 1, Set: false}, Value{Num: 1, Set: true}, false},
		{KindInt, Value{Num: 3, Set: false}, Value{Num: 7, Set: false}, true},
		{KindInt, Value{Num: 2, Set: true}, Value{Num: 2, Set: true}, true},
		{KindList, Value{List: nil}, Value{List: []string{}}, true},
		{KindList, Value{List: []string{"a", "b"}}, Value{List: []string{"b", "a"}}, false},
	}
	for _, c := range cases {
		if got := Equal(c.k, c.a, c.b); got != c.want {
			t.Errorf("Equal(%v, %+v, %+v) = %v", c.k, c.a, c.b, got)
		}
	}
}

func testSpecs(poll string, labels []string, repos ...string) []Spec {
	out := []Spec{
		{Key: "poll", Kind: KindDuration, Base: Value{Text: poll}, New: func() Input { return NewTextField("poll", 8) }},
		{Key: "labels", Kind: KindList, Base: Value{List: labels}, New: func() Input { return NewTagList("labels") }},
	}
	for _, r := range repos {
		out = append(out, Spec{Key: r + "/max", Kind: KindInt, Base: Value{Num: 1, Set: true},
			New: func() Input { return NewStepper(r+"/max", 0, 99, 1) }})
	}
	return out
}

func fieldKeys(fs []*Field) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key)
	}
	return out
}

func TestFormDirtyAndDiscard(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", []string{"homelab"}, "darkmem"))
	if len(f.Dirty()) != 0 {
		t.Fatalf("fresh form dirty: %v", fieldKeys(f.Dirty()))
	}
	f.Field("poll").Input.SetValue(Value{Text: "10000ms"})
	if len(f.Dirty()) != 0 {
		t.Fatal("an equal duration counts as dirty")
	}
	f.Field("poll").Input.SetValue(Value{Text: "30s"})
	f.Field("darkmem/max").Input.SetValue(Value{Num: 2, Set: true})
	if got := fieldKeys(f.Dirty()); !reflect.DeepEqual(got, []string{"poll", "darkmem/max"}) {
		t.Fatalf("dirty %v", got)
	}
	f.Discard()
	if len(f.Dirty()) != 0 || f.Field("poll").Input.Value().Text != "10s" {
		t.Fatal("discard did not reset")
	}
}

func TestFormMergeKeepsEditsAndControls(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", []string{"homelab"}, "darkmem", "darkcloud"))
	poll := f.Field("poll").Input
	poll.SetValue(Value{Text: "30s"})
	f.Field("labels").Input.SetValue(Value{List: []string{"homelab", "gpu"}})

	// The daemon now has poll 20s and labels [homelab gpu]; darkcloud is gone and darkagents is new.
	f.Merge(testSpecs("20s", []string{"homelab", "gpu"}, "darkmem", "darkagents"))
	if f.Field("poll").Input != poll {
		t.Fatal("merge replaced an existing control")
	}
	if got := f.Field("poll"); got.Base.Text != "20s" || got.Input.Value().Text != "30s" || !got.Dirty() {
		t.Fatalf("dirty field lost its edit: base %+v edit %q", got.Base, got.Input.Value().Text)
	}
	if f.Field("labels").Dirty() {
		t.Fatal("a field whose edit now equals the new base is still dirty")
	}
	if got := fieldKeys(f.Fields()); !reflect.DeepEqual(got, []string{"poll", "labels", "darkmem/max", "darkagents/max"}) {
		t.Fatalf("fields %v", got)
	}
	if v := f.Field("darkagents/max").Input.Value(); !reflect.DeepEqual(v, Value{Num: 1, Set: true}) {
		t.Fatalf("new field not set to its base: %+v", v)
	}
}

func TestFormMergeRefreshesCleanFieldsAndLockedDropsEdits(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", nil, "darkmem"))
	f.Field("darkmem/max").Input.SetValue(Value{Num: 5, Set: true})
	next := testSpecs("15s", nil, "darkmem")
	next[2].Locked = true // darkmem is now being removed
	f.Merge(next)
	if v := f.Field("poll").Input.Value().Text; v != "15s" {
		t.Fatalf("clean field not refreshed: %q", v)
	}
	if f.Field("darkmem/max").Dirty() {
		t.Fatal("a locked field kept its edit")
	}
}

func TestFormReset(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", nil))
	f.Field("poll").Input.SetValue(Value{Text: "30s"})
	f.Field("labels").Input.SetValue(Value{List: []string{"x"}})
	f.Reset([]string{"poll", "missing"})
	if f.Field("poll").Dirty() || !f.Field("labels").Dirty() {
		t.Fatal("Reset touched the wrong fields")
	}
}
