// Package web holds the browser UI's build output, embedded into the ghr
// binary.
package web

import "embed"

// Dist is web/dist: the Vite build, or only .gitkeep in a build without the UI.
//
//go:embed all:dist
var Dist embed.FS
