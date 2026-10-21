package runner

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// ArtifactSettingsFile is where a runner finds ghr's artifact store settings,
// relative to its instance directory. It is under the runner's work folder,
// where the actions look for it.
const ArtifactSettingsFile = "_work/_ghr/artifact-store.json"

const artifactSettingsTemp = "_work/_ghr/.artifact-store.json.tmp"

// artifactSettingsFor returns the settings file for a runner of repo, or false
// when it gets none. Only a repository the manager last saw private gets one:
// allow_public lets a public repository's runners start, and its jobs can come
// from anyone's pull request.
func (m *Manager) artifactSettingsFor(repo string) ([]byte, bool) {
	if m.ArtifactSettings == nil {
		return nil, false
	}
	m.mu.Lock()
	public, known := m.visibilityLocked(repo)
	m.mu.Unlock()
	if public || !known {
		return nil, false
	}
	return m.ArtifactSettings(repo)
}

// writeArtifactSettings replaces the settings file in the instance directory
// dir with b, through a temporary file renamed onto it, so a runner never
// reads half a file. The runner user owns dir and a job could have left links
// in it, so every step goes through an os.Root of dir, which follows no link
// out of it. On failure the temporary file is removed and the settings file is
// left as it was.
func (m *Manager) writeArtifactSettings(dir string, b []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, d := range []struct {
		name string
		mode fs.FileMode
	}{{"_work", 0o755}, {"_work/_ghr", 0o700}} {
		if err := root.Mkdir(d.name, d.mode); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		// Mkdir's mode passes through the umask, and a directory that was
		// already there keeps whatever mode it had.
		if err := root.Chmod(d.name, d.mode); err != nil {
			return err
		}
	}
	if err := root.Remove(artifactSettingsTemp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.placeSettings(root, b); err != nil {
		root.Remove(artifactSettingsTemp)
		return err
	}
	return nil
}

// placeSettings writes b to the temporary file and renames it onto the
// settings file.
func (m *Manager) placeSettings(root *os.Root, b []byte) error {
	open := m.openSettingsTemp
	if open == nil {
		open = openSettingsTemp
	}
	f, err := open(root, artifactSettingsTemp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(artifactSettingsTemp, ArtifactSettingsFile)
}

func openSettingsTemp(root *os.Root, name string) (io.WriteCloser, error) {
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}
