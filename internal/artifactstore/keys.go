// Package artifactstore keeps the self-hosted artifact store: its access
// keys, its bucket inventory and cleanup, and the repository variables and
// secrets that point workflows at it.
package artifactstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/darkraise/ghr/internal/fsutil"
)

// maxKeyInput bounds a key ID or secret, in bytes.
const maxKeyInput = 64 << 10

// Key is one S3 access key.
type Key struct {
	ID     string    `json:"id"`
	Secret string    `json:"secret"`
	SetAt  time.Time `json:"set_at"`
}

// Keys are the admin key, which the daemon lists, reads and deletes with,
// and the job key, which ghr writes into repository secrets.
type Keys struct {
	Admin *Key `json:"admin,omitempty"`
	Job   *Key `json:"job,omitempty"`
}

// LoadKeys reads the keys file; a missing file holds no keys.
func LoadKeys(path string) (Keys, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Keys{}, nil
	}
	if err != nil {
		return Keys{}, err
	}
	var k Keys
	if err := json.Unmarshal(data, &k); err != nil {
		// The decoder's message can quote the file, and the file holds secrets.
		return Keys{}, fmt.Errorf("%s is not a keys file; remove it and set the keys again", path)
	}
	return k, nil
}

// SaveKeys replaces the keys file, readable by the owner only.
func SaveKeys(path string, k Keys) error {
	data, err := json.Marshal(k)
	if err != nil {
		return err
	}
	return fsutil.WritePrivate(path, append(data, '\n'))
}

// CheckKeyInput trims a key ID and secret as typed or piped and refuses
// one that is empty, spans lines or is longer than 64 KB. An ID may not
// hold a colon, which separates it from the secret in GHR_ARTIFACT_KEY.
// No message quotes either value.
func CheckKeyInput(id, secret string) (string, string, error) {
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	for _, f := range []struct{ what, v string }{{"the key ID", id}, {"the secret", secret}} {
		switch {
		case f.v == "":
			return "", "", errors.New(f.what + " is empty")
		case strings.ContainsAny(f.v, "\r\n"):
			return "", "", errors.New(f.what + " must be on one line")
		case strings.IndexFunc(f.v, unicode.IsControl) >= 0:
			return "", "", errors.New(f.what + " must not contain control characters")
		case len(f.v) > maxKeyInput:
			return "", "", errors.New(f.what + " is longer than 64 KB")
		}
	}
	if strings.Contains(id, ":") {
		return "", "", errors.New("the key ID must not contain a colon")
	}
	return id, secret, nil
}

// ShortID shows enough of a key ID to tell keys apart: its first and last
// four characters, or only its last two when it has eight or fewer.
func ShortID(id string) string {
	r := []rune(id)
	if len(r) <= 8 {
		return "…" + string(r[max(len(r)-2, 0):])
	}
	return string(r[:4]) + "…" + string(r[len(r)-4:])
}
