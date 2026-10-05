package webui

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHashRoundTrip(t *testing.T) {
	h, err := newHash("correct horse battery", rand.Reader, 1000)
	if err != nil {
		t.Fatal(err)
	}
	s := h.String()
	if !strings.HasPrefix(s, "pbkdf2-sha256$1000$") || strings.Count(s, "$") != 3 {
		t.Fatalf("stored form %q", s)
	}
	back, err := parseHash(s)
	if err != nil {
		t.Fatal(err)
	}
	if !back.matches("correct horse battery") {
		t.Fatal("the right password does not match")
	}
	if back.matches("correct horse batterz") {
		t.Fatal("a wrong password matches")
	}
	other, _ := newHash("correct horse battery", rand.Reader, 1000)
	if other.String() == s {
		t.Fatal("two hashes of one password share a salt")
	}
}

func TestParseHashRejectsMalformed(t *testing.T) {
	key := strings.Repeat("A", 43)
	for _, s := range []string{
		"",
		"plain text",
		"pbkdf2-sha256$x$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"pbkdf2-sha256$0$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"md5$1000$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"pbkdf2-sha256$1000$AAAAAAAAAAAAAAAAAAAAAA$AAAA",
		"pbkdf2-sha256$1000$$" + key,
		"pbkdf2-sha256$1000$!!!$" + key,
		"pbkdf2-sha256$1000$AA$" + key,
		"pbkdf2-sha256$999$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"pbkdf2-sha256$10000001$AAAAAAAAAAAAAAAAAAAAAA$" + key,
	} {
		if _, err := parseHash(s); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%q: want ErrUnreadable, got %v", s, err)
		}
	}
}

// A reader racing the writer must always see a complete hash: a truncating
// write would expose an empty or partial file between truncate and write.
func TestWriteHashReplacesAtomically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to rename over a file another handle has open; CI runs this on Linux")
	}
	path := filepath.Join(t.TempDir(), "web-password")
	first, _ := newHash("correct horse battery", rand.Reader, 1000)
	if err := writeHash(path, first); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	bad := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, ok, err := readHash(path); !ok || err != nil {
				select {
				case bad <- fmt.Sprintf("ok %v err %v", ok, err):
				default:
				}
			}
		}
	}()
	for range 200 {
		h, _ := newHash("correct horse battery", rand.Reader, 1000)
		if err := writeHash(path, h); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	<-done
	select {
	case b := <-bad:
		t.Fatalf("a reader saw an incomplete password file: %s", b)
	default:
	}
}

func TestWriteAndReadHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-password")
	if _, ok, err := readHash(path); ok || err != nil {
		t.Fatalf("missing file: ok %v err %v", ok, err)
	}
	h, err := newHash("correct horse battery", rand.Reader, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHash(path, h); err != nil {
		t.Fatal(err)
	}
	got, ok, err := readHash(path)
	if !ok || err != nil || !got.matches("correct horse battery") {
		t.Fatalf("read back: ok %v err %v", ok, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v err %v", fi.Mode().Perm(), err)
		}
	}
	h2, _ := newHash("another long secret", rand.Reader, 1000)
	if err := writeHash(path, h2); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := readHash(path); !got.matches("another long secret") {
		t.Fatal("rewrite did not replace the hash")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	os.WriteFile(path, []byte("garbage\n"), 0o600)
	if _, ok, err := readHash(path); ok || !errors.Is(err, ErrUnreadable) {
		t.Fatalf("garbage: ok %v err %v", ok, err)
	}
}
