package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/packages"
	"github.com/darkraise/ghr/internal/storage"
)

type fakePkgs struct {
	list       model.Packages
	found      []model.PackageInfo
	added      model.AddPackagesResult
	err        error // returned by every method that can fail
	calls      []string
	version    atomic.Int64
	reconciled chan struct{} // ReconcilePackages sends on it when not nil
}

func (f *fakePkgs) PackageList(context.Context) (model.Packages, error) {
	f.calls = append(f.calls, "list")
	return f.list, f.err
}
func (f *fakePkgs) SearchPackages(_ context.Context, term string) ([]model.PackageInfo, error) {
	f.calls = append(f.calls, "search "+term)
	return f.found, f.err
}
func (f *fakePkgs) AddPackages(_ context.Context, names []string) (model.AddPackagesResult, error) {
	f.calls = append(f.calls, "add "+strings.Join(names, ","))
	return f.added, f.err
}
func (f *fakePkgs) UpgradePackages(names []string, all bool) error {
	f.calls = append(f.calls, fmt.Sprintf("upgrade %s all=%v", strings.Join(names, ","), all))
	return f.err
}
func (f *fakePkgs) UninstallPackage(name string) error {
	f.calls = append(f.calls, "uninstall "+name)
	return f.err
}
func (f *fakePkgs) ForgetPackage(name string) error {
	f.calls = append(f.calls, "forget "+name)
	return f.err
}
func (f *fakePkgs) RefreshPackageIndex() error {
	f.calls = append(f.calls, "refresh")
	return f.err
}
func (f *fakePkgs) PackagesVersion() int64 { return f.version.Load() }
func (f *fakePkgs) ReconcilePackages(context.Context) {
	if f.reconciled != nil {
		f.reconciled <- struct{}{}
	}
}

// pkgCalls runs every package method of b once.
func pkgCalls(b *Backend) map[string]func() error {
	ctx := context.Background()
	add := model.AddPackagesRequest{Names: []string{"ripgrep"}}
	return map[string]func() error{
		"list":      func() error { _, err := b.Packages(ctx); return err },
		"search":    func() error { _, err := b.SearchPackages(ctx, "rip"); return err },
		"add":       func() error { _, err := b.AddPackages(ctx, add); return err },
		"upgrade":   func() error { return b.UpgradePackages(model.UpgradePackagesRequest{All: true}) },
		"uninstall": func() error { return b.UninstallPackage("ripgrep") },
		"forget":    func() error { return b.ForgetPackage("ripgrep") },
		"refresh":   func() error { return b.RefreshPackageIndex() },
	}
}

func TestPackagesWithoutAServiceAnswer503(t *testing.T) {
	b, _, _ := newBackend(t)
	for name, call := range pkgCalls(b) {
		err := call()
		if apiStatus(err) != 503 || apiMsg(err) != "apt is not available on this host" {
			t.Errorf("%s: status %d message %q", name, apiStatus(err), apiMsg(err))
		}
	}
}

func TestPackagesDelegate(t *testing.T) {
	b, _, _ := newBackend(t)
	f := &fakePkgs{
		list:  model.Packages{Managed: []model.PackageInfo{{Name: "ripgrep", Installed: true}}},
		found: []model.PackageInfo{{Name: "ripgrep-all"}},
		added: model.AddPackagesResult{Install: []string{"ripgrep"}},
	}
	b.Pkgs = f
	ctx := context.Background()
	if p, err := b.Packages(ctx); err != nil || len(p.Managed) != 1 {
		t.Fatalf("list %+v, %v", p, err)
	}
	if ps, err := b.SearchPackages(ctx, "rip"); err != nil || len(ps) != 1 || ps[0].Name != "ripgrep-all" {
		t.Fatalf("search %+v, %v", ps, err)
	}
	if r, err := b.AddPackages(ctx, model.AddPackagesRequest{Names: []string{"ripgrep", "vim"}}); err != nil || !slices.Equal(r.Install, []string{"ripgrep"}) {
		t.Fatalf("add %+v, %v", r, err)
	}
	for _, err := range []error{
		b.UpgradePackages(model.UpgradePackagesRequest{Names: []string{"ripgrep"}}),
		b.UpgradePackages(model.UpgradePackagesRequest{All: true}),
		b.UninstallPackage("ripgrep"),
		b.ForgetPackage("yq"),
		b.RefreshPackageIndex(),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"list", "search rip", "add ripgrep,vim", "upgrade ripgrep all=false", "upgrade  all=true",
		"uninstall ripgrep", "forget yq", "refresh"}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("calls %q", f.calls)
	}
}

func TestUpgradePackagesRefusesNamesWithAll(t *testing.T) {
	b, _, _ := newBackend(t)
	f := &fakePkgs{}
	b.Pkgs = f
	err := b.UpgradePackages(model.UpgradePackagesRequest{Names: []string{"ripgrep"}, All: true})
	if apiStatus(err) != 400 || len(f.calls) != 0 {
		t.Fatalf("names and all: %v, calls %q", err, f.calls)
	}
}

func TestPackageErrorsMapToStatuses(t *testing.T) {
	b, _, _ := newBackend(t)
	f := &fakePkgs{}
	b.Pkgs = f
	for _, tc := range []struct {
		err  error
		want int
	}{
		{packages.ErrUnavailable, 503},
		{storage.ErrClosed, 503},
		{fmt.Errorf("%q: %w", "-o", packages.ErrInvalidName), 400},
		{packages.ErrInvalidTerm, 400},
		{fmt.Errorf("curl: %w", packages.ErrBase), 400},
		{fmt.Errorf("ripgrep: %w", packages.ErrAlreadyManaged), 400},
		{fmt.Errorf("libicu74: %w", packages.ErrNotAdoptable), 400},
		{packages.ErrNoNames, 400},
		{fmt.Errorf("yq: %w", packages.ErrNotManaged), 404},
		{fmt.Errorf("ripgrep: %w", packages.ErrBusy), 409},
		{storage.ErrQueueFull, 409},
	} {
		f.err = tc.err
		for name, call := range pkgCalls(b) {
			want := tc.want
			if name == "upgrade" && errors.Is(tc.err, packages.ErrNotManaged) {
				want = 400 // the spec's upgrade route answers 400 for a name ghr does not manage
			}
			err := call()
			if got := apiStatus(err); got != want || apiMsg(err) != tc.err.Error() {
				t.Errorf("%s with %v: status %d message %q, want %d", name, tc.err, got, apiMsg(err), want)
			}
		}
	}
	f.err = errors.New("dpkg-query: exit status 2")
	if _, err := b.Packages(context.Background()); err == nil || apiStatus(err) != 0 {
		t.Fatalf("an unexpected error must pass through unchanged: %v", err)
	}
}
