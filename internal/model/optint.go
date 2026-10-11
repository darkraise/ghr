package model

import (
	"bytes"
	"encoding/json"
)

// OptInt is an optional number in a PATCH body. It tells three cases apart:
// the key was absent (Set is false), it was null (Set with a nil Value: clear
// the value), or it was a number. Fields of this type carry the omitzero tag,
// so an unset one is left out of the body.
type OptInt struct {
	Set   bool
	Value *int
}

// Int is an OptInt holding n.
func Int(n int) OptInt { return OptInt{Set: true, Value: &n} }

// ClearInt is the OptInt that clears a value; it is written as null.
func ClearInt() OptInt { return OptInt{Set: true} }

func (o OptInt) IsZero() bool { return !o.Set }

func (o OptInt) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Value)
}

// UnmarshalJSON runs only for a key that is present; the decoder calls it for
// null too.
func (o *OptInt) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*o = ClearInt()
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*o = Int(n)
	return nil
}
