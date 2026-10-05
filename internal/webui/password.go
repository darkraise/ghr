// Package webui serves the browser UI: password login, sessions, the request
// checks in front of the control API, and the embedded single-page app.
package webui

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	hashScheme = "pbkdf2-sha256"
	// DefaultIterations is OWASP's 2023 figure for PBKDF2-HMAC-SHA256.
	DefaultIterations = 600000
	saltLen           = 16
	keyLen            = 32
	// minIterations admits the 1000-iteration hashes tests write;
	// maxIterations stops a hand-edited file from making every login hang.
	minIterations  = 1000
	maxIterations  = 10000000
	MinPasswordLen = 12
	MaxPasswordLen = 1024
)

// ErrUnreadable means the password file exists but cannot be used.
var ErrUnreadable = errors.New("web password file is unreadable; run ghr web reset-password")

type passwordHash struct {
	iter int
	salt []byte
	key  []byte
}

func newHash(password string, rand io.Reader, iter int) (passwordHash, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand, salt); err != nil {
		return passwordHash{}, err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, keyLen)
	if err != nil {
		return passwordHash{}, err
	}
	return passwordHash{iter: iter, salt: salt, key: key}, nil
}

func (h passwordHash) String() string {
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", hashScheme, h.iter, enc.EncodeToString(h.salt), enc.EncodeToString(h.key))
}

func parseHash(s string) (passwordHash, error) {
	parts := strings.Split(strings.TrimSpace(s), "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return passwordHash{}, ErrUnreadable
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < minIterations || iter > maxIterations {
		return passwordHash{}, ErrUnreadable
	}
	enc := base64.RawStdEncoding
	salt, serr := enc.DecodeString(parts[2])
	key, kerr := enc.DecodeString(parts[3])
	if serr != nil || kerr != nil || len(salt) != saltLen || len(key) != keyLen {
		return passwordHash{}, ErrUnreadable
	}
	return passwordHash{iter: iter, salt: salt, key: key}, nil
}

func (h passwordHash) matches(password string) bool {
	key, err := pbkdf2.Key(sha256.New, password, h.salt, h.iter, len(h.key))
	return err == nil && subtle.ConstantTimeCompare(key, h.key) == 1
}

// readHash returns the stored hash; ok is false when no password is set.
func readHash(path string) (h passwordHash, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return passwordHash{}, false, nil
	}
	if err != nil {
		return passwordHash{}, false, fmt.Errorf("%w (%v)", ErrUnreadable, err)
	}
	h, err = parseHash(string(data))
	if err != nil {
		return passwordHash{}, false, err
	}
	return h, true, nil
}

// writeHash replaces the password file atomically. os.CreateTemp creates the
// file with mode 0600, so the hash is never readable by other users.
func writeHash(path string, h passwordHash) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".web-password-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(h.String() + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// Syncing the directory makes the rename survive a power loss. Some
	// platforms, Windows among them, cannot sync a directory, so it is best
	// effort.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
