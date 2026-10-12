package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type golang struct{ manifestTool }

func newGo(e *Env) Installer {
	return golang{manifestTool{e: e, tool: "go", dir: "go", label: "Go", url: e.Sources.GoManifest, fileOK: linuxX64}}
}

// Resolve picks the version from the go-versions manifest, which setup-go
// resolves "stable" from, and downloads the official go.dev archive, which
// publishes a SHA-256.
func (g golang) Resolve(ctx context.Context, spec string) (Release, error) {
	v, _, err := g.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	names := []string{goArchive(v)}
	// The manifest writes every release with three parts; go.dev names the
	// first release of a minor before 1.21 without its ".0".
	if short, ok := strings.CutSuffix(v, ".0"); ok && strings.Count(v, ".") == 2 {
		names = append(names, goArchive(short))
	}
	name, sum, err := g.sha256(ctx, names)
	if err != nil {
		return Release{}, err
	}
	sv := makeSemver(v)
	return Release{Tool: "go", Version: sv, Folder: sv, URL: g.e.Sources.GoDownload + name, SHA256: sum}, nil
}

func goArchive(v string) string { return "go" + v + ".linux-amd64.tar.gz" }

// sha256 returns the first of names go.dev lists a SHA-256 for, with it.
func (g golang) sha256(ctx context.Context, names []string) (string, string, error) {
	b, err := g.e.Get(ctx, g.e.Sources.GoReleases)
	if err != nil {
		return "", "", fmt.Errorf("go.dev releases: %w", err)
	}
	var rs []struct {
		Files []struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return "", "", fmt.Errorf("go.dev releases: %w", err)
	}
	for _, name := range names {
		for _, r := range rs {
			for _, f := range r.Files {
				if f.Filename == name && f.SHA256 != "" {
					return name, f.SHA256, nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("go.dev lists no SHA-256 for %s", names[0])
}

func (g golang) Install(ctx context.Context, rel Release, progress func(string)) error {
	return g.e.installArchive(ctx, g.dir, Origin{Dir: g.e.Sources.GoDownload}, rel, progress)
}
