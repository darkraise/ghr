package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

// Storage page control IDs. A Remove button's ID is storeRmPrefix plus
// <tool>/<version>; a Clear button's is storeClearPrefix plus the cache name.
const (
	storeBody        = "storage/body"
	storePrune       = "storage/prune"
	storeKeep        = "storage/prune/keep"
	storeAll         = "storage/prune/all"
	storeDangling    = "storage/prune/dangling"
	storeVolumes     = "storage/prune/volumes"
	storeInstall     = "storage/install"
	storePopular     = "storage/popular"
	storeRefresh     = "storage/refresh"
	storeRmPrefix    = "storage/rm/"
	storeClearPrefix = "storage/clear/"
)

// storagePage is the Storage page: the last GET /storage snapshot and the
// page's buttons. The Model holds it by pointer, so it survives Bubble Tea
// copying the Model.
type storagePage struct {
	group      ui.Group
	scroll     int
	shownFocus string // the focus storageView last scrolled to
	data       model.Storage
	loaded     bool
	err        string
	seq        int    // numbers Storage requests
	shown      int    // the newest request whose reply is shown
	lastOp     string // the newest finished operation already announced

	prune, keep, all, dangling, volumes *ui.Button
	install, popular, refresh           *ui.Button
	rm                                  map[string]*ui.Button // by toolchainKey
	clear                               map[string]*ui.Button // by cache name
}

func newStoragePage() *storagePage {
	return &storagePage{
		prune:    ui.NewButton(storePrune, "Prune", ui.Primary),
		keep:     ui.NewButton(storeKeep, "Build cache to keep", ui.Secondary),
		all:      ui.NewButton(storeAll, "All build cache", ui.Danger),
		dangling: ui.NewButton(storeDangling, "Dangling images", ui.Secondary),
		volumes:  ui.NewButton(storeVolumes, "Unused volumes", ui.Danger),
		install:  ui.NewButton(storeInstall, "Install…", ui.Primary),
		popular:  ui.NewButton(storePopular, "Install popular set", ui.Secondary),
		refresh:  ui.NewButton(storeRefresh, "Refresh", ui.Secondary),
		rm:       map[string]*ui.Button{},
		clear:    map[string]*ui.Button{},
	}
}

type storageMsg struct {
	seq int
	s   model.Storage
	err error
}

// fetchStorage asks for the snapshot. Replies can arrive out of order; one
// older than the reply shown is dropped.
func (m Model) fetchStorage() tea.Cmd {
	m.store.seq++
	seq, c := m.store.seq, m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		s, err := c.Storage(cx)
		return storageMsg{seq, s, err}
	}
}

// active reports whether the page polls every tick: an operation runs or
// waits, or a measurement is in progress.
func (s *storagePage) active() bool {
	o := s.data.Operations
	return o.Current != nil || o.Queued > 0 || s.data.Measuring
}

// gotStorage shows a snapshot. A failed read keeps the last one and names
// the failure above it.
func (m Model) gotStorage(msg storageMsg) (tea.Model, tea.Cmd) {
	s := m.store
	if msg.seq < s.shown {
		return m, nil
	}
	s.shown = msg.seq
	if msg.err != nil {
		s.err = errText(msg.err)
		return m, nil
	}
	data := cleanStorage(msg.s)
	m.announce(data.Operations.Recent)
	s.data, s.loaded, s.err = data, true, ""
	return m, nil
}

// announce shows a toast for the newest operation that finished since the
// last snapshot. The first snapshot only records where the page starts, so
// opening it does not replay old results.
func (m *Model) announce(recent []model.Operation) {
	s := m.store
	if len(recent) == 0 || recent[0].ID == s.lastOp {
		return
	}
	o := recent[0]
	s.lastOp = o.ID
	if !s.loaded {
		return
	}
	text := o.Kind + " " + o.Target + ": " + o.Outcome
	if o.Message != "" {
		text = o.Kind + " " + o.Target + ": " + o.Message
	}
	m.toast.Show(text, o.Outcome != "ok" && o.Outcome != "skipped", m.now())
}

