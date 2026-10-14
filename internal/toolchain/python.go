package toolchain

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// pythonReleases is where every download_url of the python-versions manifest
// points.
const pythonReleases = "https://github.com/actions/python-versions/releases/download/"

type python struct{ manifestTool }

func newPython(e *Env) Installer {
	platform := e.PythonPlatform
	if platform == "" {
		platform = defaultPythonPlatform
	}
	return python{manifestTool{e: e, tool: "python", dir: "Python", label: "Python", url: e.Sources.PythonManifest,
		fileOK: func(f manifestFile) bool {
			return f.Platform == "linux" && f.PlatformVersion == platform && f.Arch == arch
		}}}
}

// Builds for a newer Ubuntu link a newer glibc, so an Ubuntu host takes its
// own release's. Every build targets Ubuntu, and 24.04's run on Debian 13.
const defaultPythonPlatform = "24.04"

func pythonPlatform(osRelease string) string {
	var id, version string
	for line := range strings.Lines(osRelease) {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = v
		case "VERSION_ID":
			version = v
		}
	}
	if id == "ubuntu" && version != "" {
		return version
	}
	return defaultPythonPlatform
}

func (p python) Resolve(ctx context.Context, spec string) (Release, error) {
	v, f, err := p.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	return Release{Tool: "python", Version: v, Folder: v, URL: f.DownloadURL}, nil
}

// Install runs the archive's own setup.sh, as setup-python does: the builds
// are shared-library builds whose rpath names the build machine's tool
// cache, so the script needs LD_LIBRARY_PATH to start python.
func (p python) Install(ctx context.Context, rel Release, progress func(string)) error {
	e := p.e
	if !validFolder(rel.Folder) {
		return fmt.Errorf("invalid version folder %q", rel.Folder)
	}
	if e.has(p.dir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	op, err := e.opDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(op)
	progress("downloading")
	archive := filepath.Join(op, "python.tar.gz")
	if err := e.fetch(ctx, Origin{Dir: pythonReleases}, rel.URL, archive); err != nil {
		return err
	}
	progress("extracting")
	out := filepath.Join(op, "x")
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	if err := e.Extract(ctx, archive, out); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := e.chownTree(ctx, op); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	// The runner user owns <Root>/Python, and setup.sh runs as them.
	r, err := os.OpenRoot(e.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := e.mkdirOwned(r, p.dir); err != nil {
		return err
	}
	// setup.sh replaces the version folder, so a job's setup-python that
	// completed it meanwhile must not be overwritten.
	if e.has(p.dir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	if err := checkUnfinished(r, filepath.Join(p.dir, rel.Folder, arch), rel); err != nil {
		return err
	}
	progress("running setup.sh")
	verRel := filepath.Join(p.dir, rel.Folder)
	// A version folder that is already there may be a job's own setup-python
	// at work.
	_, statErr := r.Lstat(verRel)
	existed := !errors.Is(statErr, fs.ErrNotExist)
	fail := func(err error) error {
		if !existed {
			r.RemoveAll(verRel)
		}
		return err
	}
	env := []string{"RUNNER_TOOL_CACHE=" + e.Root, "LD_LIBRARY_PATH=" + filepath.Join(out, "lib"), "HOME=" + e.Home}
	if _, err := e.runAs(ctx, out, env, "setup.sh"); err != nil {
		return fail(fmt.Errorf("setup.sh: %w", err))
	}
	if !e.has(p.dir, rel.Folder) {
		return fail(errors.New("setup.sh finished without writing the .complete marker"))
	}
	return nil
}
