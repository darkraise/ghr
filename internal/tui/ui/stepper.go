package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Stepper edits a bounded integer with − and +, the arrow keys, or typed digits.
// While unset it shows Default with a "(default)" suffix; the first change sets it.
type Stepper struct {
	id             string
	Min, Max, Step int
	Default        int    // the effective value while unset
	DefaultText    string // shown instead of Default while unset, when not empty
	ZeroText       string // shown for 0 when not empty (∞ for an unlimited cap)
	Suffix         string // appended to the number ("%")
	Disabled       bool
	val            int
	set            bool
	typed          string
}

func NewStepper(id string, lo, hi, step int) *Stepper {
	return &Stepper{id: id, Min: lo, Max: hi, Step: step, val: lo, set: true}
}

func (s *Stepper) ID() string         { return s.id }
func (s *Stepper) Focusable() bool    { return !s.Disabled }
func (s *Stepper) Capturing() bool    { return false }
func (s *Stepper) Blur()              { s.typed = "" }
func (s *Stepper) SetDisabled(d bool) { s.Disabled = d }
func (s *Stepper) Value() Value       { return Value{Num: s.val, Set: s.set} }
func (s *Stepper) SetValue(v Value)   { s.val, s.set, s.typed = v.Num, v.Set, "" }

func (s *Stepper) Hit(msg tea.MouseMsg) bool { return zone.Get(s.id).InBounds(msg) }

func (s *Stepper) TakesKey(k tea.KeyMsg) bool {
	switch k.String() {
	case "left", "right", "+", "=", "-", "backspace":
		return true
	}
	return isDigit(k)
}

func isDigit(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && len(k.Runes) == 1 && k.Runes[0] >= '0' && k.Runes[0] <= '9'
}

func (s *Stepper) clamp(n int) int { return min(max(n, s.Min), s.Max) }

// bump steps by d. A value outside the range (loaded from a hand-edited
// config) moves toward it rather than jumping to the bound.
func (s *Stepper) bump(d int) {
	start := s.val
	if !s.set {
		start = s.Default
	}
	n := start + d
	if d > 0 {
		n = min(n, max(start, s.Max))
	} else {
		n = max(n, min(start, s.Min))
	}
	s.val, s.set, s.typed = n, true, ""
}

func (s *Stepper) Update(msg tea.Msg) (Control, tea.Cmd) {
	if s.Disabled {
		return s, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch k := msg.String(); {
		case k == "right" || k == "+" || k == "=":
			s.bump(s.Step)
		case k == "left" || k == "-":
			s.bump(-s.Step)
		case k == "backspace":
			if s.typed != "" {
				s.typed = s.typed[:len(s.typed)-1]
				n, _ := strconv.Atoi(s.typed) // "" parses as 0, which clamps to Min
				s.val, s.set = s.clamp(n), true
			}
		case isDigit(msg):
			// Typed digits build a number; one that would pass Max starts over.
			s.typed += k
			if n, _ := strconv.Atoi(s.typed); n > s.Max {
				s.typed = k
			}
			n, _ := strconv.Atoi(s.typed)
			s.val, s.set = s.clamp(n), true
		}
	case tea.MouseMsg:
		if !clicked(msg) {
			return s, nil
		}
		if zone.Get(ZoneID(s.id, "dec")).InBounds(msg) {
			s.bump(-s.Step)
		} else if zone.Get(ZoneID(s.id, "inc")).InBounds(msg) {
			s.bump(s.Step)
		}
	}
	return s, nil
}

func (s *Stepper) text() string {
	if !s.set && s.DefaultText != "" {
		return s.DefaultText
	}
	n := s.val
	if !s.set {
		n = s.Default
	}
	if n == 0 && s.ZeroText != "" {
		return s.ZeroText
	}
	return strconv.Itoa(n) + s.Suffix
}

// View renders [ − ] 2 [ + ], with " (default)" while unset.
func (s *Stepper) View(focused bool, _ int) string {
	style := Bold
	if s.Disabled {
		style = Dim
	}
	body := zone.Mark(ZoneID(s.id, "dec"), style.Render("[ − ]")) + " " + style.Render(s.text()) + " " +
		zone.Mark(ZoneID(s.id, "inc"), style.Render("[ + ]"))
	if !s.set {
		body += Dim.Render(" (default)")
	}
	return glyph(focused) + zone.Mark(s.id, body)
}
