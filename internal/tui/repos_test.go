package tui

import (
	"strings"
	"testing"
)

func TestRepositoriesPageListsAndSelectsByName(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), key("2"))
	if m.page != pageRepos {
		t.Fatalf("page %v", m.page)
	}
	v := m.View()
	for _, want := range []string{"2 Repositories", "[ + Add repository ]", "darkcloud", "darkmem", "darkagents", "[ACTIVE]", "[PAUSED]", "1/1 running · 2 queued"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	m = feed(m, key("down"))
	if m.repos.selected != "darkmem" {
		t.Fatalf("selected %q", m.repos.selected)
	}
	st := sampleStatus()
	st.Repos = st.Repos[1:] // darkcloud removed: the selection stays on darkmem
	m = feed(m, statusMsg{st: st})
	if r := m.selectedRepoStatus(); r == nil || r.Name != "darkmem" {
		t.Fatalf("selection moved: %+v", r)
	}
	st.Repos = st.Repos[1:] // darkmem removed: the repo now at its position
	m = feed(m, statusMsg{st: st})
	if r := m.selectedRepoStatus(); r == nil || r.Name != "darkagents" {
		t.Fatalf("fallback: %+v", r)
	}
}

func TestRepositoriesPageActions(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), keys("2", "down", "p")...)
	if got := strings.Join(c.actions(), "|"); got != "pause darkmem" {
		t.Fatalf("p: %q", got)
	}
	if m = feed(m, key("d")); m.overlay != ovConfirm || !strings.Contains(m.confirmText, "Remove repo darkmem?") {
		t.Fatalf("d: overlay %v %q", m.overlay, m.confirmText)
	}
	m = feed(m, key("esc"), key("a"))
	if m.overlay != ovAddRepo {
		t.Fatalf("a: overlay %v", m.overlay)
	}
}

// The footer's p and d hints act on the selected repo, as the keys do.
func TestRepositoriesFooterHints(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), keys("2", "down")...)
	upd, cmd := m.footerPress("p")
	m = feed(upd.(Model), collect(cmd)...)
	if got := strings.Join(c.actions(), "|"); got != "pause darkmem" {
		t.Fatalf("p hint: %q", got)
	}
	upd, _ = m.footerPress("d")
	if mm := upd.(Model); mm.overlay != ovConfirm || !strings.Contains(mm.confirmText, "Remove repo darkmem?") {
		t.Fatalf("d hint: overlay %v %q", mm.overlay, mm.confirmText)
	}
	upd, _ = m.footerPress("a")
	if mm := upd.(Model); mm.overlay != ovAddRepo {
		t.Fatalf("a hint: overlay %v", mm.overlay)
	}
}

func TestRepositoriesPageEmptyAndNarrow(t *testing.T) {
	st := sampleStatus()
	st.Repos = nil
	m := feed(newModel(&fakeClient{}, 120, 30, st), key("2"))
	if v := m.View(); !strings.Contains(v, "No repositories yet") {
		t.Fatalf("empty:\n%s", v)
	}
	// Narrow: the list card stacks above the panel; the rows and buttons must
	// be on screen, not only within the width.
	for _, w := range []int{99, 72, 56, 40} {
		v := feed(sampleModel(&fakeClient{}, w, 30), key("2")).View()
		fits(t, "repositories", v, w, 30)
		for _, want := range []string{"darkcloud", "darkmem", "[ACTIVE]", "Pause", "Remove"} {
			if !strings.Contains(v, want) {
				t.Errorf("%d columns: missing %q:\n%s", w, want, v)
			}
		}
	}
}
