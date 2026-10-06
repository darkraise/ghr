package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/toolchain"
)

var (
	ErrUnknownCache   = errors.New("unknown package cache")
	ErrNotPresent     = errors.New("package cache not present")
	ErrUnknownPreset  = errors.New("unknown toolchain preset")
	ErrMissingVersion = errors.New("a toolchain version is required")
)

const recentOps = 10

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	var b [6]byte
	rand.Read(b[:])
	return s.now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

func (s *Service) busy() int {
	if s.Busy == nil {
		return 0
	}
	return s.Busy()
}

func (s *Service) enqueue(ops ...*op) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.queue = append(s.queue, ops...)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// work runs queued operations one at a time until Close, then drops the
// rest of the queue with a warning each.
func (s *Service) work() {
	s.cleanUp()
	for {
		s.mu.Lock()
		if s.ctx.Err() != nil {
			dropped := s.queue
			s.queue = nil
			s.mu.Unlock()
			for _, o := range dropped {
				s.Events.Add("warn", "", "%s %s dropped: ghr is shutting down", o.Kind, o.Target)
			}
			return
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			select {
			case <-s.ctx.Done():
			case <-s.wake:
			}
			continue
		}
		o := s.queue[0]
		s.queue = s.queue[1:]
		o.StartedAt = s.now()
		cur := o.Operation
		s.current = &cur
		s.mu.Unlock()
		s.Events.Add("info", "", "%s %s started", o.Kind, o.Target)
		outcome, msg := o.run(s.ctx, func(step string) {
			s.mu.Lock()
			if s.current != nil {
				s.current.Progress = step
			}
			s.mu.Unlock()
		})
		finished := s.now()
		done := o.Operation
		done.FinishedAt, done.Outcome, done.Message = &finished, outcome, msg
		s.mu.Lock()
		s.current = nil
		s.recent = append([]model.Operation{done}, s.recent...)
		if len(s.recent) > recentOps {
			s.recent = s.recent[:recentOps]
		}
		s.mu.Unlock()
		level := "info"
		if outcome != "ok" && outcome != "skipped" {
			level = "warn"
		}
		s.Events.Add(level, "", "%s %s %s: %s", o.Kind, o.Target, outcome, msg)
		s.Trigger()
	}
}

func interruptedOr(ctx context.Context, outcome string) string {
	if ctx.Err() != nil {
		return "interrupted"
	}
	return outcome
}

// Install queues one install; spec resolves when the install runs, so an
// unresolvable version fails the operation, not the request.
func (s *Service) Install(tool, spec string) error {
	inst, err := s.Tools.Get(tool)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec) == "" {
		return ErrMissingVersion
	}
	return s.enqueue(s.installOp(tool, inst, spec))
}

// InstallPreset queues each install of the preset, in its order.
func (s *Service) InstallPreset(name string) error {
	if name != "popular" {
		return fmt.Errorf("%w %q", ErrUnknownPreset, name)
	}
	var ops []*op
	for _, e := range toolchain.Popular {
		inst, err := s.Tools.Get(e.Tool)
		if err != nil {
			return err
		}
		ops = append(ops, s.installOp(e.Tool, inst, e.Spec))
	}
	return s.enqueue(ops...)
}

func (s *Service) installOp(tool string, inst toolchain.Installer, spec string) *op {
	return &op{
		Operation: model.Operation{ID: s.newID(), Kind: "install", Target: tool + " " + spec},
		run: func(ctx context.Context, progress func(string)) (string, string) {
			progress("resolving")
			rel, err := inst.Resolve(ctx, spec)
			if err != nil {
				return interruptedOr(ctx, "failed"), err.Error()
			}
			err = inst.Install(ctx, rel, progress)
			switch {
			case errors.Is(err, toolchain.ErrAlreadyInstalled):
				return "skipped", tool + " " + rel.Version + " is already installed"
			case err != nil:
				return interruptedOr(ctx, "failed"), err.Error()
			}
			return "ok", "installed " + tool + " " + rel.Version
		},
	}
}

// Remove queues the removal of an installed version. It is refused when it
// runs if any runner is busy then.
func (s *Service) Remove(tool, version string) error {
	inst, err := s.Tools.Get(tool)
	if err != nil {
		return err
	}
	installed, err := s.Tools.Installed()
	if err != nil {
		return err
	}
	found := false
	for _, in := range installed {
		if in.Tool == tool && in.Version == version {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s %s", toolchain.ErrNotInstalled, tool, version)
	}
	target := tool + " " + version
	return s.enqueue(&op{
		Operation: model.Operation{ID: s.newID(), Kind: "remove", Target: target},
		run: func(ctx context.Context, progress func(string)) (string, string) {
			if n := s.busy(); n > 0 {
				return "refused", fmt.Sprintf("refused: %d jobs running", n)
			}
			progress("removing")
			note := freed(s.toolchainBytes(tool, version))
			if err := inst.Remove(version); err != nil {
				return "failed", err.Error()
			}
			return "ok", "removed " + target + note
		},
	})
}

// Clear queues clearing a present package cache. It is refused when it runs
// if any runner is busy then.
func (s *Service) Clear(name string) error {
	c, ok := cacheByName(name)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownCache, name)
	}
	if !c.present(s.Home) {
		return fmt.Errorf("%w: %s", ErrNotPresent, name)
	}
	id := s.newID()
	return s.enqueue(&op{
		Operation: model.Operation{ID: id, Kind: "clear", Target: name},
		run: func(ctx context.Context, progress func(string)) (string, string) {
			if n := s.busy(); n > 0 {
				return "refused", fmt.Sprintf("refused: %d jobs running", n)
			}
			own, err := lookupOwner(s.User)
			if err != nil {
				return "failed", err.Error()
			}
			progress("clearing")
			note := freed(s.cacheBytes(name))
			if err := clearCache(s.Home, c, id, own); err != nil {
				return "failed", err.Error()
			}
			return "ok", "cleared " + c.Label + note
		},
	})
}

// Available lists what the Install dialog offers for tool, newest first.
func (s *Service) Available(ctx context.Context, tool string) ([]model.ToolchainChoice, error) {
	cs, err := s.Tools.Available(ctx, tool)
	if err != nil {
		return nil, err
	}
	out := make([]model.ToolchainChoice, len(cs))
	for i, c := range cs {
		out[i] = model.ToolchainChoice{Spec: c.Spec, Version: c.Version, LTS: c.LTS}
	}
	return out, nil
}

// freed names the size the last measurement saw, for an operation's message.
func freed(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	return " (" + model.HumanBytes(bytes) + " freed)"
}

func (s *Service) toolchainBytes(tool, version string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tc := range s.snap.toolchains {
		if tc.Tool == tool && tc.Version == version {
			return tc.Bytes
		}
	}
	return 0
}

func (s *Service) cacheBytes(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.snap.caches {
		if c.Name == name {
			return c.Bytes
		}
	}
	return 0
}
