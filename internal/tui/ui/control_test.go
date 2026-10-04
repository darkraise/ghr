package ui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// key builds the KeyMsg a terminal sends for k ("enter", "a", " ", "ctrl+s", …).
func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// typeText feeds each rune of s to w as a key.
func typeText(w Widget, s string) {
	for _, r := range s {
		w.Update(key(string(r)))
	}
}

// render scans view the way the TUI's View does, so its zones register.
func render(view string) string { return zone.Scan(view) }

func waitZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if z := zone.Get(id); !z.IsZero() {
			return z
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zone %s never registered", id)
	return nil
}

var syncN int

// settle waits until every zone update already queued has been applied.
// bubblezone applies Scan results from a buffered channel, so without this a
// late update from an older frame can land after a Clear and hand back stale
// coordinates.
func settle(t *testing.T) {
	t.Helper()
	syncN++
	id := fmt.Sprintf("sync/%d", syncN)
	render(zone.Mark(id, "x"))
	waitZone(t, id)
}

// clickAt renders view, waits for zone id and returns a left click inside it.
func clickAt(t *testing.T, view, id string) tea.MouseMsg {
	t.Helper()
	settle(t)
	zone.Clear(id)
	render(view)
	z := waitZone(t, id)
	return tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

// outside is a click far from anything a test renders.
var outside = tea.MouseMsg{X: 500, Y: 500, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}

func msgOf(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestZoneID(t *testing.T) {
	for _, c := range []struct {
		parts []string
		want  string
	}{
		{[]string{"settings", "repo", "darkmem", "max"}, "settings/repo/darkmem/max"},
		{[]string{"settings", "mode", "opt-1"}, "settings/mode/opt-1"},
		{[]string{"toast"}, "toast"},
	} {
		if got := ZoneID(c.parts...); got != c.want {
			t.Errorf("ZoneID(%q) = %q, want %q", c.parts, got, c.want)
		}
	}
}

func TestEditorKeyLeavesFocusAndSaveKeys(t *testing.T) {
	for _, k := range []string{"tab", "shift+tab", "up", "down", "ctrl+s"} {
		if editorKey(key(k)) {
			t.Errorf("an edited field took %q", k)
		}
	}
	for _, k := range []string{"a", "q", "1", " ", "backspace", "left", "enter", "esc"} {
		if !editorKey(key(k)) {
			t.Errorf("an edited field let %q through", k)
		}
	}
}
