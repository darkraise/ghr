package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
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
	} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "› [ darkraise") || strings.Contains(v, "[ darkraise") {
		t.Error("owner is editable")
	}
	if strings.Contains(v, "darkcloud") {
		t.Error("repo cards belong to the Repositories page")
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
	return feed(sampleModel(c, w, h), key("6"))
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
	if got := m.settings.group.FocusedID(); got != setPrune {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "› [ Prune now ]") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
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
	// A focused text field takes ? and q as typing, so the footer does not offer them.
	if v := m.View(); !strings.Contains(v, "enter edit") || !strings.Contains(v, "ctrl+s save") ||
		strings.Contains(v, "q quit") || strings.Contains(v, "? help") {
		t.Fatalf("text field footer:\n%s", v)
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

func set(m Model, key string, v ui.Value) { m.settings.input(key).SetValue(v) }

// A saved 120s comes back as 2m0s: the field resets to the new base and is
// not dirty, and no mismatch is reported.
func TestSettingsSavedDurationIsNotDirtyAfterRefetch(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "120s"})
	m = click(t, m, setSave)
	f := m.settings.form.Field(setPollInterval)
	if f.Dirty() || f.Input.Value().Text != "2m0s" {
		t.Fatalf("after save: dirty %v value %q", f.Dirty(), f.Input.Value().Text)
	}
	v := m.View()
	if !strings.Contains(v, "✔ Settings saved") || strings.Contains(v, "unsaved change") || strings.Contains(v, "did not apply") {
		t.Fatalf("after save:\n%s", v)
	}
}

func TestSettingsRejectedSaveKeepsEditsAndShowsAlert(t *testing.T) {
	c := &fakeClient{patchErr: rejected("build_cache_keep must look like 20GB; runner_limits.cpu_quota must be a positive percentage such as 200%")}
	m := onSettings(t, c, 120, 30)
	set(m, setBuildCacheKeep, ui.Value{Text: "lots"})
	set(m, setCPUQuota, ui.Value{Text: "fast"})
	m = feed(m, key("ctrl+s"))
	v := m.View()
	for _, want := range []string{"Save rejected", "✖ build_cache_keep must look like 20GB", "✖ runner_limits.cpu_quota must be a positive",
		"✖ settings not saved", "● 2 unsaved changes"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if m.settings.input(setBuildCacheKeep).Value().Text != "lots" || m.settings.saving {
		t.Fatal("rejected save lost the edit or stayed saving")
	}
	m = click(t, m, setDiscard)
	if v := m.View(); strings.Contains(v, "Save rejected") || strings.Contains(v, "unsaved change") {
		t.Fatalf("discard left the alert or edits:\n%s", v)
	}
}

// rejected is the daemon's answer to a patch that fails validation.
func rejected(msg string) error { return &api.Error{Status: 400, Msg: msg} }

// A save that never reached the daemon, or that it failed to apply, is a
// connection problem, not a rejection: a toast, no Save rejected box.
func TestSettingsSaveFailureIsNotARejection(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("ghr daemon unreachable: %w", errors.New("connection refused")),
		&api.Error{Status: 500, Msg: "write config.yaml: disk full"},
	} {
		c := &fakeClient{patchErr: err}
		m := feed(dirtySettings(t, c), keys("3", "enter")...)
		v := m.View()
		if strings.Contains(v, "Save rejected") || !strings.Contains(v, "settings not saved: "+err.Error()) {
			t.Errorf("%v:\n%s", err, v)
		}
		if m.page != pageSettings || m.leaving || m.settings.saving || m.settings.input(setPollInterval).Value().Text != "30s" {
			t.Errorf("%v: page %v leaving %v saving %v", err, m.page, m.leaving, m.settings.saving)
		}
	}
}

func TestSettingsSaveDisabledWhileInFlight(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	upd, cmd := m.Update(key("ctrl+s"))
	m = upd.(Model)
	if cmd == nil || !m.settings.saving || !strings.Contains(m.View(), "[ Saving… ]") {
		t.Fatal("save did not enter the saving state")
	}
	if _, again := m.Update(key("ctrl+s")); again != nil {
		t.Fatal("a second save was sent while one was in flight")
	}
	m = feed(m, collect(cmd)...)
	if m.settings.saving || len(c.patches) != 1 {
		t.Fatalf("saving %v patches %d", m.settings.saving, len(c.patches))
	}
}

