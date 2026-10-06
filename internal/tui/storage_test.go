package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// onStorage opens the Storage page on the sample model, w by h.
func onStorage(t *testing.T, c *fakeClient, w, h int) Model {
	t.Helper()
	m := feed(sampleModel(c, w, h), key("5"))
	if m.page != pageStorage {
		t.Fatalf("key 5 went to page %v", m.page)
	}
	return m
}

func TestStorageNavigation(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 40)
	v := m.View()
	for _, want := range []string{"5 Storage", "6 Settings", "Recent operations", "13:55", "[REFUSED]",
		"refused: 1 jobs running", "installed node 22.11.0"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if m = feed(m, key("6")); m.page != pageSettings {
		t.Fatalf("key 6 went to page %v", m.page)
	}
	if v := onStorage(t, &fakeClient{}, 80, 30).View(); !strings.Contains(v, "5 Stor") {
		t.Fatalf("tab row:\n%s", v)
	}
}

func TestStoragePollsEverySecondWhileBusy(t *testing.T) {
	c := &fakeClient{}
	m := onStorage(t, c, 120, 40)
	base := c.storageN
	if m = ticks(m, 4); c.storageN != base {
		t.Fatalf("an idle page polled on ticks 1-4: %d reads", c.storageN-base)
	}
	if m = ticks(m, 1); c.storageN != base+1 {
		t.Fatalf("an idle page did not poll on tick 5: %d reads", c.storageN-base)
	}
	st := sampleStorage()
	st.Operations.Current = &model.Operation{ID: "op3", Kind: "install", Target: "node 24", StartedAt: now, Progress: "extracting"}
	c.storage = &st
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	base = c.storageN
	if m = ticks(m, 3); c.storageN != base+3 {
		t.Fatalf("a busy page polled %d times in 3 ticks", c.storageN-base)
	}
}

func TestStorageAnnouncesOnlyNewlyFinishedOperations(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 40)
	if m.toast.Active() {
		t.Fatalf("toast on opening: %q", m.toast.Text)
	}
	st := sampleStorage()
	done := now
	st.Operations.Recent = append([]model.Operation{{ID: "op3", Kind: "clear", Target: "nuget", StartedAt: now,
		FinishedAt: &done, Outcome: "refused", Message: "refused: 2 jobs running"}}, st.Operations.Recent...)
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	if !m.toast.Active() || !m.toast.Err || m.toast.Text != "clear nuget: refused: 2 jobs running" {
		t.Fatalf("toast %q err=%v", m.toast.Text, m.toast.Err)
	}
	m.toast.Close()
	if m = feed(m, storageMsg{seq: m.store.seq, s: st}); m.toast.Active() {
		t.Fatal("the same operation was announced twice")
	}
}

func TestStorageKeepsTheNewestReply(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 40)
	old := sampleStorage()
	old.Operations.Recent = nil
	if m = feed(m, storageMsg{seq: m.store.shown - 1, s: old}); len(m.store.data.Operations.Recent) != 2 {
		t.Fatal("an older reply replaced the newer one")
	}
	m = feed(m, storageMsg{seq: m.store.shown, err: errors.New("connection refused")})
	if v := m.View(); !strings.Contains(v, "✖ connection refused") || !strings.Contains(v, "Recent operations") {
		t.Fatalf("a failed refresh should keep the last snapshot under the error:\n%s", v)
	}
}

// withConfig loads a config with the disk settings the Storage page shows.
// c serves it too: every action's doneMsg fetches the config again.
func withConfig(m Model, c *fakeClient) Model {
	c.cfg = &config.Config{Mode: "queue", DiskHighWater: 80, BuildCacheKeep: "20GB"}
	cfg := *c.cfg
	return feed(m, configMsg{seq: m.nextCfgSeq(), cfg: &cfg})
}

func TestStorageDockerCard(t *testing.T) {
	c := &fakeClient{}
	m := withConfig(onStorage(t, c, 120, 60), c)
	v := m.View()
	for _, want := range []string{"Docker disk", "61% used · prunes above 80%", "reclaimable", "Images", "6.6 GB", "5.6 GB",
		"Local Volumes", "8.7 GB", "Build Cache", "source.local", "86.0 MB",
		"auto · 13:07 · ok — build cache older than 72h 1.2 GB, dangling images 300.0 MB",
		"[ Prune ]", "( Build cache to 20GB )", "[ All build cache ]", "( Dangling images )", "[ Unused volumes ]",
		"Unused volumes: refused while 1 jobs run"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	narrow := &fakeClient{}
	if v := withConfig(onStorage(t, narrow, 80, 60), narrow).View(); strings.Contains(v, "reclaimable") {
		t.Fatalf("the reclaimable column shows at 80 columns:\n%s", v)
	}
	st := sampleStorage()
	failed := now.Add(-5 * time.Minute)
	st.LastPrune = &model.LastPrune{Trigger: "manual", Scope: "build-cache-all", StartedAt: failed, FinishedAt: &failed,
		Outcome: "errors", Steps: []model.PruneStep{{Name: "all build cache", Error: "docker: daemon busy"}}}
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	if v := m.View(); !strings.Contains(v, "manual build-cache-all · 14:00 · errors — all build cache: docker: daemon busy") {
		t.Fatalf("failed prune line:\n%s", v)
	}
	st.LastPrune = nil
	if v := feed(m, storageMsg{seq: m.store.seq, s: st}).View(); !strings.Contains(v, "no prune since start") {
		t.Fatalf("no prune:\n%s", v)
	}
}

func TestStoragePruneButtonsConfirmFirst(t *testing.T) {
	c := &fakeClient{}
	m := withConfig(onStorage(t, c, 120, 60), c)
	for _, id := range []string{storePrune, storeKeep, storeAll, storeDangling, storeVolumes} {
		if m = click(t, m, id); m.overlay != ovConfirm {
			t.Fatalf("%s: no confirmation", id)
		}
		m = feed(m, key("n"))
	}
	if len(c.actions()) != 0 {
		t.Fatalf("a declined prune ran: %v", c.actions())
	}
	if m = click(t, m, storeAll); !strings.Contains(m.confirmText, "Remove all build cache (up to 456.0 MB)?") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	m = feed(m, key("y"))
	if !strings.Contains(m.View(), "build-cache-all prune started") {
		t.Fatalf("toast:\n%s", m.View())
	}
	if m = click(t, m, storeKeep); !strings.Contains(m.confirmText, "Prune the build cache down to 20GB?") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	m = feed(m, key("y"))
	m = feed(click(t, m, storePrune), key("y"))
	if got := strings.Join(c.actions(), "|"); got != "prune build-cache-all|prune build-cache-keep|prune" {
		t.Fatalf("actions %q", got)
	}
	c.storeErr = &api.Error{Status: 409, Msg: "refused: 1 jobs running"}
	if m = click(t, m, storeVolumes); !strings.Contains(m.confirmText, "Remove every volume no container uses (up to 8.7 GB)?") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	if m = feed(m, key("y")); !m.toast.Err || !strings.Contains(m.toast.Text, "refused: 1 jobs running") {
		t.Fatalf("toast %q err=%v", m.toast.Text, m.toast.Err)
	}
	st := sampleStatus()
	st.Maintenance.Running = true
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "pruning…") || m.store.prune.Focusable() || m.store.volumes.Focusable() {
		t.Fatalf("buttons stay enabled while a prune runs:\n%s", v)
	}
}
