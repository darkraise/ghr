package endpoint

import (
	"encoding/json"
	"os"
	"testing"
)

type vector struct {
	Endpoint string `json:"endpoint"`
	Error    string `json:"error"`
}

func TestCheck(t *testing.T) {
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	if err := json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors")
	}
	known := map[string]bool{"": true, MsgForm: true, MsgExtra: true, MsgZone: true, MsgHTTPName: true, MsgHTTPAddr: true}
	for _, v := range vectors {
		if !known[v.Error] {
			t.Errorf("%q: vector error %q is not one of the Msg texts", v.Endpoint, v.Error)
			continue
		}
		got := ""
		if err := Check(v.Endpoint); err != nil {
			got = err.Error()
		}
		if got != v.Error {
			t.Errorf("Check(%q) = %q, want %q", v.Endpoint, got, v.Error)
		}
	}
}
