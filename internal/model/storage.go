package model

import (
	"fmt"
	"time"
)

// BusyError refuses an action while runners have jobs.
type BusyError struct{ N int }

func (e BusyError) Error() string { return fmt.Sprintf("refused: %d jobs running", e.N) }

// Storage is what the runner LXC keeps on disk between jobs, served by GET /storage.
type Storage struct {
	Toolchains     []Toolchain    `json:"toolchains"`
	OtherToolCache []Folder       `json:"other_tool_cache"`
	PackageCaches  []PackageCache `json:"package_caches"`
	Docker         DockerDisk     `json:"docker"`
	MeasuredAt     *time.Time     `json:"measured_at,omitempty"`
	Measuring      bool           `json:"measuring"`
	MeasureError   string         `json:"measure_error,omitempty"`
	Operations     Operations     `json:"operations"`
	LastPrune      *LastPrune     `json:"last_prune"`
}

// Toolchain is one installed version in the tool cache.
type Toolchain struct {
	Tool        string    `json:"tool"`
	Version     string    `json:"version"`
	Arch        string    `json:"arch"`
	Path        string    `json:"path"`
	Bytes       int64     `json:"bytes"`
	InstalledAt time.Time `json:"installed_at"`
}

// Folder is a tool cache folder no installer owns, such as PyPy.
type Folder struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type PackageCache struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Paths       []string   `json:"paths"`
	Present     bool       `json:"present"`
	Bytes       int64      `json:"bytes"`
	Files       int64      `json:"files"`
	LastWritten *time.Time `json:"last_written,omitempty"`
}

type DockerDisk struct {
	Rows            []DockerRow      `json:"rows"`
	BuildCacheTypes []BuildCacheType `json:"build_cache_types"`
	DiskPct         int              `json:"disk_pct"`
}

type DockerRow struct {
	Type        string `json:"type"`
	Count       int    `json:"count"`
	Active      int    `json:"active"`
	Bytes       int64  `json:"bytes"`
	Reclaimable int64  `json:"reclaimable"`
}

type BuildCacheType struct {
	Type        string `json:"type"`
	Count       int    `json:"count"`
	Bytes       int64  `json:"bytes"`
	Reclaimable int64  `json:"reclaimable"`
}

type Operations struct {
	Current *Operation  `json:"current"`
	Queued  int         `json:"queued"`
	Recent  []Operation `json:"recent"` // newest first
}

// Operation is a queued toolchain install or removal, or a cache clear.
type Operation struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"` // install, remove, clear
	Target     string     `json:"target"`
	StartedAt  time.Time  `json:"started_at"`
	Progress   string     `json:"progress,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Outcome    string     `json:"outcome,omitempty"` // ok, failed, refused, interrupted, skipped
	Message    string     `json:"message,omitempty"`
}

// LastPrune is the newest manual or automatic prune; Outcome is empty while it runs.
type LastPrune struct {
	Trigger    string      `json:"trigger"` // auto, manual
	Scope      string      `json:"scope"`   // a prune scope, or auto
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
	Outcome    string      `json:"outcome,omitempty"` // ok, errors, interrupted
	Steps      []PruneStep `json:"steps"`
}

type PruneStep struct {
	Name  string `json:"name"`
	Freed int64  `json:"freed"`
	Error string `json:"error,omitempty"`
}

// ToolchainChoice is one entry GET /toolchains/available offers: Spec is
// what POST /toolchains takes, Version what a picker shows.
type ToolchainChoice struct {
	Spec    string `json:"spec"`
	Version string `json:"version"`
	LTS     bool   `json:"lts,omitempty"`
}

// InstallRequest is POST /toolchains: a tool and version, or a preset.
type InstallRequest struct {
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
	Preset  string `json:"preset,omitempty"`
}

// HumanBytes formats n in decimal units, as Docker does.
func HumanBytes(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB", "PB"}
	f, i := float64(n), 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
