package tui

import (
	"strings"
	"testing"
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
	if m = click(t, m, "key-x"); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("the footer's x did not ask to stop the runner")
	}
	m = feed(m, key("esc"))
	m.detailSnap.Repo, m.detailSnap.State = "dark\x1b]0;pwned\x07cloud", "busy\x1b[2J"
	if v := m.View(); strings.Contains(v, "\x1b]0;") || strings.Contains(v, "\x1b[2J") || !strings.Contains(v, "darkcloud") {
		t.Fatalf("unsanitized detail text: %q", v)
	}
}
