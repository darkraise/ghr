// Package fsutil writes files that hold secrets.
package fsutil

import (
	"os"
	"path/filepath"
)

// WritePrivate replaces path with data, readable by the owner only: a 0600
// temporary file in the same directory, synced, then renamed over path.
func WritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Syncing the directory makes the rename survive a power loss. Some
	// platforms, Windows among them, cannot sync a directory, so it is best
	// effort.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
