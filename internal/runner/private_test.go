package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePrivateDeletesBothRecords(t *testing.T) {
	h := newHarness(t)
	if h.m.Paths.Private == "" || filepath.Dir(h.m.Paths.Private) == h.m.Paths.Instances {
		t.Fatalf("private dir %q must be set and outside the instance dirs", h.m.Paths.Private)
	}
	if err := os.MkdirAll(h.m.Paths.Private, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{h.m.privateMeta("aaaaaa"), h.m.privateProjects("aaaaaa")} {
		if filepath.Dir(p) != h.m.Paths.Private {
			t.Fatalf("%s is not in the private dir", p)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.m.removePrivate("aaaaaa")
	if entries, _ := os.ReadDir(h.m.Paths.Private); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}
