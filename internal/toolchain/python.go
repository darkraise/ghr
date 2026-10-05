package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type python struct{ manifestTool }

func newPython(e *Env) Installer {
	return python{manifestTool{e: e, tool: "python", dir: "Python", label: "Python", url: e.Sources.PythonManifest,
		fileOK: func(f manifestFile) bool {
			return f.Platform == "linux" && f.PlatformVersion == "24.04" && f.Arch == arch
		}}}
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
	if err := e.Fetch(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
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
	if err := e.mkdirOwned(ctx, filepath.Join(e.Root, p.dir)); err != nil {
		return err
	}
	// setup.sh replaces the version folder, so a job's setup-python that
	// completed it meanwhile must not be overwritten.
	if e.has(p.dir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	progress("running setup.sh")
	target := e.versionDir(p.dir, rel.Folder)
	env := []string{"RUNNER_TOOL_CACHE=" + e.Root, "LD_LIBRARY_PATH=" + filepath.Join(out, "lib"), "HOME=" + e.Home}
	if _, err := e.runAs(ctx, out, env, "setup.sh"); err != nil {
		dropFailed(target)
		return fmt.Errorf("setup.sh: %w", err)
	}
	if !e.has(p.dir, rel.Folder) {
		dropFailed(target)
		return errors.New("setup.sh finished without writing the .complete marker")
	}
	return nil
}
