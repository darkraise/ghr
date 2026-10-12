// Package model holds the types shared by the daemon, the control API and its clients.
package model

import "time"

type Status struct {
	Now time.Time `json:"now"`
	// Epoch changes on every daemon start; event sequence numbers restart with it.
	Epoch          string `json:"epoch"`
	Mode           string `json:"mode"`
	GlobalMax      int    `json:"global_max"`
	Degraded       bool   `json:"degraded"`
	DegradedReason string `json:"degraded_reason,omitempty"`
	RateRemaining  int    `json:"rate_remaining"`
	// RateLimit is the token's hourly request limit, absent until GitHub has answered.
	RateLimit int `json:"rate_limit,omitempty"`
	DiskPct   int `json:"disk_pct"`
	// DiskUsedBytes and DiskTotalBytes are 0 until the first measurement.
	DiskUsedBytes  int64 `json:"disk_used_bytes"`
	DiskTotalBytes int64 `json:"disk_total_bytes"`
	// DiskRoot is Docker's data root path, empty until measured in bytes.
	DiskRoot     string            `json:"disk_root,omitempty"`
	Repos        []RepoStatus      `json:"repos"`
	Instances    []InstanceStatus  `json:"instances"`
	Maintenance  MaintenanceStatus `json:"maintenance"`
	RunnerUpdate RunnerUpdate      `json:"runner_update"`
	// WebSetupRequired is true while the web UI listens with no password set.
	WebSetupRequired bool `json:"web_setup_required"`
	// Unconfigured is true while ghr has no owner or token; an older daemon
	// omits it, which reads as configured.
	Unconfigured bool `json:"unconfigured,omitempty"`
	// SetupPending is true until first-run setup is finished.
	SetupPending bool          `json:"setup_pending,omitempty"`
	Restart      RestartStatus `json:"restart"`
	// WebListenError is set when the configured web.listen failed to bind at
	// start.
	WebListenError *WebListenError `json:"web_listen_error,omitempty"`
}

type RestartStatus struct {
	// Pending names the settings whose saved value differs from the one the
	// running daemon applied: "web.listen", "web.hosts".
	Pending []string `json:"pending,omitempty"`
	// BlockedBy names the daemon work that refuses a restart now.
	BlockedBy []string `json:"blocked_by,omitempty"`
	// Available is false when this process is not ghr.service's main process.
	Available bool `json:"available"`
	// WebListen is the listen value the running web listener serves; empty
	// when none runs.
	WebListen string `json:"web_listen,omitempty"`
}

type WebListenError struct {
	Address   string `json:"address"`
	Reason    string `json:"reason"`
	ServingOn string `json:"serving_on,omitempty"`
}

// SetupState is GET /setup. Configured means the full API serves; Starting
// means owner and token are saved and the manager is still starting.
type SetupState struct {
	Configured        bool   `json:"configured"`
	Starting          bool   `json:"starting"`
	SetupPending      bool   `json:"setup_pending"`
	ToolchainsPending bool   `json:"toolchains_pending"`
	Owner             string `json:"owner"`
	WebListen         string `json:"web_listen"`
}

type SetupGitHubRequest struct {
	Owner string `json:"owner"`
	Token string `json:"token"`
}

type SetupFinishRequest struct {
	Toolchains string `json:"toolchains"`
}

