package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/packages"
)

// Managed is the managed package list in config.yaml.
type Managed interface {
	List() []string
	Add(name string) error  // no-op when listed
	Drop(name string) error // no-op when not listed
}

// staleIndex is how old the index may be before an install refreshes it.
const staleIndex = time.Hour

const upgradeAllTarget = "apt all managed packages"

func (s *Service) aptOn() bool {
	return s.Apt != nil && s.Managed != nil && s.Apt.Available()
}

// PackagesVersion changes whenever a package operation ends or a package is
// forgotten.
func (s *Service) PackagesVersion() int64 { return s.pkgVersion.Load() }

func (s *Service) setPkgErr(name, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pkgErrs[name] = msg
}

func (s *Service) clearPkgErr(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pkgErrs, name)
}

func (s *Service) pkgErr(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pkgErrs[name]
}

func (s *Service) isBase(name string) bool { return slices.Contains(s.Base, name) }

func (s *Service) isManaged(name string) bool { return slices.Contains(s.Managed.List(), name) }

// pkgOp makes a package operation. Each one can be waited on, so the
// package_index task can wait on a refresh the API queued.
func (s *Service) pkgOp(kind, target string, run func(ctx context.Context, progress func(string)) (string, string)) *op {
	return &op{
		Operation: model.Operation{ID: s.newID(), Kind: kind, Target: target},
		finished:  make(chan struct{}),
		run: func(ctx context.Context, progress func(string)) (string, string) {
			defer s.pkgVersion.Add(1)
			return run(ctx, progress)
		},
	}
}

// pkgFailed ends an install with err and records it as name's error, which
// GET /packages shows while name is managed.
func (s *Service) pkgFailed(ctx context.Context, name, outcome string, err error) (string, string) {
	outcome = interruptedOr(ctx, outcome)
	if outcome != "interrupted" {
		s.setPkgErr(name, err.Error())
	}
	return outcome, err.Error()
}

func toPackageInfo(i packages.Info) model.PackageInfo {
	return model.PackageInfo{
		Name: i.Name, Version: i.Version, Candidate: i.Candidate, Description: i.Description,
		Installed: i.Installed, Upgradable: i.Upgradable, Bytes: i.Bytes,
	}
}

// infoByName reads names and fills in a bare entry for any name apt left out.
func (s *Service) infoByName(ctx context.Context, names []string, candidates bool) (map[string]packages.Info, error) {
	out := make(map[string]packages.Info, len(names))
	if len(names) == 0 {
		return out, nil
	}
	infos, err := s.Apt.Info(ctx, names, candidates)
	if err != nil {
		return nil, err
	}
	for _, i := range infos {
		out[i.Name] = i
	}
	for _, n := range names {
		if _, ok := out[n]; !ok {
			out[n] = packages.Info{Name: n}
		}
	}
	return out, nil
}

