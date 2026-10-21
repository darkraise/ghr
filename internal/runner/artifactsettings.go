package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/darkraise/ghr/internal/sched"
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
	// A rewrite keeps the owner of the file it replaces, the runner user: a
	// file of root's would be unreadable to the job.
	if fi, err := root.Lstat(ArtifactSettingsFile); err == nil {
		if uid, gid, ok := fileOwner(fi); ok {
			if err := root.Lchown(artifactSettingsTemp, uid, gid); err != nil {
				return err
			}
		}
	}
	return root.Rename(artifactSettingsTemp, ArtifactSettingsFile)
}

func openSettingsTemp(root *os.Root, name string) (io.WriteCloser, error) {
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// syncArtifactSettings keeps the settings files of runners that have no job
// yet in step with what their repositories get now: a file is removed when
// its repository no longer gets one and rewritten when the settings changed,
// such as for a new job key. A runner without a file gets none here, and a
// busy runner keeps its file, which its job already started with.
func (m *Manager) syncArtifactSettings() {
	type runner struct{ id, repo string }
	var rs []runner
	m.mu.Lock()
	for _, i := range m.insts {
		if (i.State == sched.Starting || i.State == sched.Idle) && !i.finishing && !i.stopping {
			rs = append(rs, runner{i.ID, i.Repo})
		}
	}
	m.mu.Unlock()
	slices.SortFunc(rs, func(a, b runner) int { return strings.Compare(a.id, b.id) })
	for _, r := range rs {
		done, failed := m.syncArtifactSettingsOf(r.id, r.repo)
		if done != "" {
			m.Events.Add("info", r.repo, "%s", done)
		}
		m.mu.Lock()
		i, ok := m.insts[r.id]
		report := ok && failed != "" && failed != i.settingsErr
		if ok {
			i.settingsErr = failed
		}
		m.mu.Unlock()
		if report {
			m.Events.Add("warn", r.repo, "%s", failed)
		}
	}
}

// syncArtifactSettingsOf removes or rewrites runner id's settings file and
// returns the event text for what it did or for what failed; both are empty
// when there is no file or it is already right.
func (m *Manager) syncArtifactSettingsOf(id, repo string) (done, failed string) {
	dir := m.instanceDir(id)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", ""
	}
	defer root.Close()
	// Any answer but an entry's, such as a link out of the directory, means
	// there is no file of ghr's to keep in step.
	if _, err := root.Lstat(ArtifactSettingsFile); err != nil {
		return "", ""
	}
	m.mu.Lock()
	_, known := m.visibilityLocked(repo)
	m.mu.Unlock()
	// The manager keeps visibility in memory only, so after a restart it is
	// unread until the repository's first poll; an adopted runner's file
	// waits for that answer rather than being removed.
	if !known {
		return "", ""
	}
	b, ok := m.artifactSettingsFor(repo)
	if !ok {
		if err := root.Remove(ArtifactSettingsFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Sprintf("cannot remove the artifact store settings from %s: %v; its next job may still use self-hosted storage", id, err)
		}
		return fmt.Sprintf("artifact store settings removed from %s: its repository no longer uses self-hosted storage", id), ""
	}
	if have, err := readArtifactSettings(root); err == nil && bytes.Equal(have, b) {
		return "", ""
	}
	if err := m.writeArtifactSettings(dir, b); err != nil {
		return "", fmt.Sprintf("cannot update the artifact store settings of %s: %v", id, err)
	}
	return fmt.Sprintf("artifact store settings of %s updated", id), ""
}

// readArtifactSettings reads the settings file through root with the checks
// readJobFile makes: the job can put a link, a FIFO or a device in its place,
// and a FIFO would block the tick.
func readArtifactSettings(root *os.Root) ([]byte, error) {
	fi, err := root.Lstat(ArtifactSettingsFile)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	f, err := root.OpenFile(ArtifactSettingsFile, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	return io.ReadAll(io.LimitReader(f, maxJobFile))
}
