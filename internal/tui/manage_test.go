package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
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

func TestRegistrationsCard(t *testing.T) {
	c := &fakeClient{regs: []model.Registration{
		{ID: 1, Name: "linux-1", Status: "offline", Labels: []string{"self-hosted", "X64"}},
		{ID: 2, Name: "ghr-darkcloud-aaaaaa", Status: "online", Busy: true, GHR: true},
		{ID: 3, Name: "laptop", Status: "online"},
	}}
	m := onRepos(t, c, 140, 80)
	v := m.View()
	for _, want := range []string{"GitHub registrations", "[OFFLINE]", "linux-1", "[BUSY] [GHR]", "ghr-darkcloud-aaaaaa", "[ONLINE]", "laptop", "( Refresh )"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(v, "[ Delete ]"); n != 1 {
		t.Fatalf("%d Delete buttons; only the offline foreign runner may have one", n)
	}
	m = click(t, m, regDelID(1))
	if m.overlay != ovConfirm || !strings.Contains(m.confirmText, "linux-1 from darkcloud") {
		t.Fatalf("confirm: %v %q", m.overlay, m.confirmText)
	}
	m = feed(m, key("enter")) // Yes
	if got := strings.Join(c.actions(), "|"); got != "del-reg darkcloud 1" || !strings.Contains(m.View(), "deleted linux-1") {
		t.Fatalf("actions %q", got)
	}
	m = feed(m, regsMsg{repo: "darkcloud", seq: m.mg.regSeq - 1})
	if !strings.Contains(m.View(), "linux-1") {
		t.Fatal("a stale listing replaced the current one")
	}
	c.regs = nil
	if v := click(t, m, reposRegRefresh).View(); !strings.Contains(v, "No runners registered") {
		t.Fatalf("empty:\n%s", v)
	}
	st := sampleStatus()
	st.Degraded, st.DegradedReason = true, "GitHub rejected the token"
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "GitHub is rejecting the token") || m.mg.regRefresh.Focusable() {
		t.Fatalf("degraded:\n%s", v)
	}
}

// When a status refresh removes the selected repo, the selection falls back
// to its neighbour and the cards reload for it; the old rows never show
// under the new heading, and a late reply for the old repo is dropped.
func TestRegistrationsFollowSelectionChanges(t *testing.T) {
	c := &fakeClient{regsByRepo: map[string][]model.Registration{
		"darkcloud": {{ID: 1, Name: "linux-1", Status: "offline"}},
		"darkmem":   {{ID: 5, Name: "mem-box", Status: "online"}},
	}}
	m := onRepos(t, c, 140, 80)
	if v := m.View(); !strings.Contains(v, "linux-1") {
		t.Fatalf("darkcloud rows missing:\n%s", v)
	}
	st := sampleStatus()
	st.Repos = st.Repos[1:] // darkcloud removed: darkmem takes its place
	m = feed(m, statusMsg{st: st})
	v := m.View()
	if strings.Contains(v, "linux-1") || !strings.Contains(v, "mem-box") {
		t.Fatalf("cards did not follow the selection:\n%s", v)
	}
	// Later tasks add other card reads to the same batch; look at the
	// registration reads only.
	regReads := func() []string {
		var out []string
		for _, r := range c.reads {
			if strings.HasPrefix(r, "regs ") {
				out = append(out, r)
			}
		}
		return out
	}
	if got := strings.Join(regReads(), "|"); !strings.Contains(got, "regs darkcloud") || !strings.HasSuffix(got, "regs darkmem") {
		t.Fatalf("reads %q", got)
	}
	m = feed(m, regsMsg{repo: "darkcloud", seq: m.mg.regSeq, regs: []model.Registration{{ID: 9, Name: "stale"}}})
	if strings.Contains(m.View(), "stale") {
		t.Fatal("a reply for another repo was shown")
	}
	m = feed(m, key("down"))
	if rs := regReads(); rs[len(rs)-1] != "regs darkagents" {
		t.Fatalf("down did not fetch the new selection: %q", rs)
	}
}

