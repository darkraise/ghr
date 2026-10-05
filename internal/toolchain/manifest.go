package toolchain

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/darkraise/ghr/internal/github"
)

type manifestFile struct {
	Filename        string `json:"filename"`
	Arch            string `json:"arch"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	DownloadURL     string `json:"download_url"`
}

type manifestEntry struct {
	Version string         `json:"version"`
	Stable  bool           `json:"stable"`
	Files   []manifestFile `json:"files"`
}

// manifestTool is a tool whose versions come from an actions/*-versions
// manifest and which installs into the tool-cache layout.
type manifestTool struct {
	e      *Env
	tool   string // the name ghr uses
	dir    string // the tool cache folder the action reads
	label  string
	url    string
	fileOK func(manifestFile) bool
}

// files maps each stable version to its first file fileOK accepts.
func (m manifestTool) files(ctx context.Context) (map[string]manifestFile, error) {
	b, err := m.e.Get(ctx, m.url)
	if err != nil {
		return nil, fmt.Errorf("%s versions: %w", m.label, err)
	}
	var es []manifestEntry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("%s versions: %w", m.label, err)
	}
	out := map[string]manifestFile{}
	for _, en := range es {
		if _, ok := github.ParseVersion(en.Version); !ok || !en.Stable {
			continue
		}
		for _, f := range en.Files {
			if m.fileOK(f) {
				out[en.Version] = f
				break
			}
		}
	}
	return out, nil
}

func versions(fs map[string]manifestFile) []string {
	vs := make([]string, 0, len(fs))
	for v := range fs {
		vs = append(vs, v)
	}
	newestFirst(vs)
	return vs
}

func (m manifestTool) Available(ctx context.Context) ([]Choice, error) {
	fs, err := m.files(ctx)
	if err != nil {
		return nil, err
	}
	out := []Choice{}
	for _, v := range versions(fs) {
		out = append(out, Choice{Spec: v, Version: v})
	}
	return out, nil
}

func (m manifestTool) pick(ctx context.Context, spec string) (string, manifestFile, error) {
	fs, err := m.files(ctx)
	if err != nil {
		return "", manifestFile{}, err
	}
	v, ok := pick(versions(fs), spec)
	if !ok {
		return "", manifestFile{}, fmt.Errorf("no stable %s release matches %q", m.label, spec)
	}
	return v, fs[v], nil
}

func (m manifestTool) Installed() ([]Installed, error) {
	return m.e.listLayout(m.tool, m.dir, identity)
}

func (m manifestTool) Remove(version string) error { return m.e.removeLayout(m.dir, version) }

func linuxX64(f manifestFile) bool { return f.Platform == "linux" && f.Arch == arch }