func TestSettingsWarnsWhenDaemonIgnoresAField(t *testing.T) {
	c := &fakeClient{} // accepts the patch but applies nothing, like an older daemon
	m := onSettings(t, c, 120, 30)
	set(m, setHistoryRetention, ui.Value{Text: "14d"})
	m = feed(m, key("ctrl+s"))
	if v := m.View(); !strings.Contains(v, "daemon did not apply history_retention; is it older than this ghr?") {
		t.Fatalf("no mismatch warning:\n%s", v)
	}
}

// The duration fields check what config.Validate checks, so a value the
// daemon would reject is flagged before saving.
func TestSettingsDurationFloors(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	for _, tc := range []struct{ key, text, err string }{
		{setPollInterval, "1s", "poll_interval must be at least 5s"},
		{setPollInterval, "5s", ""},
		{setStartTimeout, "0s", "start_timeout must be greater than 0"},
		{setIdleTimeout, "-1m", "idle_timeout must be greater than 0"},
		{setHistoryRetention, "12h", "history_retention must be at least 1d"},
		{setHistoryRetention, "24h", ""},
	} {
		set(m, tc.key, ui.Value{Text: tc.text})
		if got := m.settings.checkErrors()[tc.key]; got != tc.err {
			t.Errorf("%s = %s: error %q, want %q", tc.key, tc.text, got, tc.err)
		}
	}
	if m = feed(m, key("ctrl+s")); len(c.patches) != 0 {
		t.Fatal("a duration below its floor was sent")
	}
}

// The save stays busy until the config fetched after it is handled.
func TestSettingsStaysBusyUntilRefetch(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	upd, cmd := m.Update(key("ctrl+s"))
	saved := collect(cmd) // the patch's outcome only
	upd, refetch := upd.Update(saved[0])
	m = upd.(Model)
	if !m.settings.saving || !strings.Contains(m.View(), "[ Saving… ]") {
		t.Fatal("not busy while the refetch is pending")
	}
	if _, again := m.Update(key("ctrl+s")); again != nil {
		t.Fatal("a second save overlapped the refetch")
	}
	m = feed(m, collect(refetch)...)
	if m.settings.saving || len(c.patches) != 1 || m.settings.form.Field(setPollInterval).Dirty() {
		t.Fatalf("after the refetch: saving %v patches %d", m.settings.saving, len(c.patches))
	}
}

// A field edited again between the patch and its refetch keeps the newer edit
// instead of being reset to the saved value.
func TestSettingsRefetchKeepsEditMadeWhileSaving(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	upd, cmd := m.Update(key("ctrl+s"))
	upd, refetch := upd.Update(collect(cmd)[0])
	m = upd.(Model)
	set(m, setPollInterval, ui.Value{Text: "45s"})
	m = feed(m, collect(refetch)...)
	poll := m.settings.form.Field(setPollInterval)
	if got := poll.Input.Value().Text; got != "45s" || !poll.Dirty() {
		t.Fatalf("newer edit lost: value %q dirty %v", got, poll.Dirty())
	}
	if m.toast.Err {
		t.Fatalf("unexpected error toast %q", m.toast.Text)
	}
}

func TestSettingsRefetchFailureIsReported(t *testing.T) {
	c := &fakeClient{}
	c.onPatch = func(model.ConfigPatch) { c.cfgErr = errors.New("connection refused") }
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	m = feed(m, key("ctrl+s"))
	if v := m.View(); m.settings.saving || !strings.Contains(v, "saved, but re-reading the config failed: connection refused") {
		t.Fatalf("saving %v:\n%s", m.settings.saving, v)
	}
}

// Many rejection messages cannot push the form and footer off a 22-row screen.
func TestSettingsAlertIsBounded(t *testing.T) {
	var msgs []string
	for i := 0; i < 6; i++ {
		msgs = append(msgs, "repo"+string(rune('0'+i))+": needs at least one label in labels or repo labels")
	}
	c := &fakeClient{patchErr: rejected(strings.Join(msgs, "; "))}
	m := onSettings(t, c, 120, 22)
	set(m, setLabels, ui.Value{List: []string{}})
	m = feed(m, key("ctrl+s"))
	v := m.View()
	if !strings.Contains(v, "repo2: needs") || strings.Contains(v, "repo3: needs") || !strings.Contains(v, "… and 3 more") ||
		lipgloss.Height(v) > 22 || !strings.Contains(v, "q quit") {
		t.Fatalf("alert not bounded (%d lines):\n%s", lipgloss.Height(v), v)
	}
}

