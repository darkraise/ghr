package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/storage"
	"github.com/darkraise/ghr/internal/toolchain"
)

type fakeStorage struct {
	snap     model.Storage
	err      error // returned by every mutation and by Refresh
	availErr error
	calls    []string
}

func (f *fakeStorage) Snapshot() model.Storage { return f.snap }
func (f *fakeStorage) Refresh() error {
	f.calls = append(f.calls, "refresh")
	return f.err
}
func (f *fakeStorage) Available(_ context.Context, tool string) ([]model.ToolchainChoice, error) {
	f.calls = append(f.calls, "available "+tool)
	if f.availErr != nil {
		return nil, f.availErr
	}
	return []model.ToolchainChoice{{Spec: "22", Version: "22.11.0"}}, nil
}
func (f *fakeStorage) Install(tool, spec string) error {
	f.calls = append(f.calls, "install "+tool+" "+spec)
	return f.err
}
func (f *fakeStorage) InstallPreset(name string) error {
	f.calls = append(f.calls, "preset "+name)
	return f.err
}
func (f *fakeStorage) Remove(tool, version string) error {
	f.calls = append(f.calls, "remove "+tool+" "+version)
	return f.err
}
func (f *fakeStorage) Clear(name string) error {
	f.calls = append(f.calls, "clear "+name)
	return f.err
}

func apiMsg(err error) string {
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae.Msg
	}
	return ""
}

func TestStorageMergesLastPruneAndDisk(t *testing.T) {
	b, m, _ := newBackend(t)
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	m.lastPrune = &model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: now}
	m.diskPct = 73
	b.Space.(*fakeStorage).snap = model.Storage{MeasureError: "x", Docker: model.DockerDisk{Rows: []model.DockerRow{{Type: "Images"}}}}
	st := b.Storage()
	if st.LastPrune == nil || st.LastPrune.Trigger != "auto" || st.Docker.DiskPct != 73 || st.MeasureError != "x" || len(st.Docker.Rows) != 1 {
		t.Fatalf("storage %+v", st)
	}
}

func TestStorageErrorsMapToStatuses(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w %q", toolchain.ErrUnknownTool, "ruby"), 400},
		{fmt.Errorf("%w %q", storage.ErrUnknownPreset, "all"), 400},
		{storage.ErrMissingVersion, 400},
		{fmt.Errorf("%w: node 20.0.0", toolchain.ErrNotInstalled), 404},
		{fmt.Errorf("%w %q", storage.ErrUnknownCache, "bogus"), 404},
		{fmt.Errorf("%w: cargo", storage.ErrNotPresent), 409},
		{storage.ErrClosed, 503},
	} {
		fs.err = tc.err
		for name, call := range map[string]func() error{
			"install": func() error { return b.InstallToolchain(model.InstallRequest{Tool: "node", Version: "22"}) },
			"remove":  func() error { return b.RemoveToolchain("node", "22.11.0") },
			"clear":   func() error { return b.ClearCache("nuget") },
		} {
			err := call()
			if got := apiStatus(err); got != tc.want || apiMsg(err) != tc.err.Error() {
				t.Errorf("%s with %v: status %d message %q, want %d", name, tc.err, got, apiMsg(err), tc.want)
			}
		}
	}
	fs.err = errors.New("disk on fire")
	if err := b.ClearCache("nuget"); err == nil || apiStatus(err) != 0 {
		t.Fatalf("an unexpected error must pass through unchanged: %v", err)
	}
}

func TestInstallToolchainRequests(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	if err := b.InstallToolchain(model.InstallRequest{Tool: "node", Version: "22"}); err != nil {
		t.Fatal(err)
	}
	if err := b.InstallToolchain(model.InstallRequest{Preset: "popular"}); err != nil {
		t.Fatal(err)
	}
	if err := b.InstallToolchain(model.InstallRequest{}); apiStatus(err) != 400 {
		t.Fatalf("empty request: %v", err)
	}
	if err := b.InstallToolchain(model.InstallRequest{Tool: "node", Version: "22", Preset: "popular"}); apiStatus(err) != 400 {
		t.Fatalf("tool and preset: %v", err)
	}
	if want := []string{"install node 22", "preset popular"}; !slices.Equal(fs.calls, want) {
		t.Fatalf("calls %q", fs.calls)
	}
}

func TestRefreshStorage(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	if err := b.RefreshStorage(); err != nil {
		t.Fatal(err)
	}
	fs.err = storage.ErrMeasuring
	if err := b.RefreshStorage(); apiStatus(err) != 409 {
		t.Fatalf("measuring: %v", err)
	}
	fs.err = storage.ErrClosed
	if err := b.RefreshStorage(); apiStatus(err) != 503 {
		t.Fatalf("closed: %v", err)
	}
}

func TestAvailableToolchains(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	cs, err := b.AvailableToolchains(context.Background(), "node")
	if err != nil || len(cs) != 1 || cs[0].Version != "22.11.0" {
		t.Fatalf("choices %+v, %v", cs, err)
	}
	fs.availErr = fmt.Errorf("%w %q", toolchain.ErrUnknownTool, "ruby")
	if _, err := b.AvailableToolchains(context.Background(), "ruby"); apiStatus(err) != 400 {
		t.Fatalf("unknown tool: %v", err)
	}
	fs.availErr = errors.New("GET https://api.adoptium.net/v3/info/available_releases: 503 Service Unavailable")
	_, err = b.AvailableToolchains(context.Background(), "java")
	if apiStatus(err) != 502 || apiMsg(err) != fs.availErr.Error() {
		t.Fatalf("source failure: %v", err)
	}
}

func TestPruneScopeStatuses(t *testing.T) {
	b, m, _ := newBackend(t)
	if err := b.PruneScope(runner.ScopeBuildCacheAll); err != nil {
		t.Fatal(err)
	}
	if err := b.Prune(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"build-cache-all", "standard"}; !slices.Equal(m.scopes, want) {
		t.Fatalf("scopes %q", m.scopes)
	}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w %q", runner.ErrUnknownScope, "everything"), 400},
		{runner.BusyError{N: 2}, 409},
		{runner.ErrPruneRunning, 409},
		{runner.ErrUpdateRunning, 409},
		{runner.ErrClosed, 503},
	} {
		m.pruneErr = tc.err
		err := b.PruneScope(runner.ScopeUnusedVolumes)
		if apiStatus(err) != tc.want || apiMsg(err) != tc.err.Error() {
			t.Errorf("%v: status %d message %q", tc.err, apiStatus(err), apiMsg(err))
		}
	}
}