// Every panel control has a line range, so focus on a short screen scrolls
// to the summary buttons and the registration buttons too.
func TestRegistrationButtonsScrollIntoView(t *testing.T) {
	c := &fakeClient{regs: []model.Registration{{ID: 1, Name: "linux-1", Status: "offline"}}}
	m := onRepos(t, c, 120, 16)
	for _, id := range []string{reposRegRefresh, regDelID(1), reposPause, reposRemove, reposRegRefresh} {
		m.repos.group.Focus(id)
		focusInView(t, m)
	}
}

// A throttled or failed listing shows when it may be retried.
func TestRegistrationsErrorShowsRetry(t *testing.T) {
	retry := now.Add(2 * time.Minute)
	c := &fakeClient{regErr: &api.Error{Status: 429, Msg: "GitHub rate limit", RetryAt: retry}}
	m := onRepos(t, c, 140, 80)
	if v := m.View(); !strings.Contains(v, "GitHub rate limit") || !strings.Contains(v, "try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("retry time missing:\n%s", v)
	}
}

// A selection change while GitHub rejects the token drops the old rows, and
// the cards load for the new selection once the token is accepted again.
func TestRegistrationsReloadAfterDegradedSelection(t *testing.T) {
	c := &fakeClient{regsByRepo: map[string][]model.Registration{
		"darkcloud": {{ID: 1, Name: "linux-1", Status: "offline"}},
		"darkmem":   {{ID: 5, Name: "mem-box", Status: "online"}},
	}}
	m := onRepos(t, c, 140, 80)
	st := sampleStatus()
	st.Degraded, st.DegradedReason = true, "GitHub rejected the token"
	m = feed(m, statusMsg{st: st})
	m = feed(m, key("down"))
	if !strings.EqualFold(m.mg.regRepo, "darkmem") || len(m.mg.regs) != 0 {
		t.Fatalf("old rows kept while degraded: %q %v", m.mg.regRepo, m.mg.regs)
	}
	m = feed(m, statusMsg{st: sampleStatus()})
	if v := m.View(); strings.Contains(v, "linux-1") || !strings.Contains(v, "mem-box") {
		t.Fatalf("cards did not reload after recovery:\n%s", v)
	}
	last := ""
	for _, r := range c.reads {
		if strings.HasPrefix(r, "regs ") {
			last = r
		}
	}
	if last != "regs darkmem" {
		t.Fatalf("last registration read %q", last)
	}
}

func TestLabelCheckCard(t *testing.T) {
	seen := now.Add(-2 * time.Hour)
	at := now.Add(-3 * time.Minute)
	c := &fakeClient{labels: model.LabelCheck{State: "done", CheckedAt: &at, Groups: []model.LabelGroup{
		{Labels: []string{"darkcloud-linux", "self-hosted"}, Jobs: []string{"CI / test"}, Count: 4, LastSeen: seen},
		{Labels: []string{"gpu", "self-hosted"}, Jobs: []string{"ML / train"}, Count: 2, LastSeen: seen},
		{Labels: []string{"self-hosted", "windows"}, Jobs: []string{"Win / build"}, Count: 1, LastSeen: seen},
		{Labels: []string{"ubuntu-latest"}, Jobs: []string{"CI / lint"}, More: 2, Count: 5, LastSeen: seen},
		{Labels: []string{}, Jobs: []string{"Group / job"}, Count: 1, LastSeen: seen},
	}}}
	m := onRepos(t, c, 160, 120)
	v := m.View()
	for _, want := range []string{"Workflow labels", "Checked 3m ago", "[MATCHED]", "[UNMATCHED]", "missing gpu", "( + add gpu )",
		"needs a different OS or architecture", "[OTHER]", "+2 more", "no labels (runner group)", "does not install anything"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "( + add windows )") {
		t.Fatal("OS and architecture labels are never offered")
	}
	m = click(t, m, lcAddID("gpu"))
	if got := repoInput(m, "darkcloud", "labels").Value().List; !reflect.DeepEqual(got, []string{"darkcloud-linux", "gpu"}) {
		t.Fatalf("quick add: %v", got)
	}
	if v := m.View(); strings.Count(v, "[MATCHED]") != 2 || !strings.Contains(v, "1 unsaved change (darkcloud)") {
		t.Fatalf("after quick add:\n%s", v)
	}
	m = click(t, m, reposLCCheck)
	if got := strings.Join(c.actions(), "|"); got != "label-check darkcloud" {
		t.Fatalf("actions %q", got)
	}
	retry := now.Add(time.Minute)
	c.labelErr = &api.Error{Status: 429, Msg: "this repo was checked less than a minute ago", RetryAt: retry}
	if v := click(t, m, reposLCCheck).View(); !strings.Contains(v, "checked less than a minute ago") || !strings.Contains(v, "try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("throttled:\n%s", v)
	}
	c.labelErr, c.labelGetErr = nil, &api.Error{Status: 429, Msg: "GitHub rate limit", RetryAt: retry}
	m = feed(m, lcMsg{repo: "darkcloud", seq: m.mg.lcSeq, err: c.labelGetErr})
	if v := m.View(); !strings.Contains(v, "GitHub rate limit, try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("fetch error:\n%s", v)
	}
}

