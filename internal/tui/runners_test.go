package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/model"
)

func TestRunnersPage(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2"))
	v := m.View()
	for _, want := range []string{"╭─ › Runners ", "▸ a3f9c1", "( Logs )", "[ Stop ]", "⧗ waiting", "Log preview — a3f9c1 (following)", `a3f9c1 after ""`} {
		if !strings.Contains(v, want) {
			t.Errorf("Runners page missing %q", want)
		}
	}
}

// On a short screen the log preview gives way first, down to 3 lines; then
// the table scrolls to keep the selection visible.
func TestRunnersPageShortScreen(t *testing.T) {
	st := sampleStatus()
	st.Instances = nil
	for i := 0; i < 20; i++ {
		st.Instances = append(st.Instances, model.InstanceStatus{ID: fmt.Sprintf("run%02d", i), Repo: "darkmem", State: "idle", Since: now})
	}
	m := feed(newModel(&fakeClient{}, 120, 22, st), key("2"))
	for i := 0; i < 19; i++ {
		m = feed(m, key("down"))
	}
	v := m.View()
	lines := strings.Split(v, "\n")
	logTop := lineWith(lines, "Log preview — run19")
	if logTop < 0 || !strings.Contains(v, "▸ run19") || lipgloss.Height(v) > 22 || !strings.Contains(lines[logTop+4], "╰") {
		t.Fatalf("preview not at 3 lines or selection hidden (%d lines):\n%s", lipgloss.Height(v), v)
	}
}

func TestRunnersPageLogScrolls(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2"))
	for i := 0; i < 60; i++ {
		m.logText += fmt.Sprintf("line %02d\n", i)
	}
	z := zoneOf(t, m, "log")
	m = feed(m, tea.MouseMsg{X: z.StartX + 2, Y: z.StartY + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.logScroll != 3 {
		t.Fatalf("wheel: scroll %d", m.logScroll)
	}
	if m = feed(m, key("pgup")); m.logScroll != 13 || !strings.Contains(m.View(), "line 46") {
		t.Fatalf("pgup: scroll %d\n%s", m.logScroll, m.View())
	}
}

// Once every runner has gone, the preview no longer claims to follow one.
func TestRunnersPageWithoutRunners(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2"))
	st := sampleStatus()
	st.Instances = nil
	v := feed(m, statusMsg{st: st}).View()
	if !strings.Contains(v, "Log preview — no runner selected") || strings.Contains(v, "a3f9c1 after") {
		t.Fatalf("stale preview:\n%s", v)
	}
}
