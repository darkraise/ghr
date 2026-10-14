package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The instance directory belongs to the runner user, so a job can rewrite
// anything in it. What root must trust about an instance is kept in
// Paths.Private instead, which only root can write.

func (m *Manager) privateMeta(id string) string {
	return filepath.Join(m.Paths.Private, id+".json")
}

func (m *Manager) privateProjects(id string) string {
	return filepath.Join(m.Paths.Private, id+".projects")
}

// removePrivate deletes an instance's private records.
func (m *Manager) removePrivate(id string) {
	os.Remove(m.privateMeta(id))
	os.Remove(m.privateProjects(id))
}

// writePrivateMeta records an instance's metadata where only root can write.
func (m *Manager) writePrivateMeta(meta Meta) error {
	if err := os.MkdirAll(m.Paths.Private, 0o700); err != nil {
		return err
	}
	return writeJSONAtomic(m.privateMeta(meta.ID), meta)
}

// readMeta returns the metadata root recorded for an instance. The copy in
// the instance's own directory is never read: its job can rewrite it.
func (m *Manager) readMeta(id string) (Meta, error) {
	var meta Meta
	err := readJSON(m.privateMeta(id), &meta)
	if err == nil && meta.ID != id {
		err = fmt.Errorf("%s names instance %q", m.privateMeta(id), meta.ID)
	}
	return meta, err
}

// sweepPrivate removes the private records of instances that are gone: no
// live instance and no instance directory.
func (m *Manager) sweepPrivate() {
	entries, err := os.ReadDir(m.Paths.Private)
	if err != nil {
		return
	}
	for _, e := range entries {
		id, _, _ := strings.Cut(e.Name(), ".")
		if !idRe.MatchString(id) {
			continue
		}
		m.mu.Lock()
		_, live := m.insts[id]
		m.mu.Unlock()
		if !live && !exists(m.instanceDir(id)) {
			os.Remove(filepath.Join(m.Paths.Private, e.Name()))
		}
	}
}
