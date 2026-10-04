package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	btnYes   = "dialog/yes"
	btnNo    = "dialog/no"
	btnClose = "dialog/close"
)

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

func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
	return m, nil
}

// dialogKey handles a key while a button dialog is open: enter presses the
// primary (last) button, space presses the focused one, esc presses cancel,
// and tab or the arrow keys move between the buttons. A confirmation also
// takes y and n.
func (m Model) dialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
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
	case setSave:
		return m.saveSettings()
	case setDiscard:
		m.settings.form.Discard()
		m.settings.alert = nil
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

// buttons renders the runner detail and prompt overlays' clickable buttons:
// ok runs enter, cancel runs esc.
func buttons(ok, cancel string) string {
	b := func(id, label string) string { return zone.Mark(id, sAccent.Render("[ "+label+" ]")) }
	if ok == "" {
		return b("btn-cancel", cancel)
	}
	return b("btn-ok", ok) + "  " + b("btn-cancel", cancel)
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
	case ovPrompt:
		dialog = sDialog.Render(sBold.Render(m.promptLabel) + "\n\n" + m.prompt.View() + "\n\n" + buttons("Save", "Cancel") + "\n" +
			sDim.Render("enter save · esc cancel"))
	case ovDetail:
		dialog = sDialog.Render(m.detailBody())
	}
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
}
