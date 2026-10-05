package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

func tokenStatus(days int) model.TokenStatus {
	exp := now.Add(time.Duration(days) * 24 * time.Hour)
	at := now.Add(-12 * time.Second)
	rem, lim := 4800, 5000
	return model.TokenStatus{State: "ok", ExpiresAt: &exp, CheckedAt: &at, RateRemaining: &rem, RateLimit: &lim}
}

func TestTokenSection(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87)}
	m := onSettings(t, c, 140, 60)
	v := m.View()
	for _, want := range []string{"GitHub token", "[OK]", "expires 2026-12-29 (87 days)", "4800 / 5000", "checked just now",
		"Replacing checks read access to the first repository only", "[ Replace token ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	c.token = tokenStatus(10)
	m = feed(m, tokenMsg{seq: m.mg.tokenSeq, ts: c.token})
	if v := m.View(); !strings.Contains(v, "[EXPIRES SOON]") {
		t.Fatalf("expiring:\n%s", v)
	}
	m = feed(m, tokenMsg{seq: m.mg.tokenSeq - 1, ts: model.TokenStatus{State: "rejected"}})
	if strings.Contains(m.View(), "[REJECTED]") {
		t.Fatal("a stale token reply was shown")
	}
}

// A failed token read stays visible with a Retry button until a retry succeeds.
func TestTokenSectionErrorRetries(t *testing.T) {
	c := &fakeClient{tokenErr: &api.Error{Status: 502, Msg: "daemon busy"}}
	m := onSettings(t, c, 140, 60)
	if v := m.View(); !strings.Contains(v, "✖ daemon busy") || !strings.Contains(v, "( Retry )") {
		t.Fatalf("error:\n%s", v)
	}
	c.tokenErr, c.token = nil, tokenStatus(87)
	m = click(t, m, setTokenRetry)
	if v := m.View(); !strings.Contains(v, "[OK]") || strings.Contains(v, "daemon busy") {
		t.Fatalf("after retry:\n%s", v)
	}
}

func TestTokenDialogMasksAndReplaces(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87)}
	m := onSettings(t, c, 140, 60)
	if m = click(t, m, setReplaceToken); m.overlay != ovToken {
		t.Fatalf("overlay %v", m.overlay)
	}
	m = feed(m, keys("g", "h", "p", "_", "n", "e", "w")...)
	for _, state := range []string{"editing", "blurred"} {
		if v := m.View(); strings.Contains(v, "ghp_new") || !strings.Contains(v, "•••••••") {
			t.Fatalf("%s: token visible or not masked:\n%s", state, v)
		}
		m = feed(m, key("tab"))
	}
	m.tok.group.Focus(tokOK)
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "set-token 7 chars" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone || !strings.Contains(m.View(), "GitHub token replaced") {
		t.Fatalf("after replace: overlay %v", m.overlay)
	}
}

func TestTokenDialogRejectionAndLateReply(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87), setTokenErr: &api.Error{Status: 400, Msg: "new token rejected: 401 Bad credentials"}}
	m := onSettings(t, c, 140, 60)
	m = click(t, m, setReplaceToken)
	m = feed(m, keys("b", "a", "d", "enter")...) // enter commits the field and moves focus to Cancel
	m.tok.group.Focus(tokOK)
	m = feed(m, key("enter"))
	if m.overlay != ovToken || m.tok == nil {
		t.Fatalf("rejection closed the dialog: overlay %v", m.overlay)
	}
	if v := m.View(); !strings.Contains(v, "new token rejected") || m.tok.field.Value().Text != "" {
		t.Fatalf("rejection: field %q\n%s", m.tok.field.Value().Text, v)
	}
	first := m.tok
	m = feed(m, key("esc"))
	m = click(t, m, setReplaceToken)
	upd, _ := m.Update(tokenSetMsg{d: first, err: nil}) // a late reply for the closed dialog
	m = upd.(Model)
	if m.overlay != ovToken || m.tok == first {
		t.Fatal("a late reply changed the new dialog")
	}
	var _ tea.Model = m
}

