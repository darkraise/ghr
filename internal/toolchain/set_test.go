package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSetTools(t *testing.T) {
	s := New(newFixture(t).Env)
	if got := s.Tools(); !reflect.DeepEqual(got, []string{"dotnet", "go", "java", "node", "python"}) {
		t.Fatalf("tools %v", got)
	}
	if _, err := s.Get("ruby"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ruby: %v", err)
	}
}

func TestPopularPreset(t *testing.T) {
	want := []Entry{{"node", "22"}, {"node", "24"}, {"dotnet", "8.0"}, {"dotnet", "10.0"}, {"python", "3.13"}, {"python", "3.14"}, {"go", "latest"}, {"java", "21"}, {"java", "25"}}
	if !reflect.DeepEqual(Popular, want) {
		t.Fatalf("popular %v", Popular)
	}
	s := New(newFixture(t).Env)
	for _, e := range Popular {
		if _, err := s.Get(e.Tool); err != nil {
			t.Errorf("%v: %v", e, err)
		}
	}
}

func TestSetAvailableIsCachedForAnHour(t *testing.T) {
	f := newFixture(t)
	s := New(f.Env)
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if _, err := s.Available(context.Background(), "node"); err == nil {
		t.Fatal("a missing manifest answered")
	}
	f.serve("/node.json", nodeManifest(f.url))
	for range 2 {
		if cs, err := s.Available(context.Background(), "node"); err != nil || len(cs) != 2 {
			t.Fatalf("available %v, %v", cs, err)
		}
	}
	if got := f.hitCount("/node.json"); got != 2 {
		t.Fatalf("%d manifest reads; the error must not be cached and the success must be", got)
	}
	now = now.Add(61 * time.Minute)
	if _, err := s.Available(context.Background(), "node"); err != nil {
		t.Fatal(err)
	}
	if got := f.hitCount("/node.json"); got != 3 {
		t.Fatalf("%d manifest reads after an hour", got)
	}
	if _, err := s.Available(context.Background(), "ruby"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ruby: %v", err)
	}
}

func TestSetInstalledAndOther(t *testing.T) {
	f := newFixture(t)
	s := New(f.Env)
	mkInstall(t, f.Root, "node", "22.11.0", true)
	mkInstall(t, f.Root, "Java_Temurin-Hotspot_jdk", "21.0.8-9", true)
	mkInstall(t, f.Root, "PyPy", "7.3.17", true)
	mkInstall(t, f.Root, "Ruby", "3.3.5", true)
	if err := os.MkdirAll(filepath.Join(f.Root, ".tmp", "op1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.Root, "stray"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Tool != "java" || got[0].Version != "21.0.8+9" || got[1].Tool != "node" {
		t.Fatalf("installed %+v", got)
	}
	other, err := s.Other()
	if err != nil || !reflect.DeepEqual(other, []string{"PyPy", "Ruby"}) {
		t.Fatalf("other %v, %v", other, err)
	}
	if s.Root() != f.Root {
		t.Fatalf("root %q", s.Root())
	}
	if err := s.CleanTmp(); err != nil || exists(filepath.Join(f.Root, ".tmp")) {
		t.Fatalf("clean tmp: %v", err)
	}
}

func TestSetOtherWithoutToolCache(t *testing.T) {
	e := &Env{Root: filepath.Join(t.TempDir(), "missing")}
	if got, err := New(e).Other(); err != nil || got != nil {
		t.Fatalf("other %v, %v", got, err)
	}
}
