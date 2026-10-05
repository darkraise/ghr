package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/muesli/termenv"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

func at(d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}

// available is 2.337.0 installed with 2.338.0 due in days.
func available(days int) model.RunnerUpdate {
	return model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(-time.Hour),
		Deadline: at(time.Duration(days) * 24 * time.Hour)}
}

func withRunner(u model.RunnerUpdate) model.Status {
	st := sampleStatus()
	st.RunnerUpdate = u
	return st
}

func TestRunnerLineStates(t *testing.T) {
	queued, running := available(30), available(30)
	queued.Queued = true
	running.Queued, running.Running = true, true
	for _, c := range []struct {
		name   string
		u      model.RunnerUpdate
		want   []string
		button string
	}{
		{"unknown", model.RunnerUpdate{}, []string{"version unknown (no dist/current)"}, ""},
		{"before the first check", model.RunnerUpdate{Installed: "2.337.0"}, []string{"2.337.0 checking…"}, ""},
		{"current", model.RunnerUpdate{Installed: "2.337.0", Latest: "2.337.0", CheckedAt: at(-2 * time.Hour)},
			[]string{"2.337.0 [UP TO DATE] checked 2h ago"}, ""},
		{"available", available(30), []string{"2.337.0 → 2.338.0 [UPDATE AVAILABLE]", "update by 2026-11-02 (30 days)"}, "[ Queue update ]"},
		{"overdue", available(-1), []string{"update by 2026-10-02 (overdue)"}, "[ Queue update ]"},
		{"queued", queued, []string{"2.337.0 → 2.338.0 [QUEUED]", "runs when no job is running or queued"}, "( Cancel queued update )"},
		{"running", running, []string{"2.337.0 → 2.338.0 [UPDATING]"}, ""},
		{"check failed", model.RunnerUpdate{Installed: "2.337.0", CheckError: "github: 502 bad gateway"},
			[]string{"2.337.0", "last check failed: github: 502 bad gateway"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, 140, 80), statusMsg{st: withRunner(c.u)})
			v := m.View()
			for _, want := range append([]string{"Runner"}, c.want...) {
				if !strings.Contains(v, want) {
					t.Fatalf("missing %q:\n%s", want, v)
				}
			}
			for _, b := range []string{"[ Queue update ]", "( Cancel queued update )"} {
				if strings.Contains(v, b) != (b == c.button) {
					t.Errorf("button %s shown %v, want only %q", b, strings.Contains(v, b), c.button)
				}
			}
		})
	}
}

func TestRunnerUpdateButtons(t *testing.T) {
	c := &fakeClient{}
	m := feed(onSettings(t, c, 140, 80), statusMsg{st: withRunner(available(30))})
	m = click(t, m, setRunnerQueue)
	if got := strings.Join(c.actions(), "|"); got != "runner-update" || !strings.Contains(m.View(), "runner update queued") {
		t.Fatalf("queue: %q\n%s", got, m.View())
	}
	u := available(30)
	u.Queued = true
	m = feed(m, statusMsg{st: withRunner(u)})
	m = click(t, m, setRunnerCancel)
	if got := strings.Join(c.actions(), "|"); got != "runner-update|runner-update cancel" || !strings.Contains(m.View(), "queued runner update cancelled") {
		t.Fatalf("cancel: %q\n%s", got, m.View())
	}
	c.updateErr = errors.New("runner 2.338.0 is already up to date")
	m = feed(m, statusMsg{st: withRunner(available(30))})
	if m = click(t, m, setRunnerQueue); !strings.Contains(m.View(), "runner 2.338.0 is already up to date") {
		t.Fatalf("refusal not shown:\n%s", m.View())
	}

	m = feed(m, statusMsg{err: errors.New("connection refused")})
	m.View()
	if m.mg.rnQueue.Focusable() {
		t.Fatal("Queue update enabled while the daemon is unreachable")
	}
	m.st = withRunner(u)
	m.View()
	if m.mg.rnCancel.Focusable() {
		t.Fatal("Cancel queued update enabled while the daemon is unreachable")
	}
}

func TestTopBarRunnerBadge(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if strings.Contains(m.topBar(200), "RUNNER") {
		t.Fatal("badge shown without an update")
	}
	m = feed(m, statusMsg{st: withRunner(available(30))})
	bar := m.topBar(200)
	if i, j := strings.Index(bar, "disk "), strings.Index(bar, "[RUNNER ↑]"); i < 0 || j < i {
		t.Fatalf("badge missing or before the disk gauge: %q", bar)
	}
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	for _, c := range []struct {
		days int
		kind ui.BadgeKind
	}{{30, ui.BadgeWarn}, {8, ui.BadgeWarn}, {7, ui.BadgeBad}, {-2, ui.BadgeBad}} {
		m.st = withRunner(available(c.days))
		if got := m.runnerBadge(); got != ui.Badge("runner ↑", c.kind) {
			t.Errorf("%d days: %q", c.days, got)
		}
	}
}

func TestStatusRunnerTextIsCleaned(t *testing.T) {
	u := available(30)
	u.CheckError = "bad\x1b]0;x\a gateway"
	m := feed(sampleModel(&fakeClient{}, 120, 30), statusMsg{st: withRunner(u)})
	if strings.ContainsAny(m.st.RunnerUpdate.CheckError, "\x1b\a") {
		t.Fatalf("check error kept control characters: %q", m.st.RunnerUpdate.CheckError)
	}
}

func TestSettingsRunnerUpdateGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, w, 80), statusMsg{st: withRunner(available(30))})
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

// The Runner line wraps rather than overflowing the card, down to 40 columns.
func TestRunnerLineFitsNarrow(t *testing.T) {
	for _, w := range []int{80, 56, 40} {
		m := feed(onSettings(t, &fakeClient{}, w, 80), statusMsg{st: withRunner(available(30))})
		fits(t, "settings", m.View(), w, 80)
	}
}
