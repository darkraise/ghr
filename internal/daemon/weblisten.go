package daemon

import (
	"fmt"
	"net"
	"os"
	"strings"

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
