package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/sched"
)

// activeUnits returns the IDs of running ghr-runner-* units.
func (m *Manager) activeUnits(ctx context.Context) (map[string]bool, error) {
	units, err := m.SD.List(ctx, UnitPrefix)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, u := range units {
		out[strings.TrimPrefix(u, UnitPrefix)] = true
	}
	return out, nil
}

// Adopt rebuilds state after a daemon start from running units and instance dirs.
// Instances whose unit is gone are cleaned up. A unit without a readable
// ghr.json is stopped, and its dir is removed only once the stop succeeded;
// Reconcile retries a failed stop and deletes the registration.
func (m *Manager) Adopt(ctx context.Context) error {
	active, err := m.activeUnits(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Paths.Instances, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.Paths.Instances)
	if err != nil {
		return err
	}
	now := m.Now()
	adopted, exited := 0, 0
	var toFinish []string
	for _, e := range entries {
		id := e.Name()
		if !e.IsDir() || !idRe.MatchString(id) {
			continue
		}
		dir := m.instanceDir(id)
		var meta Meta
		if err := readJSON(filepath.Join(dir, MetaFile), &meta); err != nil || meta.ID != id {
			if active[id] {
				if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
					m.Events.Add("warn", "", "instance %s has no readable %s and its unit did not stop (%v); reconciliation retries", id, MetaFile, err)
					delete(active, id)
					continue
				}
			}
			if err := os.RemoveAll(dir); err != nil {
				m.Events.Add("warn", "", "instance %s had no readable %s; its dir could not be removed (%v); reconciliation retries", id, MetaFile, err)
			} else {
				m.Events.Add("warn", "", "instance %s had no readable %s; stopped and removed", id, MetaFile)
			}
			delete(active, id)
			continue
		}
		// start_timeout counts from the spawn, so restarts cannot extend it.
		inst := &instance{Meta: meta, State: sched.Starting, StateSince: meta.SpawnedAt}
		if inst.StateSince.IsZero() {
			inst.StateSince = now
		}
		var rec JobRecord
		if readJSON(filepath.Join(dir, JobFile), &rec) == nil {
			inst.State = sched.Busy
			inst.StateSince = now
			inst.Job = jobInfo(rec, now)
		}
		m.mu.Lock()
		m.insts[id] = inst
		m.mu.Unlock()
		if active[id] {
			adopted++
		} else {
			exited++
			toFinish = append(toFinish, id)
		}
		delete(active, id)
	}
	for id := range active {
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", "", "unit %s%s has no instance dir and did not stop (%v); reconciliation retries", UnitPrefix, id, err)
			continue
		}
		m.Events.Add("warn", "", "unit %s%s had no instance dir; stopped", UnitPrefix, id)
	}
	for _, id := range toFinish {
		m.beginFinish(id)
	}
	m.Events.Add("info", "", "adopted %d running runners; cleaning up %d that exited while ghr was down", adopted, exited)
	return nil
}

// Reconcile works from the real unit inventory: it stops running units no
// instance tracks, deletes ghr-<repo>-<id> registrations that match neither a
// tracked instance (same repo and runner ID) nor a running unit, and removes
// instance dirs whose unit has confirmed exited.
func (m *Manager) Reconcile(ctx context.Context, cfg *config.Config) {
	running, err := m.activeUnits(ctx)
	if err != nil {
		m.Events.Add("warn", "", "reconcile: list units: %v", err)
		return
	}
	known := map[string]instance{}
	for _, i := range m.snapshot() {
		known[i.ID] = i
	}
	for id := range running {
		if _, ok := known[id]; ok {
			continue
		}
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", "", "reconcile: stop untracked unit %s%s: %v", UnitPrefix, id, err)
			continue
		}
		delete(running, id)
		m.Events.Add("warn", "", "reconcile: stopped untracked unit %s%s", UnitPrefix, id)
	}
	for _, r := range cfg.Repos {
		runners, err := m.GH.ListRunners(ctx, r.Name)
		if err != nil {
			m.Events.Add("warn", r.Name, "reconcile: list runners: %v", err)
			continue
		}
		prefix := "ghr-" + r.Name + "-"
		for _, rn := range runners {
			id := strings.TrimPrefix(rn.Name, prefix)
			if !strings.HasPrefix(rn.Name, prefix) || !idRe.MatchString(id) {
				continue
			}
			if i, ok := known[id]; ok {
				if strings.EqualFold(i.Repo, r.Name) && i.RunnerID == rn.ID {
					continue
				}
			} else if running[id] {
				continue // an untracked unit that failed to stop may still use it
			}
			err := m.GH.DeleteRunner(ctx, r.Name, rn.ID)
			switch {
			case err == nil:
				m.Events.Add("info", r.Name, "removed stale registration %s", rn.Name)
			case github.IsKind(err, github.ErrUnprocessable):
				// busy; retried at the next reconciliation
			default:
				m.Events.Add("warn", r.Name, "remove stale registration %s: %v", rn.Name, err)
			}
		}
	}
	entries, err := os.ReadDir(m.Paths.Instances)
	if err != nil && !os.IsNotExist(err) {
		m.Events.Add("warn", "", "reconcile: read instance dirs: %v", err)
	}
	for _, e := range entries {
		id := e.Name()
		if _, ok := known[id]; !e.IsDir() || !idRe.MatchString(id) || ok || running[id] {
			continue
		}
		if active, err := m.SD.Active(ctx, UnitPrefix+id); err == nil && !active {
			if err := os.RemoveAll(m.instanceDir(id)); err != nil {
				m.Events.Add("warn", "", "reconcile: remove orphan dir %s: %v", id, err)
			}
		}
	}
}
