package ui

import (
	"time"

	zone "github.com/lrstanley/bubblezone"
)

const (
	ToastOK    = 4 * time.Second
	ToastError = 10 * time.Second
	toastClose = "toast/close"
)

// Toast is the single transient message on the toast line. A newer one
// replaces the current one.
type Toast struct {
	Text  string
	Err   bool
	until time.Time
}

// Show replaces the toast; it clears after ToastOK, or ToastError for an error.
func (t *Toast) Show(text string, isErr bool, now time.Time) {
	d := ToastOK
	if isErr {
		d = ToastError
	}
	t.Text, t.Err, t.until = text, isErr, now.Add(d)
}

// Tick clears the toast once its time is up.
func (t *Toast) Tick(now time.Time) {
	if t.Text != "" && !now.Before(t.until) {
		t.Close()
	}
}

func (t *Toast) Close() { t.Text, t.Err = "", false }

func (t *Toast) Active() bool { return t.Text != "" }

// CloseZone is the zone of the toast's ✕.
func (t *Toast) CloseZone() string { return toastClose }

// View renders the toast across width columns with a clickable ✕ at the
// right, or "" when no toast is active.
func (t *Toast) View(width int) string {
	if t.Text == "" {
		return ""
	}
	icon, style := "✔ ", Green
	if t.Err {
		icon, style = "✖ ", Red
	}
	return " " + style.Render(Cell(icon+t.Text, max(width-3, 1))) + " " + zone.Mark(toastClose, Dim.Render("✕"))
}
