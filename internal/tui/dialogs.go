package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	btnYes   = "dialog/yes"
	btnNo    = "dialog/no"
	btnClose = "dialog/close"

	btnLeaveSave    = "unsaved/save"
	btnLeaveDiscard = "unsaved/discard"
	btnLeaveStay    = "unsaved/stay"

	addName   = "add/name"
	addMax    = "add/max"
	addLabels = "add/labels"
	addPublic = "add/public"
	addOK     = "add/ok"
	addCancel = "add/cancel"
)

// addRepoDialog is the Add repository form.
type addRepoDialog struct {
	name       *ui.TextField
	max        *ui.Stepper
	labels     *ui.TagList
	public     *ui.Toggle
	ok, cancel *ui.Button
	group      ui.Group
	err        string // the daemon's rejection, shown inside the dialog
	busy       bool
}

// addedMsg carries the dialog that sent the request, so a late reply never
// changes a dialog opened after it.
type addedMsg struct {
	d    *addRepoDialog
	name string
	err  error
}

func (m Model) openAddRepo() (tea.Model, tea.Cmd) {
	if !m.connected {
		m.toast.Show("the daemon is unreachable; add the repository once it reconnects", true, m.now())
		return m, nil
	}
	d := &addRepoDialog{
		name:   ui.NewTextField(addName, 30),
		max:    ui.NewStepper(addMax, 0, 99, 1),
		labels: ui.NewTagList(addLabels),
		public: ui.NewToggle(addPublic, false),
		ok:     ui.NewButton(addOK, "Add", ui.Primary),
		cancel: ui.NewButton(addCancel, "Cancel", ui.Secondary),
	}
	// Max shows the default a new repo gets until it is touched; untouched sends no max.
	d.max.ZeroText = "∞"
	d.max.SetValue(ui.Value{})
	d.max.Default = 1
	if m.st.Mode == config.ModeAll {
		d.max.Default, d.max.DefaultText = 0, "∞"
	}
	d.sync(true)
	m.add, m.overlay = d, ovAddRepo
	return m, nil
}

// sync updates the Add button (disabled while a request is in flight or the
// daemon is unreachable) and the focus order, so focus never rests on it
// while it is disabled.
func (d *addRepoDialog) sync(connected bool) {
	d.ok.Label = "Add"
	if d.busy {
		d.ok.Label = "Adding…"
	}
	d.ok.SetDisabled(d.busy || !connected)
	d.group.Set([]ui.Widget{d.name, d.max, d.labels, d.public, d.cancel, d.ok})
}

// addRepoKey routes a key in the Add repository dialog: the focused control
// first (enter on a button presses that button), then esc cancels, tab and
// the arrows move focus, and enter adds.
func (m Model) addRepoKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.add
	if _, onButton := d.group.Focused().(*ui.Button); onButton && k.String() == "enter" {
		return m.pressed(d.group.FocusedID())
	}
	if ok, cmd := d.group.Key(k); ok {
		return m, cmd
	}
	switch k.String() {
	case "esc":
		return m.pressed(addCancel)
	case "tab", "down":
		d.group.Next()
	case "shift+tab", "up":
		d.group.Prev()
	case "enter":
		return m.pressed(addOK)
	}
	return m, nil
}

func (m Model) submitAddRepo() (tea.Model, tea.Cmd) {
	d := m.add
	if d == nil || d.busy {
		return m, nil
	}
	if !m.connected {
		d.err = "the daemon is unreachable"
		return m, nil
	}
	name := d.name.Value().Text
	if name == "" {
		d.err = "name is required"
		return m, nil
	}
	req := model.AddRepoRequest{Name: name, Labels: d.labels.Value().List, AllowPublic: d.public.On}
	if v := d.max.Value(); v.Set {
		n := v.Num
		req.Max = &n
	}
	d.busy, d.err = true, ""
	d.sync(m.connected)
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return addedMsg{d, name, c.AddRepo(cx, req)}
	}
}