// cleanStorage sanitises the text fields of s in place; the slices come from
// a freshly decoded response nobody else holds.
func cleanStorage(s model.Storage) model.Storage {
	for i := range s.Toolchains {
		t := &s.Toolchains[i]
		t.Tool, t.Version, t.Arch, t.Path = clean(t.Tool), clean(t.Version), clean(t.Arch), clean(t.Path)
	}
	for i := range s.OtherToolCache {
		s.OtherToolCache[i].Name = clean(s.OtherToolCache[i].Name)
	}
	for i := range s.PackageCaches {
		c := &s.PackageCaches[i]
		c.Name, c.Label, c.Paths = clean(c.Name), clean(c.Label), cleanList(c.Paths)
	}
	for i := range s.Docker.Rows {
		s.Docker.Rows[i].Type = clean(s.Docker.Rows[i].Type)
	}
	for i := range s.Docker.BuildCacheTypes {
		s.Docker.BuildCacheTypes[i].Type = clean(s.Docker.BuildCacheTypes[i].Type)
	}
	s.MeasureError = clean(s.MeasureError)
	op := func(o *model.Operation) {
		o.ID, o.Kind, o.Target, o.Progress = clean(o.ID), clean(o.Kind), clean(o.Target), clean(o.Progress)
		o.Outcome, o.Message = clean(o.Outcome), clean(o.Message)
	}
	if s.Operations.Current != nil {
		op(s.Operations.Current)
	}
	for i := range s.Operations.Recent {
		op(&s.Operations.Recent[i])
	}
	if p := s.LastPrune; p != nil {
		p.Trigger, p.Scope, p.Outcome = clean(p.Trigger), clean(p.Scope), clean(p.Outcome)
		for i := range p.Steps {
			p.Steps[i].Name, p.Steps[i].Error = clean(p.Steps[i].Name), clean(p.Steps[i].Error)
		}
	}
	return s
}

// storeCard collects a card's inside lines and the line range of each of
// its buttons, counted from the card's top border.
type storeCard struct {
	lines  []string
	ranges map[string]ui.Range
}

func newStoreCard() *storeCard { return &storeCard{ranges: map[string]ui.Range{}} }

func (c *storeCard) add(lines ...string) { c.lines = append(c.lines, lines...) }

// right adds text with b at the right end of a w-column line.
func (c *storeCard) right(text string, b *ui.Button, focused string, w int) {
	v := b.View(b.ID() == focused, 0)
	c.ranges[b.ID()] = ui.Range{Start: len(c.lines) + 1, End: len(c.lines) + 2}
	c.add(cell(text, w-ansi.StringWidth(v)) + v)
}

// buttons lays bs out left to right in w columns, starting a new line when
// the next one would not fit.
func (c *storeCard) buttons(focused string, w int, bs ...*ui.Button) {
	start := len(c.lines)
	var rows []string
	for _, b := range bs {
		v := b.View(b.ID() == focused, 0)
		if n := len(rows); n > 0 && ansi.StringWidth(rows[n-1]+" "+v) <= w {
			rows[n-1] += " " + v
		} else {
			rows = append(rows, v)
		}
		c.ranges[b.ID()] = ui.Range{Start: start + len(rows), End: start + len(rows) + 1}
	}
	c.add(rows...)
}

func (c *storeCard) box(title string, w int) ([]string, map[string]ui.Range) {
	return strings.Split(box(title, w, c.lines), "\n"), c.ranges
}

// opsCard lists the last ten finished operations, newest first.
func (m Model) opsCard(w int) []string {
	c := newStoreCard()
	ops := m.store.data.Operations.Recent
	if len(ops) == 0 {
		c.add(sDim.Render("no operations since the daemon started"))
	}
	for _, o := range ops {
		at := o.StartedAt
		if o.FinishedAt != nil {
			at = *o.FinishedAt
		}
		c.add(at.Local().Format("15:04") + "  " + cell(o.Kind, 8) + cell(o.Target, 20) + opBadge(o.Outcome) + "  " + sDim.Render(o.Message))
	}
	lines, _ := c.box("Recent operations", w)
	return lines
}

func opBadge(outcome string) string {
	kind := ui.BadgeMuted
	switch outcome {
	case "ok":
		kind = ui.BadgeOK
	case "refused", "interrupted":
		kind = ui.BadgeWarn
	case "failed":
		kind = ui.BadgeBad
	}
	return ui.Badge(outcome, kind)
}

// syncStorage brings the page's buttons up to date with the model and sets
// the focus order to the layout order. It runs before every key, click and
// frame.
func (m Model) syncStorage() {
	ws := m.dockerWidgets()
	m.store.group.Set(ws)
}

// storageLines renders the page's cards w columns wide, with the line range
// of every button.
func (m Model) storageLines(w int) ([]string, map[string]ui.Range) {
	var out []string
	ranges := map[string]ui.Range{}
	add := func(lines []string, rs map[string]ui.Range) {
		for k, r := range rs {
			ranges[k] = ui.Range{Start: r.Start + len(out), End: r.End + len(out)}
		}
		out = append(out, lines...)
	}
	add(m.dockerCard(w))
	add(m.opsCard(w), nil)
	return out, ranges
}

