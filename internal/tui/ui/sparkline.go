package ui

import "strings"

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Sparkline draws the newest width values scaled to limit, newest on the
// right. A nil value draws a space; fewer values than width are padded on
// the left.
func Sparkline(vals []*float64, limit float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", width-len(vals)))
	top := len(sparkRunes) - 1
	for _, v := range vals {
		if v == nil {
			b.WriteByte(' ')
			continue
		}
		i := 0
		if limit > 0 {
			i = int(*v/limit*float64(top) + 0.5)
		}
		b.WriteRune(sparkRunes[min(max(i, 0), top)])
	}
	return b.String()
}

// Gauge draws used out of total as a bar width cells wide.
func Gauge(used, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	filled := min(used*width/total, width)
	return "▕" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "▏"
}