// added closes the dialog on success. A rejection (the token cannot see the
// repo, it is public, it is a duplicate) stays inside the open dialog.
func (m Model) added(msg addedMsg) (tea.Model, tea.Cmd) {
	mine := m.overlay == ovAddRepo && m.add == msg.d
	if msg.err != nil {
		if mine {
			m.add.busy, m.add.err = false, clean(msg.err.Error())
			m.add.sync(m.connected)
			return m, nil
		}
		m.toast.Show(clean(msg.err.Error()), true, m.now())
		return m, nil
	}
	if mine {
		m.overlay, m.add = ovNone, nil
	}
	m.toast.Show("added "+msg.name, false, m.now())
	return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())
}

// addRepoView renders the dialog to fit a screen w columns wide.
func (m Model) addRepoView(w int) string {
	d := m.add
	d.sync(m.connected)
	// The rows stay inside the modal's inner width (w-6) down to 40 columns.
	rw := max(min(72, w-14), 26)
	d.name.Width = max(min(30, rw-24), 8)
	f := d.group.FocusedID()
	lines, _ := ui.Render([]ui.Section{{Rows: []ui.Row{
		{Label: "Name", Items: []ui.Widget{d.name}, Desc: "a repository of the owner"},
		{Label: "Max", Items: []ui.Widget{d.max}},
		{Label: "Labels", Items: []ui.Widget{d.labels}},
		{Label: "Allow public repo", Items: []ui.Widget{d.public}},
	}}}, f, rw, false)
	body := strings.Join(lines, "\n") + "\n" + sAmber.Render("⚠ self-hosted runners on a public repo can run anyone's code")
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(rw).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Add repository", body, []*ui.Button{d.cancel, d.ok}, f, w)
}

// openDialog shows overlay ov with buttons, right-aligned in the given order.
// Focus starts on the last button, the primary one; esc presses cancel.
func (m *Model) openDialog(ov overlay, cancel string, buttons ...*ui.Button) {
	m.overlay, m.dlgButtons, m.dlgCancel = ov, buttons, cancel
	ws := make([]ui.Widget, len(buttons))
	for i, b := range buttons {
		ws[i] = b
	}
	m.dlg = ui.Group{}
	m.dlg.Set(ws)
	m.dlg.Focus(buttons[len(buttons)-1].ID())
}

func (m Model) openConfirm(text string, action func() tea.Cmd) (tea.Model, tea.Cmd) {
	m.confirmText, m.confirmAction = text, action
	m.openDialog(ovConfirm, btnNo, ui.NewButton(btnNo, "No", ui.Secondary), ui.NewButton(btnYes, "Yes", ui.Primary))
	return m, nil
}

// leave goes to t. Leaving Settings with unsaved changes first asks whether
// to save them, discard them or stay; ctrl+c never comes here.
func (m Model) leave(t leaveTarget) (tea.Model, tea.Cmd) {
	staying := !t.quit && t.page == pageSettings
	if m.page == pageSettings && !staying && m.settings.saving {
		// The save decides: its result leaves or stays (see refetched).
		m.toast.Show("wait for the save to finish", true, m.now())
		return m, nil
	}
	if m.page == pageSettings && !staying && len(m.settings.form.Dirty()) > 0 {
		m.leaveTo = t
		m.openDialog(ovUnsaved, btnLeaveStay, ui.NewButton(btnLeaveStay, "Stay", ui.Secondary),
			ui.NewButton(btnLeaveDiscard, "Discard", ui.Secondary), ui.NewButton(btnLeaveSave, "Save", ui.Primary))
		return m, nil
	}
	return m.goTo(t)
}

func (m Model) goTo(t leaveTarget) (tea.Model, tea.Cmd) {
	if t.quit {
		return m, tea.Quit
	}
	return m.switchPage(t.page)
}

func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
	return m, nil
}

// dialogKey handles a key while a button dialog is open: enter and space
// press the focused button (the primary at first), esc presses cancel, and
// tab or the arrow keys move between the buttons. A confirmation also takes
// y and n.
func (m Model) dialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		if id := m.dlg.FocusedID(); id != "" {
			return m.pressed(id)
		}
		return m.pressed(m.dlgButtons[len(m.dlgButtons)-1].ID())
	case "esc":
		return m.pressed(m.dlgCancel)
	case "tab", "right", "l":
		m.dlg.Next()
	case "shift+tab", "left", "h":
		m.dlg.Prev()
	case "y":
		if m.overlay == ovConfirm {
			return m.pressed(btnYes)
		}
	case "n":
		if m.overlay == ovConfirm {
			return m.pressed(btnNo)
		}
	default:
		_, cmd := m.dlg.Key(k)
		return m, cmd
	}
	return m, nil
}