// storageView renders the page in w columns and h lines, scrolled so a
// newly focused button is in view.
func (m Model) storageView(w, h int) string {
	s := m.store
	m.syncStorage()
	if !s.loaded {
		if s.err != "" {
			return sRed.Render(cell("✖ "+s.err, w))
		}
		return sDim.Render("loading…")
	}
	var top []string
	if s.err != "" {
		top = append(top, sRed.Render(cell("✖ "+s.err, w)))
	}
	bodyH := max(h-len(top), 1)
	lines, ranges := m.storageLines(w)
	// Focus moves by tab, by a click, or by Group.Set when a refresh disables
	// the focused button; checking once per frame catches every one of them.
	if f := s.group.FocusedID(); f != s.shownFocus {
		s.shownFocus = f
		if r, ok := ranges[f]; ok {
			s.scroll = ui.ScrollTo(s.scroll, bodyH, r)
		}
	}
	s.scroll = min(max(s.scroll, 0), max(len(lines)-bodyH, 0))
	body := lines[s.scroll:min(s.scroll+bodyH, len(lines))]
	return strings.Join(append(top, zone.Mark(storeBody, strings.Join(body, "\n"))), "\n")
}

// storageKey handles a key on the Storage page: the focused button first,
// then focus and scrolling. It reports false for keys that fall through to
// the global keys.
func (m Model) storageKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	s := m.store
	m.syncStorage()
	if ok, cmd := s.group.Key(k); ok {
		return true, m, cmd
	}
	_, h := m.contentSize()
	switch k.String() {
	case "tab", "down", "j":
		s.group.Next()
	case "shift+tab", "up", "k":
		s.group.Prev()
	case "pgup":
		s.scroll = max(s.scroll-h/2, 0)
	case "pgdown":
		s.scroll += h / 2
	default:
		return false, m, nil
	}
	return true, m, nil
}

// storageMouse handles the wheel over the cards and clicks on the buttons.
// It reports false for events the shell should handle.
func (m Model) storageMouse(msg tea.MouseMsg) (bool, tea.Model, tea.Cmd) {
	s := m.store
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		if !zone.Get(storeBody).InBounds(msg) {
			return false, m, nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			s.scroll = max(s.scroll-3, 0)
		} else {
			s.scroll += 3
		}
		return true, m, nil
	}
	m.syncStorage()
	ok, cmd := s.group.Mouse(msg)
	return ok, m, cmd
}

func (m Model) storageFooterKeys() []footerKey {
	return []footerKey{{"tab", "next"}, {"enter", "press"}, {"pgdown", "scroll"}, {"?", "help"}, {"q", "quit"}}
}

// storagePressed runs a Storage page button; ok is false for any other ID.
func (m Model) storagePressed(id string) (tea.Model, tea.Cmd, bool) {
	var mm tea.Model
	var cmd tea.Cmd
	switch {
	case id == storePrune, id == storeKeep, id == storeAll, id == storeDangling, id == storeVolumes:
		mm, cmd = m.confirmPrune(id)
	default:
		return m, nil, false
	}
	return mm, cmd, true
}

// busyJobs counts the runners running a job: removals, clears and the
// unused-volumes prune are refused while any is.
func (m Model) busyJobs() int {
	n := 0
	for _, i := range m.st.Instances {
		if i.State == "busy" {
			n++
		}
	}
	return n
}

// refusedHint is the line an action refused while jobs run shows then.
func (m Model) refusedHint(what string) string {
	return sAmber.Render(fmt.Sprintf("%s: refused while %d jobs run", what, m.busyJobs()))
}

// dockerWidgets are the Docker card's prune buttons, disabled while the
// daemon is unreachable or a prune runs.
func (m Model) dockerWidgets() []ui.Widget {
	s := m.store
	keep := "keep"
	if m.cfg != nil {
		keep = clean(m.cfg.BuildCacheKeep)
	}
	s.keep.Label = "Build cache to " + keep
	var ws []ui.Widget
	for _, b := range []*ui.Button{s.prune, s.keep, s.all, s.dangling, s.volumes} {
		b.SetDisabled(!m.connected || m.st.Maintenance.Running)
		ws = append(ws, b)
	}
	return ws
}

