package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// PickOption is one choice of a Picker.
type PickOption struct {
	Label    string // what the filter matches and a pick returns
	Badge    string // shown after the label
	Disabled bool   // shown dim, with Note, and never picked
	Note     string
}

// Picker is a filter field over a window of Rows options: typing filters by
// label ignoring case, up and down move the highlight and scroll the window,
// enter or a click picks, the wheel scrolls.
type Picker struct {
	id       string
	Options  []PickOption
	Rows     int
	Disabled bool
	filter   string
	cursor   int // index into the filtered options
	top      int // first filtered option in the window
	picked   int // index into Options; -1 when nothing is picked
	advance  bool
}

func NewPicker(id string, rows int) *Picker { return &Picker{id: id, Rows: rows, picked: -1} }

func (p *Picker) ID() string         { return p.id }
func (p *Picker) Focusable() bool    { return !p.Disabled }
func (p *Picker) Capturing() bool    { return false }
func (p *Picker) Blur()              {}
func (p *Picker) SetDisabled(d bool) { p.Disabled = d }
func (p *Picker) Filter() string     { return p.filter }

// Window returns the highlight and the first row of the window, so a caller
// that renders with trial Rows can put them back between renders: View clamps
// the window to the Rows it is given.
func (p *Picker) Window() (cursor, top int) { return p.cursor, p.top }

// SetWindow restores what Window returned.
func (p *Picker) SetWindow(cursor, top int) { p.cursor, p.top = cursor, top }

// SetOptions replaces the options and resets the window. The pick survives
// when its label is still offered and enabled.
func (p *Picker) SetOptions(opts []PickOption) {
	label := ""
	if o, ok := p.Picked(); ok {
		label = o.Label
	}
	p.Options, p.picked, p.cursor, p.top = opts, -1, 0, 0
	for i, o := range opts {
		if label != "" && o.Label == label && !o.Disabled {
			p.picked = i
		}
	}
}

// Picked returns the picked option.
func (p *Picker) Picked() (PickOption, bool) {
	if p.picked < 0 || p.picked >= len(p.Options) {
		return PickOption{}, false
	}
	return p.Options[p.picked], true
}

// visible lists the indexes of the options whose label contains the filter.
func (p *Picker) visible() []int {
	f := strings.ToLower(p.filter)
	var out []int
	for i, o := range p.Options {
		if strings.Contains(strings.ToLower(o.Label), f) {
			out = append(out, i)
		}
	}
	return out
}

func (p *Picker) rowZone(i int) string { return ZoneID(p.id, fmt.Sprintf("row-%d", i)) }

// TakesKey: printable keys and backspace edit the filter; up, down and enter
// work the list. Tab and esc belong to the dialog.
func (p *Picker) TakesKey(k tea.KeyMsg) bool {
	if printable(k) {
		return true
	}
	switch k.String() {
	case "backspace", "up", "down", "enter":
		return true
	}
	return false
}

// shown returns the visible indexes and the window [from, to) over them. It
// first moves the window back over the highlight, which a change of Rows can
// leave outside it.
func (p *Picker) shown() (vis []int, from, to int) {
	vis = p.visible()
	p.cursor = min(max(p.cursor, 0), max(len(vis)-1, 0))
	p.top = min(p.top, max(len(vis)-p.Rows, 0))
	p.top = min(max(p.top, p.cursor-p.Rows+1, 0), p.cursor)
	return vis, p.top, min(p.top+p.Rows, len(vis))
}

func (p *Picker) Hit(msg tea.MouseMsg) bool {
	if zone.Get(p.id).InBounds(msg) {
		return true
	}
	vis, from, to := p.shown()
	for _, i := range vis[from:to] {
		if zone.Get(p.rowZone(i)).InBounds(msg) {
			return true
		}
	}
	return false
}

func (p *Picker) takeAdvance() bool {
	a := p.advance
	p.advance = false
	return a
}

// move shifts the highlight by d within n options, scrolling it into the window.
func (p *Picker) move(d, n int) {
	if n == 0 {
		return
	}
	p.cursor = min(max(p.cursor+d, 0), n-1)
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+p.Rows {
		p.top = p.cursor - p.Rows + 1
	}
}

// pick picks option i unless it is disabled; a pick asks to advance focus.
func (p *Picker) pick(i int) {
	if !p.Options[i].Disabled {
		p.picked, p.advance = i, true
	}
}

func (p *Picker) Update(msg tea.Msg) (Control, tea.Cmd) {
	p.advance = false
	if p.Disabled {
		return p, nil
	}
	vis, from, to := p.shown()
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeySpace:
			p.filter += " "
			p.cursor, p.top = 0, 0
		case printable(msg):
			p.filter += string(msg.Runes)
			p.cursor, p.top = 0, 0
		case msg.String() == "backspace":
			if r := []rune(p.filter); len(r) > 0 {
				p.filter = string(r[:len(r)-1])
				p.cursor, p.top = 0, 0
			}
		case msg.String() == "up":
			p.move(-1, len(vis))
		case msg.String() == "down":
			p.move(1, len(vis))
		case msg.String() == "enter":
			if p.cursor < len(vis) {
				p.pick(vis[p.cursor])
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			d := 1
			if msg.Button == tea.MouseButtonWheelUp {
				d = -1
			}
			p.top = min(max(p.top+d, 0), max(len(vis)-p.Rows, 0))
			p.cursor = min(max(p.cursor, p.top), p.top+p.Rows-1)
			return p, nil
		}
		if !clicked(msg) {
			return p, nil
		}
		for n := from; n < to; n++ {
			if zone.Get(p.rowZone(vis[n])).InBounds(msg) {
				p.cursor = n
				p.pick(vis[n])
			}
		}
	}
	return p, nil
}

// View renders the filter field and, below it, the window of options cut to
// width, with a line counting the options outside the window.
func (p *Picker) View(focused bool, width int) string {
	w := max(width, 16)
	style := Bold
	if p.Disabled {
		style = Dim
	}
	text := p.filter
	if text == "" {
		text = Dim.Render("type to filter")
	}
	lines := []string{glyph(focused) + zone.Mark(p.id, style.Render("[ ")+Cell(text, w-6)+style.Render(" ]"))}
	vis, from, to := p.shown()
	switch {
	case len(p.Options) == 0:
		lines = append(lines, "  "+Dim.Render("nothing to pick"))
	case len(vis) == 0:
		lines = append(lines, "  "+Dim.Render("no match"))
	}
	for n := from; n < to; n++ {
		o := p.Options[vis[n]]
		mark := "  "
		if n == p.cursor {
			mark = Accent.Render("▸ ")
		}
		text := o.Label
		if o.Disabled {
			text = Dim.Render(o.Label)
		}
		if o.Badge != "" {
			text += "  " + o.Badge
		}
		if o.Disabled && o.Note != "" {
			text += "  " + Dim.Render(o.Note)
		}
		lines = append(lines, "  "+zone.Mark(p.rowZone(vis[n]), Cell(mark+text, w-2)))
	}
	if more := len(vis) - (to - from); more > 0 {
		lines = append(lines, "  "+Cell(Dim.Render(fmt.Sprintf("%d more, ↑↓ to scroll", more)), w-2))
	}
	return strings.Join(lines, "\n")
}
