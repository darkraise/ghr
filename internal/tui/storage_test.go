package tui

import (
	"errors"
	"strings"
	"testing"

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
