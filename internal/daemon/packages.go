package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/packages"
	"github.com/darkraise/ghr/internal/storage"
)

// PackagesService is the part of *storage.Service the package routes and the
// change topics use.
type PackagesService interface {
	PackageList(ctx context.Context) (model.Packages, error)
	SearchPackages(ctx context.Context, term string) ([]model.PackageInfo, error)
	AddPackages(ctx context.Context, names []string) (model.AddPackagesResult, error)
	UpgradePackages(names []string, all bool) error
	UninstallPackage(name string) error
	ForgetPackage(name string) error
	RefreshPackageIndex() error
	PackagesVersion() int64
	ReconcilePackages(ctx context.Context)
}

func (b *Backend) Packages(ctx context.Context) (model.Packages, error) {
	if b.Pkgs == nil {
		return model.Packages{}, packagesErr(packages.ErrUnavailable)
	}
	p, err := b.Pkgs.PackageList(ctx)
	return p, packagesErr(err)
}

func (b *Backend) SearchPackages(ctx context.Context, term string) ([]model.PackageInfo, error) {
	if b.Pkgs == nil {
		return nil, packagesErr(packages.ErrUnavailable)
	}
	ps, err := b.Pkgs.SearchPackages(ctx, term)
	return ps, packagesErr(err)
}

func (b *Backend) AddPackages(ctx context.Context, req model.AddPackagesRequest) (model.AddPackagesResult, error) {
	if b.Pkgs == nil {
		return model.AddPackagesResult{}, packagesErr(packages.ErrUnavailable)
	}
	res, err := b.Pkgs.AddPackages(ctx, req.Names)
	return res, packagesErr(err)
}

func (b *Backend) UpgradePackages(req model.UpgradePackagesRequest) error {
	if b.Pkgs == nil {
		return packagesErr(packages.ErrUnavailable)
	}
	if req.All && len(req.Names) > 0 {
		return api.BadRequest("give package names or all, not both")
	}
	err := b.Pkgs.UpgradePackages(req.Names, req.All)
	if errors.Is(err, packages.ErrNotManaged) {
		return api.BadRequest(err.Error())
	}
	return packagesErr(err)
}

func (b *Backend) UninstallPackage(name string) error {
	if b.Pkgs == nil {
		return packagesErr(packages.ErrUnavailable)
	}
	return packagesErr(b.Pkgs.UninstallPackage(name))
}

func (b *Backend) ForgetPackage(name string) error {
	if b.Pkgs == nil {
		return packagesErr(packages.ErrUnavailable)
	}
	return packagesErr(b.Pkgs.ForgetPackage(name))
}

func (b *Backend) RefreshPackageIndex() error {
	if b.Pkgs == nil {
		return packagesErr(packages.ErrUnavailable)
	}
	return packagesErr(b.Pkgs.RefreshPackageIndex())
}

// packagesErr gives a package error the status the API answers with.
func packagesErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, packages.ErrUnavailable), errors.Is(err, storage.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	case errors.Is(err, packages.ErrInvalidName), errors.Is(err, packages.ErrInvalidTerm), errors.Is(err, packages.ErrBase),
		errors.Is(err, packages.ErrAlreadyManaged), errors.Is(err, packages.ErrNotAdoptable), errors.Is(err, packages.ErrNoNames):
		return api.BadRequest(err.Error())
	case errors.Is(err, packages.ErrNotManaged):
		return api.NotFound(err.Error())
	case errors.Is(err, packages.ErrBusy), errors.Is(err, storage.ErrQueueFull):
		return api.Conflict(err.Error())
	}
	return err
}
