package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

const (
	sidebarWidth = 16
	wideMin      = 100 // below this many columns a tab row replaces the sidebar
)

func navZone(p page) string { return "nav/" + strings.ToLower(pageNames[p]) }

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
		runners = gauge(running, m.st.GlobalMax, 3) + " " + runners
	}
	api := fmt.Sprint(m.st.RateRemaining)
	if level < 2 {
		api = gauge(max(m.st.RateRemaining, 0), 5000, 10) + " " + api
	}
	disk := fmt.Sprintf("%d%%", m.st.DiskPct)
	if level < 3 {
		disk = gauge(m.st.DiskPct, 100, 10) + " " + disk
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

// sidebar is the wide layout's page list, h lines tall, with Help and Quit at the bottom.
func (m Model) sidebar(h int) string {
	var lines []string
	for i, name := range pageNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if page(i) == m.page {
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
			if page(i) == m.page {
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

// pageHeader is the first line of the content area: the page title on the
// left and the page's status on the right.
func (m Model) pageHeader(w int) string {
	title := sBold.Render(pageNames[m.page])
	right := ""
	if m.page == pageSettings && !m.connected {
		right = sRed.Render("reconnecting")
	}
	gap := max(w-ansi.StringWidth(title)-ansi.StringWidth(right), 1)
	return title + strings.Repeat(" ", gap) + right
}

type footerKey struct{ key, label string }

// footerKeys are the clickable key hints for the current page.
func (m Model) footerKeys() []footerKey {
	switch m.page {
	case pageRunners:
		return []footerKey{{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"}}
	case pageHistory:
		return []footerKey{{"r", "repo"}, {"c", "result"}, {"enter", "copy URL"}, {"?", "help"}, {"q", "quit"}}
	case pageSettings:
		return []footerKey{{"tab", "next"}, {"shift+tab", "previous"}, {"ctrl+s", "save"}, {"?", "help"}, {"q", "quit"}}
	}
	return []footerKey{
		{"p", "pause"}, {"+", "repo cap"}, {"-", ""}, {"[", "global cap"}, {"]", ""}, {"m", "mode"},
		{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"},
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

// keyMsg is the KeyMsg for a footer hint, so a click runs the same path as the key.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
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
	bodyH := max(m.height-len(top)-1, 7) // the footer takes the last line
	cw := w
	if wide {
		cw = w - sidebarWidth - 1
	}
	content := m.pageHeader(cw) + "\n" + body(cw, bodyH-1)
	if wide {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(bodyH), " ", content)
	}
	return strings.Join(append(top, content, m.footer(w)), "\n")
}
