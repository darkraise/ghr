package ui

import "github.com/darkraise/ghr/internal/config"

// Kind says how a field's values compare.
type Kind int

const (
	KindText     Kind = iota // Text, compared exactly
	KindDuration             // Text, compared by parsed value: 120s equals 2m0s
	KindInt                  // Num and Set
	KindList                 // List, element by element; nil equals empty
)

// Equal compares two values of kind k.
func Equal(k Kind, a, b Value) bool {
	switch k {
	case KindDuration:
		da, errA := config.ParseDuration(a.Text)
		db, errB := config.ParseDuration(b.Text)
		if errA == nil && errB == nil {
			return da == db
		}
		return a.Text == b.Text
	case KindInt:
		return a.Set == b.Set && (!a.Set || a.Num == b.Num)
	case KindList:
		if len(a.List) != len(b.List) {
			return false
		}
		for i := range a.List {
			if a.List[i] != b.List[i] {
				return false
			}
		}
		return true
	}
	return a.Text == b.Text
}

// Field pairs a setting's base (its value as last loaded) with the Input
// holding its edit.
type Field struct {
	Key   string
	Kind  Kind
	Base  Value
	Input Input
}

func (f *Field) Dirty() bool { return !Equal(f.Kind, f.Base, f.Input.Value()) }

// Spec describes one field as the latest config has it.
type Spec struct {
	Key    string
	Kind   Kind
	Base   Value
	New    func() Input // builds the control the first time Key appears
	Locked bool         // drop any edit and show Base (a repo being removed)
}

// Form is the ordered set of fields of a settings page.
type Form struct {
	fields []*Field
}

func (f *Form) Fields() []*Field { return f.fields }

// Field returns the field with key, or nil.
func (f *Form) Field(key string) *Field {
	for _, fl := range f.fields {
		if fl.Key == key {
			return fl
		}
	}
	return nil
}

// Dirty returns the fields whose edit differs from their base, in order.
func (f *Form) Dirty() []*Field {
	var out []*Field
	for _, fl := range f.fields {
		if fl.Dirty() {
			out = append(out, fl)
		}
	}
	return out
}

// Discard resets every edit to its base.
func (f *Form) Discard() {
	for _, fl := range f.fields {
		fl.Input.SetValue(fl.Base)
	}
}

// Reset resets the edits of the listed keys to their bases.
func (f *Form) Reset(keys []string) {
	for _, k := range keys {
		if fl := f.Field(k); fl != nil {
			fl.Input.SetValue(fl.Base)
		}
	}
}

// Merge brings the form up to date with a freshly loaded config. Each field
// takes its new base; a dirty field keeps its edit unless its spec is Locked,
// and any other field shows the new base. Fields missing from specs are
// dropped and new ones are added. Existing controls are kept, so an open
// dropdown or a field being edited survives a refresh.
func (f *Form) Merge(specs []Spec) {
	out := make([]*Field, 0, len(specs))
	for _, s := range specs {
		fl := f.Field(s.Key)
		if fl == nil {
			fl = &Field{Key: s.Key, Kind: s.Kind, Base: s.Base, Input: s.New()}
			fl.Input.SetValue(s.Base)
			out = append(out, fl)
			continue
		}
		keep := fl.Dirty() && !s.Locked
		fl.Kind, fl.Base = s.Kind, s.Base
		// A control already showing its new base is left alone, so a refresh
		// does not close an empty tag input or reset a stepper's typed digits.
		if !keep && !Equal(s.Kind, s.Base, fl.Input.Value()) {
			fl.Input.SetValue(s.Base)
		}
		out = append(out, fl)
	}
	f.fields = out
}
