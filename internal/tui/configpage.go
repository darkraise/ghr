package tui

import "github.com/darkraise/ghr/internal/tui/ui"

// configPage is what a page editing config.yaml owns: its form, focus,
// scroll, save state and rejected-save alert. Settings and Repositories
// each embed one; config responses are ordered once, by the Model.
type configPage struct {
	form          ui.Form
	group         ui.Group
	scroll        int
	saving        bool
	alert         []string // the daemon's messages from a rejected save
	save, discard *ui.Button
}

func newConfigPage(saveID, discardID string) configPage {
	return configPage{
		save:    ui.NewButton(saveID, "Save changes", ui.Primary),
		discard: ui.NewButton(discardID, "Discard", ui.Secondary),
	}
}

// cfgOrder numbers config requests. Responses can arrive out of order, so
// one answering an older request than the config already shown is dropped.
type cfgOrder struct{ seq, shown int }

func (m Model) nextCfgSeq() int {
	m.order.seq++
	return m.order.seq
}

// configPage returns the config page p, which must be pageSettings or pageRepos.
func (m Model) configPage(p page) *configPage {
	if p == pageRepos {
		return &m.repos.configPage
	}
	return &m.settings.configPage
}