// While a replacement is in flight the field takes no input, and whatever
// the outcome no token text stays in the dialog.
func TestTokenDialogFieldLockedWhilePending(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87), setTokenErr: &api.Error{Status: 400, Msg: "new token rejected: 401 Bad credentials"}}
	m := click(t, onSettings(t, c, 140, 60), setReplaceToken)
	m = feed(m, keys("o", "l", "d")...)
	upd, cmd := m.pressed(tokOK) // the request is in flight until cmd runs
	m = upd.(Model)
	m.tok.group.Focus(tokField)
	m = feed(m, keys("n", "e", "w")...)
	if m.tok.field.Value().Text != "" || m.tok.field.Focusable() {
		t.Fatalf("field editable while pending: %q", m.tok.field.Value().Text)
	}
	m = feed(m, cmd())
	if m.overlay != ovToken || m.tok.field.Value().Text != "" || !m.tok.field.Focusable() || m.tok.busy {
		t.Fatalf("after rejection: overlay %v field %q busy %v", m.overlay, m.tok.field.Value().Text, m.tok != nil && m.tok.busy)
	}
}

// The dialog follows the connection: Replace is disabled and refuses while
// the daemon is unreachable, but a degraded daemon still accepts a token.
func TestTokenDialogFollowsConnection(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87)}
	m := click(t, onSettings(t, c, 140, 60), setReplaceToken)
	m = feed(m, keys("n", "e", "w")...)
	m.connected = false
	if m.View(); !m.tok.ok.Disabled {
		t.Fatal("Replace enabled while unreachable")
	}
	upd, cmd := m.pressed(tokOK)
	m = upd.(Model)
	if cmd != nil || len(c.actions()) != 0 || !strings.Contains(m.View(), "the daemon is unreachable") {
		t.Fatalf("replace sent while unreachable: %v", c.actions())
	}
	m.connected = true
	st := sampleStatus()
	st.Degraded, st.DegradedReason = true, "GitHub rejected the token"
	m = feed(m, statusMsg{st: st})
	if m.View(); m.tok.ok.Disabled {
		t.Fatal("Replace disabled while degraded")
	}
}

func TestMaintenanceSection(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87), warnings: []string{"labels: duplicate"}}
	m := onSettings(t, c, 140, 70)
	v := m.View()
	for _, want := range []string{"Maintenance", "61% used", "not pruned since the daemon started", "[ Reload config.yaml ]", "[ Prune now ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	m = click(t, m, setReload)
	if !strings.Contains(m.View(), "config reloaded (1 warning, see Activity)") {
		t.Fatalf("reload toast:\n%s", m.View())
	}
	m = click(t, m, setPrune)
	if got := strings.Join(c.actions(), "|"); got != "reload|prune" || !strings.Contains(m.View(), "prune started — see Activity") {
		t.Fatalf("actions %q\n%s", got, m.View())
	}
	st := sampleStatus()
	st.Maintenance.Running = true
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "pruning…") || m.mg.prune.Focusable() {
		t.Fatalf("running prune:\n%s", v)
	}
	done := now.Add(-2 * time.Hour)
	st.Maintenance = model.MaintenanceStatus{LastFinished: &done, LastOutcome: "ok"}
	if v := feed(m, statusMsg{st: st}).View(); !strings.Contains(v, "last pruned 2h ago (ok)") {
		t.Fatalf("finished prune:\n%s", v)
	}
	c.reloadErr = &api.Error{Status: 400, Msg: "owner changed from darkraise to x: restart ghr to switch owners"}
	if v := click(t, m, setReload).View(); !strings.Contains(v, "reload rejected: owner changed") {
		t.Fatalf("rejected reload:\n%s", v)
	}
}
