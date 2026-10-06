package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

func nodeChoices() map[string][]model.ToolchainChoice {
	return map[string][]model.ToolchainChoice{
		"node": {{Spec: "24.9.0", Version: "24.9.0"}, {Spec: "22.11.0", Version: "22.11.0", LTS: true},
			{Spec: "22.10.0", Version: "22.10.0", LTS: true}},
		"python": {{Spec: "3.13.7", Version: "3.13.7"}},
	}
}

func TestInstallDialogInstallsAPickedVersion(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	if m.overlay != ovInstall || strings.Join(c.availTools, ",") != "node" {
		t.Fatalf("overlay %v, fetched %v", m.overlay, c.availTools)
	}
	v := m.View()
	for _, want := range []string{"Install toolchain", "Node.js", "24.9.0", "22.11.0  [LTS]", "pick one below, or type a version"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	m = feed(m, keys("tab", "2", "2", "enter")...)
	if v := m.View(); strings.Contains(v, "24.9.0") || !strings.Contains(v, "22.10.0") {
		t.Fatalf("filter 22:\n%s", v)
	}
	if f := m.inst.group.FocusedID(); f != instOK {
		t.Fatalf("after a pick focus is on %q, want Install", f)
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "install node 22.11.0" || m.overlay != ovNone {
		t.Fatalf("actions %q overlay %v", got, m.overlay)
	}
	if !strings.Contains(m.View(), "queued: install node 22.11.0") {
		t.Fatalf("toast:\n%s", m.View())
	}
}

func TestInstallDialogTakesATypedVersion(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, keys("tab", "2", "4", "tab", "tab", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "install node 24" {
		t.Fatalf("actions %q", got)
	}
}

func TestInstallDialogFetchesTheChosenTool(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, key("right"))
	if v := m.View(); strings.Join(c.availTools, ",") != "node,python" || !strings.Contains(v, "Python") || !strings.Contains(v, "3.13.7") {
		t.Fatalf("fetched %v:\n%s", c.availTools, v)
	}
	if m = feed(m, key("esc")); m.overlay != ovNone || m.inst != nil {
		t.Fatalf("esc left overlay %v", m.overlay)
	}
}

func TestInstallDialogErrorsStayInside(t *testing.T) {
	c := &fakeClient{choices: nodeChoices(), choicesErr: errors.New("go.dev: 503")}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	if v := m.View(); !strings.Contains(v, "✖ go.dev: 503") || !strings.Contains(v, "( Retry )") {
		t.Fatalf("list error:\n%s", v)
	}
	c.choicesErr = nil
	m = click(t, m, instRetry)
	if v := m.View(); len(c.availTools) != 2 || !strings.Contains(v, "22.11.0") {
		t.Fatalf("retry fetched %v:\n%s", c.availTools, v)
	}
	c.installErr = errors.New("unknown tool")
	m.inst.group.Focus(instPick) // the retried list replaced Retry, so focus moved off it
	m = feed(m, keys("2", "2", "enter", "enter")...)
	if v := m.View(); m.overlay != ovInstall || !strings.Contains(v, "✖ unknown tool") {
		t.Fatalf("install error:\n%s", v)
	}
}

func TestInstallDialogCleansTheDaemonsVersions(t *testing.T) {
	dirty := "24.9.0\x1b]0;pwned\a"
	c := &fakeClient{choices: map[string][]model.ToolchainChoice{"node": {{Spec: dirty, Version: dirty}}}}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, keys("tab", "enter")...)
	if v := m.View(); strings.Contains(v, "pwned") || strings.ContainsAny(v, "\x1b\a") {
		t.Fatalf("an escape sequence reached the dialog:\n%q", v)
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "install node 24.9.0" {
		t.Fatalf("actions %q", got)
	}
	if v := m.View(); strings.Contains(v, "pwned") || strings.ContainsAny(v, "\x1b\a") {
		t.Fatalf("an escape sequence reached the toast:\n%q", v)
	}
}

func TestInstallDialogChangingToolDropsTheTypedFilter(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, keys("tab", "2", "2", "shift+tab", "right")...)
	if got := m.inst.target(); got != "" || m.inst.ok.Focusable() {
		t.Fatalf("a node filter survived the switch to python: target %q, install enabled %v", got, m.inst.ok.Focusable())
	}
	if v := m.View(); !strings.Contains(v, "3.13.7") {
		t.Fatalf("the python list is filtered by the old text:\n%s", v)
	}
}

func TestInstallDialogRejectionKeepsTheRetryTime(t *testing.T) {
	retry := now.Add(time.Hour)
	c := &fakeClient{choices: nodeChoices(), installErr: &api.Error{Status: 429, Msg: "rate limited", RetryAt: retry}}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, keys("tab", "2", "2", "enter", "enter")...)
	if v := m.View(); !strings.Contains(v, "✖ rate limited, try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("install error:\n%s", v)
	}
}
