package daemon

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/darkraise/ghr/internal/model"
)

// listenWeb binds the configured web address. When that fails it binds the
// address recorded at path, the last one a browser used, so a bad web.listen
// cannot take the UI away. It returns the listener, nil when no address
// works, the listen string it serves, and the failure to report.
func listenWeb(addr, path string) (net.Listener, string, *model.WebListenError) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, addr, nil
	}
	failed := &model.WebListenError{Address: addr, Reason: bindReason(err)}
	if fallback := readWebListen(path); fallback != "" && fallback != addr {
		if ln, err := net.Listen("tcp", fallback); err == nil {
			failed.ServingOn = fallback
			return ln, fallback, failed
		}
	}
	return nil, "", failed
}

// readWebListen returns the address recorded at path, or "" when there is
// none: no path, a file that cannot be read, or a value config.Validate
// would refuse for web.listen.
func readWebListen(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(data))
	if _, _, ok := splitListen(v); !ok {
		return ""
	}
	return v
}

// webListenEvent is the error event for a web.listen that failed to bind.
func webListenEvent(e *model.WebListenError) string {
	if e.ServingOn != "" {
		return fmt.Sprintf("web.listen %s: %s; serving the web UI on %s", e.Address, e.Reason, e.ServingOn)
	}
	return fmt.Sprintf("web.listen %s: %s; the web UI is off until it is fixed", e.Address, e.Reason)
}

// recordWebListen returns the web handler's onSession: it records listen at
// path once a browser has used it with a session, so the next start can
// fall back to an address known to work. An empty path records nothing.
func recordWebListen(path, listen string) func() {
	if path == "" {
		return nil
	}
	r := &webListenRecorder{path: path, listen: listen}
	return r.record
}

// webListenRecorder writes at most once per process. A failed write is
// tried again at the next request, so the flag is set only on success.
type webListenRecorder struct {
	path, listen string

	mu      sync.Mutex
	written bool
	warned  bool
}

func (r *webListenRecorder) record() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.written {
		return
	}
	if data, err := os.ReadFile(r.path); err == nil && strings.TrimSpace(string(data)) == r.listen {
		r.written = true
		return
	}
	if err := writeWebListen(r.path, r.listen); err != nil {
		if !r.warned {
			log.Printf("record the web address: %v", err)
			r.warned = true
		}
		return
	}
	r.written = true
}

// writeWebListen replaces path atomically. os.CreateTemp creates the file
// with mode 0600.
func writeWebListen(path, listen string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".web-listen-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(listen + "\n"); err != nil {
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
	return os.Rename(tmp, path)
}
