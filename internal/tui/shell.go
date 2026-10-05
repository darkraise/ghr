package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	sidebarWidth = 16
	wideMin      = 100 // below this many columns a tab row replaces the sidebar
)

func navZone(p page) string { return "nav/" + strings.ToLower(pageNames[p]) }

// stop is a tab stop that is not a control, such as a table card: it takes
// no keys and no clicks of its own; the page acts on whatever has focus.
type stop struct{ id string }

func (s stop) ID() string                           { return s.id }
func (s stop) Update(tea.Msg) (ui.Control, tea.Cmd) { return s, nil }
func (s stop) View(bool, int) string                { return "" }
func (s stop) Focusable() bool                      { return true }
func (s stop) TakesKey(tea.KeyMsg) bool             { return false }
func (s stop) Capturing() bool                      { return false }
func (s stop) Hit(tea.MouseMsg) bool                { return false }
func (s stop) Blur()                                {}
func (s stop) SetDisabled(bool)                     {}

// pageGroups holds the focus groups of the pages other than Settings. The
// Model keeps it by pointer so focus survives Bubble Tea copying the Model.
type pageGroups struct {
	dash                ui.Group
	add, pauseAll       *ui.Button
	detail              ui.Group
	copyURL, stopRunner *ui.Button
	back                *ui.Button
	tabs                *tabStrip
	hist                ui.Group
	histRepo            *ui.Select
	histResult          *ui.Select
}

const (
	dashAdd      = "dash/add"
	dashPauseAll = "dash/pauseall"
	dashRepos    = "dash/repos"
	dashRunners  = "dash/runners"
)

func newPageGroups() *pageGroups {
	g := &pageGroups{
		add:        ui.NewButton(dashAdd, "+ Add", ui.Primary),
		pauseAll:   ui.NewButton(dashPauseAll, "Pause all", ui.Secondary),
		copyURL:    ui.NewButton(detailCopy, "Copy run URL", ui.Secondary),
		stopRunner: ui.NewButton(detailStop, "Stop runner", ui.Danger),
		back:       ui.NewButton(detailBack, "Back to runners", ui.Primary),
		tabs:       &tabStrip{},
		histRepo:   ui.NewSelect(histRepoSel, []ui.Option{{Value: "", Label: "all"}}),
		histResult: ui.NewSelect(histResultSel, resultOptions),
	}
	g.dash.Set([]ui.Widget{g.add, g.pauseAll, stop{dashRepos}, stop{dashRunners}})
	g.dash.Focus(dashRepos)
	g.hist.Set([]ui.Widget{g.histRepo, g.histResult, stop{histTable}})
	g.hist.Focus(histTable)
	return g
}

// topBar renders the status chips, shedding detail until they fit in w
// columns: the connection chip shrinks to its dot, then the API, disk and
// runners gauge bars are dropped in that order.
func (m Model) topBar(w int) string {
	for level := 0; level <= 4; level++ {
		if s := m.chips(level); ansi.StringWidth(s) <= w {
			return s
		}
	}
	return ansi.Truncate(m.chips(4), w, "…")
}

func (m Model) chips(level int) string {
	running := 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	runners := fmt.Sprintf("%d/%d", running, m.st.GlobalMax)
	if m.st.Mode == "all" {
		runners = fmt.Sprintf("%d/∞", running)
	} else if level < 4 {
		runners = ui.Gauge(running, m.st.GlobalMax, 3) + " " + runners
	}
	api := fmt.Sprint(m.st.RateRemaining)
	if level < 2 {
		api = ui.Gauge(max(m.st.RateRemaining, 0), 5000, 10) + " " + api
	}
	disk := fmt.Sprintf("%d%%", m.st.DiskPct)
	if level < 3 {
		disk = ui.Gauge(m.st.DiskPct, 100, 10) + " " + disk
	}
	diskStyle := sDim
	switch m.diskState() {
	case "warn":
		diskStyle = sAmber
	case "critical":
		diskStyle = sRed
	}
	disk = diskStyle.Render(disk)
	conn := sGreen.Render("● connected")
	if !m.connected {
		conn = sRed.Render("○ reconnecting")
	}
	if level >= 1 {
		conn = ansi.Truncate(conn, 1, "")
	}
	return " " + sAccent.Render("ghr") + "   mode " + sGreen.Render("● "+strings.ToUpper(m.st.Mode)) +
		"   runners " + runners + "   api " + api + "   disk " + disk + "   " + conn
}

