// Package tui is the interactive ghr dashboard (Bubble Tea, mouse via bubblezone).
package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

// Client is the subset of *api.Client the TUI uses.
type Client interface {
	Status(ctx context.Context) (model.Status, error)
	Events(ctx context.Context, after int64) ([]model.Event, error)
	History(ctx context.Context, repo, conclusion string, limit int) ([]model.HistoryEntry, error)
	Log(ctx context.Context, id, cursor string) (model.LogChunk, error)
	Steps(ctx context.Context, id string) ([]model.Step, error)
	Containers(ctx context.Context, id string) ([]model.Container, error)
	Config(ctx context.Context, out any) error
	PatchConfig(ctx context.Context, p model.ConfigPatch) error
	AddRepo(ctx context.Context, req model.AddRepoRequest) error
	RemoveRepo(ctx context.Context, name string) error
	Pause(ctx context.Context, name string) error
	Resume(ctx context.Context, name string) error
	PauseAll(ctx context.Context) error
	ResumeAll(ctx context.Context) error
	Kill(ctx context.Context, id string) error
}

type page int

const (
	pageDashboard page = iota
	pageRunners
	pageHistory
	pageSettings
	pageDetail // a runner's detail page; not in the sidebar
)

var pageNames = []string{"Dashboard", "Runners", "History", "Settings"}

type pane int

const (
	paneRepos pane = iota
	paneRunners
)

type overlay int

const (
	ovNone overlay = iota
	ovConfirm
	ovHelp
	ovUnsaved
	ovAddRepo
)

// leaveTarget is where the user was going when the unsaved-changes dialog opened.
type leaveTarget struct {
	page page
	quit bool
}

const maxEvents = 200

// slowPoll is the tick interval of polls that are costly on the daemon side:
// steps go to the GitHub jobs API on every request.
const slowPoll = 5

// Async results carry what they were requested for, so a late response for an
// earlier selection, filter, cursor or daemon epoch is dropped.
type (
	tickMsg   time.Time
	statusMsg struct {
		st  model.Status
		err error
	}
	eventsMsg struct {
		epoch string
		ev    []model.Event
	}
	historyMsg struct {
		repo, concl string
		hist        []model.HistoryEntry
	}
	logMsg struct {
		gen    int
		cursor string
		chunk  model.LogChunk
		err    error
	}
	stepsMsg struct {
		id    string
		steps []model.Step
		err   error
	}
	containersMsg struct {
		id   string
		ctrs []model.Container
		err  error
	}
	configMsg struct {
		seq int // orders config responses; see settingsPage.nextSeq
		cfg *config.Config
	}
	doneMsg struct {
		text string
		err  error
	}
)

type Model struct {
	c      Client
	now    func() time.Time
	copyFn func(string)

	width, height int
	page          page
	focus         pane
	connected     bool
	connErr       string

	st       model.Status
	epoch    string
	settings *settingsPage
	groups   *pageGroups
	events   []model.Event
	lastSeq  int64
	hist     []model.HistoryEntry
	cfg      *config.Config

	repoSel, runnerSel, histSel int
	eventScroll, logScroll      int
	histRepo, histConcl         string

	logID     string
	logText   string
	logCursor string
	logGen    int
	logBusy   bool

	steps             []model.Step
	containers        []model.Container
	stepsErr, ctrsErr string
	detailID          string
	detailFrom        page                 // the page esc returns to
	detailSnap        model.InstanceStatus // the runner as last seen in /status
	detailDone        time.Time            // when the runner left /status; zero while it runs
	detailScroll      int                  // first line shown of the Steps or Containers list

	overlay       overlay
	confirmText   string
	helpScroll    int // first Help line shown when Help is taller than the screen
	confirmAction func() tea.Cmd
	dlg           ui.Group       // focus across the open dialog's buttons
	dlgButtons    []*ui.Button   // the open dialog's buttons, in display order
	dlgCancel     string         // the button esc presses
	leaveTo       leaveTarget    // where to go once unsaved Settings changes are handled
	leaving       bool           // a save started from the unsaved-changes dialog is in flight
	add           *addRepoDialog // the open Add repository dialog

	toast     ui.Toast
	frame     int
	lastClick string
	lastAt    time.Time
}

func New(c Client) Model {
	return Model{
		c: c, now: time.Now, width: 120, height: 40, settings: newSettingsPage(), groups: newPageGroups(),
		copyFn: func(s string) {
			fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(s)))
		},
	}
}

// Run starts the full-screen TUI with mouse support.
func Run(c Client) error {
	zone.NewGlobal()
	_, err := tea.NewProgram(New(c), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (m Model) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		st, err := m.c.Status(c)
		return statusMsg{st, err}
	}
}

func (m Model) fetchEvents() tea.Cmd {
	after, epoch := m.lastSeq, m.epoch
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ev, err := m.c.Events(c, after)
		if err != nil {
			return nil
		}
		return eventsMsg{epoch, ev}
	}
}

