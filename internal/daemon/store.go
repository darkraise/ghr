// Package daemon wires config, the runner manager and the control API into the ghr service.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/darkraise/ghr/internal/config"
)

// Store owns the live config and token. Writes are validated and saved atomically.
type Store struct {
	ConfigPath string
	TokenPath  string

	mu    sync.Mutex
	cfg   atomic.Pointer[config.Config]
	token atomic.Pointer[string]
}

func OpenStore(configPath, tokenPath string) (*Store, []string, error) {
	s := &Store{ConfigPath: configPath, TokenPath: tokenPath}
	warnings, err := s.Reload()
	return s, warnings, err
}

func (s *Store) Config() *config.Config { return s.cfg.Load() }

func (s *Store) Token() string {
	if t := s.token.Load(); t != nil {
		return *t
	}
	return ""
}

// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with.
func (s *Store) Reload() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != cfg.Owner {
		return nil, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	data, err := os.ReadFile(s.TokenPath)
	if err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return nil, errors.New("token file is empty")
	}
	s.cfg.Store(cfg)
	s.token.Store(&tok)
	return warnings, nil
}

// Update applies fn to a copy of the config, validates, saves, then swaps it in.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.Config().Clone()
	if err := fn(c); err != nil {
		return nil, err
	}
	warnings, err := c.Validate()
	if err != nil {
		return nil, err
	}
	if err := config.Save(s.ConfigPath, c); err != nil {
		return nil, err
	}
	s.cfg.Store(c)
	return warnings, nil
}

// SetToken writes the token file (0600, atomic) and swaps it in.
func (s *Store) SetToken(tok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := os.CreateTemp(filepath.Dir(s.TokenPath), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.TokenPath); err != nil {
		return err
	}
	s.token.Store(&tok)
	return nil
}
