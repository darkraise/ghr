package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// unconfiguredStore opens a store whose config names owner (possibly empty)
// and whose token file holds token, or is missing when token is nil.
func unconfiguredStore(t *testing.T, owner string, token *string) *Store {
	t.Helper()
	dir := t.TempDir()
	cfg := strings.Replace(cfgYAML, "owner: darkraise", "owner: "+strconv.Quote(owner), 1)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600)
	if token != nil {
		os.WriteFile(filepath.Join(dir, "token"), []byte(*token), 0o600)
	}
	s, _, err := OpenStore(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreOpensUnconfigured(t *testing.T) {
	empty := " \n"
	for name, s := range map[string]*Store{
		"no token file":    unconfiguredStore(t, "", nil),
		"empty token file": unconfiguredStore(t, "", &empty),
		"owner, no token":  unconfiguredStore(t, "darkraise", nil),
	} {
		if s.Configured() || s.Token() != "" {
			t.Errorf("%s: configured %v token %q", name, s.Configured(), s.Token())
		}
	}
	if !newStore(t).Configured() {
		t.Fatal("owner and token: not configured")
	}
}

func TestStoreReloadSetsTheFirstOwnerAndToken(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	os.WriteFile(s.ConfigPath, []byte(cfgYAML), 0o600)
	if _, err := s.Reload(); err != nil || s.Configured() || s.Config().Owner != "darkraise" {
		t.Fatalf("owner without a token: err %v configured %v", err, s.Configured())
	}
	os.WriteFile(s.TokenPath, []byte("tok1\n"), 0o600)
	if _, err := s.Reload(); err != nil || !s.Configured() || s.Token() != "tok1" {
		t.Fatalf("owner and token: err %v token %q", err, s.Token())
	}
	os.WriteFile(s.TokenPath, []byte("\n"), 0o600)
	if _, err := s.Reload(); err == nil || s.Token() != "tok1" {
		t.Fatalf("emptied token after a token: err %v token %q", err, s.Token())
	}
	os.Remove(s.TokenPath)
	if _, err := s.Reload(); err == nil || s.Token() != "tok1" {
		t.Fatalf("removed token after a token: err %v token %q", err, s.Token())
	}
}

func TestStoreConfigure(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	if err := s.Configure("DarkRaise", "tok1"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.TokenPath)
	if !s.Configured() || s.Token() != "tok1" || string(data) != "tok1\n" || s.Config().Owner != "DarkRaise" {
		t.Fatalf("after configure: owner %q token %q file %q", s.Config().Owner, s.Token(), data)
	}
	if c, _, err := config.Load(s.ConfigPath); err != nil || c.Owner != "DarkRaise" {
		t.Fatalf("saved config: err %v", err)
	}
	if err := s.Configure("DarkRaise", "tok2"); !errors.Is(err, ErrConfigured) || s.Token() != "tok1" {
		t.Fatalf("second configure: %v", err)
	}

	s = unconfiguredStore(t, "darkraise", nil)
	var mismatch *OwnerMismatchError
	err := s.Configure("someone-else", "tok1")
	if !errors.As(err, &mismatch) || mismatch.Owner != "darkraise" ||
		err.Error() != "config.yaml names owner darkraise; edit config.yaml to change it" {
		t.Fatalf("another owner: %v", err)
	}
	if _, err := os.Stat(s.TokenPath); !os.IsNotExist(err) {
		t.Fatal("a refused configure wrote the token")
	}
	if err := s.Configure("DarkRaise", "tok1"); err != nil || s.Config().Owner != "DarkRaise" {
		t.Fatalf("config's owner in another case: err %v owner %q", err, s.Config().Owner)
	}
}

func TestStoreConfigureWriteFailures(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	os.Mkdir(s.TokenPath, 0o755)
	if err := s.Configure("darkraise", "tok1"); err == nil || s.Configured() || s.Token() != "" || s.Config().Owner != "" {
		t.Fatalf("token write failure: err %v token %q owner %q", err, s.Token(), s.Config().Owner)
	}
	if data, _ := os.ReadFile(s.ConfigPath); strings.Contains(string(data), "darkraise") {
		t.Fatal("the owner was saved although the token write failed")
	}

	s = unconfiguredStore(t, "", nil)
	saved, _ := os.ReadFile(s.ConfigPath)
	os.Remove(s.ConfigPath)
	os.Mkdir(s.ConfigPath, 0o755)
	if err := s.Configure("darkraise", "tok1"); err == nil || s.Configured() || s.Token() != "" {
		t.Fatalf("owner write failure: err %v configured %v token %q", err, s.Configured(), s.Token())
	}
	if data, _ := os.ReadFile(s.TokenPath); string(data) != "tok1\n" {
		t.Fatalf("owner write failure: token file %q", data)
	}
	os.Remove(s.ConfigPath)
	os.WriteFile(s.ConfigPath, saved, 0o600)
	if err := s.Configure("darkraise", "tok2"); err != nil || !s.Configured() || s.Token() != "tok2" {
		t.Fatalf("retry: err %v token %q", err, s.Token())
	}
}

// A case-only owner difference is still a config save, and its failure must
// leave ghr unconfigured.
func TestStoreConfigureOwnerCaseWriteFailure(t *testing.T) {
	s := unconfiguredStore(t, "darkraise", nil)
	saved, _ := os.ReadFile(s.ConfigPath)
	os.Remove(s.ConfigPath)
	os.Mkdir(s.ConfigPath, 0o755)
	if err := s.Configure("DarkRaise", "tok1"); err == nil || s.Configured() || s.Token() != "" || s.Config().Owner != "darkraise" {
		t.Fatalf("owner case change write failure: err %v configured %v token %q owner %q",
			err, s.Configured(), s.Token(), s.Config().Owner)
	}
	os.Remove(s.ConfigPath)
	os.WriteFile(s.ConfigPath, saved, 0o600)
	if err := s.Configure("DarkRaise", "tok2"); err != nil || s.Config().Owner != "DarkRaise" || s.Token() != "tok2" {
		t.Fatalf("owner case change retry: err %v owner %q token %q", err, s.Config().Owner, s.Token())
	}
}

func TestStoreConfiguresOnce(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Configure("darkraise", fmt.Sprintf("tok%d", i)) == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d configures succeeded", ok.Load())
	}
}
