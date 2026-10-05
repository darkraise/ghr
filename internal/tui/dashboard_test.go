package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

func sampleMetrics() model.Metrics {
	f := func(v float64) *float64 { return &v }
	cpu, used, total := 12.0, int64(180<<20), int64(8<<30)
	return model.Metrics{CPU: &cpu, MemUsed: &used, MemTotal: &total, DiskPct: 61, Samples: []model.MetricSample{
		{At: now.Add(-2 * time.Minute), Live: 1, Queued: 3, CPU: f(10)},
		{At: now.Add(-time.Minute), Live: 2, Queued: 1, CPU: f(30)},
	}}
}

func TestStatTiles(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.mg.metrics = sampleMetrics()
	v := m.View()
	for _, want := range []string{"╭─ Running ", "2 / 3", "▃▆", "╭─ Queued jobs ", "╭─ CPU ", "12%", "╭─ Memory ", "180M / 8.0G", "╭─ Disk ", "61%"} {
		if !strings.Contains(v, want) {
			t.Errorf("tiles missing %q", want)
		}
	}
	got := m.statTiles(100, true)
	for _, want := range []string{"Queued jobs 3", "CPU 12%", "Memory 180M / 8.0G"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact tiles %q missing %q", got, want)
		}
	}
	m.st.Mode = config.ModeAll
	if got := m.statTiles(100, true); !strings.Contains(got, "Running 2 / ∞") {
		t.Errorf("all mode: %q", got)
	}
	m.mg.metrics = model.Metrics{}
	if v := m.View(); !strings.Contains(v, "╭─ CPU ") || !strings.Contains(v, "–") {
		t.Fatalf("no metrics yet:\n%s", v)
	}
}

// Every tile keeps its graphics at 100 and 120 columns: a value line, then
// a chart line; CPU has both a gauge and a sparkline.
func TestStatTilesKeepGraphics(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.mg.metrics = sampleMetrics()
	for _, w := range []int{84, 104} { // the content width at 100 and 120 columns
		lines := strings.Split(m.statTiles(w, false), "\n")
		if len(lines) != 4 {
			t.Fatalf("%d: tiles are %d lines, want 4", w, len(lines))
		}
		if n := strings.Count(lines[1], "▕") + strings.Count(lines[2], "▕"); n < 3 {
			t.Errorf("%d: %d gauges, want CPU, Memory and Disk:\n%s", w, n, strings.Join(lines, "\n"))
		}
		if !strings.ContainsAny(lines[2], "▁▂▃▄▅▆▇█") || !strings.Contains(lines[1], "12%") || !strings.Contains(lines[1], "180M / 8.0G") {
			t.Errorf("%d: tiles:\n%s", w, strings.Join(lines, "\n"))
		}
	}
}

// A failed metrics read shows in the CPU and Memory tiles and clears on the
// next good poll; a stale reply changes nothing.
func TestStatTilesMetricsError(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.mg.metricsSeq = 3
	m = feed(m, metricsMsg{seq: 3, mt: sampleMetrics()})
	m = feed(m, metricsMsg{seq: 2, err: errors.New("stale")})
	if v := m.View(); strings.Contains(v, "stale") || !strings.Contains(v, "12%") {
		t.Fatalf("stale reply applied:\n%s", v)
	}
	m.mg.metricsSeq = 4
	m = feed(m, metricsMsg{seq: 4, err: errors.New("daemon busy")})
	if v := m.View(); !strings.Contains(v, "✖ daemon busy") || strings.Contains(v, "12%") {
		t.Fatalf("error not shown:\n%s", v)
	}
	if got := m.statTiles(100, true); !strings.Contains(got, "metrics ✖ daemon busy") {
		t.Fatalf("compact: %q", got)
	}
	m.mg.metricsSeq = 5
	m = feed(m, metricsMsg{seq: 5, mt: sampleMetrics()})
	if v := m.View(); strings.Contains(v, "daemon busy") || !strings.Contains(v, "12%") {
		t.Fatalf("not recovered:\n%s", v)
	}
}

