package artifactstore

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKeysRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact-store-keys")
	k, err := LoadKeys(path)
	if err != nil || !reflect.DeepEqual(k, Keys{}) {
		t.Fatalf("missing file: %+v %v", k, err)
	}
	at := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)
	want := Keys{Admin: &Key{ID: "ADMINKEY01", Secret: "admin-secret", SetAt: at}}
	if err := SaveKeys(path, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const file = `{"admin":{"id":"ADMINKEY01","secret":"admin-secret","set_at":"2026-10-15T09:30:00Z"}}` + "\n"
	if string(data) != file {
		t.Fatalf("file %q", data)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	want.Job = &Key{ID: "JOBKEY0002", Secret: "job:secret", SetAt: at.Add(time.Hour)}
	if err := SaveKeys(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadKeys(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded %+v %+v %v", got.Admin, got.Job, err)
	}
}

func TestLoadKeysRefusesAFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact-store-keys")
	if err := os.WriteFile(path, []byte("admin=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadKeys(path)
	if err == nil || !strings.Contains(err.Error(), path+" is not a keys file") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadKeysErrorQuotesNoContent(t *testing.T) {
	for name, content := range map[string]string{
		"number secret":     `{"admin":{"id":"ADMINKEY01","secret":918273645}}`,
		"control character": "{\"admin\":{\"id\":\"ADMINKEY01\",\"secret\":\"Qz\x01\"}}",
		"bare word":         "Qz918273645",
		"bad set_at":        `{"admin":{"id":"ADMINKEY01","secret":"s","set_at":"Qz918273645"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifact-store-keys")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadKeys(path)
			if err == nil || !strings.Contains(err.Error(), path+" is not a keys file") {
				t.Fatalf("err %v", err)
			}
			msg := strings.TrimPrefix(err.Error(), path)
			for _, quoted := range []string{"'", "Qz", "918273645", "x01", "\x01"} {
				if strings.Contains(msg, quoted) {
					t.Fatalf("err quotes the file: %q", msg)
				}
			}
		})
	}
}

func TestCheckKeyInput(t *testing.T) {
	long := strings.Repeat("s", 64<<10)
	for _, tc := range []struct {
		name, id, secret   string
		wantID, wantSecret string
		wantErr            string
	}{
		{"trimmed", "  AKIA01 \n", "\tse:cret\r\n", "AKIA01", "se:cret", ""},
		{"64 KB secret", "AKIA01", long, "AKIA01", long, ""},
		{"empty id", " \n", "secret", "", "", "the key ID is empty"},
		{"empty secret", "AKIA01", "", "", "", "the secret is empty"},
		{"id on two lines", "AKIA\n01", "secret", "", "", "the key ID must be on one line"},
		{"secret with CR", "AKIA01", "sec\rret", "", "", "the secret must be on one line"},
		{"secret with a tab", "AKIA01", "sec\tret", "", "", "the secret must not contain control characters"},
		{"id with NUL", "AKIA\x0001", "secret", "", "", "the key ID must not contain control characters"},
		{"secret over 64 KB", "AKIA01", long + "s", "", "", "the secret is longer than 64 KB"},
		{"id over 64 KB", long + "s", "secret", "", "", "the key ID is longer than 64 KB"},
		{"id with a colon", "AKIA:01", "secret", "", "", "the key ID must not contain a colon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, secret, err := CheckKeyInput(tc.id, tc.secret)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || id != tc.wantID || secret != tc.wantSecret {
				t.Fatalf("got %q %q %v", id, secret, err)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	for id, want := range map[string]string{
		"AKIAIOSFODNN7EXAMPLE": "AKIA…MPLE",
		"123456789":            "1234…6789",
		"12345678":             "…78",
		"ab":                   "…ab",
		"a":                    "…a",
		"":                     "…",
	} {
		if got := ShortID(id); got != want {
			t.Errorf("ShortID(%q) = %q, want %q", id, got, want)
		}
	}
}
