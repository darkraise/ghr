package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	dir := e.versionDir(toolDir, folder)
	if err := os.Remove(marker(dir)); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Fails, and keeps the folder, while it holds anything else.
	os.Remove(filepath.Dir(dir))
	return nil
}

// opDir makes a fresh directory for one operation under <Root>/.tmp. Both
// are root-owned; .tmp allows only traversal, so jobs cannot reach into a
// download.
func (e *Env) opDir() (string, error) {
	tmp := filepath.Join(e.Root, ".tmp")
	if err := os.MkdirAll(tmp, 0o711); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o711); err != nil {
		return "", err
	}
	dir := filepath.Join(tmp, e.OpID())
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// CleanTmp removes what unfinished operations left in <Root>/.tmp. Version
// folders without a marker are left alone: one may be a job's own setup-*
// download in progress.
func (e *Env) CleanTmp() error { return os.RemoveAll(filepath.Join(e.Root, ".tmp")) }

func (e *Env) owner() string { return e.User + ":" + e.User }

func (e *Env) chownTree(ctx context.Context, path string) error {
	_, err := e.Run(ctx, "chown", "-R", "-h", e.owner(), path)
	return err
}

// mkdirOwned creates dir, when missing, owned by the runner user, so jobs'
// setup-* steps can still add versions beside ghr's.
func (e *Env) mkdirOwned(ctx context.Context, dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	_, err := e.Run(ctx, "chown", "-h", e.owner(), dir)
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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

func verify(path, want string) error {
	if want == "" {
		return nil
	}
	got, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum: SHA-256 mismatch: the source says %s, the download is %s", want, got)
	}
	return nil
}

// singleRoot returns dir's lone subdirectory when dir holds nothing else,
// as the actions cache an archive's single top folder.
func singleRoot(dir string) (string, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(es) == 1 && es[0].IsDir() {
		return filepath.Join(dir, es[0].Name()), nil
	}
	return dir, nil
}

// removeIncomplete deletes an x64 folder and its marker.
func removeIncomplete(dir string) {
	os.Remove(marker(dir))
	os.RemoveAll(dir)
}

// dropFailed undoes a failed install: the x64 folder, its marker, and the
// version folder when nothing else is left in it.
func dropFailed(dir string) {
	removeIncomplete(dir)
	os.Remove(filepath.Dir(dir))
}

// installArchive installs rel's archive at <Root>/<toolDir>/<rel.Folder>/x64,
// the layout @actions/tool-cache find() reads, writing the marker last.
func (e *Env) installArchive(ctx context.Context, toolDir string, rel Release, progress func(string)) error {
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
	defer os.RemoveAll(op)
	progress("downloading")
	archive := filepath.Join(op, "archive.tar.gz")
	if err := e.Fetch(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := verify(archive, rel.SHA256); err != nil {
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
	src, err := singleRoot(out)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := e.chownTree(ctx, src); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	toolPath := filepath.Join(e.Root, toolDir)
	verPath := filepath.Join(toolPath, rel.Folder)
	for _, d := range []string{toolPath, verPath} {
		if err := e.mkdirOwned(ctx, d); err != nil {
			return err
		}
	}
	target := filepath.Join(verPath, arch)
	if err := ctx.Err(); err != nil {
		dropFailed(target)
		return err
	}
	// A target without a marker is a killed install; replace it, as
	// @actions/tool-cache's _createToolPath does.
	removeIncomplete(target)
	if err := os.Rename(src, target); err != nil {
		dropFailed(target)
		return err
	}
	if err := os.WriteFile(marker(target), nil, 0o644); err != nil {
		dropFailed(target)
		return err
	}
	if _, err := e.Run(ctx, "chown", "-h", e.owner(), marker(target)); err != nil {
		dropFailed(target)
		return fmt.Errorf("chown: %w", err)
	}
	return nil
}
