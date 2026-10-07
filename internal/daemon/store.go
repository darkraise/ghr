// Package daemon wires config, the runner manager and the control API into the ghr service.
package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/darkraise/ghr/internal/config"
)

// ErrConfigured refuses a first-run configure once ghr has an owner and a token.
var ErrConfigured = errors.New("already configured; the owner changes only by editing config.yaml and restarting ghr, and the token with: ghr token set")

// OwnerMismatchError refuses a first-run owner other than the one config.yaml names.
type OwnerMismatchError struct{ Owner string }

func (e *OwnerMismatchError) Error() string {
	return "config.yaml names owner " + e.Owner + "; edit config.yaml to change it"
}

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

// Configured reports whether ghr has an owner and a token, the two things
// the manager cannot start without.
func (s *Store) Configured() bool {
	return strings.TrimSpace(s.Config().Owner) != "" && s.Token() != ""
}

// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with. A first owner may be set.
func (s *Store) Reload() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != "" && cur.Owner != cfg.Owner {
		return nil, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	tok, err := s.readToken()
	if err != nil {
		return nil, err
	}
	s.cfg.Store(cfg)
	if tok != "" {
		s.token.Store(&tok)
	}
	return warnings, nil
}

// readToken returns the token file's trimmed content. Until a token has been
// loaded, a missing or empty file means "not configured yet"; after that it is
// an error, so a reload never drops a working token.
func (s *Store) readToken() (string, error) {
	loaded := s.token.Load() != nil
	data, err := os.ReadFile(s.TokenPath)
	if errors.Is(err, fs.ErrNotExist) && !loaded {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" && loaded {
		return "", errors.New("token file is empty")
	}
	return tok, nil
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
	return s.writeToken(tok)
}

// Configure sets the first owner and token. The check and both writes happen
// under one hold of s.mu, so a concurrent reload cannot configure another
// owner in between. The token is written first: if the owner write then
// fails, ghr stays unconfigured and a retry overwrites the token.
func (s *Store) Configure(owner, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Configured() {
		return ErrConfigured
	}
	cur := s.Config()
	if cur.Owner != "" && !strings.EqualFold(cur.Owner, owner) {
		return &OwnerMismatchError{Owner: cur.Owner}
	}
	if err := s.writeToken(token); err != nil {
		return err
	}
	if cur.Owner == owner {
		return nil
	}
	c := cur.Clone()
	c.Owner = owner
	if err := config.Save(s.ConfigPath, c); err != nil {
		return err
	}
	s.cfg.Store(c)
	return nil
}

// writeToken writes the token file (0600, temporary file and rename); the
// caller holds s.mu.
func (s *Store) writeToken(tok string) error {
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
