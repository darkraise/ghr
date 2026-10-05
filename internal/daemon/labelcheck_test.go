package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func labelGH(gh *fakeGH) {
	t0 := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	gh.recent = []github.Run{{ID: 1}, {ID: 2}}
	gh.runs = map[string][]github.Run{"queued": {{ID: 2}, {ID: 3}}}
	gh.jobs = map[int64][]github.Job{
		1: {{Name: "test", WorkflowName: "CI", Labels: []string{"self-hosted", "darkcloud-linux"}, CreatedAt: t0}},
		2: {{Name: "lint", WorkflowName: "CI", Labels: []string{"ubuntu-latest"}, CreatedAt: t0.Add(time.Hour)}},
		3: {
			{Name: "gpu", WorkflowName: "ML", Labels: []string{"Self-Hosted", "GPU"}, CreatedAt: t0.Add(2 * time.Hour)},
			{Name: "gpu", WorkflowName: "ML", Labels: []string{"gpu", "self-hosted"}, CreatedAt: t0.Add(30 * time.Minute)},
		},
	}
}

func waitLabelCheck(t *testing.T, b *Backend, repo string) model.LabelCheck {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		lc, err := b.LabelCheck(repo)
		if err != nil {
			t.Fatal(err)
		}
		if lc.State != "checking" {
			return lc
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("label check never finished")
	return model.LabelCheck{}
}

func TestLabelCheckGathersAndGroups(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	if lc, err := b.LabelCheck("darkcloud"); err != nil || lc.State != "not_checked" || lc.Groups == nil {
		t.Fatalf("before any check: %+v %v", lc, err)
	}
	if err := b.StartLabelCheck("DarkCloud"); err != nil {
		t.Fatal(err)
	}
	lc := waitLabelCheck(t, b, "darkcloud")
	if lc.State != "done" || lc.Partial || lc.CheckedAt == nil || len(lc.Groups) != 3 {
		t.Fatalf("%+v", lc)
	}
	if g := lc.Groups[0]; !reflect.DeepEqual(g.Labels, []string{"gpu", "self-hosted"}) || g.Count != 2 ||
		!reflect.DeepEqual(g.Jobs, []string{"ML / gpu"}) || !g.LastSeen.Equal(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("newest group %+v", g)
	}
	if !reflect.DeepEqual(gh.jobCalls, []int64{1, 2, 3}) {
		t.Fatalf("runs not de-duplicated: %v", gh.jobCalls)
	}
}

func TestLabelCheckThrottleAndTokenChange(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	waitLabelCheck(t, b, "darkcloud")
	var ae *api.Error
	if err := b.StartLabelCheck("darkcloud"); !errors.As(err, &ae) || ae.Status != 429 || !ae.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("within a minute: %v", err)
	}
	now = now.Add(time.Minute)
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("after a minute: %v", err)
	}
	waitLabelCheck(t, b, "darkcloud")
	gh.meta.Generation++
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "not_checked" {
		t.Fatalf("a new token keeps no old results: %+v", lc)
	}
}

func TestLabelCheckDeadlineErrorsAndDegraded(t *testing.T) {
	b, m, gh := newBackend(t)
	labelGH(gh)
	gh.block = true
	old := labelScanDeadline
	labelScanDeadline = 300 * time.Millisecond
	t.Cleanup(func() { labelScanDeadline = old })
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("a second start joins the running scan: %v", err)
	}
	if lc := waitLabelCheck(t, b, "darkcloud"); !lc.Partial || lc.Error != "" {
		t.Fatalf("deadline: %+v", lc)
	}

	b2, _, gh2 := newBackend(t)
	gh2.listErr = errors.New("connection reset")
	if err := b2.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	if lc := waitLabelCheck(t, b2, "darkcloud"); !lc.Partial || lc.Error != "connection reset" || len(lc.Groups) != 0 {
		t.Fatalf("listing error: %+v", lc)
	}

	m.degraded = "GitHub rejected the token"
	if _, err := b.LabelCheck("darkcloud"); apiStatus(err) != 503 {
		t.Fatalf("degraded read: %v", err)
	}
	if _, err := b.LabelCheck("nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown repo: %v", err)
	}
}

// Jobs a failed pagination returned before its error are still grouped.
func TestLabelCheckKeepsJobsReturnedWithError(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	gh.jobErr = map[int64]error{1: context.DeadlineExceeded}
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	lc := waitLabelCheck(t, b, "darkcloud")
	if !lc.Partial || lc.Error != "" || len(lc.Groups) != 1 || !reflect.DeepEqual(lc.Groups[0].Labels, []string{"darkcloud-linux", "self-hosted"}) {
		t.Fatalf("%+v", lc)
	}
}

// A token change cancels the running scan before a new one may start, and
// Close cancels whatever runs at shutdown.
func TestLabelCheckTokenChangeCancelsScan(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	gh.block = true
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	gh.meta.Generation++
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "not_checked" {
		t.Fatalf("old generation still shown: %+v", lc)
	}
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 1 })
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("new token: %v", err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "checking" {
		t.Fatalf("%+v", lc)
	}
	b.Close()
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 2 })
	if err := b.StartLabelCheck("darkcloud"); apiStatus(err) != 503 {
		t.Fatalf("start after Close: %v", err)
	}
}

// Removing a repo drops its observations and throttle, cancels its scan, and
// a cancelled scan never publishes into the entry of the re-added repo.
func TestLabelCheckForgottenOnRemoveAndAdd(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	ctx := context.Background()
	gh.block = true
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	if err := b.RemoveRepo("darkcloud"); err != nil {
		t.Fatal(err)
	}
	b.FinalizeRemovals()
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 1 })
	one := 1
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "darkcloud", Max: &one}); err != nil {
		t.Fatal(err)
	}
	if lc, err := b.LabelCheck("darkcloud"); err != nil || lc.State != "not_checked" {
		t.Fatalf("re-added repo shows old state: %+v %v", lc, err)
	}
	gh.block = false
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("re-added repo is throttled: %v", err)
	}
	if lc := waitLabelCheck(t, b, "darkcloud"); lc.State != "done" || lc.Partial || len(lc.Groups) != 3 {
		t.Fatalf("%+v", lc)
	}
}