// diskState is "critical" at 95% or more, "warn" at or above disk_high_water
// (80% until the config has loaded), else "ok".
func (m Model) diskState() string {
	threshold := 80
	if m.cfg != nil {
		threshold = m.cfg.DiskHighWater
	}
	switch {
	case m.st.DiskPct >= 95:
		return "critical"
	case m.st.DiskPct >= threshold:
		return "warn"
	}
	return "ok"
}

// alertLine is the full-width banner shown while the daemon is unreachable or degraded.
func (m Model) alertLine(w int) string {
	switch {
	case !m.connected:
		return sBanner.Width(w).Render(ansi.Truncate("daemon unreachable: "+m.connErr+" — retrying", w-2, "…"))
	case m.st.Degraded:
		return sBanner.Width(w).Render(ansi.Truncate("DEGRADED: "+m.st.DegradedReason+" — no new runners", w-2, "…"))
	}
	return ""
}

// navPage is the page the navigation marks as current: a runner's detail
// page belongs to Runners.
func (m Model) navPage() page {
	if m.page == pageDetail {
		return pageRunners
	}
	return m.page
}

// sidebar is the wide layout's page list, h lines tall, with Help and Quit at the bottom.
func (m Model) sidebar(h int) string {
	var lines []string
	for i, name := range pageNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if page(i) == m.navPage() {
			label = sAccent.Render("▌ " + label)
		} else {
			label = "  " + label
		}
		lines = append(lines, zone.Mark(navZone(page(i)), cell(label, sidebarWidth)))
	}
	bottom := []string{
		zone.Mark("nav/help", cell(sDim.Render("  ? Help"), sidebarWidth)),
		zone.Mark("nav/quit", cell(sDim.Render("  q Quit"), sidebarWidth)),
	}
	for len(lines)+len(bottom) < h {
		lines = append(lines, cell("", sidebarWidth))
	}
	return strings.Join(append(lines, bottom...), "\n")
}

// tabRow replaces the sidebar under wideMin columns.
// The labels shrink to short names, then to numbers, until the row fits.
func (m Model) tabRow(w int) string {
	short := []string{"Dash", "Run", "Hist", "Set"}
	var row string
	for form := 0; form < 3; form++ {
		var tabs []string
		for i, name := range pageNames {
			label := fmt.Sprintf(" %d %s ", i+1, name)
			switch form {
			case 1:
				label = fmt.Sprintf(" %d %s ", i+1, short[i])
			case 2:
				label = fmt.Sprintf(" %d ", i+1)
			}
			if page(i) == m.navPage() {
				label = sAccent.Render("[" + label + "]")
			} else {
				label = sDim.Render(" " + label + " ")
			}
			tabs = append(tabs, zone.Mark(navZone(page(i)), label))
		}
		bar := strings.Join(tabs, "")
		help := zone.Mark("nav/help", sDim.Render("? help"))
		gap := w - ansi.StringWidth(bar) - ansi.StringWidth(help) - 1
		row = bar + strings.Repeat(" ", max(gap, 1)) + help
		if gap >= 1 {
			break
		}
	}
	return row
}

// fitLines pads or cuts s to exactly n lines, so the footer stays on the
// bottom row and a dialog's canvas is the height of the screen.
func fitLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines[:n], "\n")
}

