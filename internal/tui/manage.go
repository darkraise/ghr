package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	setReplaceToken = "settings/token"
	setTokenRetry   = "settings/token/retry"
	setReload       = "settings/reload"
	setPrune        = "settings/prune"
	tokField        = "token/field"
	tokOK           = "token/ok"
	tokCancel       = "token/cancel"
	reposRegRefresh = "repos/reg/refresh"
)

// manageState holds the management cards' daemon data and buttons. The
// Model keeps it by pointer so it survives Bubble Tea copying the Model.
type manageState struct {
	token    model.TokenStatus
	tokenErr string
	tokenSeq int
	replace  *ui.Button
	tokRetry *ui.Button
	reload   *ui.Button
	prune    *ui.Button

	cardsRepo     string // the repo the panel's cards were last fetched for
	cardsDegraded bool   // m.st.Degraded when the cards were last fetched
	regRepo       string // the repo regs belong to
	regs          []model.Registration
	regErr        string
	regSeq        int
	regLoaded     bool
	regRefresh    *ui.Button
	regDel        map[int64]*ui.Button
}

func newManageState() *manageState {
	return &manageState{
		replace:    ui.NewButton(setReplaceToken, "Replace token", ui.Primary),
		tokRetry:   ui.NewButton(setTokenRetry, "Retry", ui.Secondary),
		reload:     ui.NewButton(setReload, "Reload config.yaml", ui.Primary),
		prune:      ui.NewButton(setPrune, "Prune now", ui.Primary),
		regRefresh: ui.NewButton(reposRegRefresh, "Refresh", ui.Secondary),
		regDel:     map[int64]*ui.Button{},
	}
}

type (
	tokenMsg struct {
		seq int
		ts  model.TokenStatus
		err error
	}
	// tokenSetMsg carries the dialog that sent it, so a late reply never
	// changes a dialog opened after it.
	tokenSetMsg struct {
		d   *tokenDialog
		err error
	}
)

func (m Model) fetchToken() tea.Cmd {
	m.mg.tokenSeq++
	seq, c := m.mg.tokenSeq, m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ts, err := c.Token(cx)
		return tokenMsg{seq, ts, err}
	}
}

func (m Model) gotToken(msg tokenMsg) {
	if msg.seq != m.mg.tokenSeq {
		return
	}
	if msg.err != nil {
		m.mg.tokenErr = errText(msg.err)
		return
	}
	m.mg.token, m.mg.tokenErr = msg.ts, ""
}

// errText is a daemon error as a card shows it, with the time a throttled
// call may be retried.
func errText(err error) string {
	s := clean(err.Error())
	var ae *api.Error
	if errors.As(err, &ae) && !ae.RetryAt.IsZero() {
		s += ", try again after " + ae.RetryAt.Local().Format("15:04")
	}
	return s
}

// tokenSection is the Settings card for the GitHub token. It never shows the token.
func (m Model) tokenSection() ui.Section {
	ts := m.mg.token
	state := ts.State
	if state == "" {
		state = "unverified"
	}
	badge := state
	exp := "expiry unknown"
	if ts.ExpiresAt != nil {
		left := ts.ExpiresAt.Sub(m.now())
		exp = fmt.Sprintf("expires %s (%d days)", ts.ExpiresAt.Local().Format("2006-01-02"), int(left.Hours()/24))
		if state == "ok" && left < 14*24*time.Hour {
			badge = "expires soon"
		}
	}
	status := stateBadge(badge) + "  " + exp
	if ts.CheckedAt != nil {
		status += sDim.Render("  · checked " + ago(m.now().Sub(*ts.CheckedAt)))
	}
	rate := "–"
	if ts.RateRemaining != nil {
		rate = fmt.Sprint(*ts.RateRemaining)
		if ts.RateLimit != nil {
			rate += fmt.Sprintf(" / %d", *ts.RateLimit)
		}
		if ts.RateReset != nil {
			rate += ", resets " + ts.RateReset.Local().Format("15:04")
		}
	}
	rows := []ui.Row{{Label: "Status", Text: status}, {Label: "Rate limit", Text: rate}}
	if ts.Reason != "" {
		rows = append(rows, ui.Row{Text: sRed.Render(clean(ts.Reason))})
	}
	if m.mg.tokenErr != "" {
		m.mg.tokRetry.SetDisabled(!m.connected)
		rows = append(rows, ui.Row{Text: sRed.Render("✖ " + m.mg.tokenErr)}, ui.Row{Items: []ui.Widget{m.mg.tokRetry}})
	}
	m.mg.replace.SetDisabled(!m.connected)
	rows = append(rows, ui.Row{Items: []ui.Widget{m.mg.replace}})
	return ui.Section{Title: "GitHub token",
		Note: "Replacing checks read access to the first repository only; registration permissions are checked when a runner is next started.",
		Rows: rows}
}

// tokenDialog is the Replace token dialog.
type tokenDialog struct {
	field      *ui.TextField
	ok, cancel *ui.Button
	group      ui.Group
	err        string
	busy       bool
}

