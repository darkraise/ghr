package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/config"
)

const cfgYAML = `
owner: darkraise
mode: queue
global_max: 2
labels: [homelab]
repos:
  - name: darkcloud
    max: 1
  - name: darkmem
`

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfgYAML), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok1\n"), 0o600)
	s, _, err := OpenStore(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreUpdateReloadToken(t *testing.T) {
	s := newStore(t)
	if s.Token() != "tok1" || s.Config().GlobalMax != 2 {
		t.Fatalf("open: token %q cfg %+v", s.Token(), s.Config())
	}
	before := s.Config()
	if _, err := s.Update(func(c *config.Config) error { c.GlobalMax = 0; return nil }); err == nil {
		t.Fatal("invalid update accepted")
	}
	if s.Config() != before {
		t.Fatal("invalid update swapped config")
	}
	if _, err := s.Update(func(c *config.Config) error { c.GlobalMax = 3; return nil }); err != nil {
		t.Fatal(err)
	}
	if before.GlobalMax != 2 || s.Config().GlobalMax != 3 {
		t.Fatal("update mutated the old config or did not apply")
	}
	os.WriteFile(s.ConfigPath, []byte("owner: \"\"\n"), 0o600)
	if _, err := s.Reload(); err == nil || s.Config().GlobalMax != 3 {
		t.Fatalf("bad reload: err %v cfg %+v", err, s.Config())
	}
	os.WriteFile(s.ConfigPath, []byte(strings.Replace(cfgYAML, "owner: darkraise", "owner: someone-else", 1)), 0o600)
	if _, err := s.Reload(); err == nil || !strings.Contains(err.Error(), "restart ghr") || s.Config().Owner != "darkraise" {
		t.Fatalf("owner change accepted: err %v owner %s", err, s.Config().Owner)
	}
	if err := s.SetToken("tok2"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.TokenPath)
	if s.Token() != "tok2" || strings.TrimSpace(string(data)) != "tok2" {
		t.Fatalf("token %q file %q", s.Token(), data)
	}
}