// pageHeader is the first line of the content area: the page title on the
// left and the page's status on the right.
func (m Model) pageHeader(w int) string {
	var title, right string
	switch {
	case m.page == pageDetail:
		title, right = sBold.Render("Runners › "+clean(m.detailID)), m.detailButtons()
	case m.page == pageSettings && !m.connected:
		title, right = sBold.Render(pageNames[m.page]), sRed.Render("reconnecting")
	case m.page == pageDashboard:
		title, right = sBold.Render(pageNames[m.page]), m.dashButtons()
	default:
		title = sBold.Render(pageNames[m.page])
	}
	// Too narrow: the title gives way first, then the buttons are cut.
	if over := ansi.StringWidth(title) + ansi.StringWidth(right) + 1 - w; over > 0 {
		title = ansi.Truncate(title, max(ansi.StringWidth(title)-over, 1), "…")
	}
	gap := max(w-ansi.StringWidth(title)-ansi.StringWidth(right), 1)
	return ansi.Truncate(title+strings.Repeat(" ", gap)+right, w, "…")
}

type footerKey struct{ key, label string }

// footerKeys are the clickable key hints for the current page.
func (m Model) footerKeys() []footerKey {
	switch m.page {
	case pageDetail:
		return []footerKey{{"esc", "back"}, {"left", "prev tab"}, {"right", "next tab"}, {"tab", "next"}, {"x", "stop"}, {"?", "help"}, {"q", "quit"}}
	case pageRunners:
		return []footerKey{{"x", "stop"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"}}
	case pageHistory:
		return m.histFooterKeys()
	case pageSettings:
		return m.settingsFooterKeys()
	}
	if !m.onCard() {
		return []footerKey{{"enter", "press"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
	}
	return []footerKey{
		{"p", "pause"}, {"+", "repo cap"}, {"-", ""}, {"[", "global cap"}, {"]", ""}, {"m", "mode"},
		{"x", "stop"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"},
	}
}

func (m Model) footer(w int) string {
	var parts []string
	for _, f := range m.footerKeys() {
		text := sAccent.Render(f.key)
		if f.label != "" {
			text += " " + sDim.Render(f.label)
		}
		parts = append(parts, zone.Mark("key-"+f.key, text))
	}
	return " " + ansi.Truncate(strings.Join(parts, "  "), w-1, "…")
}

// footerPress runs a clicked footer hint. Navigation and control keys take
// the same path as the key; the global letter keys run directly, so a click
// on "q quit" never types a q into a focused text field. The detail page has
// no text fields and its own x, so every hint there takes the key's path.
func (m Model) footerPress(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "tab", "shift+tab", "ctrl+s", "enter", "esc", "up", "left", "right":
		return m.handleKey(keyMsg(k))
	}
	if m.page == pageDetail {
		return m.handleKey(keyMsg(k))
	}
	return m.press(k)
}

// keyMsg is the KeyMsg for a footer hint.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// layout composes the shell around body, the current page's content.
func (m Model) layout(w int, body func(w, h int) string) string {
	top := []string{m.topBar(w)}
	if a := m.alertLine(w); a != "" {
		top = append(top, a)
	}
	if m.toast.Active() {
		top = append(top, m.toast.View(w))
	}
	wide := w >= wideMin
	if !wide {
		top = append(top, m.tabRow(w))
	}
	cw, ch := m.contentSize()
	content := m.pageHeader(cw) + "\n" + fitLines(body(cw, ch), ch)
	if wide {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(ch+1), " ", content)
	}
	return strings.Join(append(top, content, m.footer(w)), "\n")
}

// contentSize is the width and height layout gives the current page's body:
// what is left after the top bar, the alert and toast lines, the tab row (or
// the sidebar), the page header and the footer.
func (m Model) contentSize() (int, int) {
	w := max(m.width, 40)
	rows := 2 // the top bar and the footer
	if m.alertLine(w) != "" {
		rows++
	}
	if m.toast.Active() {
		rows++
	}
	cw := w
	if w >= wideMin {
		cw = w - sidebarWidth - 1
	} else {
		rows++
	}
	return cw, max(m.height-rows, 7) - 1
}
