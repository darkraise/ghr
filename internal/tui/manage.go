package tui

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
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
	reposLCCheck    = "repos/lc/check"
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

	lcRepo     string
	lc         model.LabelCheck
	lcErr      string
	lcSeq      int
	lcInFlight bool // a label-check read is outstanding; polls wait for it
	lcCheck    *ui.Button
	lcAdd      map[string]*ui.Button // by raw label
}

func newManageState() *manageState {
	return &manageState{
		replace:    ui.NewButton(setReplaceToken, "Replace token", ui.Primary),
		tokRetry:   ui.NewButton(setTokenRetry, "Retry", ui.Secondary),
		reload:     ui.NewButton(setReload, "Reload config.yaml", ui.Primary),
		prune:      ui.NewButton(setPrune, "Prune now", ui.Primary),
		regRefresh: ui.NewButton(reposRegRefresh, "Refresh", ui.Secondary),
		regDel:     map[int64]*ui.Button{},
		lcCheck:    ui.NewButton(reposLCCheck, "Check now", ui.Secondary),
		lcAdd:      map[string]*ui.Button{},
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
	off := !m.repoActionable()
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
	if err != nil || m.offline() || !m.repoActionable() {
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

// lcAddID is a quick-add button's ID. Labels come from GitHub and may hold
// any byte, so the ID carries the label hex-encoded; lcAddLabel reverses it.
func lcAddID(label string) string { return "repos/lc/add/" + hex.EncodeToString([]byte(label)) }

func lcAddLabel(id string) (string, bool) {
	h, ok := strings.CutPrefix(id, "repos/lc/add/")
	if !ok {
		return "", false
	}
	b, err := hex.DecodeString(h)
	return string(b), err == nil
}

// repoActionable reports whether the selected repo's GitHub management
// controls may act: the daemon is reachable, GitHub accepts the token and
// the repo is not being removed.
func (m Model) repoActionable() bool {
	r := m.selectedRepoStatus()
	return r != nil && m.connected && !m.st.Degraded && !r.Removing
}

// osArchLabels name a machine's platform; adding one to a Linux x64 runner
// would only misroute jobs, so the quick fix never offers them.
var osArchLabels = map[string]bool{"linux": true, "windows": true, "macos": true, "x64": true, "x86": true, "arm": true, "arm64": true}

type (
	lcMsg struct {
		repo string
		seq  int
		lc   model.LabelCheck
		err  error
	}
	lcStartedMsg struct {
		repo string
		err  error
	}
)

// fetchLabelCheck reads the selected repo's last label scan; it never starts
// one. A read skipped while degraded still drops another repo's scan, so it
// is never shown or quick-added under the new selection.
func (m Model) fetchLabelCheck() tea.Cmd {
	r := m.selectedRepoStatus()
	if r == nil {
		return nil
	}
	if !strings.EqualFold(m.mg.lcRepo, r.Name) {
		m.mg.lcRepo, m.mg.lc, m.mg.lcErr = r.Name, model.LabelCheck{State: "not_checked"}, ""
	}
	if m.st.Degraded {
		return nil
	}
	m.mg.lcSeq++
	m.mg.lcInFlight = true
	seq, repo, c := m.mg.lcSeq, r.Name, m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		lc, err := c.LabelCheck(cx, repo)
		return lcMsg{repo, seq, lc, err}
	}
}

// pollLabelCheck re-reads a running scan on the Repositories page, one
// request at a time: a reply slower than the tick is waited for, not
// overtaken by a newer request whose number would drop it.
func (m Model) pollLabelCheck() tea.Cmd {
	if m.page != pageRepos || m.mg.lc.State != "checking" || m.mg.lcInFlight {
		return nil
	}
	return m.fetchLabelCheck()
}

func (m Model) gotLabelCheck(msg lcMsg) {
	if msg.seq != m.mg.lcSeq {
		return
	}
	m.mg.lcInFlight = false
	r := m.selectedRepoStatus()
	if !strings.EqualFold(msg.repo, m.mg.lcRepo) || r == nil || !strings.EqualFold(msg.repo, r.Name) {
		return
	}
	if msg.err != nil {
		m.mg.lcErr = errText(msg.err)
		return
	}
	m.mg.lc, m.mg.lcErr = msg.lc, ""
}

func (m Model) startLabelCheck() (tea.Model, tea.Cmd) {
	r := m.selectedRepoStatus()
	if r == nil || m.offline() || !m.repoActionable() {
		return m, nil
	}
	repo, c := r.Name, m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return lcStartedMsg{repo, c.StartLabelCheck(cx, repo)}
	}
}

func (m Model) labelCheckStarted(msg lcStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.toast.Show("label check: "+errText(msg.err), true, m.now())
		return m, nil
	}
	if strings.EqualFold(msg.repo, m.mg.lcRepo) {
		m.mg.lc.State = "checking"
	}
	return m, m.fetchLabelCheck()
}

// effectiveLabels are the labels a newly started runner of repo name would
// carry with the draft repo labels; nil until the config has loaded.
func (m Model) effectiveLabels(name string) []string {
	if m.cfg == nil {
		return nil
	}
	r := m.cfg.Repo(name)
	if r == nil {
		return config.SystemLabels
	}
	draft := *r
	if f := m.repos.form.Field(reposKey(name, "labels")); f != nil {
		draft.Labels = f.Input.Value().List
	}
	return m.cfg.EffectiveLabels(draft)
}

