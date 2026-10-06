package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/darkraise/ghr/internal/tui/ui"
)

// Help lists every group in four columns and fits a 22-row screen from 104
// columns; narrower, it stacks and scrolls.
func TestHelpGroupsFit(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 22), key("?"))
	v := m.View()
	for _, want := range []string{"Global", "Dashboard", "Repositories", "Runner rows", "Detail", "History", "Settings",
		"1-6", "Storage", "add/remove/edit", "switch tab", "copy run URL", "takes digits", "set -g mouse on", "[ Close ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
	lines := strings.Split(ansi.Strip(v), "\n")
	for _, p := range [][2]string{{"Global", "Detail"}, {"Dashboard", "History"}, {"Repositories", "Settings"}, {"Runner rows", "Storage"}} {
		top, below := lines[lineWith(lines, p[0])], lines[lineWith(lines, p[1])]
		if ansi.StringWidth(top[:strings.Index(top, p[0])]) != ansi.StringWidth(below[:strings.Index(below, p[1])]) {
			t.Errorf("%s is not under %s:\n%s", p[1], p[0], v)
		}
	}
	for _, w := range []int{104, 120} {
		v := feed(sampleModel(&fakeClient{}, w, 22), key("?")).View()
		if lipgloss.Height(v) > 22 || strings.Contains(v, "↑↓ scroll") {
			t.Errorf("width %d: help should fit 22 rows without scrolling:\n%s", w, v)
		}
		for i, line := range strings.Split(v, "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d: line %d is %d wide", w, i, lipgloss.Width(line))
			}
		}
	}
	if v := feed(sampleModel(&fakeClient{}, 103, 22), key("?")).View(); !strings.Contains(v, "↑↓ scroll") {
		t.Fatalf("at 103 columns help stacks and scrolls:\n%s", v)
	}
}

// Below three columns' width the groups stack into one column instead of
// wrapping mid-row.
func TestHelpStacksWhenNarrow(t *testing.T) {
	text := helpText(60)
	if !strings.Contains(text, "Global") || strings.Contains(text, "Global                   Dashboard") {
		t.Fatalf("narrow help did not stack:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n")[:30] {
		if lipgloss.Width(line) > helpColW {
			t.Fatalf("stacked line %q is wider than one column", line)
		}
	}
}

// Too narrow for three columns, Help is taller than a 22-row screen: it shows
// what fits with a scroll hint, keeps Close in view, and scrolls to the rest.
func TestHelpScrollsWhenShort(t *testing.T) {
	for _, w := range []int{40, 56} {
		m := feed(sampleModel(&fakeClient{}, w, 22), key("?"))
		check := func(want, gone string) {
			t.Helper()
			v := m.View()
			if h := lipgloss.Height(v); h > 22 {
				t.Fatalf("width %d: help is %d rows:\n%s", w, h, v)
			}
			for i, line := range strings.Split(v, "\n") {
				if lipgloss.Width(line) > w {
					t.Fatalf("width %d: line %d is %d wide", w, i, lipgloss.Width(line))
				}
			}
			if !strings.Contains(v, want) || strings.Contains(v, gone) || !strings.Contains(v, "[ Close ]") || !strings.Contains(v, "↑↓ scroll") {
				t.Fatalf("width %d: want %q without %q:\n%s", w, want, gone, v)
			}
		}
		check("Global", "Shift-drag")
		m = feed(m, keys("pgdown", "pgdown", "pgdown", "pgdown", "pgdown")...)
		check("Shift-drag", "Global")
		if m = feed(m, key("esc")); m.overlay != ovNone {
			t.Fatalf("width %d: esc did not close help", w)
		}
	}
}

// A press reaches pressed() as a message, so a double click can deliver a
// second one after its dialog closed; a button acts only while its dialog
// (or, for a page button, no dialog) is open.
func TestStalePressIsIgnored(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focusCard(paneRunners)
	if m = feed(m, key("x")); m.overlay != ovConfirm {
		t.Fatalf("x on a busy runner: overlay %v", m.overlay)
	}
	m = feed(m, ui.Pressed{ID: btnYes}, ui.Pressed{ID: btnYes})
	if got := strings.Join(c.actions(), "|"); got != "kill a3f9c1" {
		t.Fatalf("actions %q", got)
	}

	c = &fakeClient{}
	applyPoll(c)
	m = dirtySettings(t, c)
	if m = feed(m, key("3")); m.overlay != ovUnsaved {
		t.Fatalf("unexpected overlay %v", m.overlay)
	}
	m, _ = pump(m, ui.Pressed{ID: setSave}, ui.Pressed{ID: btnLeaveSave}, ui.Pressed{ID: btnLeaveSave})
	if m.page != pageRunners || len(c.patches) != 1 {
		t.Fatalf("page %v patches %d", m.page, len(c.patches))
	}
}