// pressed runs the action of the button with id.
func (m Model) pressed(id string) (tea.Model, tea.Cmd) {
	switch id {
	case btnYes:
		m.overlay = ovNone
		return m, m.confirmAction()
	case btnNo, btnClose:
		m.overlay = ovNone
	case btnLeaveStay:
		m.overlay = ovNone
	case btnLeaveDiscard:
		m.overlay = ovNone
		m.settings.form.Discard()
		m.settings.alert = nil
		return m.goTo(m.leaveTo)
	case btnLeaveSave:
		// Leave only once the save succeeds; saved() does the navigation.
		m.overlay = ovNone
		upd, cmd := m.saveSettings()
		m = upd.(Model)
		m.leaving = cmd != nil
		return m, cmd
	case addCancel:
		m.overlay, m.add = ovNone, nil
	case addOK:
		return m.submitAddRepo()
	case setAddRepo, dashAdd:
		return m.openAddRepo()
	case dashPauseAll:
		return m, m.togglePauseAll()
	case detailCopy, detailStop, detailBack:
		return m.detailPressed(id)
	case setSave:
		return m.saveSettings()
	case setDiscard:
		m.settings.form.Discard()
		m.settings.alert = nil
	default:
		if strings.HasPrefix(id, "settings/repo/") {
			return m.repoAction(id)
		}
	}
	return m, nil
}

// modal renders a dialog no wider than maxW: a bold title, the body (wrapped
// when too wide), and the buttons aligned right.
func modal(title, body string, buttons []*ui.Button, focused string, maxW int) string {
	inner := max(maxW-6, 20) // the dialog's border and padding take 6 columns
	if lipgloss.Width(body) > inner {
		body = lipgloss.NewStyle().Width(inner).Render(body)
	}
	var bs []string
	for _, b := range buttons {
		bs = append(bs, b.View(b.ID() == focused, 0))
	}
	row := strings.Join(bs, "  ")
	w := max(lipgloss.Width(title), lipgloss.Width(body), lipgloss.Width(row))
	row = strings.Repeat(" ", w-lipgloss.Width(row)) + row
	return sDialog.Render(sBold.Render(title) + "\n\n" + body + "\n\n" + row)
}

func helpText() string {
	return strings.Join([]string{
		"1-4         switch page           ↑↓ / j k   move selection",
		"tab         move focus             ctrl+s     save settings",
		"h / →       focus repos / runners  p          pause/resume repo",
		"+ / -       repo cap               [ / ]      global cap",
		"m           toggle queue/all       P          pause/resume all",
		"a / d       add / remove repo      x          stop runner",
		"l           follow runner log      enter      details / copy URL",
		"pgup/pgdn   scroll                 q          quit",
		"",
		"A focused number field takes digits, so 1-4 type into it instead of switching pages.",
		"Mouse: click the sidebar, rows, buttons and footer keys; double-click a runner; wheel scrolls.",
		"Selecting terminal text needs Shift-drag (Option-drag in iTerm2).",
		"Under tmux, mouse input requires `set -g mouse on`.",
	}, "\n")
}

// withOverlay replaces the screen with the open dialog, centred on a blank
// canvas the size of base; base itself is not drawn behind it.
func (m Model) withOverlay(base string, w int) string {
	var dialog string
	switch m.overlay {
	case ovConfirm:
		dialog = modal("Confirm", m.confirmText, m.dlgButtons, m.dlg.FocusedID(), w)
	case ovHelp:
		dialog = modal("Keys", helpText(), m.dlgButtons, m.dlg.FocusedID(), w)
	case ovUnsaved:
		n := len(m.settings.form.Dirty())
		body := fmt.Sprintf("You have %d unsaved changes on the Settings page.", n)
		if n == 1 {
			body = "You have 1 unsaved change on the Settings page."
		}
		dialog = modal("Unsaved changes", body, m.dlgButtons, m.dlg.FocusedID(), w)
	case ovAddRepo:
		dialog = m.addRepoView(w)
	}
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
}
