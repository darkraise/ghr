package daemon

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func TestListenWeb(t *testing.T) {
	file := func(v string) string {
		p := filepath.Join(t.TempDir(), "web-listen")
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	also, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer also.Close()
	busy, busy2 := taken.Addr().String(), also.Addr().String()

	free := freeAddr(t)
	ln, served, failed := listenWeb(free, "")
	if ln == nil || served != free || failed != nil {
		t.Fatalf("a free address: %v %q %+v", ln, served, failed)
	}
	ln.Close()

	fallback := freeAddr(t)
	ln, served, failed = listenWeb(busy, file(" "+fallback+"\n"))
	want := model.WebListenError{Address: busy, Reason: "address already in use", ServingOn: fallback}
	if ln == nil || served != fallback || failed == nil || *failed != want {
		t.Fatalf("the fallback: %v %q %+v", ln, served, failed)
	}
	ln.Close()

	off := model.WebListenError{Address: busy, Reason: "address already in use"}
	for name, path := range map[string]string{
		"no file":             "",
		"the failing address": file(busy),
		"a taken fallback":    file(busy2),
		"not an address":      file("garbage\n"),
		"port zero":           file("127.0.0.1:0"),
		"no port":             file("127.0.0.1:"),
		"a named port":        file("127.0.0.1:http"),
		"a port too high":     file("127.0.0.1:65536"),
		"an empty file":       file(""),
		"a missing file":      filepath.Join(t.TempDir(), "missing"),
		"a directory":         t.TempDir(),
	} {
		ln, served, failed := listenWeb(busy, path)
		if ln != nil || served != "" || failed == nil || *failed != off {
			if ln != nil {
				ln.Close()
			}
			t.Errorf("%s: %v %q %+v", name, ln, served, failed)
		}
	}
}

func TestRecordWebListen(t *testing.T) {
	if recordWebListen("", "0.0.0.0:8080") != nil {
		t.Fatal("an empty path records")
	}
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "web-listen")
	record := recordWebListen(path, "0.0.0.0:8080")
	record()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a write into a missing directory: %v", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	record()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "0.0.0.0:8080\n" {
		t.Fatalf("after a failed write: %q %v", data, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	record()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("written twice in one process: %v", err)
	}
}

func TestRecordWebListenKeepsAFileThatHoldsTheValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-listen")
	if err := os.WriteFile(path, []byte(" 0.0.0.0:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	recordWebListen(path, "0.0.0.0:8080")()
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("the file was rewritten: %v %v", info.ModTime(), err)
	}
}
