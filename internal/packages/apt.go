package packages

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/darkraise/ghr/internal/system"
)

// CommandTimeout bounds one apt or dpkg command; an install of a large
// package on a slow mirror takes minutes.
const CommandTimeout = 30 * time.Minute

var errNotImplemented = errors.New("not implemented")

type apt struct {
	run      system.Runner
	lookPath func(string) (string, error)
}

var _ Manager = (*apt)(nil)

// New returns the Manager that runs apt and dpkg through run.
func New(run system.Runner) Manager {
	return &apt{run: run, lookPath: exec.LookPath}
}

func (a *apt) Available() bool {
	for _, tool := range []string{"apt-get", "apt-cache", "apt-mark", "dpkg", "dpkg-query"} {
		if _, err := a.lookPath(tool); err != nil {
			return false
		}
	}
	return true
}

func (a *apt) Info(ctx context.Context, names []string, candidates bool) ([]Info, error) {
	return nil, errNotImplemented
}

func (a *apt) Search(ctx context.Context, term string) ([]Info, error) {
	return nil, errNotImplemented
}

func (a *apt) Detected(ctx context.Context, exclude []string) ([]Info, error) {
	return nil, errNotImplemented
}

func (a *apt) Refresh(ctx context.Context) error { return errNotImplemented }

func (a *apt) UpgradesOthers(ctx context.Context, names []string) (bool, error) {
	return false, errNotImplemented
}

func (a *apt) Install(ctx context.Context, names []string) error { return errNotImplemented }

func (a *apt) Upgrade(ctx context.Context, names []string) error { return errNotImplemented }

func (a *apt) MarkManual(ctx context.Context, name string) error { return errNotImplemented }

func (a *apt) Remove(ctx context.Context, name string) error { return errNotImplemented }
