package daemon

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestManagedListAddsAndDropsInConfig(t *testing.T) {
	s := newStore(t)
	m := managedList{store: s}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("initial list %q", got)
	}
	for _, n := range []string{"ripgrep", "jq", "ripgrep"} {
		if err := m.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.List(); !slices.Equal(got, []string{"ripgrep", "jq"}) {
		t.Fatalf("after adds %q", got)
	}
	data, _ := os.ReadFile(s.ConfigPath)
	if !strings.Contains(string(data), "- ripgrep") || !strings.Contains(string(data), "- jq") {
		t.Fatalf("config.yaml without the packages:\n%s", data)
	}
	got := m.List()
	got[0] = "changed"
	if m.List()[0] != "ripgrep" {
		t.Fatal("List returned the config's own slice")
	}
	for _, n := range []string{"ripgrep", "yq"} {
		if err := m.Drop(n); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.List(); !slices.Equal(got, []string{"jq"}) {
		t.Fatalf("after drops %q", got)
	}
}

func TestManagedListSavesOnlyAChange(t *testing.T) {
	s := newStore(t)
	m := managedList{store: s}
	if err := m.Add("ripgrep"); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(s.ConfigPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("ripgrep"); err != nil {
		t.Fatal(err)
	}
	if err := m.Drop("yq"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(s.ConfigPath); err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("an unchanged list rewrote config.yaml: %v %v", fi.ModTime(), err)
	}
}

func TestManagedListReturnsASaveError(t *testing.T) {
	s := newStore(t)
	s.ConfigPath = t.TempDir() + "/missing/config.yaml"
	var se *SaveError
	if err := (managedList{store: s}).Add("ripgrep"); !errors.As(err, &se) {
		t.Fatalf("save failure: %v", err)
	}
}