// RunnerUpdate is the GitHub Actions runner's version state, served inside
// GET /status. Each field is absent while unknown.
type RunnerUpdate struct {
	Installed       string     `json:"installed,omitempty"`
	Latest          string     `json:"latest,omitempty"`
	LatestPublished *time.Time `json:"latest_published,omitempty"`
	// Deadline is set only while a newer runner than Installed exists.
	Deadline     *time.Time `json:"deadline,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
	CheckError   string     `json:"check_error,omitempty"`
	Queued       bool       `json:"queued,omitempty"`
	QueuedAt     *time.Time `json:"queued_at,omitempty"`
	Running      bool       `json:"running,omitempty"`
	LastOutcome  string     `json:"last_outcome,omitempty"` // "ok", "failed", "current"
	LastError    string     `json:"last_error,omitempty"`
	LastFinished *time.Time `json:"last_finished,omitempty"`
}

type RepoStatus struct {
	Name     string `json:"name"`
	Paused   bool   `json:"paused"`
	Removing bool   `json:"removing,omitempty"`
	Max      int    `json:"max"` // 0 = unlimited
	Active   int    `json:"active"`
	Queued   int    `json:"queued"`
	// OldestQueuedAt is when the repo's longest-waiting matching job was queued,
	// as of the scheduler's last successful poll.
	OldestQueuedAt *time.Time    `json:"oldest_queued_at,omitempty"`
	Error          string        `json:"error,omitempty"`
	LastJob        *HistoryEntry `json:"last_job,omitempty"`
	// Public is the visibility GitHub last reported for the repository; nil
	// until GitHub has answered.
	Public *bool `json:"public,omitempty"`
}

type InstanceStatus struct {
	ID         string    `json:"id"`
	Repo       string    `json:"repo"`
	RunnerName string    `json:"runner_name"`
	State      string    `json:"state"`
	Since      time.Time `json:"since"`
	Job        *JobInfo  `json:"job,omitempty"`
	// Stopping is true from a person's stop request until the unit has exited.
	Stopping bool `json:"stopping,omitempty"`
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

// Failed reports whether a job conclusion counts as a failure: anything that
// is not a success, a cancellation, a skip or "unknown", ghr's own conclusion
// for a job whose result it never learned.
func Failed(conclusion string) bool {
	switch conclusion {
	case "success", "cancelled", "skipped", "unknown":
		return false
	}
	return true
}

type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	// A pending step has neither time, and a running step only StartedAt.
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
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
	Max                 OptInt    `json:"max,omitzero"`
	Warm                OptInt    `json:"warm,omitzero"`
	Labels              *[]string `json:"labels,omitempty"`
	CleanupNamePrefixes *[]string `json:"cleanup_name_prefixes,omitempty"`
	Paused              *bool     `json:"paused,omitempty"`
	AllowPublic         *bool     `json:"allow_public,omitempty"`
}

type RunnerLimitsPatch struct {
	MemoryMax *string `json:"memory_max,omitempty"`
	CPUQuota  *string `json:"cpu_quota,omitempty"`
}

// WebPatch changes the web listener's settings; they apply at the next start.
type WebPatch struct {
	Listen *string   `json:"listen,omitempty"`
	Hosts  *[]string `json:"hosts,omitempty"`
}

type ConfigPatch struct {
	Mode             *string              `json:"mode,omitempty"`
	GlobalMax        *int                 `json:"global_max,omitempty"`
	PollInterval     *string              `json:"poll_interval,omitempty"`
	StartTimeout     *string              `json:"start_timeout,omitempty"`
	IdleTimeout      *string              `json:"idle_timeout,omitempty"`
	HistoryRetention *string              `json:"history_retention,omitempty"`
	DiskHighWater    *int                 `json:"disk_high_water,omitempty"`
	BuildCacheKeep   *string              `json:"build_cache_keep,omitempty"`
	Labels           *[]string            `json:"labels,omitempty"`
	RunnerLimits     *RunnerLimitsPatch   `json:"runner_limits,omitempty"`
	Web              *WebPatch            `json:"web,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
}

type AddRepoRequest struct {
	Name        string   `json:"name"`
	Max         *int     `json:"max,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	AllowPublic bool     `json:"allow_public"`
}

// WatchRequest is POST /watch.
type WatchRequest struct {
	Name string `json:"name"`
}

// MaintenanceStatus is the daemon's prune state, served inside GET /status.
type MaintenanceStatus struct {
	Running      bool       `json:"running"`
	LastStarted  *time.Time `json:"last_started,omitempty"`
	LastFinished *time.Time `json:"last_finished,omitempty"`
	LastOutcome  string     `json:"last_outcome,omitempty"` // "ok", "errors", "interrupted"
}

// TokenStatus is the GitHub token's state, served by GET /token.
type TokenStatus struct {
	State         string     `json:"state"` // "ok", "rejected", "unverified"
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	RateRemaining *int       `json:"rate_remaining,omitempty"`
	RateLimit     *int       `json:"rate_limit,omitempty"`
	RateReset     *time.Time `json:"rate_reset,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// Registration is one runner registered on a repo, served by GET /repos/{name}/registrations.
type Registration struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"` // "online", "offline"
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels"`
	GHR    bool     `json:"ghr"`
}

