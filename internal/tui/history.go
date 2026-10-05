package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	histRepoSel   = "hist/repo"
	histResultSel = "hist/result"
	histTable     = "hist/table"
	rowCopy       = "row/copy"
)

var resultOptions = []ui.Option{
	{Value: "", Label: "all"}, {Value: "success", Label: "success"},
	{Value: "failure", Label: "failure"}, {Value: "cancelled", Label: "cancelled"},
}

// histFilters brings the filter selects up to date with the model, which
// stays the source of truth because r and c change the filters too. The repo
// options follow the configured repos, keeping a filtered repo that has since
// been removed so the select still shows it.
func (m Model) histFilters() {
	g := m.groups
	names := m.repoNames()
	if m.histRepo != "" && !slices.Contains(names, m.histRepo) {
		names = append(names, m.histRepo)
	}
	opts := []ui.Option{{Value: "", Label: "all"}}
	for _, n := range names {
		opts = append(opts, ui.Option{Value: n, Label: clean(n)})
	}
	if !slices.Equal(g.histRepo.Options, opts) {
		g.histRepo = ui.NewSelect(histRepoSel, opts)
	}
	g.histRepo.SetValue(ui.Value{Text: m.histRepo})
	g.histResult.SetValue(ui.Value{Text: m.histConcl})
	g.hist.Set([]ui.Widget{g.histRepo, g.histResult, stop{histTable}})
}

// histApply adopts the selects' values as the filters, refetching the
// history when they changed.
func (m *Model) histApply() tea.Cmd {
	repo, concl := m.groups.histRepo.Value().Text, m.groups.histResult.Value().Text
	if repo == m.histRepo && concl == m.histConcl {
		return nil
	}
	m.histRepo, m.histConcl = repo, concl
	return m.fetchHistory()
}

// histFooterKeys are the footer hints for the focused History element.
func (m Model) histFooterKeys() []footerKey {
	if s, ok := m.groups.hist.Focused().(*ui.Select); ok {
		if s.Open() {
			return []footerKey{{"up", "move"}, {"enter", "pick"}, {"esc", "close"}}
		}
		return []footerKey{{"left", "prev"}, {"right", "next"}, {"enter", "open"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
	}
	return []footerKey{{"r", "repo"}, {"c", "result"}, {"enter", "copy URL"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
}

// histKey handles the History page's focus keys: tab and shift+tab move
// between the two filters and the table, and a focused filter takes its own
// keys. It reports false for keys the page and global keys handle.
func (m Model) histKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	m.histFilters()
	g := m.groups
	if ok, _ := g.hist.Key(k); ok {
		cmd := m.histApply()
		return true, m, cmd
	}
	switch k.String() {
	case "tab":
		g.hist.Next()
	case "shift+tab":
		g.hist.Prev()
	default:
		return false, m, nil
	}
	return true, m, nil
}

// historyPage renders the filter bar above the History table. An open
// dropdown's options push the table down, under their own select.
func (m Model) historyPage(w, h int) string {
	m.histFilters()
	g := m.groups
	f := g.hist.FocusedID()
	repo := strings.Split(g.histRepo.View(f == histRepoSel, 0), "\n")
	result := strings.Split(g.histResult.View(f == histResultSel, 0), "\n")
	left := sDim.Render("Repo ") + repo[0] + "    "
	bar := []string{left + sDim.Render("Result ") + result[0]}
	for _, l := range repo[1:] {
		bar = append(bar, "     "+l)
	}
	for _, l := range result[1:] {
		bar = append(bar, strings.Repeat(" ", ansi.StringWidth(left)+7)+l)
	}
	for i := range bar {
		bar[i] = ansi.Truncate(bar[i], w, "…") // a long repo name must not widen the page
	}
	bar = append(bar, "")

	inner := w - 4
	showFinished, showRepo, showDur, barW := true, true, true, 12
	lead := func() int { // "  ", FINISHED, REPO, "RUN "
		n := 2 + 8
		if showFinished {
			n += 17
		}
		if showRepo {
			n += 13
		}
		return n
	}
	tail := func() int { // " RESULT", " DURATION", " BAR"
		n := 12
		if showDur {
			n += 9
		}
		if barW > 0 {
			n += barW + 1
		}
		return n
	}
	for _, giveWay := range []func(){
		func() { barW = 6 }, func() { barW = 0 }, func() { showRepo = false },
		func() { showFinished = false }, func() { showDur = false },
	} {
		if inner-lead()-tail() >= 14 {
			break
		}
		giveWay()
	}
	jobW := max(inner-lead()-tail(), 1)
	hdr := "  "
	if showFinished {
		hdr += fmt.Sprintf("%-16s ", "FINISHED")
	}
	if showRepo {
		hdr += fmt.Sprintf("%-12s ", "REPO")
	}
	hdr += fmt.Sprintf("%-7s %-*s %-11s", "RUN", jobW, "JOB", "RESULT")
	if showDur {
		hdr += " DURATION"
	}
	lines := []string{sDim.Render(hdr)}
	visible := max(h-len(bar)-3, 1)
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	var longest time.Duration
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		longest = max(longest, m.hist[i].FinishedAt.Sub(m.hist[i].StartedAt))
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		d := e.FinishedAt.Sub(e.StartedAt)
		line := "  "
		if showFinished {
			line += cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " "
		}
		if showRepo {
			line += cell(e.Repo, 12) + " "
		}
		line += cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, jobW)
		badge := stateBadge(e.Conclusion)
		after := strings.Repeat(" ", max(11-ansi.StringWidth(badge), 0))
		if showDur {
			after += " " + cell(dur(d), 8)
		}
		if barW > 0 {
			after += " " + durBar(d, longest, barW)
		}
		id := fmt.Sprintf("hist-%d", i)
		if i == m.histSel {
			tailW := tail()
			lines = append(lines, m.selectedRow(id, line, "", "", rowButtons(ui.NewButton(rowCopy, "Copy run URL", ui.Secondary)), "", inner-tailW)+
				ui.BadgeRow(sSel, true, " ", badge, after, tailW))
		} else {
			lines = append(lines, zone.Mark(id, ui.BadgeRow(sSel, false, line+" ", badge, after, inner)))
		}
	}
	if len(m.hist) == 0 {
		lines = append(lines, sDim.Render("  no finished jobs yet"))
	}
	title := "History"
	if f == histTable {
		title = "› History"
	}
	return strings.Join(bar, "\n") + "\n" + box(title, w, lines)
}