func unmatchedGPU() model.LabelCheck {
	at := now.Add(-time.Minute)
	return model.LabelCheck{State: "done", CheckedAt: &at, Groups: []model.LabelGroup{
		{Labels: []string{"gpu", "self-hosted"}, Jobs: []string{"ML / train"}, Count: 2, LastSeen: at},
	}}
}

// Label-check data can arrive before the config; the card then lists the
// groups without classifying them, and classifies once the config loads.
func TestLabelCheckBeforeConfig(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 80)
	m.page = pageRepos
	m.mg.lcRepo, m.mg.lcSeq = "darkcloud", 1
	m = feed(m, lcMsg{repo: "darkcloud", seq: 1, lc: unmatchedGPU()})
	if v := m.View(); !strings.Contains(v, "gpu, self-hosted") || strings.Contains(v, "[UNMATCHED]") {
		t.Fatalf("before config:\n%s", v)
	}
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: parseConfig(t, settingsYAML)})
	if v := m.View(); !strings.Contains(v, "[UNMATCHED]") || !strings.Contains(v, "+ add gpu") {
		t.Fatalf("after config:\n%s", v)
	}
}

// While a scan runs, at most one poll is in flight; a slow reply is still
// accepted rather than overtaken by the next tick's request.
func TestLabelCheckPollsOneAtATime(t *testing.T) {
	c := &fakeClient{labels: model.LabelCheck{State: "checking", Groups: []model.LabelGroup{}}}
	m := onRepos(t, c, 140, 80)
	first := m.pollLabelCheck()
	if first == nil {
		t.Fatal("no poll while checking")
	}
	if m.pollLabelCheck() != nil {
		t.Fatal("a second poll started while one is in flight")
	}
	c.labels = unmatchedGPU()
	m = feed(m, first())
	if m.mg.lc.State != "done" || m.mg.lcInFlight {
		t.Fatalf("slow reply dropped: state %q in flight %v", m.mg.lc.State, m.mg.lcInFlight)
	}
	// A reply carrying the latest number but another repo's data is dropped.
	m = feed(m, lcMsg{repo: "darkmem", seq: m.mg.lcSeq, lc: model.LabelCheck{State: "checking"}})
	if m.mg.lc.State != "done" {
		t.Fatalf("another repo's reply was applied: %q", m.mg.lc.State)
	}
}

