// Package model holds the types shared by the daemon, the control API and its clients.
package model

import "time"

type Status struct {
	Now time.Time `json:"now"`
	// Epoch changes on every daemon start; event sequence numbers restart with it.
	Epoch          string           `json:"epoch"`
	Mode           string           `json:"mode"`
	GlobalMax      int              `json:"global_max"`
	Degraded       bool             `json:"degraded"`
	DegradedReason string           `json:"degraded_reason,omitempty"`
	RateRemaining  int              `json:"rate_remaining"`
	DiskPct        int              `json:"disk_pct"`
	Repos          []RepoStatus     `json:"repos"`
	Instances      []InstanceStatus `json:"instances"`
}

type RepoStatus struct {
	Name     string        `json:"name"`
	Paused   bool          `json:"paused"`
	Removing bool          `json:"removing,omitempty"`
	Max      int           `json:"max"` // 0 = unlimited
	Active   int           `json:"active"`
	Queued   int           `json:"queued"`
	Error    string        `json:"error,omitempty"`
	LastJob  *HistoryEntry `json:"last_job,omitempty"`
}

type InstanceStatus struct {
	ID         string    `json:"id"`
	Repo       string    `json:"repo"`
	RunnerName string    `json:"runner_name"`
	State      string    `json:"state"`
	Since      time.Time `json:"since"`
	Job        *JobInfo  `json:"job,omitempty"`
}

type JobInfo struct {
	RunID     int64     `json:"run_id"`
	RunNumber string    `json:"run_number"`
	Workflow  string    `json:"workflow"`
	Name      string    `json:"name"`
	HTMLURL   string    `json:"html_url,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

type Event struct {
	Seq   int64     `json:"seq"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // info | ok | warn | error
	Repo  string    `json:"repo,omitempty"`
	Msg   string    `json:"msg"`
}

type HistoryEntry struct {
	ID         string    `json:"id"`
	Repo       string    `json:"repo"`
	RunID      int64     `json:"run_id"`
	RunNumber  string    `json:"run_number"`
	Workflow   string    `json:"workflow"`
	JobName    string    `json:"job_name"`
	Conclusion string    `json:"conclusion"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	HTMLURL    string    `json:"html_url,omitempty"`
}

type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// LogChunk is the next part of a runner's _diag logs. Next is an opaque cursor
// holding one offset per log file; pass it back to continue, "" to start over.
type LogChunk struct {
	Data string `json:"data"`
	Next string `json:"next"`
}

// Container is one container of a runner instance's compose projects.
type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Project string `json:"project"`
}

type RepoPatch struct {
	Max                 *int      `json:"max,omitempty"`
	Warm                *int      `json:"warm,omitempty"`
	Labels              *[]string `json:"labels,omitempty"`
	CleanupNamePrefixes *[]string `json:"cleanup_name_prefixes,omitempty"`
	Paused              *bool     `json:"paused,omitempty"`
}

type ConfigPatch struct {
	Mode         *string              `json:"mode,omitempty"`
	GlobalMax    *int                 `json:"global_max,omitempty"`
	StartTimeout *string              `json:"start_timeout,omitempty"`
	IdleTimeout  *string              `json:"idle_timeout,omitempty"`
	Repos        map[string]RepoPatch `json:"repos,omitempty"`
}

type AddRepoRequest struct {
	Name        string   `json:"name"`
	Max         *int     `json:"max,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	AllowPublic bool     `json:"allow_public"`
}
