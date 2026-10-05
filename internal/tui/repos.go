package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	reposList   = "repos/list"   // the list card's tab stop
	reposAdd    = "repos/add"    // the header's Add repository button
	reposPause  = "repos/pause"  // the panel's Pause/Resume button
	reposRemove = "repos/remove" // the panel's Remove button

	reposListW = 30 // the list card's width in the wide layout
)

// reposPage is the Repositories page. The Model holds it by pointer, so its
// state survives Bubble Tea copying the Model.
type reposPage struct {
	configPage
	selected      string // the selected repo's name; selection follows the name across refreshes
	idx           int    // the selection's last position, used when its repo disappears
	add           *ui.Button
	pause, remove *ui.Button
}

func newReposPage() *reposPage {
	rp := &reposPage{
		configPage: newConfigPage("repos/save", "repos/discard"),
		add:        ui.NewButton(reposAdd, "+ Add repository", ui.Primary),
		pause:      ui.NewButton(reposPause, "Pause", ui.Secondary),
		remove:     ui.NewButton(reposRemove, "Remove", ui.Danger),
	}
	rp.group.Set([]ui.Widget{rp.add, stop{reposList}})
	rp.group.Focus(reposList)
	return rp
}

// repoIndex resolves the selection by name, falling back to the repo now at
// the selection's last position when its repo has gone; -1 with no repos.
func (m Model) repoIndex() int {
	rp := m.repos
	for i, r := range m.st.Repos {
		if strings.EqualFold(r.Name, rp.selected) {
			rp.idx = i
			return i
		}
	}
	if len(m.st.Repos) == 0 {
		return -1
	}
	i := min(max(rp.idx, 0), len(m.st.Repos)-1)
	rp.selected, rp.idx = m.st.Repos[i].Name, i
	return i
}

func (m Model) selectedRepoStatus() *model.RepoStatus {
	if i := m.repoIndex(); i >= 0 {
		return &m.st.Repos[i]
	}
	return nil
}

func (m Model) moveRepo(d int) {
	if i := m.repoIndex(); i >= 0 {
		j := clamp(i+d, len(m.st.Repos))
		m.repos.selected, m.repos.idx = m.st.Repos[j].Name, j
	}
}

// repoState is the state a repo's badge shows.
func repoState(r model.RepoStatus) string {
	switch {
	case r.Error != "":
		return "error"
	case r.Removing:
		return "removing"
	case r.Paused:
		return "paused"
	}
	return "active"
}

// syncRepoControls updates the panel buttons for the selected repo and sets
// the page's focus order: the header button, the list, then the panel.
func (m Model) syncRepoControls() {
	rp := m.repos
	items := []ui.Widget{rp.add, stop{reposList}}
	rp.add.SetDisabled(!m.connected)
	if r := m.selectedRepoStatus(); r != nil {
		rp.pause.Label = "Pause"
		if r.Paused {
			rp.pause.Label = "Resume"
		}
		for _, b := range []*ui.Button{rp.pause, rp.remove} {
			b.SetDisabled(!m.connected || r.Removing)
		}
		items = append(items, rp.pause, rp.remove)
	}
	rp.group.Set(items)
}

// reposListCard renders the list card, w columns wide and h lines tall.
func (m Model) reposListCard(w, h int) string {
	sel := m.repoIndex()
	focused := m.repos.group.FocusedID() == reposList
	var lines []string
	for i, r := range m.st.Repos {
		cursor := "  "
		if i == sel {
			cursor = "› "
		}
		badge := stateBadge(repoState(r))
		row := ui.BadgeRow(sSel, i == sel && focused, cursor+cell(clean(r.Name), 11)+" ", badge, "", w-4)
		lines = append(lines, zone.Mark(fmt.Sprintf("repos/row/%d", i), row))
	}
	title := "Repos"
	if focused {
		title = "› Repos"
	}
	return box(title, w, fit(lines, 0, max(sel, 0), max(h-2, 1)))
}

// repoSummary is the panel's first lines: state, counts, the last job and the actions.
func (m Model) repoSummary(r model.RepoStatus) []string {
	f := m.repos.group.FocusedID()
	counts := fmt.Sprintf("%d/%s running · %d queued", r.Active, maxText(r.Max), r.Queued)
	last := sDim.Render("no finished jobs yet")
	if j := r.LastJob; j != nil {
		icon, style := "✔", sGreen
		if j.Conclusion != "success" {
			icon, style = "✖", sRed
		}
		last = style.Render(icon) + fmt.Sprintf(" #%s %s · %s", j.RunNumber, j.JobName, ago(m.now().Sub(j.FinishedAt)))
	}
	out := []string{stateBadge(repoState(r)) + "  " + counts, last}
	if r.Removing {
		out = append(out, sAmber.Render("removing… running jobs finish first"))
	}
	if r.Error != "" {
		out = append(out, sRed.Render(r.Error))
	}
	return append(out, m.repos.pause.View(f == reposPause, 0)+"  "+m.repos.remove.View(f == reposRemove, 0))
}

