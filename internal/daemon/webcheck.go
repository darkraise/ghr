package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

// checkWeb refuses a web patch that would turn the UI off, that does not
// parse, that would lock out the browser that sent it, or whose address
// would not bind at the next start. It runs inside the store's update, so
// its network calls are made with Store.mu held; they are quick local ones.
func (b *Backend) checkWeb(ctx context.Context, w *model.WebPatch) error {
	browserHost, browser := api.Browser(ctx)
	if w.Listen != nil {
		if err := b.checkListen(ctx, *w.Listen, browser); err != nil {
			return err
		}
	}
	if w.Hosts != nil && browser {
		name := hostOf(browserHost)
		_, ipErr := netip.ParseAddr(name)
		named := ipErr != nil && !strings.EqualFold(name, "localhost")
		if named && !slices.ContainsFunc(*w.Hosts, func(h string) bool { return strings.EqualFold(h, name) }) {
			return api.BadRequest("web.hosts: you are using " + name + "; removing it would lock this browser out")
		}
	}
	return nil
}

func (b *Backend) checkListen(ctx context.Context, listen string, browser bool) error {
	if listen == "" {
		return api.BadRequest("web.listen: the web UI cannot turn itself off; clear it in config.yaml to turn the UI off")
	}
	host, port, ok := splitListen(listen)
	if !ok {
		return api.BadRequest("web.listen: web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080")
	}
	// A browser's request always has its local address; without one the
	// request is checked as one over the socket.
	if local, ok := localIP(ctx); browser && ok {
		if !reachable(host, local) {
			return api.BadRequest("web.listen: you reached ghr on " + local.String() + "; listening on " + host + " would lock this browser out")
		}
	} else if !b.ownHost(host) {
		return api.BadRequest("web.listen: " + host + " is not an address of this host")
	}
	// The running listener holds its own port until the restart releases it.
	if _, held, ok := splitListen(b.WebApplied.Listen); ok && held == port {
		return nil
	}
	if err := b.bind(listen); err != nil {
		return api.BadRequest("web.listen: " + listen + " is not free: " + bindReason(err))
	}
	return nil
}

// splitListen parses a web.listen value as config.Validate does.
func splitListen(listen string) (host string, port int, ok bool) {
	host, p, err := net.SplitHostPort(listen)
	if err != nil {
		return "", 0, false
	}
	port, err = strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

// hostOf is the hostname of a Host header, without its port or brackets.
func hostOf(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

// localIP is the address the request's connection reached, unmapped, so a
// browser on IPv4 that reached a dual-stack listener reads as IPv4.
func localIP(ctx context.Context) (netip.Addr, bool) {
	ta, ok := ctx.Value(http.LocalAddrContextKey).(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	a, ok := netip.AddrFromSlice(ta.IP)
	return a.Unmap(), ok
}

// reachable reports whether a listener on host still accepts a browser whose
// connection reached local.
func reachable(host string, local netip.Addr) bool {
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return local.IsLoopback()
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	switch {
	case a == netip.IPv6Unspecified(), a == local:
		return true
	case a == netip.IPv4Unspecified():
		return local.Is4()
	}
	return a.IsLoopback() && local.IsLoopback()
}

// ownHost reports whether host is a wildcard, localhost, or an address of
// one of this host's interfaces.
func (b *Backend) ownHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	if a.IsUnspecified() {
		return true
	}
	list := b.InterfaceAddrs
	if list == nil {
		list = net.InterfaceAddrs
	}
	addrs, err := list()
	if err != nil {
		log.Printf("web.listen check: %v", err)
		return false
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if own, ok := netip.AddrFromSlice(ip); ok && own.Unmap() == a {
			return true
		}
	}
	return false
}

func (b *Backend) bind(addr string) error {
	if b.Bind != nil {
		return b.Bind(addr)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return l.Close()
}

// bindReason is the operating system's reason, without net.Listen's
// "listen tcp <addr>: bind: " in front; the message names the address.
func bindReason(err error) string {
	var se *os.SyscallError
	if errors.As(err, &se) {
		return se.Err.Error()
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return oe.Err.Error()
	}
	return err.Error()
}
