package tui

import (
	"errors"
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

// The cards are tab stops; focus and the row keys' pane move together.
func TestDashboardCardFocus(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if m.groups.dash.FocusedID() != dashRepos || m.focus != paneRepos {
		t.Fatalf("start: focus %q pane %v", m.groups.dash.FocusedID(), m.focus)
	}
	ok, mm, _ := m.dashKey(key("tab"))
	if m = mm.(Model); !ok || m.groups.dash.FocusedID() != dashRunners || m.focus != paneRunners {
		t.Fatalf("tab: ok %v focus %q pane %v", ok, m.groups.dash.FocusedID(), m.focus)
	}
	if ok, _, _ = m.dashKey(key("p")); ok {
		t.Fatal("dashKey took a row key")
	}
	m.focusCard(paneRepos)
	if m.groups.dash.FocusedID() != dashRepos || m.focus != paneRepos {
		t.Fatalf("focusCard: focus %q pane %v", m.groups.dash.FocusedID(), m.focus)
	}
}

// Tab cycles the Dashboard's tab stops: the two cards, then the header buttons.
func TestDashboardTabOrder(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 40)
	if v := m.View(); !strings.Contains(v, "╭─ › Repositories ") {
		t.Fatalf("Repositories card not focused at start:\n%s", v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); m.focus != paneRunners || !strings.Contains(v, "╭─ › Runners ") {
		t.Fatalf("tab: focus %v\n%s", m.focus, v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); !strings.Contains(v, "› [ + Add ]") {
		t.Fatalf("tab did not reach + Add:\n%s", v)
	}
	if m = feed(m, keys("p", "x")...); len(c.actions()) != 0 || m.overlay != ovNone {
		t.Fatalf("row keys acted while a header button had focus: %v", c.actions())
	}
	if m = feed(m, key("enter")); m.overlay != ovAddRepo {
		t.Fatal("enter on + Add did not open the dialog")
	}
	m = feed(m, keys("esc", "tab", " ")...) // Pause all
	if got := strings.Join(c.actions(), "|"); got != "pause-all" {
		t.Fatalf("actions %q", got)
	}
}

// While a header button has focus, the arrows move no table and the footer
// offers the button's keys; a button disabled under focus hands focus on.
func TestDashboardHeaderFocus(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), keys("tab", "tab")...) // + Add
	if m = feed(m, key("down")); m.repoSel != 0 || m.runnerSel != 0 {
		t.Fatalf("down moved repo %d runner %d", m.repoSel, m.runnerSel)
	}
	if v := m.View(); !strings.Contains(v, "enter press") || strings.Contains(v, "p pause") {
		t.Fatalf("footer does not follow the focused button:\n%s", v)
	}
	m = feed(m, statusMsg{err: errors.New("connection refused")})
	// The first tab pointed the row keys at Runners, so focus returns to that card.
	if v := m.View(); m.groups.dash.FocusedID() != dashRunners || m.focus != paneRunners || !strings.Contains(v, "╭─ › Runners") {
		t.Fatalf("focus %q pane %v: not handed to the row keys' card", m.groups.dash.FocusedID(), m.focus)
	}
}

// The header offers Resume all once every repo not being removed is paused.
func TestDashboardHeaderButtons(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 40)
	if v := m.View(); !strings.Contains(v, "[ + Add ]") || !strings.Contains(v, "( Pause all )") {
		t.Fatalf("header buttons missing:\n%s", v)
	}
	if m = click(t, m, dashAdd); m.overlay != ovAddRepo {
		t.Fatal("clicking + Add did not open the dialog")
	}
	m = feed(m, key("esc"))
	st := sampleStatus()
	st.Repos[0].Paused, st.Repos[1].Paused = true, true
	st.Repos[2].Removing = true // still paused, and ignored
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "( Resume all )") {
		t.Fatalf("Resume all not offered:\n%s", v)
	}
	m = click(t, m, dashPauseAll)
	if got := strings.Join(c.actions(), "|"); got != "resume-all" {
		t.Fatalf("actions %q", got)
	}
}
