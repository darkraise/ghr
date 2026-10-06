package storage

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
)

type fakeTools struct {
	mu        sync.Mutex
	root      string
	installed []toolchain.Installed
	other     []string
	tools     map[string]*fakeInstaller
	cleaned   int
}

func (f *fakeTools) Get(tool string) (toolchain.Installer, error) {
	if i, ok := f.tools[tool]; ok {
		return i, nil
	}
	return nil, fmt.Errorf("%w %q", toolchain.ErrUnknownTool, tool)
}

func (f *fakeTools) Available(ctx context.Context, tool string) ([]toolchain.Choice, error) {
	i, err := f.Get(tool)
	if err != nil {
		return nil, err
	}
	return i.Available(ctx)
}

func (f *fakeTools) Installed() ([]toolchain.Installed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.installed), nil
}

func (f *fakeTools) Other() ([]string, error) { return f.other, nil }
func (f *fakeTools) Root() string             { return f.root }

func (f *fakeTools) CleanTmp() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleaned++
	return nil
}

func (f *fakeTools) cleanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cleaned
}

// fakeInstaller resolves specs from a table. With gate set, Install waits for
// a send on gate or the end of its context; with started set, Install sends
// the version it is installing there first.
type fakeInstaller struct {
	mu       sync.Mutex
	resolve  map[string]string
	have     map[string]bool
	installs []string
	removed  []string
	gate     chan struct{}
	started  chan string
}

func newInstaller(resolve map[string]string) *fakeInstaller {
	return &fakeInstaller{resolve: resolve, have: map[string]bool{}}
}

func (f *fakeInstaller) Available(context.Context) ([]toolchain.Choice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []toolchain.Choice
	for spec, v := range f.resolve {
		out = append(out, toolchain.Choice{Spec: spec, Version: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out, nil
}

func (f *fakeInstaller) Resolve(_ context.Context, spec string) (toolchain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.resolve[spec]
	if !ok {
		return toolchain.Release{}, fmt.Errorf("no release matches %q", spec)
	}
	return toolchain.Release{Version: v, Folder: v}, nil
}

func (f *fakeInstaller) Install(ctx context.Context, rel toolchain.Release, progress func(string)) error {
	progress("downloading")
	if f.started != nil {
		f.started <- rel.Version
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.have[rel.Version] {
		return toolchain.ErrAlreadyInstalled
	}
	f.have[rel.Version] = true
	f.installs = append(f.installs, rel.Version)
	return nil
}

func (f *fakeInstaller) Installed() ([]toolchain.Installed, error) { return nil, nil }

func (f *fakeInstaller) Remove(version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, version)
	return nil
}

func (f *fakeInstaller) installList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.installs)
}

func (f *fakeInstaller) removedList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.removed)
}

// fakeDisk serves fixed Docker figures. With entered set, each DiskUsage call
// sends on it first; with gate set, it then waits for a send on gate or the
// end of its context.
type fakeDisk struct {
	mu      sync.Mutex
	rows    []system.DiskRow
	types   []system.CacheTypeUsage
	err     error
	calls   int
	entered chan struct{}
	gate    chan struct{}
}

func (f *fakeDisk) DiskUsage(ctx context.Context) ([]system.DiskRow, error) {
	f.mu.Lock()
	f.calls++
	entered, gate := f.entered, f.gate
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows, f.err
}

func (f *fakeDisk) BuildCacheUsage(context.Context) ([]system.CacheTypeUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.types, f.err
}

func (f *fakeDisk) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