func TestSeriesMarksGaps(t *testing.T) {
	s := []model.MetricSample{{At: now, Live: 1}, {At: now.Add(time.Minute), Live: 2}, {At: now.Add(4 * time.Minute), Live: 3}}
	got := series(s, func(x model.MetricSample) *float64 { v := float64(x.Live); return &v })
	if len(got) != 4 || got[2] != nil || *got[3] != 3 {
		t.Fatalf("series %v", got)
	}
}

// Under 100 columns, or when rows run short, the tiles take one line.
func TestStatTilesCollapse(t *testing.T) {
	if v := sampleModel(&fakeClient{}, 80, 40).View(); strings.Contains(v, "╭─ Running ") || !strings.Contains(v, "Running 2 / 3") {
		t.Fatalf("tiles not compact under 100 columns:\n%s", v)
	}
	// The Activity card gives way first: at 24 rows the tiles are still boxed
	// and Activity is down to its 3 lines; one row fewer collapses the tiles.
	v := sampleModel(&fakeClient{}, 120, 24).View()
	lines := strings.Split(v, "\n")
	top := lineWith(lines, "╭─ Activity ")
	if !strings.Contains(v, "╭─ Running ") || top < 0 || !strings.Contains(lines[top+4], "╰") {
		t.Fatalf("at 24 rows the tiles should stay boxed with a 3-line Activity card:\n%s", v)
	}
	if v := sampleModel(&fakeClient{}, 120, 23).View(); strings.Contains(v, "╭─ Running ") || !strings.Contains(v, "Running 2 / 3") {
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
	if v := m.View(); !strings.Contains(v, "› [ + Add repository ]") {
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
	if v := m.View(); !strings.Contains(v, "[ + Add repository ]") || !strings.Contains(v, "( Pause all )") {
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

// The focused card's selected row carries buttons that run the row keys.
// The selected row's buttons belong to the focused card: they go while a
// header button has focus, and a selected runner keeps its elapsed time.
func TestRowButtonsFollowFocus(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.focusCard(paneRunners)
	if v := m.View(); !strings.Contains(v, "( Logs )") || !strings.Contains(v, "[ Stop ] 12m04s") {
		t.Fatalf("selected runner row:\n%s", v)
	}
	if v := feed(m, key("tab")).View(); strings.Contains(v, "( Logs )") || strings.Contains(v, "( Pause )") {
		t.Fatalf("row buttons shown while + Add has focus:\n%s", v)
	}
}

func TestRowButtons(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 40)
	v := m.View()
	if !strings.Contains(v, "▸ darkcloud") || !strings.Contains(v, "( Pause )") || !strings.Contains(v, "[ Remove ]") || strings.Contains(v, "( Logs )") {
		t.Fatalf("repo row buttons:\n%s", v)
	}
	m = click(t, m, rowPause)
	if m = click(t, m, rowRemove); m.overlay != ovConfirm {
		t.Fatal("Remove did not ask first")
	}
	m = feed(m, key("esc"))
	m = feed(m, keys("down", "down")...) // darkagents is paused
	if v := m.View(); !strings.Contains(v, "( Resume )") {
		t.Fatalf("paused repo row:\n%s", v)
	}
	m = feed(m, key("right"))
	if v := m.View(); !strings.Contains(v, "( Logs )") || !strings.Contains(v, "[ Stop ]") || strings.Contains(v, "( Resume )") {
		t.Fatalf("runner row buttons:\n%s", v)
	}
	if m = click(t, m, rowStop); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("Stop on a busy runner did not ask first")
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "pause darkcloud|kill a3f9c1" {
		t.Fatalf("actions %q", got)
	}
}

func TestDashboardEditOpensTheRepository(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), key("down")) // the Repositories card: darkmem
	if v := m.View(); !strings.Contains(v, "( Edit )") || !strings.Contains(v, "[ + Add repository ]") {
		t.Fatalf("dashboard:\n%s", v)
	}
	if m = feed(m, key("e")); m.page != pageRepos || m.repos.selected != "darkmem" {
		t.Fatalf("e: page %v selected %q", m.page, m.repos.selected)
	}
	m = feed(m, key("1"))
	if m = click(t, m, rowEdit); m.page != pageRepos || m.repos.selected != "darkmem" {
		t.Fatalf("click: page %v selected %q", m.page, m.repos.selected)
	}
}