// Check now, Delete, Refresh and the quick-add buttons are unavailable while
// GitHub rejects the token or the repo is being removed, and their actions
// refuse then too.
func TestManagementControlsFollowAvailability(t *testing.T) {
	c := &fakeClient{labels: unmatchedGPU(), regs: []model.Registration{{ID: 1, Name: "linux-1", Status: "offline"}}}
	m := onRepos(t, c, 140, 80)
	m.View()
	if m.mg.lcAdd["gpu"] == nil || m.mg.lcAdd["gpu"].Disabled {
		t.Fatal("quick add unavailable while everything is fine")
	}
	for _, mut := range []func(*model.Status){
		func(st *model.Status) { st.Degraded, st.DegradedReason = true, "GitHub rejected the token" },
		func(st *model.Status) { st.Repos[0].Removing = true },
	} {
		st := sampleStatus()
		mut(&st)
		mm := feed(m, statusMsg{st: st})
		mm.View()
		for _, b := range []*ui.Button{mm.mg.lcCheck, mm.mg.lcAdd["gpu"], mm.mg.regRefresh, mm.mg.regDel[1]} {
			if b != nil && !b.Disabled {
				t.Errorf("%s enabled (degraded %v)", b.ID(), st.Degraded)
			}
		}
		upd, _ := mm.pressed(lcAddID("gpu"))
		if got := repoInput(upd.(Model), "darkcloud", "labels").Value().List; len(got) != 1 {
			t.Errorf("quick add applied anyway: %v", got)
		}
		upd, cmd := mm.pressed(reposLCCheck)
		if cmd != nil || len(c.actions()) != 0 {
			t.Errorf("check started anyway: %v", c.actions())
		}
		_ = upd
	}
}

// GitHub label text never reaches the screen raw, and long label lists wrap
// so every quick-add button and the note stay whole on every width.
func TestLabelCheckSanitisesAndWraps(t *testing.T) {
	at := now.Add(-time.Minute)
	labels := []string{"self-hosted", "gpu\x1b]0;x\a", "cuda-12-runtime", "big-memory-box", "nvme-scratch-disk"}
	c := &fakeClient{labels: model.LabelCheck{State: "done", CheckedAt: &at, Groups: []model.LabelGroup{
		{Labels: labels, Jobs: []string{"ML / train-a-very-long-job-name"}, Count: 2, LastSeen: at},
	}}}
	for _, w := range []int{100, 99, 56, 40} {
		m := onRepos(t, c, w, 200)
		m.View() // a frame syncs the controls, which creates the quick-add buttons
		pw, _ := m.reposPanelSize(m.contentSize())
		r := m.selectedRepoStatus()
		lines, _ := m.reposPanelLines(*r, pw)
		v := zone.Scan(strings.Join(lines, "\n"))
		if strings.ContainsAny(v, "\x1b\a") {
			t.Fatalf("%d columns: raw control bytes: %q", w, v)
		}
		for _, want := range []string{"+ add gpu ", "+ add cuda-12-runtime", "+ add big-memory-box", "+ add nvme-scratch-disk", "on the runner."} {
			if !strings.Contains(v, want) {
				t.Errorf("%d columns: missing %q:\n%s", w, want, v)
			}
		}
		fits(t, "label check", m.View(), w, 200)
	}
	m := onRepos(t, c, 140, 80)
	m = click(t, m, lcAddID("gpu\x1b]0;x\a"))
	if got := repoInput(m, "darkcloud", "labels").Value().List; !reflect.DeepEqual(got, []string{"darkcloud-linux", "gpu\x1b]0;x\a"}) {
		t.Fatalf("the raw label is what gets added: %q", got)
	}
}

func TestActivityCard(t *testing.T) {
	e := func(concl string, ago, d time.Duration) model.HistoryEntry {
		return model.HistoryEntry{Repo: "darkcloud", Conclusion: concl, FinishedAt: now.Add(-ago), StartedAt: now.Add(-ago - d)}
	}
	c := &fakeClient{hist: []model.HistoryEntry{ // newest first, as the daemon returns them
		e("success", time.Hour, 2*time.Minute), e("failure", 2*time.Hour, 4*time.Minute), e("cancelled", 3*time.Hour, time.Minute),
		e("unknown", 4*time.Hour, time.Minute), e("success", 9*24*time.Hour, time.Minute),
	}}
	m := onRepos(t, c, 160, 120)
	v := m.View()
	// The 9-day-old success is outside the window: neither counted nor drawn.
	for _, want := range []string{"Activity", "4 jobs · 33% success · avg 2m00s", "last 7 days", "?○✖■"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "■?○✖■") {
		t.Error("the result strip draws a job outside the window")
	}
	if got := c.histReqs[len(c.histReqs)-1]; got != "darkcloud|" {
		t.Fatalf("history request %q", got)
	}
	m = feed(m, actMsg{repo: "darkcloud", seq: m.mg.actSeq - 1})
	if !strings.Contains(m.View(), "4 jobs") {
		t.Fatal("a stale reply replaced the card")
	}
	short := parseConfig(t, settingsYAML)
	short.HistoryRetention, _ = config.ParseDuration("3d")
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: short})
	if !strings.Contains(m.View(), "last 3d") {
		t.Fatal("the window follows a shorter retention")
	}
}

