package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"

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