// indexTime is when ghr last refreshed the index; without its stamp, the
// newest *_InRelease file in AptLists. Zero when neither exists.
func (s *Service) indexTime() time.Time {
	if s.IndexStamp != "" {
		if fi, err := os.Stat(s.IndexStamp); err == nil {
			return fi.ModTime()
		}
	}
	var newest time.Time
	if s.AptLists == "" {
		return newest
	}
	matches, _ := filepath.Glob(filepath.Join(s.AptLists, "*_InRelease"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

func (s *Service) stampIndex() {
	if s.IndexStamp == "" {
		return
	}
	now := s.now()
	err := os.MkdirAll(filepath.Dir(s.IndexStamp), 0o755)
	if err == nil {
		err = os.WriteFile(s.IndexStamp, nil, 0o644)
	}
	if err == nil {
		err = os.Chtimes(s.IndexStamp, now, now)
	}
	if err != nil {
		s.Events.Add("warn", "", "packages: recording the index refresh: %v", err)
	}
}

func (s *Service) refreshIndex(ctx context.Context, progress func(string)) error {
	progress("refreshing the index")
	if err := s.Apt.Refresh(ctx); err != nil {
		return err
	}
	s.stampIndex()
	return nil
}

// PackageList reads the managed, base and detected packages. Candidates are
// read for the managed names only, since apt-cache policy is slow.
func (s *Service) PackageList(ctx context.Context) (model.Packages, error) {
	if !s.aptOn() {
		return model.Packages{}, packages.ErrUnavailable
	}
	managed := s.Managed.List()
	var own []string
	for _, n := range managed {
		if !s.isBase(n) {
			own = append(own, n)
		}
	}
	slices.Sort(own)
	out := model.Packages{Managed: []model.PackageInfo{}, Base: []model.PackageInfo{}, Detected: []model.PackageInfo{}}
	infos, err := s.infoByName(ctx, own, true)
	if err != nil {
		return model.Packages{}, err
	}
	for _, n := range own {
		p := toPackageInfo(infos[n])
		p.Managed, p.Error = true, s.pkgErr(n)
		out.Managed = append(out.Managed, p)
	}
	base, err := s.infoByName(ctx, s.Base, false)
	if err != nil {
		return model.Packages{}, err
	}
	for _, n := range s.Base {
		if i := base[n]; i.Installed {
			p := toPackageInfo(i)
			p.Managed = slices.Contains(managed, n)
			out.Base = append(out.Base, p)
		}
	}
	detected, err := s.Apt.Detected(ctx, append(slices.Clone(managed), s.Base...))
	if err != nil {
		return model.Packages{}, err
	}
	for _, i := range detected {
		out.Detected = append(out.Detected, toPackageInfo(i))
	}
	if at := s.indexTime(); !at.IsZero() {
		out.IndexUpdatedAt = &at
	}
	return out, nil
}

// SearchPackages searches the package names in the configured apt sources.
func (s *Service) SearchPackages(ctx context.Context, term string) ([]model.PackageInfo, error) {
	if !s.aptOn() {
		return nil, packages.ErrUnavailable
	}
	if !packages.ValidTerm(term) {
		return nil, packages.ErrInvalidTerm
	}
	found, err := s.Apt.Search(ctx, term)
	if err != nil {
		return nil, err
	}
	managed := s.Managed.List()
	out := make([]model.PackageInfo, 0, len(found))
	for _, i := range found {
		p := toPackageInfo(i)
		p.Managed = slices.Contains(managed, i.Name)
		out = append(out, p)
	}
	return out, nil
}

// AddPackages queues an adopt for each installed name and an install for
// each other one. It queues none when any name is refused.
func (s *Service) AddPackages(ctx context.Context, names []string) (model.AddPackagesResult, error) {
	res := model.AddPackagesResult{Adopt: []string{}, Install: []string{}}
	if !s.aptOn() {
		return res, packages.ErrUnavailable
	}
	if len(names) == 0 {
		return res, packages.ErrNoNames
	}
	managed := s.Managed.List()
	for i, n := range names {
		switch {
		case !packages.ValidName(n):
			return res, fmt.Errorf("%w: %q", packages.ErrInvalidName, n)
		case slices.Contains(names[:i], n):
			return res, fmt.Errorf("%w: %s is named twice", packages.ErrInvalidName, n)
		case s.isBase(n):
			return res, fmt.Errorf("%w: %s", packages.ErrBase, n)
		case slices.Contains(managed, n):
			return res, fmt.Errorf("%w: %s", packages.ErrAlreadyManaged, n)
		}
	}
	infos, err := s.infoByName(ctx, names, false)
	if err != nil {
		return res, err
	}
	var ops []*op
	for _, n := range names {
		i := infos[n]
		if !i.Installed {
			res.Install = append(res.Install, n)
			ops = append(ops, s.installPkgOp(n))
			continue
		}
		if !packages.Adoptable(i) {
			return model.AddPackagesResult{Adopt: []string{}, Install: []string{}},
				fmt.Errorf("%w: %s is priority %s in section %s", packages.ErrNotAdoptable, n, i.Priority, i.Section)
		}
		res.Adopt = append(res.Adopt, n)
		ops = append(ops, s.adoptOp(n))
	}
	if err := s.enqueue(ops...); err != nil {
		return model.AddPackagesResult{Adopt: []string{}, Install: []string{}}, err
	}
	return res, nil
}

const noCandidate = "no installation candidate in the configured apt sources"

// installPkgOp installs name, then adds it to the list. It holds the runners
// only when apt would also upgrade a package already installed.
func (s *Service) installPkgOp(name string) *op {
	return s.pkgOp("install", "apt "+name, func(ctx context.Context, progress func(string)) (string, string) {
		if s.now().Sub(s.indexTime()) >= staleIndex {
			if err := s.refreshIndex(ctx, progress); err != nil {
				return s.pkgFailed(ctx, name, "failed", err)
			}
		}
		progress("checking")
		// apt-get reads a name no package has exactly as a regular expression
		// ("ripgre.") or a modifier ("ripgrep-" removes ripgrep), so only a
		// name apt-cache policy lists by itself goes on.
		infos, err := s.infoByName(ctx, []string{name}, true)
		if err != nil {
			return s.pkgFailed(ctx, name, "failed", err)
		}
		if infos[name].Candidate == "" {
			return s.pkgFailed(ctx, name, "failed", fmt.Errorf("%s", noCandidate))
		}
		others, err := s.Apt.UpgradesOthers(ctx, []string{name})
		if err != nil {
			return s.pkgFailed(ctx, name, "failed", err)
		}
		if others {
			release, err := s.hold(ctx)
			if err != nil {
				return s.pkgFailed(ctx, name, "refused", err)
			}
			defer release()
		}
		progress("installing")
		if err := s.Apt.Install(ctx, []string{name}); err != nil {
			return s.pkgFailed(ctx, name, "failed", err)
		}
		s.clearPkgErr(name)
		if err := s.Managed.Add(name); err != nil {
			return "failed", "installed " + name + " but could not add it to config.yaml: " + err.Error()
		}
		return "ok", "installed " + name
	})
}

func (s *Service) adoptOp(name string) *op {
	return s.pkgOp("adopt", "apt "+name, func(ctx context.Context, progress func(string)) (string, string) {
		progress("marking manual")
		if err := s.Apt.MarkManual(ctx, name); err != nil {
			return interruptedOr(ctx, "failed"), err.Error()
		}
		if err := s.Managed.Add(name); err != nil {
			return "failed", "could not add " + name + " to config.yaml: " + err.Error()
		}
		return "ok", "adopted " + name
	})
}

// UpgradePackages queues one upgrade of names, or of every managed package
// when all is set.
func (s *Service) UpgradePackages(names []string, all bool) error {
	if !s.aptOn() {
		return packages.ErrUnavailable
	}
	target := upgradeAllTarget
	if !all {
		if len(names) == 0 {
			return packages.ErrNoNames
		}
		managed := s.Managed.List()
		for _, n := range names {
			switch {
			case !slices.Contains(managed, n):
				return fmt.Errorf("%w: %s", packages.ErrNotManaged, n)
			case s.isBase(n):
				return fmt.Errorf("%w: %s", packages.ErrBase, n)
			}
		}
		target = "apt " + strings.Join(names, ", ")
		if len(target) > 80 {
			target = target[:77] + "..."
		}
	}
	names = slices.Clone(names)
	return s.enqueue(s.pkgOp("upgrade", target, func(ctx context.Context, progress func(string)) (string, string) {
		// The list may have changed since the request was queued.
		var keep []string
		for _, n := range s.Managed.List() {
			if (all || slices.Contains(names, n)) && !s.isBase(n) {
				keep = append(keep, n)
			}
		}
		if len(keep) == 0 {
			return "skipped", "nothing to upgrade"
		}
		if err := s.refreshIndex(ctx, progress); err != nil {
			return interruptedOr(ctx, "failed"), err.Error()
		}
		infos, err := s.infoByName(ctx, keep, true)
		if err != nil {
			return interruptedOr(ctx, "failed"), err.Error()
		}
		var up []string
		for _, n := range keep {
			if infos[n].Upgradable {
				up = append(up, n)
			}
		}
		if len(up) == 0 {
			return "skipped", "nothing to upgrade"
		}
		release, err := s.hold(ctx)
		if err != nil {
			return interruptedOr(ctx, "refused"), err.Error()
		}
		defer release()
		progress("upgrading")
		if err := s.Apt.Upgrade(ctx, up); err != nil {
			return interruptedOr(ctx, "failed"), err.Error()
		}
		return "ok", "upgraded " + strings.Join(up, ", ")
	}))
}

// UninstallPackage queues the removal of a managed package. dpkg refuses it
// while another installed package depends on it, and the name then stays
// in config.yaml.
func (s *Service) UninstallPackage(name string) error {
	if !s.aptOn() {
		return packages.ErrUnavailable
	}
	if !s.isManaged(name) {
		return fmt.Errorf("%w: %s", packages.ErrNotManaged, name)
	}
	if s.isBase(name) {
		return fmt.Errorf("%w: %s", packages.ErrBase, name)
	}
	return s.enqueue(s.pkgOp("remove", "apt "+name, func(ctx context.Context, progress func(string)) (string, string) {
		// A Forget while this waited must not leave dpkg acting on a
		// package ghr no longer manages.
		if !s.isManaged(name) {
			return "failed", name + " is no longer managed"
		}
		release, err := s.hold(ctx)
		if err != nil {
			return interruptedOr(ctx, "refused"), err.Error()
		}
		defer release()
		progress("removing")
		if err := s.Apt.Remove(ctx, name); err != nil {
			return interruptedOr(ctx, "failed"), err.Error()
		}
		s.clearPkgErr(name)
		if err := s.Managed.Drop(name); err != nil {
			return "failed", "uninstalled " + name + " but could not remove it from config.yaml: " + err.Error()
		}
		return "ok", "uninstalled " + name
	}))
}

// ForgetPackage drops a managed name from the list and every operation on
// it still queued, and leaves the package installed. It is refused while an
// operation on the name runs.
func (s *Service) ForgetPackage(name string) error {
	if !s.aptOn() {
		return packages.ErrUnavailable
	}
	if !s.isManaged(name) {
		return fmt.Errorf("%w: %s", packages.ErrNotManaged, name)
	}
	target := "apt " + name
	s.mu.Lock()
	if s.current != nil && s.current.Target == target {
		s.mu.Unlock()
		return packages.ErrBusy
	}
	var dropped []*op
	s.queue = slices.DeleteFunc(s.queue, func(o *op) bool {
		if o.Target == target {
			dropped = append(dropped, o)
			return true
		}
		return false
	})
	delete(s.pkgErrs, name)
	s.mu.Unlock()
	for _, o := range dropped {
		d := o.Operation
		d.Outcome, d.Message = "skipped", name+" was forgotten"
		o.end(d)
	}
	defer s.pkgVersion.Add(1)
	return s.Managed.Drop(name)
}

// RefreshPackageIndex queues an apt-get update.
func (s *Service) RefreshPackageIndex() error {
	if !s.aptOn() {
		return packages.ErrUnavailable
	}
	return s.enqueue(s.refreshOp(false))
}

func (s *Service) refreshOp(quiet bool) *op {
	o := s.pkgOp("refresh", "apt index", func(ctx context.Context, progress func(string)) (string, string) {
		if err := s.refreshIndex(ctx, progress); err != nil {
			return interruptedOr(ctx, "failed"), err.Error()
		}
		return "ok", "refreshed the package index"
	})
	o.quiet = quiet
	return o
}
