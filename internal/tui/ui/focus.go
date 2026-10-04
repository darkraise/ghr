package ui

import tea "github.com/charmbracelet/bubbletea"

// Group is the ordered set of widgets on a page or dialog that focus moves
// between. It tracks focus by widget ID, so a page can rebuild its widget
// list every frame without losing the focused control.
type Group struct {
	items []Widget
	focus string
	at    int // index of the focused widget the last time it was found
}

// Set replaces the widgets. Focus stays on the same ID when it is still
// present and focusable; otherwise it moves to the next focusable widget.
func (g *Group) Set(items []Widget) {
	g.items = items
	for i, w := range items {
		if w.ID() == g.focus {
			g.at = i
		}
	}
	w := g.Focused()
	if w != nil && w.Focusable() {
		return
	}
	if w != nil {
		w.Blur()
	}
	g.focus = ""
	n := len(items)
	start := min(g.at, n-1)
	for i := 0; i < n; i++ {
		if w := items[(start+i)%n]; w.Focusable() {
			g.focus, g.at = w.ID(), (start+i)%n
			return
		}
	}
}

func (g *Group) Items() []Widget { return g.items }

func (g *Group) FocusedID() string { return g.focus }

// Focused returns the focused widget, or nil.
func (g *Group) Focused() Widget {
	for _, w := range g.items {
		if w.ID() == g.focus {
			return w
		}
	}
	return nil
}

// Focus moves focus to id when it names a focusable widget.
func (g *Group) Focus(id string) bool {
	for i, w := range g.items {
		if w.ID() == id && w.Focusable() {
			if old := g.Focused(); old != nil && old != w {
				old.Blur()
			}
			g.focus, g.at = id, i
			return true
		}
	}
	return false
}

func (g *Group) Next() { g.step(1) }
func (g *Group) Prev() { g.step(-1) }

// step moves focus d places, skipping widgets that cannot take focus, and wraps.
func (g *Group) step(d int) {
	n := len(g.items)
	if n == 0 {
		return
	}
	cur := -1
	for i, w := range g.items {
		if w.ID() == g.focus {
			cur = i
		}
	}
	if cur < 0 {
		cur = min(g.at, n-1)
		if d > 0 {
			cur--
		}
	}
	for i := 1; i <= n; i++ {
		if w := g.items[((cur+d*i)%n+n)%n]; w.Focusable() {
			g.Focus(w.ID())
			return
		}
	}
}

// Key gives k to the focused widget when it takes it.
func (g *Group) Key(k tea.KeyMsg) (bool, tea.Cmd) {
	w := g.Focused()
	if w == nil || !w.TakesKey(k) {
		return false, nil
	}
	_, cmd := w.Update(k)
	return true, cmd
}

// Mouse handles a click: a capturing widget gets every click; otherwise the
// widget under the pointer takes focus and the click. It reports false when
// the event was not a click on one of the group's widgets.
func (g *Group) Mouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	if !clicked(msg) {
		return false, nil
	}
	if w := g.Focused(); w != nil && w.Capturing() {
		_, cmd := w.Update(msg)
		return true, cmd
	}
	for _, w := range g.items {
		if w.Focusable() && w.Hit(msg) {
			g.Focus(w.ID())
			_, cmd := w.Update(msg)
			return true, cmd
		}
	}
	return false, nil
}
