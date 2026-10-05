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

	addRepo   = "add/repo"
	addRetry  = "add/retry"
	addMax    = "add/max"
	addLabels = "add/labels"
	addPublic = "add/public"
	addOK     = "add/ok"
	addCancel = "add/cancel"
)

// pickerRows is how many repositories the Add dialog's picker shows at once.
const pickerRows = 8

// addRepoDialog is the Add repository form. The repository is picked from
// the ones the daemon lists as available.
type addRepoDialog struct {
	picker            *ui.Picker
	max               *ui.Stepper
	labels            *ui.TagList
	public            *ui.Toggle
	ok, cancel, retry *ui.Button
	group             ui.Group
	loading           bool   // the repository list is being fetched
	listErr           string // why the list could not be fetched
	err               string // the daemon's rejection, shown inside the dialog
	busy              bool
}

// addedMsg and availMsg carry the dialog that sent the request, so a late
// reply never changes a dialog opened after it.
type (
	addedMsg struct {
		d    *addRepoDialog
		name string
		err  error
	}
	availMsg struct {
		d     *addRepoDialog
		repos []model.AvailableRepo
		err   error
	}
)

func (m Model) openAddRepo() (tea.Model, tea.Cmd) {
	if !m.connected {
		m.toast.Show("the daemon is unreachable; add the repository once it reconnects", true, m.now())
		return m, nil
	}
	d := &addRepoDialog{
		picker:  ui.NewPicker(addRepo, pickerRows),
		max:     ui.NewStepper(addMax, 0, 99, 1),
		labels:  ui.NewTagList(addLabels),
		public:  ui.NewToggle(addPublic, false),
		ok:      ui.NewButton(addOK, "Add", ui.Primary),
		cancel:  ui.NewButton(addCancel, "Cancel", ui.Secondary),
		retry:   ui.NewButton(addRetry, "Retry", ui.Secondary),
		loading: true,
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
	return m, m.fetchAvailable(d)
}

// fetchAvailable asks the daemon which repositories d can offer.
func (m Model) fetchAvailable(d *addRepoDialog) tea.Cmd {
	c := m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		rs, err := c.AvailableRepos(cx)
		return availMsg{d, rs, err}
	}
}

// gotAvailable fills the picker, or shows why the list failed. Focus moves
// to the picker, or to Retry, unless it has left Max since the dialog opened.
func (m Model) gotAvailable(msg availMsg) (tea.Model, tea.Cmd) {
	d := m.add
	if m.overlay != ovAddRepo || d != msg.d {
		return m, nil
	}
	d.loading = false
	if msg.err != nil {
		d.listErr = errText(msg.err)
	} else {
		d.listErr = ""
		opts := make([]ui.PickOption, len(msg.repos))
		for i, r := range msg.repos {
			vis := ui.Badge("private", ui.BadgeMuted)
			if !r.Private {
				vis = ui.Badge("public", ui.BadgeWarn)
			}
			opts[i] = ui.PickOption{Label: clean(r.Name), Badge: vis, Disabled: r.Configured, Note: "added"}
		}
		d.picker.SetOptions(opts)
	}
	first := d.group.FocusedID() == addMax
	d.sync(m.connected)
	if first {
		d.group.Focus(d.group.Items()[0].ID())
	}
	return m, nil
}

// retryAvailable fetches the repository list again after a failure.
func (m Model) retryAvailable() (tea.Model, tea.Cmd) {
	d := m.add
	if d == nil || d.loading || m.offline() {
		return m, nil
	}
	d.loading, d.listErr = true, ""
	d.sync(m.connected)
	return m, m.fetchAvailable(d)
}

// sync updates the Add button (disabled until a repository is picked, while
// a request is in flight or while the daemon is unreachable) and the focus
// order, so focus never rests on a disabled control.
func (d *addRepoDialog) sync(connected bool) {
	d.ok.Label = "Add"
	if d.busy {
		d.ok.Label = "Adding…"
	}
	_, picked := d.picker.Picked()
	d.ok.SetDisabled(d.busy || !connected || !picked)
	d.retry.SetDisabled(!connected)
	var ws []ui.Widget
	switch {
	case d.listErr != "":
		ws = append(ws, d.retry)
	case !d.loading:
		ws = append(ws, d.picker)
	}
	d.group.Set(append(ws, d.max, d.labels, d.public, d.cancel, d.ok))
}

