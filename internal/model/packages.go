package model

import "time"

// PackageInfo is one apt package as GET /packages and GET /packages/search
// show it. Version is empty when the package is not installed.
type PackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Candidate   string `json:"candidate,omitempty"`
	Description string `json:"description,omitempty"`
	Installed   bool   `json:"installed"`
	Upgradable  bool   `json:"upgradable,omitempty"`
	Bytes       int64  `json:"bytes"`
	Managed     bool   `json:"managed,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Packages is served by GET /packages: the names ghr manages, the base set
// setup.sh installs, and installed packages ghr could adopt.
type Packages struct {
	IndexUpdatedAt *time.Time    `json:"index_updated_at,omitempty"`
	Managed        []PackageInfo `json:"managed"`
	Base           []PackageInfo `json:"base"`
	Detected       []PackageInfo `json:"detected"`
}

// AddPackagesRequest is the body of POST /packages.
type AddPackagesRequest struct {
	Names []string `json:"names"`
}

// AddPackagesResult says which names POST /packages queued for adopting
// (already installed) and which for installing.
type AddPackagesResult struct {
	Adopt   []string `json:"adopt"`
	Install []string `json:"install"`
}

// UpgradePackagesRequest is the body of POST /packages/upgrade: the named
// packages, or every managed package when All is set.
type UpgradePackagesRequest struct {
	Names []string `json:"names,omitempty"`
	All   bool     `json:"all,omitempty"`
}
