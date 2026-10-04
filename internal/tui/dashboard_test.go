package tui

import (
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/config"
)

func TestStatTiles(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	v := m.View()
	for _, want := range []string{"╭─ Running ", "2 / 3", "╭─ Queued jobs ", "╭─ Repositories ", "2 active · 1 paused", "╭─ Disk ", "61%"} {
		if !strings.Contains(v, want) {
			t.Errorf("tiles missing %q", want)
		}
	}
	if got := m.statTiles(100, true); !strings.Contains(got, "Queued jobs 3") {
		t.Fatalf("queued is the sum over repos: %q", got)
	}
	m.st.Mode = config.ModeAll
	m.st.Repos[0].Removing, m.st.Repos[0].Paused = true, true
	got := m.statTiles(100, true)
	for _, want := range []string{"Running 2 / ∞", "Repositories 1 active · 2 paused"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact tiles %q missing %q", got, want)
		}
	}
}

// Under 100 columns, or when rows run short, the tiles take one line.
func TestStatTilesCollapse(t *testing.T) {
	if v := sampleModel(&fakeClient{}, 80, 40).View(); strings.Contains(v, "╭─ Running ") || !strings.Contains(v, "Running 2 / 3") {
		t.Fatalf("tiles not compact under 100 columns:\n%s", v)
	}
	// The Activity card gives way first: at 23 rows the tiles are still boxed
	// and Activity is down to its 3 lines; one row fewer collapses the tiles.
	v := sampleModel(&fakeClient{}, 120, 23).View()
	lines := strings.Split(v, "\n")
	top := lineWith(lines, "╭─ Activity ")
	if !strings.Contains(v, "╭─ Running ") || top < 0 || !strings.Contains(lines[top+4], "╰") {
		t.Fatalf("at 23 rows the tiles should stay boxed with a 3-line Activity card:\n%s", v)
	}
	if v := sampleModel(&fakeClient{}, 120, 22).View(); strings.Contains(v, "╭─ Running ") || !strings.Contains(v, "Running 2 / 3") {
		t.Fatalf("tiles not compact on a short screen:\n%s", v)
	}
}