// LabelGroup is one distinct runs-on label set seen in a repo's runs, part of LabelCheck.
type LabelGroup struct {
	Labels   []string  `json:"labels"` // lower-cased, sorted
	Jobs     []string  `json:"jobs"`   // up to three "workflow / job" names
	More     int       `json:"more"`   // further distinct names not listed
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

// LabelCheck is a repo's workflow label observations, served by GET /repos/{name}/label-check.
type LabelCheck struct {
	State     string       `json:"state"` // "not_checked", "checking", "done"
	CheckedAt *time.Time   `json:"checked_at,omitempty"`
	Partial   bool         `json:"partial"`
	Error     string       `json:"error,omitempty"` // why the last scan stopped early, other than its deadline
	Groups    []LabelGroup `json:"groups"`
}

// MetricSample is one point of the daemon's load history, part of Metrics.
type MetricSample struct {
	At     time.Time `json:"at"`
	Live   int       `json:"live"`
	Queued int       `json:"queued"`
	CPU    *float64  `json:"cpu,omitempty"`
	Mem    *int64    `json:"mem,omitempty"`
}

// MetricRollup is one UTC hour of metric samples. CPUAvg and MemAvg are
// absent when no sample in the hour carried them.
type MetricRollup struct {
	At        time.Time `json:"at"`
	Samples   int       `json:"samples"`
	QueuedMax int       `json:"queued_max"`
	CPUAvg    *float64  `json:"cpu_avg,omitempty"`
	MemAvg    *int64    `json:"mem_avg,omitempty"`
}

// Metrics is the host load history and gauges, served by GET /metrics.
type Metrics struct {
	Samples  []MetricSample `json:"samples"`
	CPU      *float64       `json:"cpu,omitempty"`
	MemUsed  *int64         `json:"mem_used,omitempty"`
	MemTotal *int64         `json:"mem_total,omitempty"`
	DiskPct  int            `json:"disk_pct"`
}

// AvailableRepo is a repository of the configured owner that the token can
// access, served by GET /repos/available.
type AvailableRepo struct {
	Name       string `json:"name"`
	Private    bool   `json:"private"`
	Configured bool   `json:"configured"`
	Watched    bool   `json:"watched"`
}

// Actions is GET /actions: the recent workflow runs of every configured and
// watched repository, whoever ran them.
type Actions struct {
	FetchedAt time.Time     `json:"fetched_at"`
	Runs      []ActionsRun  `json:"runs"`
	Repos     []ActionsRepo `json:"repos"`
}

type ActionsRun struct {
	Repo       string    `json:"repo"`
	ID         int64     `json:"id"`
	RunNumber  int64     `json:"run_number"`
	Workflow   string    `json:"workflow"`
	Title      string    `json:"title"`
	Branch     string    `json:"branch"`
	Event      string    `json:"event"`
	Actor      string    `json:"actor"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
	GHR        bool      `json:"ghr"`
	Watched    bool      `json:"watched"`
}

// ActionsRepo is one covered repository; Error is set when its runs could
// not be read, and RetryAt when GitHub calls are paused.
type ActionsRepo struct {
	Repo    string     `json:"repo"`
	Watched bool       `json:"watched"`
	Error   string     `json:"error,omitempty"`
	RetryAt *time.Time `json:"retry_at,omitempty"`
}

// RunDetail is GET /actions/{repo}/runs/{id}: one attempt of a workflow run.
// Attempts is the run's latest attempt number. Graph is nil when GraphNote
// says why the page draws a timeline instead.
type RunDetail struct {
	Repo         string        `json:"repo"`
	Watched      bool          `json:"watched"`
	ID           int64         `json:"id"`
	RunNumber    int64         `json:"run_number"`
	Attempt      int           `json:"attempt"`
	Attempts     int           `json:"attempts"`
	Workflow     string        `json:"workflow"`
	Title        string        `json:"title"`
	Branch       string        `json:"branch"`
	Event        string        `json:"event"`
	Actor        string        `json:"actor"`
	Status       string        `json:"status"`
	Conclusion   string        `json:"conclusion"`
	HeadSHA      string        `json:"head_sha"`
	Path         string        `json:"path"`
	PullRequests []int         `json:"pull_requests"`
	StartedAt    time.Time     `json:"started_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	HTMLURL      string        `json:"html_url"`
	Jobs         []RunJob      `json:"jobs"`
	Graph        *RunGraph     `json:"graph,omitempty"`
	GraphNote    string        `json:"graph_note,omitempty"`
	Artifacts    []RunArtifact `json:"artifacts"`
	FetchedAt    time.Time     `json:"fetched_at"`
}

// RunJob is one job of a run attempt; GHR is set when ghr's runners ran a
// job of that name in the run.
type RunJob struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	RunnerName  string     `json:"runner_name"`
	Labels      []string   `json:"labels"`
	HTMLURL     string     `json:"html_url"`
	GHR         bool       `json:"ghr"`
	Steps       []Step     `json:"steps"`
}

type RunGraph struct {
	Nodes []GraphNode `json:"nodes"`
}

// GraphNode is one workflow job: Key is its YAML ID, Column the length of
// its longest needs chain, JobIDs its jobs in the jobs API's order. Group is
// set for a matrix or a reusable-workflow caller.
type GraphNode struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Column int      `json:"column"`
	Needs  []string `json:"needs"`
	JobIDs []int64  `json:"job_ids"`
	Group  bool     `json:"group"`
}

