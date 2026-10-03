package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func TestInitLoadsLastJobPerRepo(t *testing.T) {
	h := newHarness(t)
	h.m.History.Append(model.HistoryEntry{ID: "1", Repo: "darkmem", RunNumber: "1", FinishedAt: h.now.Add(-time.Hour)})
	h.m.History.Append(model.HistoryEntry{ID: "2", Repo: "darkmem", RunNumber: "2", FinishedAt: h.now})
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	st := h.m.Status()
	if st.Repos[1].Name != "darkmem" || st.Repos[1].LastJob == nil || st.Repos[1].LastJob.RunNumber != "2" {
		t.Fatalf("repos %+v", st.Repos)
	}
	if st.Repos[0].LastJob != nil {
		t.Fatal("darkcloud has no history")
	}
}

// Cleanup finishes out of order: an older job finalized later must not replace the last job.
func TestLastJobKeepsNewest(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.recordLastJob(model.HistoryEntry{ID: "new", Repo: "darkmem", FinishedAt: h.now})
	h.m.recordLastJob(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: h.now.Add(-time.Hour)})
	h.m.mu.Unlock()
	if got := h.m.Status().Repos[1].LastJob; got == nil || got.ID != "new" {
		t.Fatalf("last job %+v", got)
	}
}

func TestStatusIgnoresRepoCase(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.insts["dddddd"] = &instance{Meta: Meta{ID: "dddddd", Repo: "DarkMem"}, State: sched.Idle, StateSince: h.now}
	h.m.recordLastJob(model.HistoryEntry{ID: "renamed", Repo: "DARKMEM", FinishedAt: h.now})
	h.m.mu.Unlock()
	st := h.m.Status()
	if st.Repos[1].Active != 1 || st.Repos[1].LastJob == nil || st.Repos[1].LastJob.ID != "renamed" {
		t.Fatalf("repos %+v", st.Repos)
	}
}

func TestStatusEpochChangesPerStart(t *testing.T) {
	h := newHarness(t)
	first := h.m.Status().Epoch
	h.now = h.now.Add(time.Second)
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	if first == "" || h.m.Status().Epoch == first {
		t.Fatalf("epochs %q %q", first, h.m.Status().Epoch)
	}
}

func TestStatusCountsAndSorts(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.insts["bbbbbb"] = &instance{Meta: Meta{ID: "bbbbbb", Repo: "darkmem"}, State: sched.Busy, StateSince: h.now}
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkmem"}, State: sched.Cleaning, StateSince: h.now}
	h.m.insts["cccccc"] = &instance{Meta: Meta{ID: "cccccc", Repo: "darkcloud"}, State: sched.Idle, StateSince: h.now}
	h.m.demand = sched.Demand{"darkcloud": {{Repo: "darkcloud", ID: 1}, {Repo: "darkcloud", ID: 2}}}
	h.m.mu.Unlock()
	st := h.m.Status()
	if st.Mode != "queue" || st.GlobalMax != 2 || st.RateRemaining != 4999 {
		t.Fatalf("header %+v", st)
	}
	if st.Repos[0].Active != 1 || st.Repos[0].Queued != 2 || st.Repos[0].Max != 1 || st.Repos[1].Active != 2 || st.Repos[1].Max != 1 {
		t.Fatalf("repos %+v", st.Repos)
	}
	var order []string
	for _, i := range st.Instances {
		order = append(order, i.ID)
	}
	if len(order) != 3 || order[0] != "cccccc" || order[1] != "aaaaaa" || order[2] != "bbbbbb" {
		t.Fatalf("instances not sorted by repo then id: %v", order)
	}
}

func TestUnknownRunner(t *testing.T) {
	h := newHarness(t)
	var u ErrUnknownRunner
	if err := h.m.Kill(context.Background(), "zzzzzz"); !errors.As(err, &u) {
		t.Fatalf("kill err %v", err)
	}
	if _, _, _, err := h.m.RunnerRepoAndRun("zzzzzz"); !errors.As(err, &u) {
		t.Fatalf("lookup err %v", err)
	}
	h.m.mu.Lock()
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerName: "ghr-darkcloud-aaaaaa"},
		Job: &model.JobInfo{RunID: 55}}
	h.m.mu.Unlock()
	repo, runID, name, err := h.m.RunnerRepoAndRun("aaaaaa")
	if err != nil || repo != "darkcloud" || runID != 55 || name != "ghr-darkcloud-aaaaaa" {
		t.Fatalf("lookup %s %d %s %v", repo, runID, name, err)
	}
}
