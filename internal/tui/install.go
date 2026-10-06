package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	instTool   = "install/tool"
	instPick   = "install/version"
	instRetry  = "install/retry"
	instOK     = "install/ok"
	instCancel = "install/cancel"
)

// availTimeout bounds GET /toolchains/available: on a cold cache the daemon
// fetches the tool's release index first.
const availTimeout = 30 * time.Second

var toolOptions = []ui.Option{
	{Value: "node", Label: "Node.js"},
	{Value: "python", Label: "Python"},
	{Value: "go", Label: "Go"},
	{Value: "java", Label: "Java (Temurin)"},
	{Value: "dotnet", Label: ".NET SDK"},
}

// installDialog is the Install toolchain form: a tool, then a version picked
// from the ones the daemon offers or typed as is.
type installDialog struct {
	tool              *ui.Select
	picker            *ui.Picker
	ok, cancel, retry *ui.Button
	group             ui.Group
	shown             string // the tool whose versions the picker holds or is fetching
	choices           []model.ToolchainChoice
	loading           bool
	listErr           string // why the versions could not be fetched
	err               string // the daemon's rejection of the install
	busy              bool
}

// choicesMsg and installedMsg carry the dialog that sent the request, so a
// late reply never changes a dialog opened after it.
type (
	choicesMsg struct {
		d    *installDialog
		tool string
		cs   []model.ToolchainChoice
		err  error
	}
	installedMsg struct {
		d      *installDialog
		target string
		err    error
	}
)

func (m Model) openInstall() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	d := &installDialog{
		tool:   ui.NewSelect(instTool, toolOptions),
		picker: ui.NewPicker(instPick, pickerRows),
		ok:     ui.NewButton(instOK, "Install", ui.Primary),
		cancel: ui.NewButton(instCancel, "Cancel", ui.Secondary),
		retry:  ui.NewButton(instRetry, "Retry", ui.Secondary),
	}
	m.inst, m.overlay = d, ovInstall
	return m, m.fetchChoices(d)
}

// fetchChoices loads the versions the selected tool offers into d.
func (m Model) fetchChoices(d *installDialog) tea.Cmd {
	tool := d.tool.Value().Text
	if tool != d.shown {
		d.picker = ui.NewPicker(instPick, pickerRows)
	}
	d.shown, d.loading, d.listErr, d.choices = tool, true, "", nil
	d.picker.SetOptions(nil)
	d.sync(m.connected)
	c := m.c
	return func() tea.Msg {
		cx, cancel := context.WithTimeout(context.Background(), availTimeout)
		defer cancel()
		cs, err := c.AvailableToolchains(cx, tool)
		return choicesMsg{d, tool, cs, err}
	}
}

func (m Model) gotChoices(msg choicesMsg) (tea.Model, tea.Cmd) {
	d := m.inst
	if m.overlay != ovInstall || d != msg.d || msg.tool != d.shown {
		return m, nil
	}
	d.loading = false
	if msg.err != nil {
		d.listErr = errText(msg.err)
	} else {
		d.choices = make([]model.ToolchainChoice, len(msg.cs))
		opts := make([]ui.PickOption, len(msg.cs))
		for i, c := range msg.cs {
			c.Spec, c.Version = clean(c.Spec), clean(c.Version)
			d.choices[i] = c
			opts[i] = ui.PickOption{Label: c.Version}
			if c.LTS {
				opts[i].Badge = ui.Badge("lts", ui.BadgeOK)
			}
		}
		d.picker.SetOptions(opts)
	}
	d.sync(m.connected)
	return m, nil
}

func (m Model) retryChoices() (tea.Model, tea.Cmd) {
	d := m.inst
	if d == nil || d.loading || m.offline() {
		return m, nil
	}
	return m, m.fetchChoices(d)
}

// target is the version Install sends: the picked choice's spec, else the
// filter as typed, so a partial version such as 22 installs as is.
func (d *installDialog) target() string {
	if o, ok := d.picker.Picked(); ok {
		for _, c := range d.choices {
			if c.Version == o.Label {
				return c.Spec
			}
		}
	}
	return strings.TrimSpace(d.picker.Filter())
}

// sync updates Install (disabled until there is a version, while a request
// is in flight or while the daemon is unreachable) and the focus order, so
// focus never rests on a disabled control.
func (d *installDialog) sync(connected bool) {
	d.ok.Label = "Install"
	if d.busy {
		d.ok.Label = "Queuing…"
	}
	d.ok.SetDisabled(d.busy || !connected || d.target() == "")
	d.retry.SetDisabled(!connected)
	d.tool.SetDisabled(d.busy)
	ws := []ui.Widget{d.tool}
	switch {
	case d.listErr != "":
		ws = append(ws, d.retry)
	case !d.loading:
		ws = append(ws, d.picker)
	}
	d.group.Set(append(ws, d.cancel, d.ok))
}

