package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// onSettings is the sample model on the Settings page with settingsYAML loaded.
func onSettings(t *testing.T, c *fakeClient, w, h int) Model {
	t.Helper()
	c.cfg = parseConfig(t, settingsYAML)
	return feed(sampleModel(c, w, h), key("4"))
}

func quits(cmd tea.Cmd) bool {
	for _, msg := range collect(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func TestSettingsFocusOrderScrollsIntoView(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 22)
	m.View()
	if got := m.settings.group.FocusedID(); got != setMode {
		t.Fatalf("initial focus %q", got)
	}
	m = feed(m, keys("j", "tab", "down")...) // j moves focus off a select; a text field would take it
	if got := m.settings.group.FocusedID(); got != setStartTimeout {
		t.Fatalf("after j, tab, down: %q", got)
	}
	m = feed(m, keys("up", "shift+tab", "k")...)
	if got := m.settings.group.FocusedID(); got != setMode {
		t.Fatalf("after up, shift+tab, k: %q", got)
	}
	m = feed(m, key("shift+tab")) // wraps to the last control
	if got := m.settings.group.FocusedID(); got != repoKey("darkagents", "cleanup") {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "Cleanup prefixes   › + add") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
		t.Fatalf("last control not scrolled into view (scroll %d):\n%s", m.settings.scroll, v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); !strings.Contains(v, "Mode               › [ queue ▾ ]") || m.settings.scroll != 0 {
		t.Fatalf("first control not scrolled back into view:\n%s", v)
	}
}

// A focused stepper takes digits, so they do not switch pages; a focused
// text field takes letters, so q types instead of quitting.
func TestSettingsKeyPrecedence(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	m = feed(m, keys("tab", "2")...)
	if m.page != pageSettings || m.settings.input(setGlobalMax).Value().Num != 2 {
		t.Fatalf("digit: page %v global max %+v", m.page, m.settings.input(setGlobalMax).Value())
	}
	m = feed(m, keys("tab", "q")...)
	f := m.settings.input(setPollInterval).(*ui.TextField)
	if !f.Editing() || f.Value().Text != "q" {
		t.Fatalf("letter: editing %v value %q", f.Editing(), f.Value().Text)
	}
	upd, cmd := m.Update(key("q"))
	if m = upd.(Model); quits(cmd) || f.Value().Text != "qq" {
		t.Fatal("q quit while a text field was being edited")
	}
	m = feed(m, keys("esc", "esc")...) // stop editing; then nothing to back out of
	if f.Editing() || m.page != pageSettings || f.Value().Text != "qq" {
		t.Fatalf("esc: editing %v page %v value %q", f.Editing(), m.page, f.Value().Text)
	}
	m.settings.group.Focus(setMode) // a closed select passes these keys to the page
	for _, k := range []string{"m", "]", "+", "-"} {
		m = feed(m, key(k))
	}
	if len(c.actions()) != 0 {
		t.Fatalf("config keys acted on the Settings page: %v", c.actions())
	}
}

func TestSettingsSelectOpensInlineAndHoldsKeys(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m = feed(m, key("enter"))
	v := m.View()
	if !strings.Contains(v, "▸ queue  start runners only") || !strings.Contains(v, "all  keep warm runners") {
		t.Fatalf("dropdown not open inline:\n%s", v)
	}
	m = feed(m, keys("1", "q", "down", "enter")...)
	if m.page != pageSettings || m.settings.input(setMode).Value().Text != config.ModeAll {
		t.Fatalf("page %v mode %q", m.page, m.settings.input(setMode).Value().Text)
	}
	if v := m.View(); !strings.Contains(v, "Mode               › [ all ▾ ] ●") {
		t.Fatalf("picked mode not marked dirty:\n%s", v)
	}
}

func TestSettingsTextFieldEnterAdvances(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m.View()
	m.settings.group.Focus(setPollInterval)
	m = feed(m, keys("enter", "backspace", "backspace", "backspace", "1", "5", "s", "enter")...)
	if got := m.settings.input(setPollInterval).Value().Text; got != "15s" {
		t.Fatalf("value %q", got)
	}
	if got := m.settings.group.FocusedID(); got != setStartTimeout {
		t.Fatalf("enter did not advance focus: %q", got)
	}
}

func TestSettingsMouse(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m = click(t, m, setPollInterval)
	if f := m.settings.input(setPollInterval).(*ui.TextField); m.settings.group.FocusedID() != setPollInterval || !f.Editing() {
		t.Fatal("click did not focus and edit the field")
	}
	m = click(t, m, setMode)
	if !m.settings.input(setMode).(*ui.Select).Open() {
		t.Fatal("click did not open the select")
	}
	m = click(t, m, "nav/history") // swallowed by the open dropdown
	if m.page != pageSettings || m.settings.input(setMode).(*ui.Select).Open() {
		t.Fatalf("outside click: page %v", m.page)
	}
	z := zoneOf(t, m, "settings/body")
	m = feed(m, tea.MouseMsg{X: z.StartX + 2, Y: z.StartY + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.settings.scroll != 3 {
		t.Fatalf("wheel: scroll %d", m.settings.scroll)
	}
	m = feed(m, key("pgup"))
	if m.settings.scroll != 0 {
		t.Fatalf("pgup: scroll %d", m.settings.scroll)
	}
}

func TestCtrlCQuitsFromAnywhere(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x") // a confirmation is open
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c did not quit from a dialog")
	}
	m = onSettings(t, &fakeClient{}, 120, 30)
	m = feed(m, keys("tab", "tab", "enter")...) // editing a text field
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c did not quit while editing")
	}
}

// The footer follows the focused control, and a click on a global hint runs
// the key directly instead of typing it into a focused text field.
func TestSettingsFooterFollowsFocus(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m.View()
	m.settings.group.Focus(setPollInterval)
	m = feed(m, key("enter"))
	if v := m.View(); !strings.Contains(v, "enter commit") || !strings.Contains(v, "esc stop editing") || strings.Contains(v, "q quit") {
		t.Fatalf("editing footer:\n%s", v)
	}
	m = click(t, m, "key-esc")
	f := m.settings.input(setPollInterval).(*ui.TextField)
	if f.Editing() {
		t.Fatal("the esc hint did not stop editing")
	}
	z := zoneOf(t, m, "key-q")
	upd, cmd := m.Update(leftClick(z.StartX, z.StartY))
	if !quits(cmd) || upd.(Model).settings.input(setPollInterval).Value().Text != "10s" {
		t.Fatal("the q hint typed into the field instead of quitting")
	}
	m.settings.group.Focus(setGlobalMax)
	if v := m.View(); !strings.Contains(v, "left less") || !strings.Contains(v, "right more") {
		t.Fatalf("stepper footer:\n%s", v)
	}
}

// Before the config loads, the config keys still do nothing on Settings.
func TestSettingsConfigKeysInactiveWhileLoading(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.page = pageSettings // no config fetched yet
	for _, k := range []string{"m", "]", "["} {
		m = feed(m, key(k))
	}
	if m.cfg != nil || len(c.actions()) != 0 {
		t.Fatalf("config keys acted while loading: %v", c.actions())
	}
}
