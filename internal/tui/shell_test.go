package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The chips shed detail in a fixed order as the bar narrows.
func TestTopBarShedsDetailInOrder(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	full := m.topBar(200)
	for _, want := range []string{"runners ▕", "api ▕", "disk ▕", "● connected"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full bar missing %q: %q", want, full)
		}
	}
	steps := []struct{ gone, kept string }{
		{"connected", "api ▕"},  // 1: the connection chip shrinks to its dot
		{"api ▕", "disk ▕"},     // 2: the API gauge bar goes
		{"disk ▕", "runners ▕"}, // 3: the disk gauge bar goes
		{"runners ▕", "2/3"},    // 4: the runners gauge bar goes
	}
	for level, s := range steps {
		w := ansi.StringWidth(m.chips(level)) - 1 // one column too narrow for the previous level
		bar := m.topBar(w)
		if strings.Contains(bar, s.gone) || !strings.Contains(bar, s.kept) || ansi.StringWidth(bar) > w {
			t.Fatalf("level %d at %d columns: %q", level+1, w, bar)
		}
	}
	if bar := m.topBar(30); ansi.StringWidth(bar) > 30 {
		t.Fatalf("bar overflows 30 columns: %q", bar)
	}
}

func TestDiskStateUsesHighWater(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	for _, c := range []struct {
		pct, hw int
		want    string
	}{{61, 0, "ok"}, {80, 0, "warn"}, {61, 60, "warn"}, {59, 60, "ok"}, {95, 99, "critical"}} {
		m.st.DiskPct, m.cfg = c.pct, nil
		if c.hw > 0 {
			m.cfg = sampleConfig(t, "darkcloud")
			m.cfg.DiskHighWater = c.hw
		}
		if got := m.diskState(); got != c.want {
			t.Errorf("disk %d%% high-water %d: %s, want %s", c.pct, c.hw, got, c.want)
		}
	}
}

func TestSidebarAndTabRowNavigate(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if v := m.View(); !strings.Contains(v, "▌ 1 Dashboard") || !strings.Contains(v, "? Help") {
		t.Fatalf("wide layout has no sidebar:\n%s", v)
	}
	if m = click(t, m, "nav/history"); m.page != pageHistory {
		t.Fatalf("sidebar click: page %v", m.page)
	}
	if m = click(t, m, "nav/help"); m.overlay != ovHelp {
		t.Fatal("sidebar Help did not open help")
	}
	m = feed(m, key("esc"))

	n := sampleModel(&fakeClient{}, 80, 30)
	if v := n.View(); strings.Contains(v, "▌ 1 Dashboard") || !strings.Contains(v, "[ 1 Dashboard ]") {
		t.Fatalf("narrow layout should use the tab row:\n%s", v)
	}
	if n = click(t, n, "nav/settings"); n.page != pageSettings {
		t.Fatalf("tab row click: page %v", n.page)
	}
}

// Down to the 40-column minimum, every frame line fits and the tab row
// shrinks its labels instead of overflowing.
func TestNarrowFramesFit(t *testing.T) {
	for _, w := range []int{99, 72, 56, 40} {
		m := sampleModel(&fakeClient{}, w, 30)
		for i, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d, line %d is %d wide: %q", w, i, lipgloss.Width(line), line)
			}
		}
		if row := m.tabRow(w); !strings.Contains(row, "? help") || !strings.Contains(row, "4") {
			t.Fatalf("width %d tab row %q", w, row)
		}
		if m = click(t, m, "nav/settings"); m.page != pageSettings {
			t.Fatalf("width %d: tab row click went to page %v", w, m.page)
		}
		// Settings with an edit: the unsaved bar keeps both buttons whole, and so
		// does the unsaved-changes dialog.
		m = dirtySettings(t, &fakeClient{})
		m = feed(m, tea.WindowSizeMsg{Width: w, Height: 30})
		for _, tc := range []struct {
			name  string
			wants []string
		}{{"settings", []string{"( Discard )", "[ Save changes ]"}}, {"dialog", []string{"( Stay )", "( Discard )", "[ Save ]"}}} {
			name, wants := tc.name, tc.wants
			if name == "dialog" {
				m = feed(m, key("2"))
			}
			v := m.View()
			for i, line := range strings.Split(v, "\n") {
				if lipgloss.Width(line) > w {
					t.Fatalf("width %d %s: line %d is %d wide: %q", w, name, i, lipgloss.Width(line), line)
				}
			}
			for _, want := range wants {
				if !strings.Contains(v, want) {
					t.Errorf("width %d %s: missing %q:\n%s", w, name, want, v)
				}
			}
		}
	}
}

func TestAlertLineAndSettingsHeader(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), statusMsg{err: errors.New("connection refused")})
	m.page = pageSettings
	v := m.View()
	for _, want := range []string{"daemon unreachable: connection refused — retrying", "○ reconnecting", "reconnecting"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if lipgloss.Width(strings.Split(v, "\n")[1]) != 120 {
		t.Error("the alert line is not full width")
	}
}

func TestFooterFollowsPageAndClicksRunKeys(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if m = click(t, m, "key-x"); m.overlay != ovNone {
		t.Fatal("x on the Repos card opened something")
	}
	m.page = pageSettings
	if v := m.View(); !strings.Contains(v, "ctrl+s save") || strings.Contains(v, "x stop") {
		t.Fatalf("Settings footer:\n%s", v)
	}
	m.page = pageDashboard
	if m = click(t, m, "key-enter"); m.overlay != ovNone {
		t.Fatal("enter on the Repos card opened the detail view")
	}
	m.focus = paneRunners
	if m = click(t, m, "key-enter"); m.page != pageDetail {
		t.Fatal("a footer click did not run the key")
	}
}
