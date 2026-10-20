package model

import "time"

// ArtifactKeyInfo describes a stored access key without its secret; ID is
// already shortened.
type ArtifactKeyInfo struct {
	ID    string    `json:"id"`
	SetAt time.Time `json:"set_at"`
}

// ArtifactStore is the self-hosted artifact storage snapshot of
// GET /artifact-store.
type ArtifactStore struct {
	Configured    bool                `json:"configured"`
	Endpoint      string              `json:"endpoint,omitempty"`
	Bucket        string              `json:"bucket,omitempty"`
	Region        string              `json:"region,omitempty"`
	AdminKey      *ArtifactKeyInfo    `json:"admin_key,omitempty"`
	JobKey        *ArtifactKeyInfo    `json:"job_key,omitempty"`
	CheckedAt     *time.Time          `json:"checked_at,omitempty"`
	Error         string              `json:"error,omitempty"`
	UsedBytes     int64               `json:"used_bytes"`
	Count         int                 `json:"count"`
	CapacityBytes int64               `json:"capacity_bytes,omitempty"`
	OtherBytes    int64               `json:"other_bytes,omitempty"`
	Repos         []ArtifactStoreRepo `json:"repos"`
	Operation     *GitHubOperation    `json:"operation,omitempty"`
	Deletions     []Deletion          `json:"deletions"`
}

// ArtifactStoreRepo is one repository's row. State is "set", "missing",
// "stale", "foreign", "public", "error" or "off".
type ArtifactStoreRepo struct {
	Name          string              `json:"name"`
	Private       bool                `json:"private"`
	Enabled       bool                `json:"enabled"`
	State         string              `json:"state"`
	StateError    string              `json:"state_error,omitempty"`
	Cleanup       string              `json:"cleanup"`
	CleanupSource string              `json:"cleanup_source"`
	Bytes         int64               `json:"bytes"`
	Count         int                 `json:"count"`
	Items         []ArtifactStoreItem `json:"items"`
}

// ArtifactStoreItem is one stored artifact object.
type ArtifactStoreItem struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	RunID      int64      `json:"run_id"`
	Attempt    int        `json:"attempt"`
	Workflow   string     `json:"workflow,omitempty"`
	Job        string     `json:"job,omitempty"`
	Branch     string     `json:"branch,omitempty"`
	SHA        string     `json:"sha,omitempty"`
	Bytes      int64      `json:"bytes"`
	Files      int        `json:"files"`
	Digest     string     `json:"digest,omitempty"`
	UploadedAt time.Time  `json:"uploaded_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// ArtifactStoreSummary is the self-hosted storage line of GET /status.
type ArtifactStoreSummary struct {
	UsedBytes     int64  `json:"used_bytes"`
	CapacityBytes int64  `json:"capacity_bytes,omitempty"`
	ReposOn       int    `json:"repos_on"`
	Error         string `json:"error,omitempty"`
}

type ArtifactStoreCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type ArtifactStoreTest struct {
	Checks []ArtifactStoreCheck `json:"checks"`
}

// ArtifactKeyRequest replaces one key; Which is "admin" or "job".
type ArtifactKeyRequest struct {
	Which  string `json:"which"`
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

type ArtifactRepoRequest struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Cleanup *string `json:"cleanup,omitempty"`
}

type ArtifactDeleteRequest struct {
	Keys []string `json:"keys"`
}

// ArtifactLinkRequest asks for a share link; TTL is "1h", "24h" or "7d".
type ArtifactLinkRequest struct {
	Key string `json:"key"`
	TTL string `json:"ttl"`
}

type ArtifactLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