// dockerCard renders the Docker disk card w columns wide: the disk bar
// against disk_high_water, Docker's disk table with a row per build cache
// type, the last prune and the prune buttons.
func (m Model) dockerCard(w int) ([]string, map[string]ui.Range) {
	s, d := m.store, m.store.data.Docker
	f, in, c := s.group.FocusedID(), w-4, newStoreCard()
	high := 80
	if m.cfg != nil {
		high = m.cfg.DiskHighWater
	}
	c.add(ui.Gauge(d.DiskPct, 100, 20) + fmt.Sprintf(" %d%% used · prunes above %d%%", d.DiskPct, high))
	full := m.width >= wideMin
	row := func(typ, count, active string, size, reclaimable int64) string {
		line := cell(typ, 16) + cell(count, 7) + cell(active, 8) + cell(model.HumanBytes(size), 11)
		if full {
			line += model.HumanBytes(reclaimable)
		}
		return line
	}
	head := cell("", 16) + cell("count", 7) + cell("active", 8) + cell("size", 11)
	if full {
		head += "reclaimable"
	}
	c.add(sDim.Render(head))
	for _, r := range d.Rows {
		c.add(row(r.Type, fmt.Sprint(r.Count), fmt.Sprint(r.Active), r.Bytes, r.Reclaimable))
		if r.Type == "Build Cache" {
			for _, t := range d.BuildCacheTypes {
				c.add(sDim.Render(row("  "+t.Type, fmt.Sprint(t.Count), "", t.Bytes, t.Reclaimable)))
			}
		}
	}
	c.add(m.lastPruneLines(in)...)
	c.buttons(f, in, s.prune, s.keep, s.all, s.dangling, s.volumes)
	if m.busyJobs() > 0 {
		c.add(m.refusedHint("Unused volumes"))
	}
	return c.box("Docker disk", w)
}

// lastPruneLines describe the newest prune in w columns, for example
// "auto · 14:05 · ok — build cache older than 72h 1.2 GB, dangling images
// 300.0 MB"; a failed step shows its error in red.
func (m Model) lastPruneLines(w int) []string {
	p := m.store.data.LastPrune
	switch {
	case m.st.Maintenance.Running || (p != nil && p.FinishedAt == nil):
		return []string{sAmber.Render("pruning…")}
	case p == nil:
		return []string{sDim.Render("no prune since start")}
	}
	head := p.Trigger
	if p.Trigger != "auto" {
		head += " " + p.Scope
	}
	line := head + " · " + p.FinishedAt.Local().Format("15:04") + " · " + p.Outcome
	var steps []string
	for _, st := range p.Steps {
		if st.Error != "" {
			steps = append(steps, sRed.Render(st.Name+": "+st.Error))
		} else {
			steps = append(steps, st.Name+" "+model.HumanBytes(st.Freed))
		}
	}
	if len(steps) > 0 {
		line += " — " + strings.Join(steps, ", ")
	}
	return ui.WrapWords(line, max(w, 1))
}

// confirmPrune asks before the prune a Docker card button names, saying
// what it removes.
func (m Model) confirmPrune(id string) (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	keep := "the build_cache_keep size"
	if m.cfg != nil {
		keep = clean(m.cfg.BuildCacheKeep)
	}
	d := m.store.data.Docker
	var scope, text string
	switch id {
	case storePrune:
		scope, text = "standard", "Prune now? Removes build cache beyond "+keep+", dangling images, and history and logs past retention."
	case storeKeep:
		scope, text = "build-cache-keep", "Prune the build cache down to "+keep+"?"
	case storeAll:
		scope, text = "build-cache-all", "Remove all build cache"+reclaim(d, "Build Cache")+"? The next builds start cold."
	case storeDangling:
		scope, text = "dangling-images", "Remove dangling images (untagged and used by no container)?"
	case storeVolumes:
		scope, text = "unused-volumes", "Remove every volume no container uses"+reclaim(d, "Local Volumes")+"? It is refused while jobs run."
	default:
		return m, nil
	}
	c := m.c
	return m.openConfirm(text, func() tea.Cmd {
		return m.action(scope+" prune started", func(cx context.Context) error {
			if scope == "standard" {
				return c.Prune(cx)
			}
			return c.PruneScope(cx, scope)
		})
	})
}

// reclaim is " (up to 1.2 GB)" for Docker's row typ, or "" when it frees nothing.
func reclaim(d model.DockerDisk, typ string) string {
	for _, r := range d.Rows {
		if r.Type == typ && r.Reclaimable > 0 {
			return " (up to " + model.HumanBytes(r.Reclaimable) + ")"
		}
	}
	return ""
}
