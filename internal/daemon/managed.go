package daemon

import (
	"errors"
	"slices"

	"github.com/darkraise/ghr/internal/config"
)

// managedList is the storage service's view of config.yaml's packages list.
type managedList struct{ store *Store }

// errUnchanged aborts a Store.Update that would save an unchanged config.
var errUnchanged = errors.New("unchanged")

func (m managedList) List() []string { return slices.Clone(m.store.Config().Packages) }

func (m managedList) Add(name string) error {
	return m.change(func(c *config.Config) bool {
		if slices.Contains(c.Packages, name) {
			return false
		}
		c.Packages = append(c.Packages, name)
		return true
	})
}

func (m managedList) Drop(name string) error {
	return m.change(func(c *config.Config) bool {
		i := slices.Index(c.Packages, name)
		if i < 0 {
			return false
		}
		c.Packages = slices.Delete(c.Packages, i, i+1)
		return true
	})
}

// change saves config.yaml only when edit changed the list.
func (m managedList) change(edit func(c *config.Config) bool) error {
	_, err := m.store.Update(func(c *config.Config) error {
		if !edit(c) {
			return errUnchanged
		}
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return nil
	}
	return err
}
