package config

import (
	"fmt"
	"time"
)

// Background task names: the keys of the jobs block.
const (
	JobGhrUpdateCheck    = "ghr_update_check"
	JobRunnerUpdateCheck = "runner_update_check"
	JobPrune             = "prune"
	JobStorageMeasure    = "storage_measure"
	JobPackageIndex      = "package_index"
)

// JobNames lists the background tasks in the order the UI and the CLI show them.
var JobNames = []string{JobGhrUpdateCheck, JobRunnerUpdateCheck, JobPrune, JobStorageMeasure, JobPackageIndex}

// What turning a task off costs. Validate warns with these words and the web
// UI's Jobs page shows the same ones, so change both together.
const (
	RunnerCheckOffReason = "GitHub stops assigning jobs to a runner past its forced-update deadline, and ghr no longer learns of new runner releases or deadlines"
	PruneOffReason       = "history and logs older than history_retention are kept, and a full disk is no longer pruned automatically"
)

// Job is one background task's schedule.
type Job struct {
	Enabled bool     `yaml:"enabled" json:"enabled"`
	Every   Duration `yaml:"every" json:"every"`
}

// Jobs schedules ghr's background tasks.
type Jobs struct {
	GhrUpdateCheck    Job `yaml:"ghr_update_check" json:"ghr_update_check"`
	RunnerUpdateCheck Job `yaml:"runner_update_check" json:"runner_update_check"`
	Prune             Job `yaml:"prune" json:"prune"`
	StorageMeasure    Job `yaml:"storage_measure" json:"storage_measure"`
	PackageIndex      Job `yaml:"package_index" json:"package_index"`
}

// IsZero lets yaml's omitempty leave out a schedule that is the defaults:
// releases before the jobs block reject the key, and a rollback must still
// start on a config nobody customised.
func (j Jobs) IsZero() bool { return j == DefaultJobs() }

// DefaultJobs is the schedule of a config without a jobs block: every task
// on, at the intervals ghr used before the block existed.
func DefaultJobs() Jobs {
	daily := Job{Enabled: true, Every: Duration(24 * time.Hour)}
	return Jobs{
		GhrUpdateCheck:    daily,
		RunnerUpdateCheck: daily,
		Prune:             daily,
		StorageMeasure:    Job{Enabled: true, Every: Duration(30 * time.Minute)},
		PackageIndex:      daily,
	}
}

// Ref returns the named task's schedule for changing it, or nil for an unknown name.
func (j *Jobs) Ref(name string) *Job {
	switch name {
	case JobGhrUpdateCheck:
		return &j.GhrUpdateCheck
	case JobRunnerUpdateCheck:
		return &j.RunnerUpdateCheck
	case JobPrune:
		return &j.Prune
	case JobStorageMeasure:
		return &j.StorageMeasure
	case JobPackageIndex:
		return &j.PackageIndex
	}
	return nil
}

// Get returns the named task's schedule; ok is false for an unknown name.
func (j Jobs) Get(name string) (Job, bool) {
	if p := j.Ref(name); p != nil {
		return *p, true
	}
	return Job{}, false
}

// minJobEvery is each task's shortest interval and the text its error shows.
var minJobEvery = map[string]struct {
	d    time.Duration
	text string
}{
	JobGhrUpdateCheck:    {time.Hour, "1h"},
	JobRunnerUpdateCheck: {time.Hour, "1h"},
	JobPrune:             {time.Hour, "1h"},
	JobStorageMeasure:    {5 * time.Minute, "5m"},
	JobPackageIndex:      {time.Hour, "1h"},
}

// validate returns the jobs block's errors and warnings.
func (j Jobs) validate() (errs, warnings []string) {
	for _, name := range JobNames {
		job, _ := j.Get(name)
		if m := minJobEvery[name]; job.Every.D() < m.d {
			errs = append(errs, fmt.Sprintf("jobs.%s.every must be >= %s", name, m.text))
		}
	}
	if !j.RunnerUpdateCheck.Enabled {
		warnings = append(warnings, "jobs."+JobRunnerUpdateCheck+" is off: "+RunnerCheckOffReason)
	}
	if !j.Prune.Enabled {
		warnings = append(warnings, "jobs."+JobPrune+" is off: "+PruneOffReason)
	}
	return errs, warnings
}