func TestConfigRefreshesEveryFiveTicksOnEveryPage(t *testing.T) {
	c := &fakeClient{cfg: parseConfig(t, settingsYAML)}
	m := sampleModel(c, 120, 30) // on the Dashboard, no config loaded yet
	if m = ticks(m, slowPoll-1); m.cfg != nil {
		t.Fatal("config fetched before the fifth tick")
	}
	if m = ticks(m, 1); m.cfg == nil || m.cfg.Owner != "darkraise" {
		t.Fatal("config not fetched on the fifth tick")
	}
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "global_max: 3", "global_max: 4", 1))
	st := sampleStatus()
	st.Epoch = "e2" // the daemon restarted
	m = feed(m, statusMsg{st: st})
	if m.cfg.GlobalMax != 4 {
		t.Fatalf("config not re-fetched after a daemon restart: global max %d", m.cfg.GlobalMax)
	}
}

// A config response to an older request never replaces a newer one.
func TestStaleConfigResponseIsDropped(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	older, newer := m.fetchConfig(), m.fetchConfig()
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "global_max: 3", "global_max: 7", 1))
	fresh := newer()
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "global_max: 3", "global_max: 5", 1))
	stale := older()
	m = feed(m, fresh, stale)
	if m.cfg.GlobalMax != 7 || m.settings.input(setGlobalMax).Value().Num != 7 {
		t.Fatalf("stale response applied: global max %d", m.cfg.GlobalMax)
	}
}

// dirtySettings is onSettings with one unsaved change.
func dirtySettings(t *testing.T, c *fakeClient) Model {
	t.Helper()
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	return m
}

func TestLeaveGuardAsksOnQDigitsAndSidebar(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	upd, cmd := m.Update(key("q"))
	m = upd.(Model)
	if quits(cmd) || m.overlay != ovUnsaved {
		t.Fatalf("q with unsaved changes: overlay %v", m.overlay)
	}
	v := m.View()
	for _, want := range []string{"Unsaved changes", "You have 1 unsaved change on the Settings page.", "( Stay )    ( Discard )  › [ Save ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q:\n%s", want, v)
		}
	}
	m = feed(m, key("esc")) // esc means Stay
	if m.overlay != ovNone || m.page != pageSettings || m.settings.input(setPollInterval).Value().Text != "30s" {
		t.Fatalf("esc: overlay %v page %v", m.overlay, m.page)
	}
	if m = feed(m, key("1")); m.overlay != ovUnsaved || m.leaveTo != (leaveTarget{page: pageDashboard}) {
		t.Fatalf("1: overlay %v target %+v", m.overlay, m.leaveTo)
	}
	m = click(t, m, btnLeaveStay)
	if m = click(t, m, "nav/history"); m.overlay != ovUnsaved || m.leaveTo != (leaveTarget{page: pageHistory}) {
		t.Fatalf("sidebar: overlay %v target %+v", m.overlay, m.leaveTo)
	}
	m = click(t, m, btnLeaveDiscard)
	if m.overlay != ovNone || m.page != pageHistory || len(m.settings.form.Dirty()) != 0 {
		t.Fatalf("discard: overlay %v page %v dirty %d", m.overlay, m.page, len(m.settings.form.Dirty()))
	}
}

func TestLeaveGuardSaveThenNavigate(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := feed(dirtySettings(t, c), keys("3", "enter")...) // enter presses Save, the primary
	if m.page != pageRunners || len(c.patches) != 1 || *c.patches[0].PollInterval != "30s" {
		t.Fatalf("page %v patches %d", m.page, len(c.patches))
	}
}

func TestLeaveGuardRejectedSaveStays(t *testing.T) {
	c := &fakeClient{patchErr: rejected("poll_interval must be >= 5s")}
	m := feed(dirtySettings(t, c), keys("4", "enter")...)
	if m.page != pageSettings || !strings.Contains(m.View(), "✖ poll_interval must be >= 5s") || m.leaving {
		t.Fatalf("page %v leaving %v", m.page, m.leaving)
	}
}

// pump applies msgs and every message their commands produce, running each
// command exactly once, and reports whether any of them asked to quit.
func pump(m Model, msgs ...tea.Msg) (Model, bool) {
	quit := false
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		if _, ok := msg.(tea.QuitMsg); ok {
			quit = true
			continue
		}
		upd, cmd := m.Update(msg)
		m = upd.(Model)
		msgs = append(msgs, collect(cmd)...)
	}
	return m, quit
}