// installKey routes a key in the Install dialog: enter on a button presses
// it, then the focused control, then esc cancels, tab and the arrows move
// focus, and enter installs. A new tool fetches its versions.
func (m Model) installKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.inst
	d.sync(m.connected)
	if _, onButton := d.group.Focused().(*ui.Button); onButton && k.String() == "enter" {
		return m.pressed(d.group.FocusedID())
	}
	before := d.tool.Value().Text
	if ok, cmd := d.group.Key(k); ok {
		if d.tool.Value().Text != before {
			return m, tea.Batch(cmd, m.fetchChoices(d))
		}
		return m, cmd
	}
	switch k.String() {
	case "esc":
		return m.pressed(instCancel)
	case "tab", "down":
		d.group.Next()
	case "shift+tab", "up":
		d.group.Prev()
	case "enter":
		return m.pressed(instOK)
	}
	return m, nil
}

// installMouse handles the wheel over the version list and clicks in the dialog.
func (m Model) installMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	d := m.inst
	d.sync(m.connected)
	wheel := msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown)
	if wheel && d.picker.Hit(msg) {
		d.picker.Update(msg)
		return m, nil
	}
	before := d.tool.Value().Text
	_, cmd := d.group.Mouse(msg)
	if d.tool.Value().Text != before {
		return m, tea.Batch(cmd, m.fetchChoices(d))
	}
	return m, cmd
}

func (m Model) submitInstall() (tea.Model, tea.Cmd) {
	d := m.inst
	if d == nil || d.busy {
		return m, nil
	}
	if !m.connected {
		d.err = "the daemon is unreachable"
		return m, nil
	}
	spec := d.target()
	if spec == "" {
		d.err = "pick or type a version first"
		return m, nil
	}
	tool := d.tool.Value().Text
	d.busy, d.err = true, ""
	d.sync(m.connected)
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return installedMsg{d, tool + " " + spec, c.InstallToolchain(cx, tool, spec)}
	}
}

// installed closes the dialog once the install is queued. A rejection stays
// inside the open dialog.
func (m Model) installed(msg installedMsg) (tea.Model, tea.Cmd) {
	mine := m.overlay == ovInstall && m.inst == msg.d
	if msg.err != nil {
		if mine {
			m.inst.busy, m.inst.err = false, errText(msg.err)
			m.inst.sync(m.connected)
			return m, nil
		}
		m.toast.Show(errText(msg.err), true, m.now())
		return m, nil
	}
	if mine {
		m.overlay, m.inst = ovNone, nil
	}
	m.toast.Show("queued: install "+msg.target, false, m.now())
	return m, m.fetchStorage()
}

// installView renders the dialog to fit a screen w columns wide, giving up
// version rows until it fits the height.
func (m Model) installView(w int) string {
	d := m.inst
	d.sync(m.connected)
	rw := max(min(72, w-14), 26)
	cursor, top := d.picker.Window()
	render := func(rows int) string {
		d.picker.Rows = rows
		d.picker.SetWindow(cursor, top)
		return m.installModal(w, rw)
	}
	rows := pickerRows
	view := render(rows)
	for rows > 1 && lipgloss.Height(view) > m.height {
		rows--
		view = render(rows)
	}
	return view
}

func (m Model) installModal(w, rw int) string {
	d := m.inst
	f := d.group.FocusedID()
	version := sDim.Render("pick one below, or type a version")
	if t := d.target(); t != "" {
		version = sBold.Render(t)
	}
	rows := []ui.Row{{Label: "Tool", Items: []ui.Widget{d.tool}}, {Label: "Version", Text: version}}
	switch {
	case d.loading:
		rows = append(rows, ui.Row{Text: sDim.Render("loading versions…")})
	case d.listErr != "":
		rows = append(rows, ui.Row{Lines: []string{sRed.Render("✖ " + d.listErr)}}, ui.Row{Items: []ui.Widget{d.retry}})
	default:
		rows = append(rows, ui.Row{Items: []ui.Widget{d.picker}})
	}
	lines, _ := ui.Render([]ui.Section{{Rows: rows}}, f, rw, false)
	body := strings.Join(lines, "\n")
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(rw).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Install toolchain", body, []*ui.Button{d.cancel, d.ok}, f, w)
}