// Every conclusion GitHub reports counts toward the success rate; only a
// missing conclusion does not.
func TestActivityRateCountsEveryKnownConclusion(t *testing.T) {
	e := func(concl string, ago time.Duration) model.HistoryEntry {
		return model.HistoryEntry{Repo: "darkcloud", Conclusion: concl, FinishedAt: now.Add(-ago), StartedAt: now.Add(-ago - time.Minute)}
	}
	c := &fakeClient{hist: []model.HistoryEntry{
		e("success", time.Hour), e("neutral", 2*time.Hour), e("action_required", 3*time.Hour), e("unknown", 4*time.Hour), e("", 5*time.Hour),
	}}
	if v := onRepos(t, c, 160, 120).View(); !strings.Contains(v, "5 jobs · 33% success") {
		t.Fatalf("rate:\n%s", v)
	}
}

// A failed fetch stays visible in the card with a Retry button; retrying
// recovers the card alone.
func TestActivityErrorRetries(t *testing.T) {
	c := &fakeClient{histErr: errors.New("daemon busy")}
	m := onRepos(t, c, 160, 120)
	if v := m.View(); !strings.Contains(v, "✖ daemon busy") || !strings.Contains(v, "( Retry )") || !strings.Contains(v, "Workflow labels") {
		t.Fatalf("error:\n%s", v)
	}
	c.histErr = nil
	c.hist = []model.HistoryEntry{{Repo: "darkcloud", Conclusion: "success", FinishedAt: now.Add(-time.Hour), StartedAt: now.Add(-time.Hour - time.Minute)}}
	m = click(t, m, reposActRetry)
	if v := m.View(); !strings.Contains(v, "1 jobs · 100% success") || strings.Contains(v, "daemon busy") {
		t.Fatalf("after retry:\n%s", v)
	}
}

func TestRegistrationStatusIsSanitised(t *testing.T) {
	c := &fakeClient{regs: []model.Registration{{ID: 1, Name: "linux-1", Status: "off\x1b]0;x\aline"}}}
	m := onRepos(t, c, 140, 80)
	m.View()
	pw, _ := m.reposPanelSize(m.contentSize())
	r := m.selectedRepoStatus()
	lines, _ := m.reposPanelLines(*r, pw)
	v := zone.Scan(strings.Join(lines, "\n"))
	if strings.ContainsAny(v, "\x1b\a") || !strings.Contains(v, "[OFFLINE]") {
		t.Fatalf("registration status not sanitised: %q", v)
	}
}

// A Delete press for a registration the listing no longer holds, or one that
// is no longer deletable, opens no dialog.
func TestConfirmDeleteNeedsAListedDeletableRegistration(t *testing.T) {
	c := &fakeClient{regs: []model.Registration{
		{ID: 1, Name: "linux-1", Status: "offline"},
		{ID: 2, Name: "laptop", Status: "online"},
	}}
	m := onRepos(t, c, 140, 80)
	m.View()
	for _, id := range []int64{99, 2} {
		if upd, _ := m.pressed(regDelID(id)); upd.(Model).overlay != ovNone {
			t.Errorf("id %d opened a dialog", id)
		}
	}
	upd, _ := m.pressed(regDelID(1))
	if mm := upd.(Model); mm.overlay != ovConfirm || !strings.Contains(mm.confirmText, "linux-1") {
		t.Fatalf("overlay %v text %q", mm.overlay, mm.confirmText)
	}
}

func TestTokenCardBeforeFirstReply(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 140, 80)
	m.mg.token = model.TokenStatus{}
	if v := m.View(); strings.Contains(v, "UNVERIFIED") || !strings.Contains(v, "not read yet") {
		t.Fatalf("token card before the first reply:\n%s", v)
	}
}
