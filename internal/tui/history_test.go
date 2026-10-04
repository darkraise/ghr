package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/model"
)

// historyModel is the sample model on the History page; its client holds two
// finished jobs and filters them as the daemon would.
func historyModel(t *testing.T) (Model, *fakeClient) {
	t.Helper()
	c := &fakeClient{hist: []model.HistoryEntry{
		{Repo: "darkcloud", RunNumber: "411", JobName: "lint", Conclusion: "success",
			StartedAt: now.Add(-4 * time.Minute), FinishedAt: now.Add(-2 * time.Minute), HTMLURL: "https://github.com/darkraise/darkcloud/actions/runs/411"},
		{Repo: "darkmem", RunNumber: "87", JobName: "build / test", Conclusion: "failure",
			StartedAt: now.Add(-70 * time.Minute), FinishedAt: now.Add(-time.Hour)},
	}}
	return feed(sampleModel(c, 120, 30), key("3")), c
}

func lastReq(c *fakeClient) string { return c.histReqs[len(c.histReqs)-1] }

func TestHistoryFilterBar(t *testing.T) {
	m, c := historyModel(t)
	v := m.View()
	for _, want := range []string{"Repo   [ all ▾ ]", "Result   [ all ▾ ]", "› History", "( Copy run URL ) success    2m00s", "#87", "tab next"} {
		if !strings.Contains(v, want) {
			t.Errorf("History page missing %q", want)
		}
	}
	// Tab wraps from the table to the Repo filter, which enter opens.
	m = feed(m, key("tab"), key("enter"), key("down"), key("enter"))
	if v = m.View(); lastReq(c) != "darkcloud|" || !strings.Contains(v, "› [ darkcloud ▾ ]") || strings.Contains(v, "#87") {
		t.Fatalf("repo filter: request %q\n%s", lastReq(c), v)
	}
	m = feed(m, key("tab"), key("right")) // a closed select cycles with →
	if lastReq(c) != "darkcloud|success" || !strings.Contains(m.View(), "#411") {
		t.Fatalf("result filter: request %q", lastReq(c))
	}
}

func TestHistoryFilterMouse(t *testing.T) {
	m, c := historyModel(t)
	if m = click(t, m, histResultSel); !m.groups.histResult.Open() {
		t.Fatal("a click did not open the Result filter")
	}
	m = click(t, m, histResultSel+"/opt-2")
	if v := m.View(); lastReq(c) != "|failure" || m.groups.histResult.Open() || !strings.Contains(v, "#87") || strings.Contains(v, "#411") {
		t.Fatalf("option click: request %q open %v\n%s", lastReq(c), m.groups.histResult.Open(), v)
	}
	// r and c still cycle, and the selects follow.
	m = feed(m, key("r"))
	if v := m.View(); lastReq(c) != "darkcloud|failure" || !strings.Contains(v, "[ darkcloud ▾ ]") || !strings.Contains(v, "[ failure ▾ ]") {
		t.Fatalf("r: request %q\n%s", lastReq(c), v)
	}
}

func TestHistoryCopyRunURL(t *testing.T) {
	m, _ := historyModel(t)
	var copied string
	m.copyFn = func(s string) { copied = s }
	if m = click(t, m, rowCopy); copied != "https://github.com/darkraise/darkcloud/actions/runs/411" {
		t.Fatalf("row button copied %q", copied)
	}
	copied = ""
	m = feed(m, key("down"), key("enter"))
	if copied != "" || !strings.Contains(m.View(), "no run URL recorded") {
		t.Fatalf("enter on a row without a URL copied %q", copied)
	}
	// With a filter focused, enter opens it instead of copying.
	m = feed(m, key("tab"), key("enter"))
	if !m.groups.histRepo.Open() || copied != "" {
		t.Fatal("enter on the Repo filter did not open it")
	}
}

// A focused filter owns the arrows and the footer; pgup and pgdn page the
// table whatever has focus.
func TestHistoryFocusKeys(t *testing.T) {
	m, c := historyModel(t)
	for i := 0; i < 30; i++ {
		c.hist = append(c.hist, model.HistoryEntry{Repo: "darkmem", RunNumber: fmt.Sprint(i), JobName: "test", Conclusion: "success", FinishedAt: now})
	}
	m = feed(m, key("r"), key("r"), key("r"), key("r")) // all repos again, refetched
	m = feed(m, key("tab"))                             // the Repo filter
	if m = feed(m, key("down")); m.histSel != 0 {
		t.Fatalf("down moved the table to %d while a filter had focus", m.histSel)
	}
	if v := m.View(); !strings.Contains(v, "enter open") || strings.Contains(v, "copy URL") {
		t.Fatalf("footer does not follow the filter:\n%s", v)
	}
	if m = feed(m, key("pgdown")); m.histSel != 10 {
		t.Fatalf("pgdn: selection %d", m.histSel)
	}
}

// A long repo name is cut to the width instead of widening the page.
func TestHistoryLongRepoName(t *testing.T) {
	st := sampleStatus()
	long := strings.Repeat("very-long-repository-name-", 4)
	st.Repos = append(st.Repos, model.RepoStatus{Name: long})
	m := feed(newModel(&fakeClient{}, 80, 30, st), key("3"))
	m.histRepo = long
	for i, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d is %d wide: %q", i, lipgloss.Width(line), line)
		}
	}
}