func (m Model) openTokenDialog() (tea.Model, tea.Cmd) {
	d := &tokenDialog{
		field:  ui.NewTextField(tokField, 40),
		ok:     ui.NewButton(tokOK, "Replace", ui.Primary),
		cancel: ui.NewButton(tokCancel, "Cancel", ui.Secondary),
	}
	d.field.Mask = true
	d.group.Set([]ui.Widget{d.field, d.cancel, d.ok})
	m.tok, m.overlay = d, ovToken
	return m, nil
}

// syncToken sets the dialog's controls from its state and the connection:
// the field is locked while a replacement is in flight, and Replace needs a
// reachable daemon. A degraded daemon still takes a token; that is how a
// rejected one is fixed. It runs before every key, click and frame.
func (m Model) syncToken() {
	d := m.tok
	d.field.SetDisabled(d.busy)
	d.ok.SetDisabled(d.busy || !m.connected)
	d.group.Set([]ui.Widget{d.field, d.cancel, d.ok})
}

// tokenKey routes a key in the dialog: the focused control first (enter on
// a button presses it), then esc cancels, tab moves focus, enter replaces.
func (m Model) tokenKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.tok
	m.syncToken()
	if _, onButton := d.group.Focused().(*ui.Button); onButton && k.String() == "enter" {
		return m.pressed(d.group.FocusedID())
	}
	if ok, cmd := d.group.Key(k); ok {
		return m, cmd
	}
	switch k.String() {
	case "esc":
		return m.pressed(tokCancel)
	case "tab", "down":
		d.group.Next()
	case "shift+tab", "up":
		d.group.Prev()
	case "enter":
		return m.pressed(tokOK)
	}
	return m, nil
}

// submitToken sends the token and clears the field at once; only the
// request holds the value until it returns.
func (m Model) submitToken() (tea.Model, tea.Cmd) {
	d := m.tok
	if d == nil || d.busy {
		return m, nil
	}
	if !m.connected {
		d.err = "the daemon is unreachable"
		return m, nil
	}
	tok := d.field.Value().Text
	d.field.SetValue(ui.Value{})
	if tok == "" {
		d.err = "paste the new token first"
		return m, nil
	}
	d.busy, d.err = true, ""
	m.syncToken()
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return tokenSetMsg{d, c.SetToken(cx, tok)}
	}
}

func (m Model) tokenSet(msg tokenSetMsg) (tea.Model, tea.Cmd) {
	mine := m.overlay == ovToken && m.tok == msg.d
	if !mine {
		return m, nil
	}
	m.tok.field.SetValue(ui.Value{})
	if msg.err != nil {
		m.tok.busy, m.tok.err = false, clean(msg.err.Error())
		m.syncToken()
		return m, nil
	}
	m.overlay, m.tok = ovNone, nil
	m.toast.Show("GitHub token replaced", false, m.now())
	return m, m.fetchToken()
}

func (m Model) tokenView(w int) string {
	d := m.tok
	m.syncToken()
	f := d.group.FocusedID()
	body := "Paste a fine-grained token with Administration: read/write\nand Actions: read on every configured repository.\n\n" +
		d.field.View(f == tokField, 0)
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(max(min(60, w-14), 20)).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Replace GitHub token", body, []*ui.Button{d.cancel, d.ok}, f, w)
}

type reloadMsg struct {
	warnings []string
	err      error
}

func (m Model) reloadConfig() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ws, err := c.Reload(cx)
		return reloadMsg{ws, err}
	}
}

func (m Model) reloaded(msg reloadMsg) (tea.Model, tea.Cmd) {
	switch n := len(msg.warnings); {
	case msg.err != nil:
		m.toast.Show("reload rejected: "+clean(msg.err.Error()), true, m.now())
		return m, nil
	case n == 0:
		m.toast.Show("config reloaded", false, m.now())
	case n == 1:
		m.toast.Show("config reloaded (1 warning, see Activity)", false, m.now())
	default:
		m.toast.Show(fmt.Sprintf("config reloaded (%d warnings, see Activity)", n), false, m.now())
	}
	return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig(), m.fetchToken())
}

func (m Model) startPrune() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	return m, m.action("prune started — see Activity", func(c context.Context) error { return m.c.Prune(c) })
}

// maintenanceSection is the Settings card for disk use, the last manual
// prune, Reload and Prune now.
func (m Model) maintenanceSection() ui.Section {
	ms := m.st.Maintenance
	last := "not pruned since the daemon started"
	switch {
	case ms.Running:
		last = "pruning…"
	case ms.LastFinished != nil:
		last = fmt.Sprintf("last pruned %s (%s)", ago(m.now().Sub(*ms.LastFinished)), ms.LastOutcome)
	}
	m.mg.reload.SetDisabled(!m.connected)
	m.mg.prune.SetDisabled(!m.connected || ms.Running)
	return ui.Section{Title: "Maintenance", Rows: []ui.Row{
		{Label: "Disk", Text: fmt.Sprintf("%d%% used", m.st.DiskPct)},
		{Label: "Prune", Text: last},
		{Items: []ui.Widget{m.mg.reload, m.mg.prune}},
	}}
}

