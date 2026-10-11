package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

// An OptInt field tells three cases apart: the key is absent, it is null, or
// it is a number. Each survives a round trip.
func TestOptIntJSON(t *testing.T) {
	type patch struct {
		Max  OptInt `json:"max,omitzero"`
		Warm OptInt `json:"warm,omitzero"`
	}
	for _, tc := range []struct {
		name  string
		patch patch
		body  string
	}{
		{"absent", patch{}, `{}`},
		{"number", patch{Max: Int(3)}, `{"max":3}`},
		{"zero", patch{Warm: Int(0)}, `{"warm":0}`},
		{"null", patch{Max: ClearInt()}, `{"max":null}`},
		{"both", patch{Max: ClearInt(), Warm: Int(2)}, `{"max":null,"warm":2}`},
	} {
		got, err := json.Marshal(tc.patch)
		if err != nil || string(got) != tc.body {
			t.Errorf("%s: marshalled %s err %v, want %s", tc.name, got, err, tc.body)
		}
		var back patch
		if err := json.Unmarshal([]byte(tc.body), &back); err != nil || !reflect.DeepEqual(back, tc.patch) {
			t.Errorf("%s: decoded %+v err %v, want %+v", tc.name, back, err, tc.patch)
		}
	}
	if !(OptInt{}).IsZero() || Int(0).IsZero() || ClearInt().IsZero() {
		t.Error("only an unset value is zero")
	}
	for _, body := range []string{`{"max":"3"}`, `{"max":1.5}`, `{"warm":true}`} {
		var p patch
		if err := json.Unmarshal([]byte(body), &p); err == nil {
			t.Errorf("%s decoded as %+v", body, p)
		}
	}
}
