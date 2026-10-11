package runner

import (
	"os"
	"path/filepath"
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
