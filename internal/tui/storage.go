package tui

import (
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
	var ws []ui.Widget
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
	return m, nil, false
}