func (m Model) fetchHistory() tea.Cmd {
	repo, concl := m.histRepo, m.histConcl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		h, err := m.c.History(c, repo, concl, 200)
		if err != nil {
			return doneMsg{err: err}
		}
		return historyMsg{repo, concl, h}
	}
}

// fetchLog requests the followed log after the current cursor. Only one request
// is in flight: overlapping polls would carry the same cursor.
func (m *Model) fetchLog() tea.Cmd {
	if m.logID == "" || m.logBusy {
		return nil
	}
	m.logBusy = true
	cl, id, cur, gen := m.c, m.logID, m.logCursor, m.logGen
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		chunk, err := cl.Log(c, id, cur)
		return logMsg{gen, cur, chunk, err}
	}
}

// logTarget is the runner whose log a view on screen shows: the Runners
// page's preview follows the selection, the detail page's Log tab its runner
// (also after the runner finished: the daemon serves archived logs).
func (m Model) logTarget() string {
	switch {
	case m.page == pageRunners:
		if r := m.selectedRunner(); r != nil {
			return r.ID
		}
	case m.page == pageDetail && m.groups.tabs.active == tabLog:
		return m.detailID
	}
	return ""
}

// follow points the log at the runner a visible view shows, starting it over
// when that is a different runner. Only one log is followed at a time.
func (m *Model) follow() tea.Cmd {
	id := m.logTarget()
	if id == "" || id == m.logID {
		return nil
	}
	m.logID, m.logText, m.logCursor, m.logScroll = id, "", "", 0
	m.logGen++
	m.logBusy = false
	return m.fetchLog()
}

func (m Model) fetchSteps() tea.Cmd {
	id := m.detailID
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		s, err := m.c.Steps(c, id)
		return stepsMsg{id, s, err}
	}
}

func (m Model) fetchContainers() tea.Cmd {
	id := m.detailID
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		cs, err := m.c.Containers(c, id)
		return containersMsg{id, cs, err}
	}
}

func (m Model) fetchConfig() tea.Cmd {
	seq := m.settings.nextSeq()
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		var cfg config.Config
		if err := m.c.Config(c, &cfg); err != nil {
			return nil
		}
		return configMsg{seq, &cfg}
	}
}

