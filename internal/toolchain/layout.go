package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func marker(dir string) string { return dir + ".complete" }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (e *Env) versionDir(toolDir, folder string) string {
	return filepath.Join(e.Root, toolDir, folder, arch)
}

// has reports whether folder is a complete install: its x64 folder and its
// marker both exist.
func (e *Env) has(toolDir, folder string) bool {
	dir := e.versionDir(toolDir, folder)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	_, err = os.Stat(marker(dir))
	return err == nil
}

// listLayout lists the complete installs under <Root>/<toolDir>, newest
// first, showing each folder through show.
func (e *Env) listLayout(tool, toolDir string, show func(folder string) string) ([]Installed, error) {
	es, err := os.ReadDir(filepath.Join(e.Root, toolDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, v := range es {
		if !v.IsDir() || !e.has(toolDir, v.Name()) {
			continue
		}
		dir := e.versionDir(toolDir, v.Name())
		fi, err := os.Stat(marker(dir))
		if err != nil {
			continue
		}
		out = append(out, Installed{Tool: tool, Version: show(v.Name()), Arch: arch, Path: dir, InstalledAt: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return looseCompare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// removeLayout deletes the marker first, so a job starting meanwhile no
// longer finds the version, then the version itself.
func (e *Env) removeLayout(toolDir, folder string) error {
	if !validFolder(folder) || !e.has(toolDir, folder) {
		return ErrNotInstalled
	}
	r, err := os.OpenRoot(e.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	dir := filepath.Join(toolDir, folder, arch)
	if err := r.Remove(marker(dir)); err != nil {
		return err
	}
	if err := r.RemoveAll(dir); err != nil {
		return err
	}
	// Fails, and keeps the folder, while it holds anything else.
	r.Remove(filepath.Dir(dir))
	return nil
}

// operation is one operation's directory under <Root>/.tmp. The runner user
// owns <Root> and can swap .tmp for a link at any moment, so path is only for
// the commands that need one; everything ghr does there itself goes through
// the handles, which stay on the directories ghr made.
type operation struct {
	*os.Root // the operation directory
	tmp      *os.Root
	id       string
	path     string
	rel      string // path, relative to <Root>
}

// Close removes the operation directory.
func (o *operation) Close() error {
	o.Root.Close()
	err := o.tmp.RemoveAll(o.id)
	o.tmp.Close()
	return err
}

// opDir makes a fresh directory for one operation under <Root>/.tmp. Both
// are root-owned; .tmp allows only traversal, so jobs cannot reach into a
// download.
func (e *Env) opDir() (*operation, error) {
	tmp := filepath.Join(e.Root, ".tmp")
	// A job can plant a symlink or its own directory here; a foreign directory
	// would let the job swap an operation directory for a link.
	if fi, err := os.Lstat(tmp); err == nil && (!fi.IsDir() || !ownedByMe(fi)) {
		if err := os.RemoveAll(tmp); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(tmp, 0o711); err != nil {
		return nil, err
	}
	// The job can swap .tmp again after that look, so the rest goes through
	// one handle on the directory and never through its path.
	r, err := openOwned(tmp)
	if err != nil {
		return nil, err
	}
	if err := r.Chmod(".", 0o711); err != nil {
		r.Close()
		return nil, err
	}
	id := e.OpID()
	if err := r.Mkdir(id, 0o700); err != nil {
		r.Close()
		return nil, err
	}
	op, err := r.OpenRoot(id)
	if err != nil {
		r.RemoveAll(id)
		r.Close()
		return nil, err
	}
	return &operation{Root: op, tmp: r, id: id, path: filepath.Join(tmp, id), rel: filepath.Join(".tmp", id)}, nil
}

// openOwned opens the directory at path, and only one that belongs to this
// process. os.OpenRoot follows a link at path, so what it opened is compared
// with what path itself is.
func openOwned(path string) (*os.Root, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	if err == nil {
		var named os.FileInfo
		named, err = os.Lstat(path)
		if err == nil && (!os.SameFile(named, opened) || !ownedByMe(opened)) {
			err = fmt.Errorf("%s is a link or belongs to another user", path)
		}
	}
	if err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// CleanTmp removes what unfinished operations left in <Root>/.tmp. Version
// folders without a marker are left alone: one may be a job's own setup-*
// download in progress.
func (e *Env) CleanTmp() error { return os.RemoveAll(filepath.Join(e.Root, ".tmp")) }

// chownTree gives rel, inside r, and everything below it to the runner user,
// following no link.
func (e *Env) chownTree(r *os.Root, rel string) error {
	return fs.WalkDir(r.FS(), filepath.ToSlash(rel), func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return e.Chown(r, filepath.FromSlash(p))
	})
}

// mkdirOwned creates rel inside r, when missing, owned by the runner user, so
// jobs' setup-* steps can still add versions beside ghr's.
func (e *Env) mkdirOwned(r *os.Root, rel string) error {
	if err := r.Mkdir(rel, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	if err := e.Chown(r, rel); err != nil {
		// Left root-owned, a job's setup-* step could not add to it.
		r.Remove(rel)
		return err
	}
	return nil
}

func fileSHA256(r *os.Root, name string) (string, error) {
	f, err := r.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verify(r *os.Root, name, want string) error {
	if want == "" {
		return nil
	}
	got, err := fileSHA256(r, name)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum: SHA-256 mismatch: the source says %s, the download is %s", want, got)
	}
	return nil
}

// singleRoot returns dir's lone subdirectory when dir, inside r, holds
// nothing else, as the actions cache an archive's single top folder.
func singleRoot(r *os.Root, dir string) (string, error) {
	f, err := r.Open(dir)
	if err != nil {
		return "", err
	}
	es, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return "", err
	}
	if len(es) == 1 && es[0].IsDir() {
		return filepath.Join(dir, es[0].Name()), nil
	}
	return dir, nil
}

// unfinishedFor is how long an x64 folder without a marker counts as a
// running job's own setup-* install rather than a killed one.
const unfinishedFor = time.Hour

// checkUnfinished returns ErrInstallInProgress when rel's x64 folder dir,
// inside r and known to have no marker, was written within unfinishedFor.
func checkUnfinished(r *os.Root, dir string, rel Release) error {
	fi, err := r.Lstat(dir)
	if err != nil || time.Since(fi.ModTime()) >= unfinishedFor {
		return nil
	}
	return fmt.Errorf("%s %s: %w", rel.Tool, rel.Version, ErrInstallInProgress)
}

// removeIncomplete deletes an x64 folder inside r and its marker.
func removeIncomplete(r *os.Root, dir string) {
	r.Remove(marker(dir))
	r.RemoveAll(dir)
}

// dropFailed undoes a failed install: the x64 folder, its marker, and the
// version folder when nothing else is left in it.
func dropFailed(r *os.Root, dir string) {
	removeIncomplete(r, dir)
	r.Remove(filepath.Dir(dir))
}

// fetch is the one way an installer downloads: Allow judges rawURL against
// the installer's own origin, and Fetch then gets that same string.
func (e *Env) fetch(ctx context.Context, from Origin, rawURL, dst string) error {
	if err := e.Allow(rawURL, from); err != nil {
		return err
	}
	if err := e.Fetch(ctx, rawURL, dst); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}

// installArchive installs rel's archive, which must come from from, at
// <Root>/<toolDir>/<rel.Folder>/x64, the layout @actions/tool-cache find()
// reads, writing the marker last.
func (e *Env) installArchive(ctx context.Context, toolDir string, from Origin, rel Release, progress func(string)) error {
	if !validFolder(rel.Folder) {
		return fmt.Errorf("invalid version folder %q", rel.Folder)
	}
	if e.has(toolDir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	op, err := e.opDir()
	if err != nil {
		return err
	}
	defer op.Close()
	progress("downloading")
	if err := e.fetch(ctx, from, rel.URL, filepath.Join(op.path, "archive.tar.gz")); err != nil {
		return err
	}
	if err := verify(op.Root, "archive.tar.gz", rel.SHA256); err != nil {
		return err
	}
	progress("extracting")
	if err := op.Mkdir("x", 0o755); err != nil {
		return err
	}
	if err := e.Extract(ctx, filepath.Join(op.path, "archive.tar.gz"), filepath.Join(op.path, "x")); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	src, err := singleRoot(op.Root, "x")
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := e.chownTree(op.Root, src); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	// The runner user owns the tool cache and can swap any folder in it for a
	// link at any moment, so every step from here goes through a root, which
	// never follows a link out of the cache.
	r, err := os.OpenRoot(e.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	verRel := filepath.Join(toolDir, rel.Folder)
	for _, d := range []string{toolDir, verRel} {
		if err := e.mkdirOwned(r, d); err != nil {
			return err
		}
	}
	target := filepath.Join(verRel, arch)
	if err := ctx.Err(); err != nil {
		// A marker-less target may be a job's own download, not ours to drop.
		r.Remove(verRel)
		return err
	}
	// A job's setup-* step may have completed this version while ghr downloaded.
	if e.has(toolDir, rel.Folder) {
		return ErrAlreadyInstalled
	}
	if err := checkUnfinished(r, target, rel); err != nil {
		return err
	}
	// An older target without a marker is a killed install; replace it, as
	// @actions/tool-cache's _createToolPath does.
	removeIncomplete(r, target)
	// Through the tool cache's root, a .tmp swapped meanwhile can only lead
	// to the runner user's own files.
	if err := r.Rename(filepath.Join(op.rel, src), target); err != nil {
		dropFailed(r, target)
		return err
	}
	// O_EXCL, so a link planted at the marker's path is never followed.
	f, err := r.OpenFile(marker(target), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		dropFailed(r, target)
		return err
	}
	f.Close()
	if err := e.Chown(r, marker(target)); err != nil {
		dropFailed(r, target)
		return fmt.Errorf("chown: %w", err)
	}
	return nil
}

// runAs runs script, relative to dir, as the runner user with env added and
// AGENT_TOOLSDIRECTORY removed (python-versions' setup.sh prefers it over
// RUNNER_TOOL_CACHE).
func (e *Env) runAs(ctx context.Context, dir string, env []string, script string, args ...string) ([]byte, error) {
	a := []string{"-u", e.User, "--", "env", "-u", "AGENT_TOOLSDIRECTORY"}
	a = append(a, env...)
	a = append(a, "bash", "-c", `cd -- "$0" && exec bash "$@"`, dir, script)
	a = append(a, args...)
	return e.Run(ctx, "runuser", a...)
}
