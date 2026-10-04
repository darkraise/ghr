package tui

import (
	"strings"
	"testing"

	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const settingsYAML = `owner: darkraise
mode: queue
global_max: 3
labels: [homelab]
repos:
  - name: darkcloud
    max: 2
    labels: [darkcloud-linux]
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
  - name: darkagents
    paused: true
`

func parseConfig(t *testing.T, y string) *config.Config {
	t.Helper()
	cfg, _, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// settingsModel is the sample model with settingsYAML loaded into the form,
// without going through the page wiring.
func settingsModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := sampleModel(&fakeClient{}, w, h)
	m.cfg = parseConfig(t, settingsYAML)
	m.settings.load(m.cfg)
	return m
}

// settingsLines renders the whole form, unscrolled, as plain lines.
func settingsLines(m Model, w int) []string {
	return strings.Split(zone.Scan(m.settingsView(w, 1000)), "\n")
}

func lineWith(lines []string, s string) int {
	for i, l := range lines {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func TestSettingsShowsEverySettingButOwnerAsAControl(t *testing.T) {
	m := settingsModel(t, 140, 40)
	v := strings.Join(settingsLines(m, 123), "\n")
	for _, want := range []string{
		"[ queue ▾ ]", "start runners only for queued jobs", "[ − ] 3 [ + ]", "applies in queue mode",
		"Owner", "darkraise", "change in config.yaml and restart the daemon",
		"[ 10s", "[ 2m0s", "[ 5m0s", "80%", "[ 20GB", "[ 30d", "homelab ✕", "[ 6G", "[ 200%",
		"limits apply to newly started runners",
		"darkcloud", "darkcloud-linux ✕", "dc-e2e- ✕", "darkmem", "darkagents (paused)",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "› [ darkraise") || strings.Contains(v, "[ darkraise") {
		t.Error("owner is editable")
	}
	lines := settingsLines(m, 123)
	cloud, mem := lineWith(lines, "─ darkcloud "), lineWith(lines, "─ darkmem ")
	if !strings.Contains(lines[cloud+1], "[ − ] 2 [ + ]") || strings.Contains(lines[cloud+1], "(default)") {
		t.Errorf("explicit max: %q", lines[cloud+1])
	}
	if !strings.Contains(lines[mem+1], "[ − ] 1 [ + ] (default)") || !strings.Contains(lines[mem+2], "[ − ] 1 [ + ] (default)") {
		t.Errorf("default max and warm: %q / %q", lines[mem+1], lines[mem+2])
	}
}

func TestSettingsRepoDefaultFollowsEditedMode(t *testing.T) {
	m := settingsModel(t, 140, 40)
	m.settings.input(setMode).SetValue(ui.Value{Text: config.ModeAll})
	lines := settingsLines(m, 123)
	mem := lineWith(lines, "─ darkmem ")
	if !strings.Contains(lines[mem+1], "[ − ] ∞ [ + ] (default)") {
		t.Fatalf("all-mode default max: %q", lines[mem+1])
	}
}

func TestSettingsRemovingRepoIsDisabled(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 40)
	m.cfg = parseConfig(t, strings.Replace(settingsYAML, "  - name: darkmem\n", "  - name: darkmem\n    paused: true\n    removing: true\n", 1))
	m.settings.load(m.cfg)
	v := strings.Join(settingsLines(m, 123), "\n")
	if !strings.Contains(v, "darkmem (removing…)") || !strings.Contains(v, "its running jobs finish first") {
		t.Fatalf("removing repo not marked:\n%s", v)
	}
	for _, f := range []string{"max", "warm", "labels", "cleanup"} {
		if m.settings.input(repoKey("darkmem", f)).Focusable() {
			t.Errorf("darkmem %s is focusable while removing", f)
		}
	}
	if !m.settings.input(repoKey("darkcloud", "max")).Focusable() {
		t.Error("another repo was disabled")
	}
}

func TestSettingsDisabledWhileUnreachableKeepsEdits(t *testing.T) {
	m := settingsModel(t, 140, 40)
	m.settings.input(setPollInterval).SetValue(ui.Value{Text: "30s"})
	m.connected = false
	m.settingsView(123, 40)
	for _, f := range m.settings.form.Fields() {
		if f.Input.Focusable() {
			t.Fatalf("%s is focusable while unreachable", f.Key)
		}
	}
	if m.settings.group.Focused() != nil {
		t.Fatal("something has focus while unreachable")
	}
	m.connected = true
	m.settingsView(123, 40)
	if got := m.settings.input(setPollInterval).Value().Text; got != "30s" || m.settings.group.FocusedID() != setMode {
		t.Fatalf("after reconnect: poll %q focus %q", got, m.settings.group.FocusedID())
	}
}

func TestSettingsNarrowPutsDescriptionsBelow(t *testing.T) {
	m := settingsModel(t, 80, 40)
	lines := settingsLines(m, 80)
	i := lineWith(lines, "[ queue ▾ ]")
	if strings.Contains(lines[i], "start runners") || !strings.Contains(lines[i+1], "start runners only") {
		t.Fatalf("narrow description placement:\n%s\n%s", lines[i], lines[i+1])
	}
}

// Config text is rendered through clean, like any other daemon text.
func TestSettingsSanitisesConfigText(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 40)
	// YAML's \e and \a escapes put real ESC and BEL bytes into the parsed config.
	m.cfg = parseConfig(t, `owner: "evil\e]0;pwned\a"
labels: ["bad\e[2Jlabel"]
repos:
  - name: darkmem
    cleanup_name_prefixes: ["x\e]52;c;Zm9v\ay"]
`)
	m.settings.load(m.cfg)
	v := strings.Join(settingsLines(m, 123), "\n")
	if strings.ContainsAny(v, "\x1b\a") || !strings.Contains(v, "evil") || !strings.Contains(v, "badlabel") || !strings.Contains(v, "xy") {
		t.Fatalf("config text not sanitised: %q", v)
	}
}

func TestSettingsViewClampsScroll(t *testing.T) {
	m := settingsModel(t, 140, 40)
	all := settingsLines(m, 123)
	m.settings.scroll = 999
	v := strings.Split(zone.Scan(m.settingsView(123, 10)), "\n")
	if m.settings.scroll != len(all)-10 || v[9] != all[len(all)-1] {
		t.Fatalf("scroll %d of %d lines; last line %q", m.settings.scroll, len(all), v[9])
	}
	m.settings.scroll = -5
	m.settingsView(123, 10)
	if m.settings.scroll != 0 {
		t.Fatalf("negative scroll kept: %d", m.settings.scroll)
	}
}
