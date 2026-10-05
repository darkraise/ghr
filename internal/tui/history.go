package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
	lines := []string{sDim.Render(fmt.Sprintf("  %-16s %-12s %-7s %-*s %-10s %s", "FINISHED", "REPO", "RUN", inner-62, "JOB", "RESULT", "DURATION"))}
	visible := max(h-len(bar)-3, 1)
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		line := "  " + cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " " + cell(e.Repo, 12) + " " +
			cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, inner-62)
		tail := " " + stateStyle(e.Conclusion).Render(cell(e.Conclusion, 10)) + " " + dur(e.FinishedAt.Sub(e.StartedAt))
		id := fmt.Sprintf("hist-%d", i)
		if i == m.histSel {
			lines = append(lines, m.selectedRow(id, line, "", "", rowButtons(ui.NewButton(rowCopy, "Copy run URL", ui.Secondary)), tail, inner))
		} else {
			lines = append(lines, m.row(id, false, line+tail, inner))
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
