package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)

// pkgFake is fakeBackend's package state.
type pkgFake struct {
	list  model.Packages
	found []model.PackageInfo
	added model.AddPackagesResult
	err   error // returned by every method
	calls []string
}

func (f *fakeBackend) Packages(context.Context) (model.Packages, error) {
	f.pkgs.calls = append(f.pkgs.calls, "list")
	return f.pkgs.list, f.pkgs.err
}
func (f *fakeBackend) SearchPackages(_ context.Context, term string) ([]model.PackageInfo, error) {
	f.pkgs.calls = append(f.pkgs.calls, "search "+term)
	return f.pkgs.found, f.pkgs.err
}
func (f *fakeBackend) AddPackages(_ context.Context, req model.AddPackagesRequest) (model.AddPackagesResult, error) {
	f.pkgs.calls = append(f.pkgs.calls, "add "+strings.Join(req.Names, ","))
	return f.pkgs.added, f.pkgs.err
}
func (f *fakeBackend) UpgradePackages(req model.UpgradePackagesRequest) error {
	f.pkgs.calls = append(f.pkgs.calls, fmt.Sprintf("upgrade %s all=%v", strings.Join(req.Names, ","), req.All))
	return f.pkgs.err
}
func (f *fakeBackend) UninstallPackage(name string) error {
	f.pkgs.calls = append(f.pkgs.calls, "uninstall "+name)
	return f.pkgs.err
}
func (f *fakeBackend) ForgetPackage(name string) error {
	f.pkgs.calls = append(f.pkgs.calls, "forget "+name)
	return f.pkgs.err
}
func (f *fakeBackend) RefreshPackageIndex() error {
	f.pkgs.calls = append(f.pkgs.calls, "refresh")
	return f.pkgs.err
}

func TestPackageRoutesThroughTheClient(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.pkgs.list = model.Packages{Managed: []model.PackageInfo{
		{Name: "ripgrep", Version: "14.1.0-1", Candidate: "14.1.1-1", Installed: true, Upgradable: true},
	}}
	p, err := c.Packages(ctx)
	if err != nil || len(p.Managed) != 1 || !p.Managed[0].Upgradable || p.Managed[0].Candidate != "14.1.1-1" {
		t.Fatalf("packages %+v, %v", p, err)
	}
	b.pkgs.found = []model.PackageInfo{{Name: "ripgrep", Installed: true, Managed: true}}
	found, err := c.SearchPackages(ctx, "rip+g")
	if err != nil || len(found) != 1 || !found[0].Managed {
		t.Fatalf("search %+v, %v", found, err)
	}
	b.pkgs.added = model.AddPackagesResult{Adopt: []string{"vim"}, Install: []string{"ripgrep"}}
	res, err := c.AddPackages(ctx, []string{"ripgrep", "vim"})
	if err != nil || !reflect.DeepEqual(res, b.pkgs.added) {
		t.Fatalf("add %+v, %v", res, err)
	}
	for _, call := range []func() error{
		func() error { return c.UpgradePackages(ctx, []string{"ripgrep"}, false) },
		func() error { return c.UpgradePackages(ctx, nil, true) },
		func() error { return c.UninstallPackage(ctx, "ripgrep") },
		func() error { return c.ForgetPackage(ctx, "yq") },
		func() error { return c.RefreshPackageIndex(ctx) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"list", "search rip+g", "add ripgrep,vim",
		"upgrade ripgrep all=false", "upgrade  all=true",
		"uninstall ripgrep", "forget yq", "refresh",
	}
	if !reflect.DeepEqual(b.pkgs.calls, want) {
		t.Fatalf("calls %q", b.pkgs.calls)
	}
}

func TestPackageRoutesAnswer(t *testing.T) {
	srv := httptest.NewServer(NewServer(&fakeBackend{}))
	defer srv.Close()
	for _, r := range []struct {
		method, path, body string
		status             int
		answer             string // the response body, trimmed; "" checks nothing
	}{
		{http.MethodGet, "/packages", "", http.StatusOK, ""},
		{http.MethodGet, "/packages/search?q=rip", "", http.StatusOK, "[]"},
		{http.MethodPost, "/packages", `{"names":["ripgrep"]}`, http.StatusAccepted, `{"adopt":[],"install":[]}`},
		{http.MethodPost, "/packages/upgrade", `{"all":true}`, http.StatusAccepted, ""},
		{http.MethodPost, "/packages/refresh", "", http.StatusAccepted, ""},
		{http.MethodDelete, "/packages/ripgrep?uninstall=1", "", http.StatusAccepted, ""},
		{http.MethodDelete, "/packages/ripgrep", "", http.StatusNoContent, ""},
		{http.MethodPost, "/packages", "{", http.StatusBadRequest, ""},
		{http.MethodPost, "/packages/upgrade", "{", http.StatusBadRequest, ""},
	} {
		req, err := http.NewRequest(r.method, srv.URL+r.path, strings.NewReader(r.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != r.status {
			t.Errorf("%s %s: %d", r.method, r.path, resp.StatusCode)
		}
		if r.answer != "" && strings.TrimSpace(string(data)) != r.answer {
			t.Errorf("%s %s: body %s", r.method, r.path, data)
		}
	}
}

func TestPackageErrorsReachTheClient(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.pkgs.err = &Error{Status: http.StatusServiceUnavailable, Msg: "apt is not available on this host"}
	for name, call := range map[string]func() error{
		"list":      func() error { _, err := c.Packages(ctx); return err },
		"search":    func() error { _, err := c.SearchPackages(ctx, "rip"); return err },
		"add":       func() error { _, err := c.AddPackages(ctx, []string{"ripgrep"}); return err },
		"upgrade":   func() error { return c.UpgradePackages(ctx, nil, true) },
		"uninstall": func() error { return c.UninstallPackage(ctx, "ripgrep") },
		"forget":    func() error { return c.ForgetPackage(ctx, "ripgrep") },
		"refresh":   func() error { return c.RefreshPackageIndex(ctx) },
	} {
		var ae *Error
		if err := call(); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "apt is not available on this host" {
			t.Errorf("%s: %v", name, err)
		}
	}
	b.pkgs.err = Conflict("an operation on this package is running; try again when it ends")
	var ae *Error
	if err := c.ForgetPackage(ctx, "ripgrep"); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("forget while busy: %v", err)
	}
}