// action runs fn against the daemon and reports text on success.
func (m Model) action(text string, fn func(c context.Context) error) tea.Cmd {
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		return doneMsg{text: text, err: fn(c)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.frame++
		m.toast.Tick(m.now())
		cmds := []tea.Cmd{tick(), m.fetchStatus(), m.fetchEvents()}
		if m.logTarget() != "" {
			cmds = append(cmds, m.fetchLog())
		}
		if m.page == pageHistory && m.frame%slowPoll == 0 {
			cmds = append(cmds, m.fetchHistory())
		}
		if m.page == pageDetail && m.frame%slowPoll == 0 && m.instance(m.detailID) != nil {
			cmds = append(cmds, m.fetchSteps(), m.fetchContainers())
		}
		if m.frame%slowPoll == 0 || (m.page == pageSettings && m.cfg == nil) {
			cmds = append(cmds, m.fetchConfig())
		}
		return m, tea.Batch(cmds...)
	case statusMsg:
		if msg.err != nil {
			m.connected = false
			m.connErr = clean(msg.err.Error())
			return m, nil
		}
		m.connected = true
		var runner, repo string
		if r := m.selectedRunner(); r != nil {
			runner = r.ID
		}
		if r := m.selectedRepo(); r != nil {
			repo = r.Name
		}
		m.st = cleanStatus(msg.st)
		m.reselect(runner, repo)
		m.clampSelections()
		if inst := m.instance(m.detailID); inst != nil {
			m.detailSnap = *inst
		} else if m.page == pageDetail && !m.finished() {
			m.detailDone = m.now()
		}
		var cmds []tea.Cmd
		if msg.st.Epoch != m.epoch {
			// The daemon restarted: its event sequence numbers started over, and
			// it may have reloaded a hand-edited config.yaml.
			m.epoch, m.lastSeq, m.events, m.eventScroll = msg.st.Epoch, 0, nil, 0
			cmds = append(cmds, m.fetchEvents(), m.fetchConfig())
		}
		cmds = append(cmds, m.follow())
		return m, tea.Batch(cmds...)
	case eventsMsg:
		if msg.epoch != m.epoch {
			return m, nil
		}
		for _, e := range msg.ev {
			e.Repo, e.Msg = clean(e.Repo), clean(e.Msg)
			if e.Seq > m.lastSeq {
				m.events = append(m.events, e)
				m.lastSeq = e.Seq
			}
		}
		if len(m.events) > maxEvents {
			m.events = m.events[len(m.events)-maxEvents:]
		}
		return m, nil
	case historyMsg:
		if msg.repo == m.histRepo && msg.concl == m.histConcl {
			for i := range msg.hist {
				cleanEntry(&msg.hist[i])
			}
			m.hist = msg.hist
			m.clampSelections()
		}
		return m, nil
	case logMsg:
		if msg.gen != m.logGen {
			return m, nil
		}
		m.logBusy = false
		if msg.err == nil && msg.cursor == m.logCursor {
			m.logText += clean(msg.chunk.Data)
			m.logCursor = msg.chunk.Next
			if len(m.logText) > 256*1024 {
				m.logText = m.logText[len(m.logText)-256*1024:]
			}
		}
		return m, nil
	case stepsMsg:
		if m.page == pageDetail && msg.id == m.detailID {
			if msg.err != nil {
				m.stepsErr = clean(msg.err.Error())
			} else {
				for i := range msg.steps {
					msg.steps[i].Name = clean(msg.steps[i].Name)
				}
				m.steps, m.stepsErr = msg.steps, ""
			}
		}
		return m, nil
	case containersMsg:
		if m.page == pageDetail && msg.id == m.detailID {
			if msg.err != nil {
				m.ctrsErr = clean(msg.err.Error())
			} else {
				for i := range msg.ctrs {
					c := &msg.ctrs[i]
					c.Name, c.Image, c.Project, c.State = clean(c.Name), clean(c.Image), clean(c.Project), clean(c.State)
				}
				m.containers, m.ctrsErr = msg.ctrs, ""
			}
		}
		return m, nil
	case configMsg:
		m.loadConfig(msg.seq, msg.cfg)
		return m, nil
	case savedMsg:
		return m.saved(msg)
	case refetchedMsg:
		return m.refetched(msg)
	case addedMsg:
		return m.added(msg)
	case doneMsg:
		if msg.err != nil {
			m.toast.Show(clean(msg.err.Error()), true, m.now())
		} else if msg.text != "" {
			m.toast.Show(msg.text, false, m.now())
		}
		return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())
	case ui.Pressed:
		return m.pressed(msg.ID)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func cleanEntry(h *model.HistoryEntry) {
	h.Repo, h.JobName, h.RunNumber = clean(h.Repo), clean(h.JobName), clean(h.RunNumber)
	h.Conclusion, h.HTMLURL = clean(h.Conclusion), clean(h.HTMLURL)
}

// cleanStatus sanitises the text fields of st in place; the slices come from
// a freshly decoded response nobody else holds.
func cleanStatus(st model.Status) model.Status {
	st.Mode, st.DegradedReason = clean(st.Mode), clean(st.DegradedReason)
	for i := range st.Repos {
		r := &st.Repos[i]
		r.Name, r.Error = clean(r.Name), clean(r.Error)
		if r.LastJob != nil {
			r.LastJob.JobName, r.LastJob.RunNumber = clean(r.LastJob.JobName), clean(r.LastJob.RunNumber)
		}
	}
	for i := range st.Instances {
		in := &st.Instances[i]
		in.ID, in.Repo, in.RunnerName, in.State = clean(in.ID), clean(in.Repo), clean(in.RunnerName), clean(in.State)
		if j := in.Job; j != nil {
			j.Name, j.Workflow, j.RunNumber, j.HTMLURL = clean(j.Name), clean(j.Workflow), clean(j.RunNumber), clean(j.HTMLURL)
		}
	}
	return st
}

func clamp(v, n int) int {
	if v >= n {
		v = n - 1
	}
	if v < 0 {
		v = 0
	}
	return v
}

func (m *Model) clampSelections() {
	m.repoSel = clamp(m.repoSel, len(m.st.Repos))
	m.runnerSel = clamp(m.runnerSel, len(m.st.Instances))
	m.histSel = clamp(m.histSel, len(m.hist))
}

// reselect points the selections at the runner and repo that were selected
// before a refresh, which may have moved them. Row buttons act on the
// selection, so it must follow the row, not its position.
func (m *Model) reselect(runner, repo string) {
	for i := range m.st.Instances {
		if m.st.Instances[i].ID == runner {
			m.runnerSel = i
		}
	}
	for i := range m.st.Repos {
		if m.st.Repos[i].Name == repo {
			m.repoSel = i
		}
	}
}

func (m Model) selectedRepo() *model.RepoStatus {
	if m.repoSel < len(m.st.Repos) {
		return &m.st.Repos[m.repoSel]
	}
	return nil
}

func (m Model) selectedRunner() *model.InstanceStatus {
	if m.runnerSel < len(m.st.Instances) {
		return &m.st.Instances[m.runnerSel]
	}
	return nil
}

func (m Model) instance(id string) *model.InstanceStatus {
	for i := range m.st.Instances {
		if m.st.Instances[i].ID == id {
			return &m.st.Instances[i]
		}
	}
	return nil
}
