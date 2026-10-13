// Package ghstorage keeps the snapshot of the owner's GitHub artifact
// storage, runs the age-based cleanup and the manual deletes and retention
// changes, and computes the month's quota and warning level.
package ghstorage

import (
	"math"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// LagHours is how far GitHub's usage figure trails: it recalculates every
// 6 to 12 hours.
const LagHours = 12

// Billing is one billing read. PackagesDaily is the Packages GB-hours of the
// last full UTC day before ReadAt; Over is the summary's net Actions and
// Packages storage, nil when the summary could not be read.
type Billing struct {
	Month                            string
	ReadAt                           time.Time
	Actions, Packages, PackagesDaily float64
	Over                             *float64
}

// MonthBounds is the start of t's UTC month and the start of the next.
func MonthBounds(t time.Time) (start, resets time.Time) {
	t = t.UTC()
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func SumUsage(items []github.UsageItem, readAt time.Time) (actions, packages, packagesDaily float64) {
	r := readAt.UTC()
	today := time.Date(r.Year(), r.Month(), r.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	for _, it := range items {
		switch it.SKU {
		case github.SKUActionsStorage:
			actions += it.Quantity
		case github.SKUPackagesStorage:
			packages += it.Quantity
			if !it.Date.Before(yesterday) && it.Date.Before(today) {
				packagesDaily += it.Quantity
			}
		}
	}
	return actions, packages, packagesDaily
}

// ComputeQuota measures the month against the allowance from one billing
// read and the artifact bytes stored now. Everything is relative to
// b.ReadAt, so the result changes only when a pass changes its inputs.
func ComputeQuota(b Billing, artifactBytes int64, includedMB int, warn [2]int) model.GitHubQuota {
	start, resets := MonthBounds(b.ReadAt)
	q := model.GitHubQuota{
		Month:              b.Month,
		ReadAt:             b.ReadAt,
		IncludedGBHours:    float64(includedMB) / 1024 * resets.Sub(start).Hours(),
		UsedGBHours:        b.Actions + b.Packages,
		ActionsGBHours:     b.Actions,
		PackagesGBHours:    b.Packages,
		RateGBHoursPerHour: float64(artifactBytes)/model.GiB + b.PackagesDaily/24,
		ResetsAt:           resets,
	}
	if b.Over != nil {
		v := *b.Over
		q.OverAllowanceGBHours = &v
	}
	left := resets.Sub(b.ReadAt).Hours()
	q.EstimatedGBHours = q.UsedGBHours + q.RateGBHoursPerHour*LagHours
	q.ProjectedGBHours = q.UsedGBHours + q.RateGBHoursPerHour*left
	// Comparing the hours with what is left first keeps a tiny rate from
	// overflowing time.Duration.
	if q.RateGBHoursPerHour > 0 && q.EstimatedGBHours < q.IncludedGBHours {
		if h := (q.IncludedGBHours - q.EstimatedGBHours) / q.RateGBHoursPerHour; h < left {
			at := b.ReadAt.Add(time.Duration(h * float64(time.Hour)))
			q.RunsOutAt = &at
		}
	}
	q.Level = level(q, warn)
	return q
}

// level compares the thresholds with the estimate, because the billed figure
// trails; exhausted uses what GitHub billed, because it promises failures.
func level(q model.GitHubQuota, warn [2]int) string {
	switch {
	case q.UsedGBHours >= q.IncludedGBHours, q.OverAllowanceGBHours != nil && *q.OverAllowanceGBHours > 0.001:
		return "exhausted"
	case q.EstimatedGBHours >= float64(warn[1])/100*q.IncludedGBHours:
		return "critical"
	case q.EstimatedGBHours >= float64(warn[0])/100*q.IncludedGBHours:
		return "warn"
	case q.ProjectedGBHours >= q.IncludedGBHours:
		return "projected"
	}
	return ""
}

// AlertFrom is the status alert for a quota, nil without one.
func AlertFrom(q *model.GitHubQuota) *model.StorageAlert {
	if q == nil {
		return nil
	}
	a := &model.StorageAlert{Level: q.Level, Month: q.Month, UsedGBHours: q.UsedGBHours, IncludedGBHours: q.IncludedGBHours, ResetsAt: q.ResetsAt}
	if q.IncludedGBHours > 0 {
		a.UsedPct = int(math.Round(q.UsedGBHours / q.IncludedGBHours * 100))
	}
	if q.RunsOutAt != nil {
		at := *q.RunsOutAt
		a.RunsOutAt = &at
	}
	return a
}