// addRepoKey routes a key in the Add repository dialog: the focused control
// first (enter on a button presses that button), then esc cancels, tab and
// the arrows move focus, and enter adds.
func (m Model) addRepoKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.add
	// Keys read together arrive without a frame between them, so a pick
	// must enable Add before the next key is routed.
	d.sync(m.connected)
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
	o, ok := d.picker.Picked()
	if !ok {
		d.err = "pick a repository first"
		return m, nil
	}
	name := o.Label
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
	// The list gives up rows, keeping one, until the dialog fits the screen.
	// Each trial starts from the window the user left, because rendering
	// clamps it to the trial's Rows.
	cursor, top := d.picker.Window()
	render := func(rows int) string {
		d.picker.Rows = rows
		d.picker.SetWindow(cursor, top)
		return m.addRepoModal(w, rw)
	}
	rows := pickerRows
	view := render(rows)
	for rows > 1 && lipgloss.Height(view) > m.height {
		rows--
		view = render(rows)
	}
	return view
}

// addRepoModal renders the Add repository dialog with the picker's Rows.
func (m Model) addRepoModal(w, rw int) string {
	d := m.add
	f := d.group.FocusedID()
	picked := sDim.Render("pick one below")
	if o, ok := d.picker.Picked(); ok {
		picked = sBold.Render(o.Label)
	}
	rows := []ui.Row{{Label: "Repository", Text: picked}}
	switch {
	case d.loading:
		rows = append(rows, ui.Row{Text: sDim.Render("loading repositories…")})
	case d.listErr != "":
		rows = append(rows, ui.Row{Lines: []string{sRed.Render("✖ " + d.listErr)}}, ui.Row{Items: []ui.Widget{d.retry}})
	default:
		rows = append(rows, ui.Row{Items: []ui.Widget{d.picker}})
	}
	rows = append(rows,
		ui.Row{Label: "Max", Items: []ui.Widget{d.max}},
		ui.Row{Label: "Labels", Items: []ui.Widget{d.labels}},
		ui.Row{Label: "Allow public repo", Items: []ui.Widget{d.public}},
	)
	lines, _ := ui.Render([]ui.Section{{Rows: rows}}, f, rw, false)
	body := strings.Join(lines, "\n") + "\n" + sAmber.Render("⚠ self-hosted runners on a public repo can run anyone's code")
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(rw).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Add repository", body, []*ui.Button{d.cancel, d.ok}, f, w)
}

