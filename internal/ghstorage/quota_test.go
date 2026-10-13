package ghstorage

import (
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func TestMonthBounds(t *testing.T) {
	for _, tc := range []struct {
		at            time.Time
		start, resets time.Time
	}{
		{time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		// 03:00 on Nov 1 at UTC+7 is still October in UTC.
		{time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("ICT", 7*3600)), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
	} {
		start, resets := MonthBounds(tc.at)
		if !start.Equal(tc.start) || !resets.Equal(tc.resets) || start.Location() != time.UTC {
			t.Errorf("MonthBounds(%v) = %v, %v", tc.at, start, resets)
		}
	}
}

func TestSumUsage(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	items := []github.UsageItem{
		{Date: day(5), SKU: github.SKUActionsStorage, Repo: "darkmem", Quantity: 204.5},
		{Date: day(12), SKU: github.SKUActionsStorage, Repo: "darkcloud", Quantity: 4.25},
		{Date: day(11), SKU: github.SKUPackagesStorage, Quantity: 0.5},
		{Date: day(12), SKU: github.SKUPackagesStorage, Quantity: 0.75},
		{Date: day(13), SKU: github.SKUPackagesStorage, Quantity: 0.125},
	}
	actions, packages, daily := SumUsage(items, time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC))
	if actions != 208.75 || packages != 1.375 || daily != 0.75 {
		t.Fatalf("actions %v packages %v daily %v", actions, packages, daily)
	}
}

// October 2026 has 744 hours; 500 MB is 500/1024 × 744 = 363.28125 GB-h.
// Read at 09:00 on Oct 13, 447 hours are left before Nov 1.
func TestComputeQuota(t *testing.T) {
	readAt := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	q := ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 90, Packages: 10, PackagesDaily: 6}, model.GiB/2, 500, [2]int{70, 90})
	if q.Month != "2026-10" || !q.ReadAt.Equal(readAt) || !q.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("quota %+v", q)
	}
	// Binary units: half a GiB of artifacts accrues 0.5 GB-h an hour, and
	// Packages at 6 GB-h a day 0.25 an hour.
	if q.IncludedGBHours != 363.28125 || q.UsedGBHours != 100 || q.ActionsGBHours != 90 || q.PackagesGBHours != 10 ||
		q.RateGBHoursPerHour != 0.75 || q.EstimatedGBHours != 109 || q.ProjectedGBHours != 435.25 {
		t.Fatalf("quota %+v", q)
	}

	q = ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 100}, model.GiB, 500, [2]int{70, 90})
	// (363.28125 - 112) / 1 = 251.28125 hours after the read.
	want := readAt.Add(251*time.Hour + 16*time.Minute + 52*time.Second + 500*time.Millisecond)
	if q.RunsOutAt == nil || !q.RunsOutAt.Equal(want) {
		t.Fatalf("runs out at %v, want %v", q.RunsOutAt, want)
	}
	for _, tc := range []struct {
		name  string
		b     Billing
		bytes int64
	}{
		{"zero rate", Billing{Actions: 100}, 0},
		{"estimate already over", Billing{Actions: 360}, model.GiB},
		{"after the reset", Billing{Actions: 100}, model.GiB / 1024},
		{"too slow to fit a duration", Billing{Actions: 100}, 1},
	} {
		tc.b.ReadAt = readAt
		if q := ComputeQuota(tc.b, tc.bytes, 500, [2]int{70, 90}); q.RunsOutAt != nil {
			t.Errorf("%s: runs out at %v", tc.name, q.RunsOutAt)
		}
	}
}

func TestQuotaLevel(t *testing.T) {
	readAt := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		b     Billing
		bytes int64
		warn  [2]int
		want  string
	}{
		{"billed at the allowance", Billing{Actions: 363.28125}, 0, [2]int{70, 90}, "exhausted"},
		{"estimate past 90%", Billing{Actions: 320}, model.GiB, [2]int{70, 90}, "critical"},
		{"estimate past 70%", Billing{Actions: 250}, model.GiB, [2]int{70, 90}, "warn"},
		{"own thresholds", Billing{Actions: 200}, 0, [2]int{50, 60}, "warn"},
		{"projected past the allowance", Billing{Actions: 100}, model.GiB, [2]int{70, 90}, "projected"},
		{"none", Billing{Actions: 10}, 0, [2]int{70, 90}, ""},
	} {
		tc.b.ReadAt = readAt
		if got := ComputeQuota(tc.b, tc.bytes, 500, tc.warn).Level; got != tc.want {
			t.Errorf("%s: level %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAlertFrom(t *testing.T) {
	if AlertFrom(nil) != nil {
		t.Fatal("alert without a quota")
	}
	readAt := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	q := ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 373.3}, 0, 500, [2]int{70, 90})
	a := AlertFrom(&q)
	if a.Level != "exhausted" || a.Month != "2026-10" || a.UsedGBHours != 373.3 || a.IncludedGBHours != 363.28125 ||
		a.UsedPct != 103 || a.RunsOutAt != nil || !a.ResetsAt.Equal(q.ResetsAt) {
		t.Fatalf("alert %+v", a)
	}
	q = ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 100}, model.GiB, 500, [2]int{70, 90})
	if a := AlertFrom(&q); a.RunsOutAt == nil || !a.RunsOutAt.Equal(*q.RunsOutAt) || a.RunsOutAt == q.RunsOutAt {
		t.Fatalf("runs out %v", a.RunsOutAt)
	}
}
