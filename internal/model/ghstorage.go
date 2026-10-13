package model

import "time"

// GiB is the binary gigabyte GitHub bills storage in.
const GiB = 1 << 30

// GitHubStorage is the GitHub artifact storage snapshot, served by
// GET /github-storage.
type GitHubStorage struct {
	CheckedAt  *time.Time   `json:"checked_at,omitempty"`
	Quota      *GitHubQuota `json:"quota,omitempty"`
	QuotaError string       `json:"quota_error,omitempty"`
	// Error is the last failed listing of the token's repositories; Repos
	// keeps the last complete list.
	Error     string           `json:"error,omitempty"`
	Repos     []RepoStorage    `json:"repos"`
	Operation *GitHubOperation `json:"operation,omitempty"`
	Deletions []Deletion       `json:"deletions"`
}

// GitHubQuota is the month's storage against the plan's allowance, in
// GB-hours.
type GitHubQuota struct {
	Month                string     `json:"month"`
	ReadAt               time.Time  `json:"read_at"`
	IncludedGBHours      float64    `json:"included_gb_hours"`
	UsedGBHours          float64    `json:"used_gb_hours"`
	ActionsGBHours       float64    `json:"actions_gb_hours"`
	PackagesGBHours      float64    `json:"packages_gb_hours"`
	RateGBHoursPerHour   float64    `json:"rate_gb_hours_per_hour"`
	EstimatedGBHours     float64    `json:"estimated_gb_hours"`
	OverAllowanceGBHours *float64   `json:"over_allowance_gb_hours,omitempty"`
	ProjectedGBHours     float64    `json:"projected_gb_hours"`
	RunsOutAt            *time.Time `json:"runs_out_at,omitempty"`
	// Level is "", "projected", "warn", "critical" or "exhausted".
	Level    string    `json:"level"`
	ResetsAt time.Time `json:"resets_at"`
}

type RepoStorage struct {
	Name          string `json:"name"`
	Visibility    string `json:"visibility"`
	Covered       bool   `json:"covered"`
	ArtifactCount int    `json:"artifact_count"`
	ArtifactBytes int64  `json:"artifact_bytes"`
	CacheCount    int    `json:"cache_count"`
	CacheBytes    int64  `json:"cache_bytes"`
	// RetentionDays and RetentionMaxDays are 0 while unknown.
	RetentionDays    int `json:"retention_days"`
	RetentionMaxDays int `json:"retention_max_days"`
	// Cleanup is "off" or an age such as "2d"; CleanupSource is "global",
	// "override" or "none".
	Cleanup       string         `json:"cleanup"`
	CleanupSource string         `json:"cleanup_source"`
	Artifacts     []ArtifactItem `json:"artifacts"`
	Caches        []CacheItem    `json:"caches"`
	Error         string         `json:"error,omitempty"`
}

type ArtifactItem struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Bytes     int64      `json:"bytes"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RunID     int64      `json:"run_id"`
	Branch    string     `json:"branch"`
}

type CacheItem struct {
	ID         int64     `json:"id"`
	Key        string    `json:"key"`
	Ref        string    `json:"ref"`
	Bytes      int64     `json:"bytes"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// GitHubOperation is a bulk delete or retention change running in the
// background. Kind is "delete-artifacts", "delete-caches" or
// "set-retention"; Repo is empty for retention across repositories.
type GitHubOperation struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`
	Repo       string             `json:"repo,omitempty"`
	Total      int                `json:"total"`
	Done       int                `json:"done"`
	Bytes      int64              `json:"bytes"`
	Failed     []OperationFailure `json:"failed"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
}

type OperationFailure struct {
	Repo  string `json:"repo"`
	ID    int64  `json:"id,omitempty"`
	Error string `json:"error"`
}

// Deletion is one artifact or cache ghr deleted. Kind is "artifact" or
// "cache"; By is "cleanup" or "manual".
type Deletion struct {
	At    time.Time `json:"at"`
	Repo  string    `json:"repo"`
	Kind  string    `json:"kind"`
	Name  string    `json:"name"`
	Bytes int64     `json:"bytes"`
	Age   string    `json:"age"`
	By    string    `json:"by"`
}

// StorageAlert is the quota's level in GET /status.
type StorageAlert struct {
	Level           string     `json:"level"`
	Month           string     `json:"month"`
	UsedGBHours     float64    `json:"used_gb_hours"`
	IncludedGBHours float64    `json:"included_gb_hours"`
	UsedPct         int        `json:"used_pct"`
	RunsOutAt       *time.Time `json:"runs_out_at,omitempty"`
	ResetsAt        time.Time  `json:"resets_at"`
}

// GitHubStoragePatch changes the github_storage block. CleanupMaxAge ""
// turns the cleanup off; a nil Repos value removes that override.
type GitHubStoragePatch struct {
	IncludedMB    *int               `json:"included_mb,omitempty"`
	WarnPct       *[]int             `json:"warn_pct,omitempty"`
	CleanupMaxAge *string            `json:"cleanup_max_age,omitempty"`
	Repos         map[string]*string `json:"repos,omitempty"`
}

// StoragePreview is what a cleanup age would delete in one repository.
type StoragePreview struct {
	Repo  string `json:"repo"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

type DeleteArtifactsRequest struct {
	IDs       []int64 `json:"ids,omitempty"`
	OlderThan string  `json:"older_than,omitempty"`
}

type DeleteCachesRequest struct {
	IDs []int64 `json:"ids,omitempty"`
	All bool    `json:"all,omitempty"`
}

type RetentionRequest struct {
	Days int `json:"days"`
}

type OperationStarted struct {
	ID string `json:"id"`
}