// openDialog shows overlay ov with buttons, right-aligned in the given order.
// Focus starts on the last button, the primary one; esc presses cancel.
func (m *Model) openDialog(ov overlay, cancel string, buttons ...*ui.Button) {
	if m.overlay == ovToken && m.tok != nil {
		m.tok.field.SetValue(ui.Value{})
		m.tok = nil
	}
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

// leave goes to t. Leaving a config page (Settings or Repositories) with
// unsaved changes first asks whether to save them, discard them or stay;
// ctrl+c never comes here.
func (m Model) leave(t leaveTarget) (tea.Model, tea.Cmd) {
	onConfig := m.page == pageSettings || m.page == pageRepos
	staying := !t.quit && t.page == m.page
	if onConfig && !staying {
		cp := m.configPage(m.page)
		if cp.saving {
			// The save decides: its result leaves or stays (see refetched).
			m.toast.Show("wait for the save to finish", true, m.now())
			return m, nil
		}
		if len(cp.form.Dirty()) > 0 {
			m.leaveTo, m.leaveFrom = t, m.page
			m.openDialog(ovUnsaved, btnLeaveStay, ui.NewButton(btnLeaveStay, "Stay", ui.Secondary),
				ui.NewButton(btnLeaveDiscard, "Discard", ui.Secondary), ui.NewButton(btnLeaveSave, "Save", ui.Primary))
			return m, nil
		}
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
	m.helpScroll = 0
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
	return m, nil
}

// dialogKey handles a key while a button dialog is open: enter and space
// press the focused button (the primary at first), esc presses cancel, and
// tab or the arrow keys move between the buttons. A confirmation also takes
// y and n.
func (m Model) dialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.overlay == ovHelp {
		if d, ok := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10}[k.String()]; ok {
			lines, n := m.helpLines(max(m.width, 40))
			m.helpScroll = min(max(m.helpScroll+d, 0), max(len(lines)-n, 0))
			return m, nil
		}
	}
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

// buttonOverlay is the dialog a button belongs to; page buttons belong to ovNone.
func buttonOverlay(id string) overlay {
	switch id {
	case btnYes, btnNo:
		return ovConfirm
	case btnClose:
		return ovHelp
	case btnLeaveSave, btnLeaveDiscard, btnLeaveStay:
		return ovUnsaved
	case addOK, addCancel, addRetry:
		return ovAddRepo
	case tokOK, tokCancel:
		return ovToken
	}
	return ovNone
}

// pressed runs the action of the button with id. A press arrives as a
// message, so a double click can deliver one after its dialog has closed or
// once another has opened; it acts only while the button's own dialog shows.
func (m Model) pressed(id string) (tea.Model, tea.Cmd) {
	if buttonOverlay(id) != m.overlay {
		return m, nil
	}
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
		cp := m.configPage(m.leaveFrom)
		cp.form.Discard()
		cp.alert = nil
		return m.goTo(m.leaveTo)
	case btnLeaveSave:
		// Leave only once the save succeeds; refetched does the navigation.
		m.overlay = ovNone
		upd, cmd := m.saveConfig(m.leaveFrom)
		m = upd.(Model)
		m.leaving = cmd != nil
		return m, cmd
	case addCancel:
		m.overlay, m.add = ovNone, nil
	case addOK:
		return m.submitAddRepo()
	case addRetry:
		return m.retryAvailable()
	case dashAdd:
		return m.openAddRepo()
	case dashPauseAll:
		return m, m.togglePauseAll()
	case detailCopy, detailStop, detailBack:
		return m.detailPressed(id)
	case reposAdd, reposAddEmpty:
		return m.openAddRepo()
	case reposPause, reposRemove:
		return m.reposAction(id)
	case setSave:
		return m.saveSettings()
	case setDiscard:
		m.settings.form.Discard()
		m.settings.alert = nil
	case reposSave:
		return m.saveRepos()
	case reposDiscard:
		m.repos.form.Discard()
		m.repos.alert = nil
	case setReload:
		return m.reloadConfig()
	case setPrune:
		return m.startPrune()
	case setRunnerQueue:
		return m.queueRunnerUpdate()
	case setRunnerCancel:
		return m.cancelRunnerUpdate()
	case setReplaceToken:
		return m.openTokenDialog()
	case setTokenRetry:
		return m, m.fetchToken()
	case tokOK:
		return m.submitToken()
	case tokCancel:
		if m.tok != nil {
			m.tok.field.SetValue(ui.Value{})
		}
		m.overlay, m.tok = ovNone, nil
	case reposRegRefresh:
		return m, m.fetchRegs()
	case reposLCCheck:
		return m.startLabelCheck()
	case reposActRetry:
		return m, m.fetchActivity()
	default:
		if label, ok := lcAddLabel(id); ok {
			if !m.offline() {
				m.addLabel(label)
			}
			return m, nil
		}
		if strings.HasPrefix(id, "repos/reg/del/") {
			return m.confirmDeleteReg(id)
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
	// The buttons wrap onto more rows when one row would not fit inside.
	var rows []string
	for _, b := range buttons {
		v := b.View(b.ID() == focused, 0)
		if n := len(rows); n > 0 && lipgloss.Width(rows[n-1]+"  "+v) <= inner {
			rows[n-1] += "  " + v
		} else {
			rows = append(rows, v)
		}
	}
	w := max(lipgloss.Width(title), lipgloss.Width(body))
	for _, r := range rows {
		w = max(w, lipgloss.Width(r))
	}
	for i, r := range rows {
		rows[i] = strings.Repeat(" ", w-lipgloss.Width(r)) + r
	}
	return sDialog.Render(sBold.Render(title) + "\n\n" + body + "\n\n" + strings.Join(rows, "\n"))
}

type helpGroup struct {
	title string
	keys  [][2]string // key, what it does
}

// helpGroups in reading order. They render as four columns: Global over
// Detail, Dashboard over History, Repositories over Settings, then Runner
// rows. No column exceeds ten rows, so the dialog fits the 22-row minimum.
var helpGroups = []helpGroup{
	{"Global", [][2]string{{"1-5", "switch page"}, {"tab", "move focus"}, {"↑↓ j k", "move selection"}, {"? / q", "help / quit"}}},
	{"Dashboard", [][2]string{{"h / →", "repos / runners"}, {"p / P", "pause repo/all"}, {"+ - [ ]", "repo/global cap"}, {"m", "queue/all mode"}, {"a d e", "add/remove/edit"}}},
	{"Repositories", [][2]string{{"a / d", "add / remove"}, {"p", "pause / resume"}, {"ctrl+s", "save"}}},
	// The Runners page and the Dashboard's Runners card share these keys.
	{"Runner rows", [][2]string{{"enter", "open details"}, {"l / x", "log / stop"}, {"pgup/dn", "scroll the log"}}},
	{"Detail", [][2]string{{"← / →", "switch tab"}, {"x / esc", "stop / back"}, {"pgup/dn", "scroll the list"}}},
	{"History", [][2]string{{"r / c", "repo / result"}, {"enter", "copy run URL"}}},
	{"Settings", [][2]string{{"ctrl+s", "save"}, {"← / →", "choose / step"}, {"enter", "open / edit"}, {"esc", "close / stop"}}},
}

const helpColW = 23 // an 8-column key and a 15-column description

// helpText lists the keys by group in four columns, stacked into one when
// inner columns cannot hold four, followed by the notes on digits, the mouse
// and tmux.
func helpText(inner int) string {
	col := func(gs ...helpGroup) []string {
		var out []string
		for i, g := range gs {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, sAccent.Render(cell(g.title, helpColW)))
			for _, k := range g.keys {
				out = append(out, sBold.Render(cell(k[0], 8))+cell(k[1], helpColW-8))
			}
		}
		return out
	}
	g := helpGroups
	var lines []string
	if inner < 4*helpColW+3*2 {
		lines = col(g...)
	} else {
		cols := [][]string{col(g[0], g[4]), col(g[1], g[5]), col(g[2], g[6]), col(g[3])}
		rows := 0
		for _, c := range cols {
			rows = max(rows, len(c))
		}
		for i := 0; i < rows; i++ {
			row := make([]string, len(cols))
			for c := range cols {
				if i < len(cols[c]) {
					row[c] = cols[c][i]
				}
				row[c] = cell(row[c], helpColW)
			}
			lines = append(lines, strings.TrimRight(strings.Join(row, "  "), " "))
		}
	}
	return strings.Join(append(lines, "",
		"A focused stepper takes digits: 1-5 type into it instead of switching.",
		"Mouse: click anything; double-click a runner; the wheel scrolls.",
		"Shift-drag selects text (Option-drag in iTerm2). tmux: set -g mouse on",
	), "\n")
}

// helpLines is the Help text wrapped for a w-column screen, and how many of
// its lines the dialog can show: all of them when they fit the height,
// otherwise one less than the room, leaving a line for the scroll hint.
func (m Model) helpLines(w int) ([]string, int) {
	inner := max(w-6, 20) // as modal computes it
	text := helpText(inner)
	if lipgloss.Width(text) > inner {
		text = lipgloss.NewStyle().Width(inner).Render(text)
	}
	lines := strings.Split(text, "\n")
	room := max(m.height-8, 3) // the dialog's border, padding, title and buttons take 8 rows
	if len(lines) <= room {
		return lines, len(lines)
	}
	return lines, room - 1
}

// helpBody is the part of the Help text that fits the screen, with a hint
// line when ↑/↓ or pgup/pgdn can scroll the rest into view.
func (m Model) helpBody(w int) string {
	lines, n := m.helpLines(w)
	if n == len(lines) {
		return strings.Join(lines, "\n")
	}
	start := min(m.helpScroll, len(lines)-n)
	hint := sDim.Render(fmt.Sprintf("↑↓ scroll · lines %d-%d of %d", start+1, start+n, len(lines)))
	return strings.Join(append(lines[start:start+n:start+n], hint), "\n")
}

// withOverlay replaces the screen with the open dialog, centred on a blank
// canvas the size of base; base itself is not drawn behind it.
func (m Model) withOverlay(base string, w int) string {
	var dialog string
	switch m.overlay {
	case ovConfirm:
		dialog = modal("Confirm", m.confirmText, m.dlgButtons, m.dlg.FocusedID(), w)
	case ovHelp:
		dialog = modal("Keys", m.helpBody(w), m.dlgButtons, m.dlg.FocusedID(), w)
	case ovUnsaved:
		n := len(m.configPage(m.leaveFrom).form.Dirty())
		where := pageNames[m.leaveFrom]
		body := fmt.Sprintf("You have %d unsaved changes on the %s page.", n, where)
		if n == 1 {
			body = fmt.Sprintf("You have 1 unsaved change on the %s page.", where)
		}
		dialog = modal("Unsaved changes", body, m.dlgButtons, m.dlg.FocusedID(), w)
	case ovAddRepo:
		dialog = m.addRepoView(w)
	case ovToken:
		dialog = m.tokenView(w)
	}
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
}
