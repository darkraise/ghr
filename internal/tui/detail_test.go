package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// detailModel is the sample model on a3f9c1's detail page, opened from the
// Runners page; the run's URL is known.
func detailModel(t *testing.T, c *fakeClient) Model {
	t.Helper()
	st := sampleStatus()
	st.Instances[0].Job.HTMLURL = "https://github.com/darkraise/darkcloud/actions/runs/1"
	c.st = &st
	m := newModel(c, 120, 40, st)
	return feed(m, keys("2", "enter")...)
}

func TestDetailPage(t *testing.T) {
	m := detailModel(t, &fakeClient{})
	if m.page != pageDetail || m.detailID != "a3f9c1" {
		t.Fatalf("page %v id %q", m.page, m.detailID)
	}
	v := m.View()
	for _, want := range []string{"Runners › a3f9c1", "( Copy run URL )", "[ Stop runner ]", "● busy", "darkcloud",
		"CI / e2e-journeys  #412", "12m04s", "started 2026-10-03 13:52:56", "▌ 2 Runners", "esc back"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	if m = feed(m, key("esc")); m.page != pageRunners {
		t.Fatalf("esc went to page %v", m.page)
	}
}

func TestDetailButtons(t *testing.T) {
	c := &fakeClient{}
	m := detailModel(t, c)
	var copied string
	m.copyFn = func(s string) { copied = s }
	m = click(t, m, detailCopy)
	if copied != "https://github.com/darkraise/darkcloud/actions/runs/1" || !strings.Contains(m.View(), "✔ copied https://github.com") {
		t.Fatalf("copy: %q", copied)
	}
	if m = click(t, m, detailStop); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("Stop runner on a busy runner did not ask first")
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "kill a3f9c1" {
		t.Fatalf("actions %q", got)
	}
	m = feed(m, key("x")) // x stops from the keyboard too
	if m.overlay != ovConfirm {
		t.Fatal("x on the detail page did not ask")
	}
}

// Without a known run URL, the header offers only Stop runner.
func TestDetailWithoutURL(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.focus = paneRunners
	m = feed(m, key("enter"))
	if v := m.View(); strings.Contains(v, "Copy run URL") || !strings.Contains(v, "[ Stop runner ]") {
		t.Fatalf("header:\n%s", v)
	}
	if m = feed(m, key("esc")); m.page != pageDashboard {
		t.Fatalf("esc went to page %v", m.page)
	}
}

// The footer's x stops the runner like the key, and daemon text on the page
// is sanitized.
func TestDetailFooterAndCleanText(t *testing.T) {
	m := detailModel(t, &fakeClient{})
	if v := m.View(); !strings.Contains(v, "tab next") || !strings.Contains(v, "x stop") {
		t.Fatalf("detail footer:\n%s", v)
	}
	if m = click(t, m, "key-x"); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("the footer's x did not ask to stop the runner")
	}
	m = feed(m, key("esc"))
	m.detailSnap.Repo, m.detailSnap.State = "dark\x1b]0;pwned\x07cloud", "busy\x1b[2J"
	if v := m.View(); strings.Contains(v, "\x1b]0;") || strings.Contains(v, "\x1b[2J") || !strings.Contains(v, "darkcloud") {
		t.Fatalf("unsanitized detail text: %q", v)
	}
}

// ←/→ and clicks switch the tabs; only the Log tab follows the runner's log.
func TestDetailTabs(t *testing.T) {
	c := &fakeClient{steps: []model.Step{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"}},
		ctrs: []model.Container{{Name: "darkcloud-db-1", State: "running"}}}
	m := detailModel(t, c)
	m.logID, m.logText = "", ""
	v := m.View()
	if !strings.Contains(v, "[ Steps ]") || !strings.Contains(v, "✔ Set up job") || strings.Contains(v, "darkcloud-db-1") {
		t.Fatalf("Steps tab:\n%s", v)
	}
	if m = ticks(m, slowPoll); m.logID != "" {
		t.Fatalf("the Steps tab followed log %q", m.logID)
	}
	m = feed(m, key("right"))
	if m.groups.tabs.active != tabLog || m.logID != "a3f9c1" {
		t.Fatalf("right: tab %d log %q", m.groups.tabs.active, m.logID)
	}
	m.logText = "hello from the job\n"
	if v = m.View(); !strings.Contains(v, "Log — following") || !strings.Contains(v, "hello from the job") {
		t.Fatalf("Log tab:\n%s", v)
	}
	if m = click(t, m, tabZone(tabContainers)); m.groups.tabs.active != tabContainers || !strings.Contains(m.View(), "darkcloud-db-1") {
		t.Fatalf("click: tab %d\n%s", m.groups.tabs.active, m.View())
	}
	if m = feed(m, key("right")); m.groups.tabs.active != tabSteps {
		t.Fatalf("right wraps to tab %d", m.groups.tabs.active)
	}
	if m = feed(m, key("down"), key("down")); m.runnerSel != 0 || m.detailID != "a3f9c1" {
		t.Fatalf("down moved the hidden selection to %d", m.runnerSel)
	}
}

// l on a runner row opens its detail page on the Log tab; Tab reaches the
// tab strip after the header buttons.
func TestLogKeyOpensDetailLog(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), keys("2", "l")...)
	if m.page != pageDetail || m.groups.tabs.active != tabLog || m.logID != "a3f9c1" {
		t.Fatalf("page %v tab %d log %q", m.page, m.groups.tabs.active, m.logID)
	}
	if v := m.View(); !strings.Contains(v, "›   Steps   [ Log ]") || !strings.Contains(v, "right next tab") {
		t.Fatalf("tab strip not focused or footer missing:\n%s", v)
	}
	if m = feed(m, key("tab")); m.groups.detail.FocusedID() == detailTabs {
		t.Fatal("tab did not move focus off the tab strip")
	}
}

