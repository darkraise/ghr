package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkraise/ghr/internal/github"
)

type dotnet struct{ e *Env }

func newDotnet(e *Env) Installer { return dotnet{e} }

type dotnetChannel struct {
	Channel   string `json:"channel-version"`
	LatestSDK string `json:"latest-sdk"`
	Phase     string `json:"support-phase"`
	Type      string `json:"release-type"`
}

func (d dotnet) root() string { return filepath.Join(d.e.Root, "dotnet") }

func (d dotnet) sdkDir(ver string) string { return filepath.Join(d.root(), "sdk", ver) }

func (d dotnet) hasSDK(ver string) bool {
	return validFolder(ver) && exists(filepath.Join(d.sdkDir(ver), "dotnet.dll"))
}

// channels are the supported ones: support-phase active or maintenance.
func (d dotnet) channels(ctx context.Context) ([]dotnetChannel, error) {
	b, err := d.e.Get(ctx, d.e.Sources.DotnetIndex)
	if err != nil {
		return nil, fmt.Errorf(".NET versions: %w", err)
	}
	var r struct {
		Index []dotnetChannel `json:"releases-index"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf(".NET versions: %w", err)
	}
	var out []dotnetChannel
	for _, c := range r.Index {
		if c.Phase == "active" || c.Phase == "maintenance" {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d dotnet) Available(ctx context.Context) ([]Choice, error) {
	cs, err := d.channels(ctx)
	if err != nil {
		return nil, err
	}
	specs := make([]string, 0, len(cs))
	by := map[string]dotnetChannel{}
	for _, c := range cs {
		specs = append(specs, c.Channel)
		by[c.Channel] = c
	}
	newestFirst(specs)
	out := []Choice{}
	for _, s := range specs {
		out = append(out, Choice{Spec: s, Version: by[s].LatestSDK, LTS: by[s].Type == "lts"})
	}
	return out, nil
}

// Resolve takes a channel ("8.0", its latest SDK) or a full SDK version.
func (d dotnet) Resolve(ctx context.Context, spec string) (Release, error) {
	rel := func(v string) Release {
		return Release{Tool: "dotnet", Version: v, Folder: v, URL: d.e.Sources.DotnetScript}
	}
	if v, ok := github.ParseVersion(spec); ok && len(v) == 3 {
		return rel(spec), nil
	}
	cs, err := d.channels(ctx)
	if err != nil {
		return Release{}, err
	}
	for _, c := range cs {
		if c.Channel == spec {
			return rel(c.LatestSDK), nil
		}
	}
	return Release{}, fmt.Errorf("no supported .NET channel %q; use a channel such as 8.0 or a full SDK version", spec)
}

// Install runs dotnet-install.sh with --skip-non-versioned-files, as
// setup-dotnet does: the first install still writes the dotnet host, and
// later ones never overwrite it while a job may be running it.
func (d dotnet) Install(ctx context.Context, rel Release, progress func(string)) error {
	e := d.e
	if !validFolder(rel.Version) {
		return fmt.Errorf("invalid SDK version %q", rel.Version)
	}
	if d.hasSDK(rel.Version) {
		return ErrAlreadyInstalled
	}
	existed := exists(d.sdkDir(rel.Version))
	fail := func(err error) error {
		if !existed {
			os.RemoveAll(d.sdkDir(rel.Version))
		}
		return err
	}
	op, err := e.opDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(op)
	progress("downloading")
	if err := e.Fetch(ctx, rel.URL, filepath.Join(op, "dotnet-install.sh")); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := e.chownTree(ctx, op); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	if err := e.mkdirOwned(ctx, d.root()); err != nil {
		return err
	}
	progress("running dotnet-install.sh")
	if _, err := e.runAs(ctx, op, []string{"HOME=" + e.Home}, "dotnet-install.sh",
		"--version", rel.Version, "--install-dir", d.root(), "--skip-non-versioned-files"); err != nil {
		return fail(fmt.Errorf("dotnet-install.sh: %w", err))
	}
	if !d.hasSDK(rel.Version) {
		return fail(fmt.Errorf("dotnet-install.sh finished without SDK %s", rel.Version))
	}
	return nil
}

func (d dotnet) Installed() ([]Installed, error) {
	es, err := os.ReadDir(filepath.Join(d.root(), "sdk"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, en := range es {
		if v, ok := github.ParseVersion(en.Name()); !ok || len(v) != 3 || !en.IsDir() || !d.hasSDK(en.Name()) {
			continue
		}
		fi, err := os.Stat(d.sdkDir(en.Name()))
		if err != nil {
			continue
		}
		out = append(out, Installed{Tool: "dotnet", Version: en.Name(), Arch: arch, Path: d.sdkDir(en.Name()), InstalledAt: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return looseCompare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// dotnetMajorDirs hold one folder per version, named <major>.<…>, that the
// last SDK of a major leaves behind.
var dotnetMajorDirs = []string{"shared/*", "packs/*", "host/fxr", "templates", "sdk-manifests", "metadata/workloads", "library-packs"}

// Remove deletes one SDK. When it was the last SDK of its major, that
// major's runtimes and packs go too; the dotnet host stays.
func (d dotnet) Remove(version string) error {
	if v, ok := github.ParseVersion(version); !ok || len(v) != 3 || !d.hasSDK(version) {
		return ErrNotInstalled
	}
	if err := os.RemoveAll(d.sdkDir(version)); err != nil {
		return err
	}
	major := version[:strings.IndexByte(version, '.')+1]
	others, err := filepath.Glob(filepath.Join(d.root(), "sdk", major+"*", "dotnet.dll"))
	if err != nil || len(others) > 0 {
		return err
	}
	var errs []error
	for _, pattern := range dotnetMajorDirs {
		matches, err := filepath.Glob(filepath.Join(d.root(), filepath.FromSlash(pattern), major+"*"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
