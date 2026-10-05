package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

// ownerRepos is what the daemon offers: darkcloud is configured already.
func ownerRepos() []model.AvailableRepo {
	return []model.AvailableRepo{
		{Name: "booklore"},
		{Name: "darkcloud", Private: true, Configured: true},
		{Name: "immich", Private: true},
		{Name: "newrepo", Private: true},
		{Name: "site", Private: true},
	}
}

func TestAddRepoDialog(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	m := feed(sampleModel(c, 120, 30), key("a"))
	if m.overlay != ovAddRepo || c.availCalls != 1 {
		t.Fatalf("dialog: overlay %v, %d list calls", m.overlay, c.availCalls)
	}
	v := m.View()
	for _, want := range []string{"Add repository", "Repository", "pick one below", "› [ type to filter", "booklore  [PUBLIC]",
		"darkcloud  [PRIVATE]  added", "immich  [PRIVATE]", "[ − ] 1 [ + ] (default)", "Allow public repo",
		"self-hosted runners on a public repo can run anyone's code", "( Cancel )", "[ Add ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q", want)
		}
	}
	if m.add.group.FocusedID() != addRepo || m.add.ok.Focusable() {
		t.Fatalf("focus %q, Add enabled %v; Add waits for a pick", m.add.group.FocusedID(), m.add.ok.Focusable())
	}
	// enter in the picker picks the highlighted repository and moves focus to Max.
	m = feed(m, keys("n", "e", "w", "enter", "+", "+", "tab", "enter", "g", "p", "u", "enter", "esc", "tab", " ", "tab", "tab", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "add newrepo gpu max=3 public=true" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone || !strings.Contains(m.View(), "✔ added newrepo") {
		t.Fatalf("after add: overlay %v", m.overlay)
	}
}

// Two enters in one read (a paste, or tmux send-keys Enter Enter): the first
// picks and moves to Max, the second adds the repo.
func TestAddRepoDialogTwoEntersSubmit(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m, _ = pump(m, keys("n", "e", "w", "enter", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "add newrepo  max=- public=false" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone {
		t.Fatalf("overlay %v", m.overlay)
	}
}

func TestAddRepoDialogPicksWithTheMouse(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = click(t, m, "add/repo/row-1")
	if _, ok := m.add.picker.Picked(); ok {
		t.Fatal("a configured repository was picked")
	}
	m = click(t, m, "add/repo/row-2")
	v := m.View()
	lines := strings.Split(v, "\n")
	if row := strings.Join(strings.Fields(lines[lineWith(lines, "Repository")]), " "); !strings.Contains(row, "Repository immich") || !m.add.ok.Focusable() {
		t.Fatalf("picked row not shown, or Add still disabled:\n%s", v)
	}
	m = click(t, m, addOK)
	if got := strings.Join(c.actions(), "|"); got != "add immich  max=- public=false" {
		t.Fatalf("actions %q", got)
	}
}

// The wheel over the list scrolls it; elsewhere in the dialog it does nothing.
func TestAddRepoDialogWheelScrollsTheList(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 12 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	m := feed(sampleModel(&fakeClient{avail: many}, 120, 30), key("a"))
	z := zoneOf(t, m, "add/repo/row-0")
	m = feed(m, tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if v := m.View(); strings.Contains(v, "repo00") || !strings.Contains(v, "repo08") {
		t.Fatalf("wheel did not scroll the list:\n%s", v)
	}
	m = feed(m, tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if v := m.View(); strings.Contains(v, "repo00") || m.overlay != ovAddRepo {
		t.Fatalf("a wheel outside the list moved it:\n%s", v)
	}
}

// On a screen too short for the whole list, the picker shrinks; the wheel must
// still reach the last repository.
func TestAddRepoDialogWheelReachesTheLastRepositoryOnAShortScreen(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 20 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	m := feed(sampleModel(&fakeClient{avail: many}, 120, 22), key("a"))
	z := zoneOf(t, m, addRepo)
	for range 30 {
		m = feed(m, tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	}
	if v := m.View(); !strings.Contains(v, "repo19") {
		t.Fatalf("the wheel never reached the last repository:\n%s", v)
	}
}

func TestAddRepoDialogLoadingAndRetry(t *testing.T) {
	c := &fakeClient{availErr: errors.New("GitHub is rejecting the token")}
	upd, cmd := sampleModel(c, 120, 30).Update(key("a"))
	m := upd.(Model)
	if v := m.View(); !strings.Contains(v, "loading repositories…") || m.add.ok.Focusable() {
		t.Fatalf("loading:\n%s", v)
	}
	m = feed(m, collect(cmd)...)
	v := m.View()
	if !strings.Contains(v, "✖ GitHub is rejecting the token") || !strings.Contains(v, "( Retry )") || m.add.ok.Focusable() {
		t.Fatalf("error:\n%s", v)
	}
	if m.add.group.FocusedID() != addRetry {
		t.Fatalf("focus %q", m.add.group.FocusedID())
	}
	c.availErr, c.avail = nil, ownerRepos()
	m = feed(m, key("enter"))
	if v := m.View(); c.availCalls != 2 || !strings.Contains(v, "booklore") || strings.Contains(v, "( Retry )") {
		t.Fatalf("retry: %d calls\n%s", c.availCalls, v)
	}
	if m.add.group.FocusedID() != addRepo {
		t.Fatalf("focus after retry %q", m.add.group.FocusedID())
	}
}

// A list reply for a dialog that was cancelled never fills the one opened after it.
func TestAddRepoLateListReplyDropped(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	upd, cmd := sampleModel(c, 120, 30).Update(key("a"))
	m := feed(upd.(Model), ui.Pressed{ID: addCancel})
	upd, _ = m.Update(key("a"))
	m = upd.(Model)
	fresh := m.add
	m = feed(m, collect(cmd)...)
	if m.add != fresh || !fresh.loading || len(fresh.picker.Options) != 0 {
		t.Fatalf("late list reached the new dialog: loading %v, %d options", fresh.loading, len(fresh.picker.Options))
	}
}

// Picking a public repository leaves Allow public off; the daemon refuses it.
func TestAddRepoPublicStillNeedsTheToggle(t *testing.T) {
	c := &fakeClient{avail: ownerRepos(), addErr: errors.New("booklore is public; self-hosted runners must only serve private repos (pass --allow-public to override)")}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = feed(m, keys("b", "o", "o", "k", "enter")...)
	m = feed(m, ui.Pressed{ID: addOK})
	if got := strings.Join(c.actions(), "|"); got != "add booklore  max=- public=false" {
		t.Fatalf("actions %q", got)
	}
	if v := m.View(); m.overlay != ovAddRepo || !strings.Contains(v, "✖ booklore is public;") {
		t.Fatalf("refusal not shown in the dialog:\n%s", v)
	}
}

func TestAddRepoDialogErrorsStayInside(t *testing.T) {
	// The daemon's own message (internal/daemon/backend.go), on an 80-column screen.
	c := &fakeClient{avail: ownerRepos(), addErr: errors.New("site is public; self-hosted runners must only serve private repos (pass --allow-public to override)")}
	m := feed(sampleModel(c, 80, 30), key("a"))
	if m = click(t, m, addCancel); m.overlay != ovNone {
		t.Fatal("cancel did not close")
	}
	m = feed(m, key("a"))
	m = feed(m, keys("s", "i", "t", "e", "enter")...)
	upd, cmd := m.Update(ui.Pressed{ID: addOK})
	m = upd.(Model)
	if !strings.Contains(m.View(), "[ Adding… ]") || m.add.group.FocusedID() == addOK {
		t.Fatalf("Add not disabled while in flight, or still focused (%q)", m.add.group.FocusedID())
	}
	if _, again := m.Update(ui.Pressed{ID: addOK}); again != nil {
		t.Fatal("a second add was sent while one was in flight")
	}
	m = feed(m, collect(cmd)...)
	v := m.View()
	if m.overlay != ovAddRepo || !strings.Contains(v, "✖ site is public;") || !strings.Contains(v, "--allow-public") || m.add.busy {
		t.Fatalf("rejection: overlay %v\n%s", m.overlay, v)
	}
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d is %d wide:\n%s", i, lipgloss.Width(line), v)
		}
	}
	if got := strings.Join(c.actions(), "|"); got != "add site  max=- public=false" {
		t.Fatalf("untouched max sent: %q", got)
	}
}

// A reply to a dialog that was cancelled never touches the dialog opened after it.
func TestAddRepoLateReplyIgnoresNewDialog(t *testing.T) {
	c := &fakeClient{avail: ownerRepos(), addErr: errors.New("repo site is already configured")}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = feed(m, keys("s", "i", "t", "e", "enter")...)
	upd, cmd := m.Update(ui.Pressed{ID: addOK})
	m = feed(upd.(Model), ui.Pressed{ID: addCancel}, key("a"))
	fresh := m.add
	m = feed(m, collect(cmd)...)
	// The open dialog hides the toast line, so check the toast itself.
	if m.add != fresh || fresh.err != "" || fresh.busy || m.toast.Text != "repo site is already configured" {
		t.Fatalf("late reply reached the new dialog: err %q busy %v", fresh.err, fresh.busy)
	}
}

// The dialog fits the narrowest supported screen, long names cut short.
func TestAddRepoDialogFitsNarrowScreen(t *testing.T) {
	c := &fakeClient{avail: append(ownerRepos(), model.AvailableRepo{Name: strings.Repeat("a-very-long-repository-name-", 3), Private: true})}
	m := feed(sampleModel(c, 40, 30), key("a"))
	v := m.View()
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line %d is %d wide:\n%s", i, lipgloss.Width(line), v)
		}
	}
	if !strings.Contains(v, "Add repository") || !strings.Contains(v, "[ Add ]") || !strings.Contains(v, "a-very-long") {
		t.Fatalf("dialog incomplete:\n%s", v)
	}
}

func TestAddRepoDialogGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(sampleModel(&fakeClient{avail: ownerRepos()}, w, 30), key("a"))
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

// With many repositories the dialog still fits the 22-row minimum screen.
func TestAddRepoDialogFitsShortScreen(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 20 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	for _, h := range []int{22, 30} {
		m := feed(sampleModel(&fakeClient{avail: many}, 120, h), key("a"))
		fits(t, "add repository", m.View(), 120, h)
		if v := m.View(); !strings.Contains(v, "more, ↑↓ to scroll") || !strings.Contains(v, "[ Add ]") {
			t.Fatalf("height %d:\n%s", h, v)
		}
	}
}

// On a narrow, short screen, with an error and many repositories, the list
// gives up rows so the dialog and its buttons still fit.
func TestAddRepoDialogFitsNarrowShortScreenWithAnError(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 20 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	c := &fakeClient{avail: many, addErr: errors.New("token cannot see darkraise/repo00: add it to the PAT's repository access first")}
	m := feed(sampleModel(c, 40, 30), key("a"))
	m = feed(m, key("enter"), ui.Pressed{ID: addOK})
	for _, h := range []int{30, 28} {
		m = feed(m, tea.WindowSizeMsg{Width: 40, Height: h})
		v := m.View()
		fits(t, "add repository", v, 40, h)
		if !strings.Contains(v, "[ Add ]") || !strings.Contains(v, "✖ token cannot see") || !strings.Contains(v, "more, ↑↓ to scroll") {
			t.Fatalf("height %d:\n%s", h, v)
		}
	}
}