// A step list longer than the page scrolls with pgdn, pgup and the arrows,
// and starts at the top again on another tab.
func TestDetailListScrolls(t *testing.T) {
	c := &fakeClient{}
	for i := 1; i <= 60; i++ {
		c.steps = append(c.steps, model.Step{Number: i, Name: fmt.Sprintf("step %02d", i), Status: "completed", Conclusion: "success"})
	}
	m := detailModel(t, c)
	if v := m.View(); !strings.Contains(v, "step 01") || strings.Contains(v, "step 60") {
		t.Fatalf("top of the list:\n%s", v)
	}
	m = feed(m, keys("pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown")...)
	if v := m.View(); strings.Contains(v, "step 01") || !strings.Contains(v, "step 60") {
		t.Fatalf("after pgdn:\n%s", v)
	}
	before := m.detailScroll
	if m = feed(m, key("pgup"), key("down")); m.detailScroll != before-9 {
		t.Fatalf("pgup then down: scroll %d, want %d", m.detailScroll, before-9)
	}
	if m = feed(m, key("right"), key("left")); m.detailScroll != 0 {
		t.Fatalf("tab change kept scroll %d", m.detailScroll)
	}
}

// Paging only scrolls what is on screen: a log that is not shown keeps its
// tail, so the Log tab never opens on a stale window titled "following".
func TestPagingScrollsOnlyAShownLog(t *testing.T) {
	c := &fakeClient{}
	for i := 1; i <= 60; i++ {
		c.steps = append(c.steps, model.Step{Number: i, Name: fmt.Sprintf("step %02d", i), Status: "completed", Conclusion: "success"})
	}
	m := detailModel(t, c)
	m = feed(m, keys("pgup", "pgup")...)
	if m.logScroll != 0 || m.eventScroll != 0 {
		t.Fatalf("paging the Steps tab moved the log (%d) or the events (%d)", m.logScroll, m.eventScroll)
	}
	if m = feed(m, key("right")); m.logScroll != 0 {
		t.Fatalf("the Log tab opened %d lines above the tail", m.logScroll)
	}
	if m = feed(m, key("pgup")); m.logScroll != 10 {
		t.Fatalf("paging a shown log: scroll %d, want 10", m.logScroll)
	}
	m = feed(m, keys("esc", "1", "pgup")...)
	if m.eventScroll != 5 || m.logScroll != 10 {
		t.Fatalf("paging the Dashboard: events %d (want 5), log %d (want 10 kept)", m.eventScroll, m.logScroll)
	}
}

// Clicking the Log tab follows this runner's log at once, not the log of
// the runner followed before.
func TestDetailLogTabClickFollows(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2")) // follows a3f9c1
	m = feed(m, keys("1", "right", "down")...)               // Dashboard, Runners card, 7be210
	if m = feed(m, key("enter")); m.detailID != "7be210" || m.logID != "a3f9c1" {
		t.Fatalf("detail %q log %q", m.detailID, m.logID)
	}
	if m = click(t, m, tabZone(tabLog)); m.logID != "7be210" {
		t.Fatalf("log tab click follows %q", m.logID)
	}
}

// When the runner leaves /status the page keeps its snapshot, freezes the
// elapsed time and offers Back to runners; only the Log tab keeps polling.
func TestDetailAfterRunnerFinishes(t *testing.T) {
	c := &fakeClient{}
	m := detailModel(t, c)
	c.st.Instances = c.st.Instances[1:] // a3f9c1 is gone
	m.now = func() time.Time { return now.Add(5 * time.Minute) }
	m = ticks(m, 1)
	m.now = func() time.Time { return now.Add(10 * time.Minute) }
	v := m.View()
	for _, want := range []string{"⚠ This runner has finished", "Back to runners", "● busy", "17m04s", "CI / e2e-journeys  #412"} {
		if !strings.Contains(v, want) {
			t.Errorf("finished detail page missing %q", want)
		}
	}
	c.calls = nil
	if m = ticks(m, slowPoll); strings.Contains(strings.Join(c.calls, "|"), "containers") {
		t.Fatalf("containers still polled: %v", c.calls)
	}
	c.calls = nil
	if m = ticks(feed(m, key("right")), 2); !strings.Contains(strings.Join(c.calls, "|"), "log a3f9c1") {
		t.Fatalf("the Log tab stopped polling: %v", c.calls)
	}
	if m = feed(m, key("x")); m.overlay != ovNone || strings.Contains(strings.Join(c.actions(), "|"), "kill") {
		t.Fatal("x tried to stop a finished runner")
	}
	if m = click(t, m, detailBack); m.page != pageRunners {
		t.Fatalf("Back to runners went to page %v", m.page)
	}
}