// applyPoll makes the fake daemon apply a patched poll interval.
func applyPoll(c *fakeClient) {
	c.onPatch = func(p model.ConfigPatch) {
		if p.PollInterval != nil {
			d, _ := config.ParseDuration(*p.PollInterval)
			c.cfg.PollInterval = d
		}
	}
}

// Save, then quit: the quit comes only after the patch and the check of the
// refetched config.
func TestLeaveGuardSaveThenQuit(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m, quit := pump(dirtySettings(t, c), key("q"), key("enter"))
	if !quit || len(c.patches) != 1 || m.settings.form.Field(setPollInterval).Dirty() || m.settings.saving {
		t.Fatalf("quit %v patches %d", quit, len(c.patches))
	}
}

// When the daemon ignores a saved field, the save does not leave: the
// warning stays on screen.
func TestLeaveGuardStaysWhenDaemonIgnoresAField(t *testing.T) {
	c := &fakeClient{} // accepts the patch but applies nothing
	m, quit := pump(dirtySettings(t, c), key("q"), key("enter"))
	if quit || m.page != pageSettings || !strings.Contains(m.View(), "daemon did not apply poll_interval") {
		t.Fatalf("quit %v page %v", quit, m.page)
	}
}

// While a save is in flight, leaving waits for it instead of opening a second guard.
func TestLeaveWaitsForSaveInFlight(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	upd, _ := m.Update(key("ctrl+s"))
	upd, cmd := upd.Update(key("q"))
	m = upd.(Model)
	if quits(cmd) || m.overlay != ovNone || !strings.Contains(m.View(), "wait for the save to finish") {
		t.Fatalf("overlay %v", m.overlay)
	}
}

// l acts on a runner row; Settings has none, so l leaves nothing unsaved behind.
func TestLogKeyIsInactiveOnSettings(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	m.View()
	if m = feed(m, key("l")); m.overlay != ovNone || m.page != pageSettings {
		t.Fatalf("l: overlay %v page %v", m.overlay, m.page)
	}
}

func TestNoGuardWhenCleanOrOnCtrlC(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	if m = feed(m, key("1")); m.page != pageDashboard || m.overlay != ovNone {
		t.Fatalf("clean form guarded: page %v overlay %v", m.page, m.overlay)
	}
	m = dirtySettings(t, &fakeClient{})
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c was guarded")
	}
}

func TestSettingsDialogGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, w, 30), key("a"))
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

func TestSettingsDropdownGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, w, 30), key("enter"))
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

// A refresh that leaves nothing unsaved while the unsaved-changes dialog is
// open (the daemon now holds the edited value) carries on where the user was
// going: there is nothing left to save or discard.
func TestUnsavedDialogClosesWhenNothingIsLeft(t *testing.T) {
	c := &fakeClient{}
	m := dirtySettings(t, c)
	if m = feed(m, key("3")); m.overlay != ovUnsaved {
		t.Fatalf("unexpected overlay %v", m.overlay)
	}
	cfg := parseConfig(t, settingsYAML)
	cfg.PollInterval, _ = config.ParseDuration("30s")
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: cfg})
	if m.overlay != ovNone || m.page != pageRunners {
		t.Fatalf("overlay %v page %v", m.overlay, m.page)
	}
}

// An edit made while a save from the unsaved-changes dialog is in flight
// keeps the page: leaving now would lose it.
func TestLeaveSaveStaysWhenEditedDuringSave(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := dirtySettings(t, c)
	if m = feed(m, key("3")); m.overlay != ovUnsaved {
		t.Fatalf("unexpected overlay %v", m.overlay)
	}
	upd, cmd := m.Update(key("enter")) // Save, the primary
	m = upd.(Model)
	set(m, setIdleTimeout, ui.Value{Text: "9m"})
	m, _ = pump(m, collect(cmd)...)
	if m.page != pageSettings || m.overlay != ovUnsaved {
		t.Fatalf("page %v overlay %v", m.page, m.overlay)
	}
	if f := m.settings.form.Field(setPollInterval); f.Dirty() {
		t.Fatal("the saved field should be reset")
	}
	if f := m.settings.form.Field(setIdleTimeout); !f.Dirty() {
		t.Fatal("the edit made during the save should stay")
	}
}