type RunArtifact struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	SizeBytes int64      `json:"size_bytes"`
	Expired   bool       `json:"expired"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// RunAnnotations is GET /actions/{repo}/runs/{id}/annotations. Omitted counts,
// per job ID, the annotations beyond the per-job cap; Unread counts finished
// jobs whose logs were not ready within the wait.
type RunAnnotations struct {
	Items       []Annotation  `json:"items"`
	Omitted     map[int64]int `json:"omitted"`
	Unread      int           `json:"unread"`
	ExpiredLogs int           `json:"expired_logs"`
	TooLarge    int           `json:"too_large"`
	Failed      int           `json:"failed"`
}

// Annotation is an error, warning or notice line of a job log; Line counts
// within its step.
type Annotation struct {
	Level    string `json:"level"`
	JobID    int64  `json:"job_id"`
	JobName  string `json:"job_name"`
	Step     int    `json:"step"`
	StepName string `json:"step_name"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// JobLog is GET /actions/{repo}/jobs/{jobId}/log: a finished job's log split
// into its steps. NoLog marks a job that never ran, so GitHub has no log for
// it.
type JobLog struct {
	JobID    int64     `json:"job_id"`
	TooLarge bool      `json:"too_large"`
	Expired  bool      `json:"expired"`
	NoLog    bool      `json:"no_log"`
	Steps    []LogStep `json:"steps"`
}

type LogStep struct {
	Number int       `json:"number"`
	Name   string    `json:"name"`
	Lines  []LogLine `json:"lines"`
}

// LogLine is one log line. Kind is "", group, endgroup, error, warning,
// notice, command or debug; Text has the ##[kind] prefix removed and keeps
// ANSI escapes.
type LogLine struct {
	N    int    `json:"n"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// DownloadURL is GET /actions/{repo}/artifacts/{id}/download: GitHub's
// signed address, valid for about a minute.
type DownloadURL struct {
	URL string `json:"url"`
}

// Activity is GET /activity: what the runners did over a window, as lanes of
// runs (1h, 3h) or time buckets (24h, 7d, 30d), with every time in TZ.
type Activity struct {
	Window   string    `json:"window"`
	TZ       string    `json:"tz"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Capacity *int      `json:"capacity"` // global_max in queue mode, null in all mode
	// HistoryFrom is the oldest finish time history_retention keeps.
	HistoryFrom time.Time        `json:"history_from"`
	Lanes       []ActivityLane   `json:"lanes"`
	Buckets     []ActivityBucket `json:"buckets"`
	Waiting     []ActivityPoint  `json:"waiting"`
	CPU         []ActivityCPU    `json:"cpu"`
	Repos       []ActivityRepo   `json:"repos"`
}

// ActivityLane holds runs one after another; it is not a fixed runner slot.
type ActivityLane struct {
	Runs []ActivityRun `json:"runs"`
}

// ActivityRun is one runner instance's time in the window.
type ActivityRun struct {
	InstanceID string            `json:"instance_id"`
	Repo       string            `json:"repo"`
	Workflow   string            `json:"workflow,omitempty"`
	Job        string            `json:"job,omitempty"`
	RunNumber  string            `json:"run_number,omitempty"`
	Segments   []ActivitySegment `json:"segments"`
	HTMLURL    string            `json:"html_url,omitempty"`
	// Stopping is the live instance's flag; a run built from a finished job
	// never carries it.
	Stopping bool `json:"stopping,omitempty"`
}

// ActivitySegment is a stretch of one state; To is null while it lasts.
type ActivitySegment struct {
	State string     `json:"state"` // starting | warm | running | finishing | succeeded | failed | cancelled | skipped | unknown
	From  time.Time  `json:"from"`
	To    *time.Time `json:"to"`
}

type ActivityBucket struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	BusyMinutes float64   `json:"busy_minutes"`
	BusyPct     *float64  `json:"busy_pct"`
	Succeeded   int       `json:"succeeded"`
	Failed      int       `json:"failed"`
	Cancelled   int       `json:"cancelled"`
	Unknown     int       `json:"unknown"`
	WaitingMax  *int      `json:"waiting_max"`
	CPUAvg      *float64  `json:"cpu_avg"`
}

type ActivityPoint struct {
	At    time.Time `json:"at"`
	Value int       `json:"value"`
}

type ActivityCPU struct {
	At  time.Time `json:"at"`
	CPU *float64  `json:"cpu"`
	Mem *int64    `json:"mem"`
}

type ActivityRepo struct {
	Repo  string         `json:"repo"`
	Hours []ActivityHour `json:"hours"`
	// Week counts the runs that finished in the 7 days ending now, whatever
	// the window.
	Week ActivityWeek `json:"week"`
}

type ActivityWeek struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Unknown   int `json:"unknown"`
}

type ActivityHour struct {
	Start     time.Time `json:"start"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
	Unknown   int       `json:"unknown"`
}