func regDelID(id int64) string { return fmt.Sprintf("repos/reg/del/%d", id) }

type (
	regsMsg struct {
		repo string
		seq  int
		regs []model.Registration
		err  error
	}
	regDeletedMsg struct {
		repo, name string
		err        error
	}
)

// deletable reports whether the daemon would delete g: offline, idle, and
// outside ghr's namespace.
func deletable(g model.Registration) bool { return !g.GHR && !g.Busy && g.Status == "offline" }

// fetchRegs lists the selected repo's registrations. A reply for an older
// request or another repo is dropped.
func (m Model) fetchRegs() tea.Cmd {
	r := m.selectedRepoStatus()
	if r == nil || m.st.Degraded {
		return nil
	}
	m.mg.regSeq++
	seq, repo, c := m.mg.regSeq, r.Name, m.c
	if !strings.EqualFold(m.mg.regRepo, repo) {
		m.mg.regRepo, m.mg.regs, m.mg.regErr, m.mg.regLoaded = repo, nil, "", false
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		rs, err := c.Registrations(cx, repo)
		return regsMsg{repo, seq, rs, err}
	}
}

func (m Model) gotRegs(msg regsMsg) {
	r := m.selectedRepoStatus()
	if msg.seq != m.mg.regSeq || !strings.EqualFold(msg.repo, m.mg.regRepo) || r == nil || !strings.EqualFold(msg.repo, r.Name) {
		return
	}
	m.mg.regLoaded = true
	if msg.err != nil {
		m.mg.regErr = errText(msg.err)
		return
	}
	m.mg.regs, m.mg.regErr = msg.regs, ""
}

// regWidgets are the card's focusable buttons, in order.
func (m Model) regWidgets() []ui.Widget {
	off := !m.connected || m.st.Degraded
	m.mg.regRefresh.SetDisabled(off)
	ws := []ui.Widget{m.mg.regRefresh}
	for _, g := range m.mg.regs {
		if !deletable(g) {
			continue
		}
		b := m.mg.regDel[g.ID]
		if b == nil {
			b = ui.NewButton(regDelID(g.ID), "Delete", ui.Danger)
			m.mg.regDel[g.ID] = b
		}
		b.SetDisabled(off)
		ws = append(ws, b)
	}
	return ws
}

// regCard renders the GitHub registrations card w columns wide, with the
// line range of each of its buttons (counted from the card's top border).
func (m Model) regCard(w int) ([]string, map[string]ui.Range) {
	f := m.repos.group.FocusedID()
	ranges := map[string]ui.Range{}
	var lines []string
	mark := func(id string) { ranges[id] = ui.Range{Start: len(lines) + 1, End: len(lines) + 2} }
	switch {
	case m.st.Degraded:
		lines = append(lines, sRed.Render("GitHub is rejecting the token: "+clean(m.st.DegradedReason)))
	case m.mg.regErr != "":
		lines = append(lines, sRed.Render("✖ "+m.mg.regErr))
	case !m.mg.regLoaded:
		lines = append(lines, sDim.Render("loading…"))
	case len(m.mg.regs) == 0:
		lines = append(lines, sDim.Render("No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode)."))
	}
	if !m.st.Degraded {
		for _, g := range m.mg.regs {
			state := g.Status
			if g.Busy {
				state = "busy"
			}
			line := stateBadge(state)
			if g.GHR {
				line += " " + ui.Badge("ghr", ui.BadgeMuted)
			}
			if b := m.mg.regDel[g.ID]; b != nil && deletable(g) {
				line += " " + b.View(f == b.ID(), 0)
				mark(b.ID())
			}
			line += " " + clean(g.Name) + "  " + sDim.Render(clean(strings.Join(g.Labels, " ")))
			lines = append(lines, line)
		}
	}
	mark(reposRegRefresh)
	lines = append(lines, m.mg.regRefresh.View(f == reposRegRefresh, 0))
	return strings.Split(box("GitHub registrations", w, lines), "\n"), ranges
}

// confirmDeleteReg asks before deleting the registration a Delete button
// names, capturing the repo and runner now.
func (m Model) confirmDeleteReg(id string) (tea.Model, tea.Cmd) {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "repos/reg/del/"), 10, 64)
	if err != nil || m.offline() {
		return m, nil
	}
	name := ""
	for _, g := range m.mg.regs {
		if g.ID == n {
			name = g.Name
		}
	}
	repo, c := m.mg.regRepo, m.c
	return m.openConfirm(fmt.Sprintf("Delete the runner registration %s from %s?", clean(name), clean(repo)), func() tea.Cmd {
		return func() tea.Msg {
			cx, cancel := ctx()
			defer cancel()
			return regDeletedMsg{repo, name, c.DeleteRegistration(cx, repo, n)}
		}
	})
}

func (m Model) regDeleted(msg regDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.toast.Show("delete failed: "+clean(msg.err.Error()), true, m.now())
	} else {
		m.toast.Show("deleted "+clean(msg.name), false, m.now())
	}
	return m, m.fetchRegs() // a new request number drops any listing sent before the delete
}
