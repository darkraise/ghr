package storage

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/packages"
)

// indexLoop reconciles the managed packages at start, then runs the
// package_index task when it is due: a refresh of the index, then a
// reconcile.
func (s *Service) indexLoop() {
	s.ReconcilePackages(s.ctx)
	until := func() time.Duration { return s.Jobs.Until(config.JobPackageIndex, s.now()) }
	timer := time.NewTimer(until())
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.pkgWake:
		case <-timer.C:
		}
		if !s.Jobs.Due(config.JobPackageIndex, s.now()) {
			timer.Reset(until())
			continue
		}
		s.Jobs.Started(config.JobPackageIndex, s.now())
		err := s.runIndexTask()
		s.Jobs.Finished(config.JobPackageIndex, s.now(), err, nil)
		if s.ctx.Err() != nil {
			return
		}
		// A full queue would refuse the reinstalls too; the next run retries.
		if !errors.Is(err, ErrQueueFull) {
			s.ReconcilePackages(s.ctx)
		}
		timer.Reset(until())
	}
}

// runIndexTask refreshes the index through the queue and returns the
// refresh's outcome. A refresh already queued, by the API say, is waited on
// rather than queued twice.
func (s *Service) runIndexTask() error {
	if !s.aptOn() {
		return packages.ErrUnavailable
	}
	o, err := s.enqueueWaiting(s.refreshOp(true))
	if err != nil {
		return err
	}
	select {
	case <-o.finished:
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	if o.result.Outcome != "ok" {
		return errors.New(o.result.Message)
	}
	return nil
}

// pkgTargetsBusy is the targets of the running and queued operations.
func (s *Service) pkgTargetsBusy() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	busy := map[string]bool{}
	if s.current != nil {
		busy[s.current.Target] = true
	}
	for _, o := range s.queue {
		busy[o.Target] = true
	}
	return busy
}

// ReconcilePackages queues an install of each managed package that is not
// installed and has a candidate, one per name so a bad name blocks no
// other, and records an error for each name without a candidate. Base
// names and names with an operation queued or running are left alone.
func (s *Service) ReconcilePackages(ctx context.Context) {
	if s.Managed == nil {
		return
	}
	// Every config reload reconciles, and a reload may change the managed list.
	s.pkgVersion.Add(1)
	busy := s.pkgTargetsBusy()
	var want []string
	for _, n := range s.Managed.List() {
		if !s.isBase(n) && !busy["apt "+n] {
			want = append(want, n)
		}
	}
	if len(want) == 0 {
		return
	}
	if s.Apt == nil || !s.Apt.Available() {
		s.Events.Add("warn", "", "packages: apt is not available on this host; not checking %s", strings.Join(want, ", "))
		return
	}
	infos, err := s.infoByName(ctx, want, true)
	if err != nil {
		if ctx.Err() == nil {
			s.Events.Add("warn", "", "packages: reading the managed packages: %v", err)
		}
		return
	}
	var missing, unqueued []string
	var queueErr error
	for _, n := range want {
		i := infos[n]
		switch {
		case i.Installed:
			// Installed by hand since an install failed.
			s.clearPkgErr(n)
		case i.Candidate == "":
			s.setPkgErr(n, noCandidate)
			missing = append(missing, n)
		case queueErr != nil:
			unqueued = append(unqueued, n)
		default:
			o := s.installPkgOp(n)
			install := o.run
			// A reinstall must not put back a name taken out of config.yaml
			// while it waited.
			o.run = func(ctx context.Context, progress func(string)) (string, string) {
				if !s.isManaged(n) {
					s.pkgVersion.Add(1)
					return "skipped", n + " is no longer managed"
				}
				return install(ctx, progress)
			}
			if err := s.enqueue(o); err != nil {
				queueErr = err
				unqueued = append(unqueued, n)
			}
		}
	}
	if len(unqueued) > 0 {
		s.Events.Add("warn", "", "packages: reinstalling %s: %v", strings.Join(unqueued, ", "), queueErr)
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		s.Events.Add("warn", "", "packages: %s for %s", noCandidate, strings.Join(missing, ", "))
	}
}