// reposPanel renders the selected repo's panel, w columns wide.
func (m Model) reposPanel(r model.RepoStatus, w int) []string {
	return strings.Split(box(clean(r.Name), w, m.repoSummary(r)), "\n")
}

// reposView renders the page in w columns and h lines: the list card and
// the panel side by side when wide, stacked otherwise.
func (m Model) reposView(w, h int) string {
	m.syncRepoControls()
	r := m.selectedRepoStatus()
	if r == nil {
		return box("Repos", min(w, reposListW), []string{sDim.Render("No repositories yet"), "",
			m.repos.add.View(m.repos.group.FocusedID() == reposAdd, 0)})
	}
	if m.width >= wideMin {
		panel := strings.Join(m.reposPanel(*r, w-reposListW-1), "\n")
		return lipgloss.JoinHorizontal(lipgloss.Top, m.reposListCard(reposListW, h), " ", panel)
	}
	list := m.reposListCard(w, min(len(m.st.Repos)+2, max(h/3, 3)))
	return list + "\n" + strings.Join(m.reposPanel(*r, w), "\n")
}

// reposHandleKey handles the page's keys. It reports false for keys that fall
// through to the global keys.
func (m Model) reposHandleKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	rp := m.repos
	m.syncRepoControls()
	if ok, cmd := rp.group.Key(k); ok {
		return true, m, cmd
	}
	switch k.String() {
	case "tab":
		rp.group.Next()
	case "shift+tab":
		rp.group.Prev()
	case "up", "k", "down", "j":
		if rp.group.FocusedID() == reposList {
			d := 1
			if k.String() == "up" || k.String() == "k" {
				d = -1
			}
			m.moveRepo(d)
		}
	case "a":
		mm, cmd := m.openAddRepo()
		return true, mm, cmd
	case "p":
		mm, cmd := m.reposAction(reposPause)
		return true, mm, cmd
	case "d":
		mm, cmd := m.reposAction(reposRemove)
		return true, mm, cmd
	default:
		return false, m, nil
	}
	return true, m, nil
}

// reposAction pauses, resumes or (after asking) removes the selected repo.
func (m Model) reposAction(id string) (tea.Model, tea.Cmd) {
	r := m.selectedRepoStatus()
	if r == nil || r.Removing || m.offline() {
		return m, nil
	}
	name := r.Name
	if id == reposRemove {
		return m.openConfirm(fmt.Sprintf("Remove repo %s? Its running jobs finish first.", name), func() tea.Cmd {
			return m.action("removing "+name, func(c context.Context) error { return m.c.RemoveRepo(c, name) })
		})
	}
	if r.Paused {
		return m, m.action("resumed "+name, func(c context.Context) error { return m.c.Resume(c, name) })
	}
	return m, m.action("paused "+name, func(c context.Context) error { return m.c.Pause(c, name) })
}

// reposMouse handles clicks on the list, the wheel over the list, and the
// page's buttons. It reports false for events the shell should handle.
func (m Model) reposMouse(msg tea.MouseMsg) (bool, tea.Model, tea.Cmd) {
	rp := m.repos
	m.syncRepoControls()
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		for i := range m.st.Repos {
			if zone.Get(fmt.Sprintf("repos/row/%d", i)).InBounds(msg) {
				d := 1
				if msg.Button == tea.MouseButtonWheelUp {
					d = -1
				}
				m.moveRepo(d)
				return true, m, nil
			}
		}
		return false, m, nil
	}
	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
		for i, r := range m.st.Repos {
			if zone.Get(fmt.Sprintf("repos/row/%d", i)).InBounds(msg) {
				rp.selected, rp.idx = r.Name, i
				rp.group.Focus(reposList)
				return true, m, nil
			}
		}
	}
	ok, cmd := rp.group.Mouse(msg)
	return ok, m, cmd
}

func (m Model) reposFooterKeys() []footerKey {
	return []footerKey{{"up", "select"}, {"tab", "next"}, {"a", "add"}, {"p", "pause"}, {"d", "remove"}, {"?", "help"}, {"q", "quit"}}
}
