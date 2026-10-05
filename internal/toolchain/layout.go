package toolchain

import (
	"os"
	"path/filepath"
	"sort"
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