func TestSettingsSanitisesConfigText(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 40)
	// YAML's \e and \a escapes put real ESC and BEL bytes into the parsed config.
	m.cfg = parseConfig(t, `owner: "evil\e]0;pwned\a"
labels: ["bad\e[2Jlabel"]
`)
	m.settings.load(m.cfg)
	v := strings.Join(settingsLines(m, 123), "\n")
	if strings.ContainsAny(v, "\x1b\a") || !strings.Contains(v, "evil") || !strings.Contains(v, "badlabel") {
		t.Fatalf("config text not sanitised: %q", v)
	}
}

func TestSettingsSavePatchHoldsOnlyDirtyFields(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	set(m, setLabels, ui.Value{List: []string{"homelab", "gpu"}})
	set(m, setMemoryMax, ui.Value{Text: "4G"})
	if v := m.View(); !strings.Contains(v, "● 3 unsaved changes") || !strings.Contains(v, "( Discard )") || !strings.Contains(v, "[ Save changes ]") {
		t.Fatalf("unsaved bar missing:\n%s", v)
	}
	m = feed(m, key("ctrl+s"))
	if len(c.patches) != 1 {
		t.Fatalf("patches %d", len(c.patches))
	}
	p := c.patches[0]
	if *p.PollInterval != "30s" || strings.Join(*p.Labels, ",") != "homelab,gpu" || *p.RunnerLimits.MemoryMax != "4G" || p.RunnerLimits.CPUQuota != nil {
		t.Fatalf("patch %+v", p)
	}
	if p.Mode != nil || p.GlobalMax != nil || p.StartTimeout != nil || p.IdleTimeout != nil || p.HistoryRetention != nil ||
		p.DiskHighWater != nil || p.BuildCacheKeep != nil || len(p.Repos) != 0 {
		t.Fatalf("patch carries clean fields: %+v", p)
	}
}

func TestSettingsInAppChecksBlockSave(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setIdleTimeout, ui.Value{Text: "soon"})
	m = feed(m, key("ctrl+s"))
	if v := m.View(); len(c.patches) != 0 || !strings.Contains(v, "fix the highlighted settings first") {
		t.Fatalf("patches %d:\n%s", len(c.patches), v)
	}
}

// A save started from the unsaved-changes dialog finishing while another
// dialog is open must not replace it: the typed token stays in its own
// dialog and the page stays where it is.
func TestLeaveSaveDoesNotReplaceAnOpenDialog(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := dirtySettings(t, c)
	if m = feed(m, key("3")); m.overlay != ovUnsaved {
		t.Fatalf("unexpected overlay %v", m.overlay)
	}
	upd, cmd := m.Update(key("enter")) // Save, the primary
	m = upd.(Model)
	set(m, setIdleTimeout, ui.Value{Text: "9m"})
	upd, _ = m.openTokenDialog()
	m = upd.(Model)
	m.tok.field.SetValue(ui.Value{Text: "ghp_typed"})
	m, _ = pump(m, collect(cmd)...)
	if m.overlay != ovToken || m.tok == nil || m.tok.field.Value().Text != "ghp_typed" || m.page != pageSettings {
		t.Fatalf("overlay %v page %v: the token dialog was replaced", m.overlay, m.page)
	}
}

// Opening another dialog over the token dialog drops what was typed.
func TestOpenDialogClearsTypedToken(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	upd, _ := m.openTokenDialog()
	m = upd.(Model)
	d := m.tok
	d.field.SetValue(ui.Value{Text: "ghp_typed"})
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
	if m.tok != nil || d.field.Value().Text != "" {
		t.Fatalf("tok %v value %q", m.tok, d.field.Value().Text)
	}
}

func TestRepositoriesSaveToastNamesThePage(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkmem", "max").SetValue(ui.Value{Num: 2, Set: true})
	c.onPatch = func(p model.ConfigPatch) {
		if rp, ok := p.Repos["darkmem"]; ok && rp.Max != nil {
			n := *rp.Max
			c.cfg.Repo("darkmem").Max = &n
		}
	}
	m = feed(m, key("ctrl+s"))
	if v := m.View(); !strings.Contains(v, "Repositories saved") || strings.Contains(v, "Settings saved") || !strings.Contains(v, "ctrl+s save") {
		t.Fatalf("toast or footer:\n%s", v)
	}
}