// classify compares a job label group with the draft effective labels using
// the daemon's own predicate: matched (ghr takes these jobs), unmatched (they
// ask for self-hosted, with the labels missing) or other. Before the config
// has loaded there is nothing to compare with, and it reports "".
func (m Model) classify(name string, g model.LabelGroup) (kind string, missing []string) {
	if m.cfg == nil {
		return "", nil
	}
	if len(g.Labels) == 0 {
		return "other", nil
	}
	eff := m.effectiveLabels(name)
	if sched.MatchLabels(g.Labels, eff) {
		return "matched", nil
	}
	has := map[string]bool{}
	for _, l := range eff {
		has[strings.ToLower(l)] = true
	}
	self := false
	for _, l := range g.Labels {
		if l == "self-hosted" {
			self = true
		}
		if !has[l] {
			missing = append(missing, l)
		}
	}
	if !self {
		return "other", nil
	}
	return "unmatched", missing
}

// lcWidgets are the card's focusable buttons, in order.
func (m Model) lcWidgets(name string) []ui.Widget {
	off := !m.repoActionable()
	m.mg.lcCheck.SetDisabled(off || m.mg.lc.State == "checking")
	ws := []ui.Widget{m.mg.lcCheck}
	for _, l := range m.quickAdds(name) {
		b := m.mg.lcAdd[l]
		if b == nil {
			b = ui.NewButton(lcAddID(l), "+ add "+clean(l), ui.Secondary)
			m.mg.lcAdd[l] = b
		}
		b.SetDisabled(off)
		ws = append(ws, b)
	}
	return ws
}

// quickAdds are the custom labels the unmatched groups miss, in order.
func (m Model) quickAdds(name string) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range m.mg.lc.Groups {
		if kind, missing := m.classify(name, g); kind == "unmatched" {
			for _, l := range missing {
				if !osArchLabels[l] && !seen[l] {
					seen[l] = true
					out = append(out, l)
				}
			}
		}
	}
	return out
}

// lcCard renders the Workflow labels card w columns wide, with the line
// range of each of its buttons. Text wraps to the card and every button has
// a line of its own, so nothing is cut on a narrow panel. Label and job
// names come from GitHub and pass through clean; the raw labels stay in the
// model for matching and adding.
func (m Model) lcCard(name string, w int) ([]string, map[string]ui.Range) {
	f := m.repos.group.FocusedID()
	lc := m.mg.lc
	inner := max(w-4, 1)
	ranges := map[string]ui.Range{}
	var lines []string
	plain := func(s ...string) string { return strings.Join(s, "") }
	text := func(s string, style func(...string) string) {
		for _, l := range strings.Split(ansi.Wrap(s, inner, ""), "\n") {
			lines = append(lines, style(l))
		}
	}
	shown := map[string]bool{}
	button := func(b *ui.Button, indent string) {
		if shown[b.ID()] {
			return
		}
		shown[b.ID()] = true
		ranges[b.ID()] = ui.Range{Start: len(lines) + 1, End: len(lines) + 2}
		lines = append(lines, indent+b.View(f == b.ID(), 0))
	}
	head := "Not checked yet"
	switch {
	case lc.State == "checking":
		head = "checking…"
	case lc.CheckedAt != nil:
		head = "Checked " + ago(m.now().Sub(*lc.CheckedAt))
		if lc.Partial {
			head += " · partial"
		}
	}
	text(head, sDim.Render)
	button(m.mg.lcCheck, "")
	if m.st.Degraded {
		text("GitHub is rejecting the token: "+clean(m.st.DegradedReason), sRed.Render)
	}
	if lc.Error != "" {
		text("✖ "+clean(lc.Error), sRed.Render)
	}
	if m.mg.lcErr != "" {
		text("✖ "+m.mg.lcErr, sRed.Render)
	}
	offered := false
	for _, g := range lc.Groups {
		kind, missing := m.classify(name, g)
		var shownLabels []string
		for _, l := range g.Labels {
			shownLabels = append(shownLabels, clean(l))
		}
		labels := strings.Join(shownLabels, ", ")
		if len(g.Labels) == 0 {
			labels = "no labels (runner group)"
		}
		jobs := clean(strings.Join(g.Jobs, ", "))
		if g.More > 0 {
			jobs += fmt.Sprintf(" +%d more", g.More)
		}
		badge := ""
		if kind != "" {
			badge = stateBadge(kind) + " "
		}
		text(badge+labels+"  "+jobs+sDim.Render(fmt.Sprintf(" · %d jobs · %s", g.Count, ago(m.now().Sub(g.LastSeen)))), plain)
		if kind != "unmatched" {
			continue
		}
		var shownMissing []string
		platform := false
		for _, l := range missing {
			shownMissing = append(shownMissing, clean(l))
			platform = platform || osArchLabels[l]
		}
		text("  missing "+strings.Join(shownMissing, ", "), plain)
		for _, l := range missing {
			if b := m.mg.lcAdd[l]; b != nil && !osArchLabels[l] {
				button(b, "    ")
				offered = true
			}
		}
		if platform {
			text("  needs a different OS or architecture", sAmber.Render)
		}
	}
	if offered {
		text("Adding a label changes which jobs ghr accepts; it does not install anything on the runner.", sDim.Render)
	}
	return strings.Split(box("Workflow labels", w, lines), "\n"), ranges
}

// addLabel puts label into the selected repo's labels as an unsaved edit,
// only while the repo's controls may act.
func (m Model) addLabel(label string) {
	if !m.repoActionable() {
		return
	}
	f := m.repos.form.Field(reposKey(m.mg.lcRepo, "labels"))
	if f == nil || !f.Input.Focusable() {
		return
	}
	v := f.Input.Value()
	for _, l := range v.List {
		if strings.EqualFold(l, label) {
			return
		}
	}
	v.List = append(append([]string{}, v.List...), label)
	f.Input.SetValue(v)
}
