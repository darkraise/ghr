package daemon

import (
	"net"
	"net/netip"
	"slices"
	"sync"
)

const (
	// maxWebConns bounds the connections open on the web listener at once.
	// Every one holds a file descriptor of the process that also starts runners.
	maxWebConns = 256
	// maxWebConnsPerClient bounds one client's share of them, so that one
	// host cannot hold every slot and shut the others out.
	maxWebConnsPerClient = 64
)

// limitListener keeps at most cap(slots) accepted connections open, and at
// most perClient of them from one client. With every slot taken it does not
// call the inner Accept, so further clients wait in the kernel's accept queue.
// A trusted proxy has no per-client bound: it carries every client behind it,
// and bounds them itself.
type limitListener struct {
	net.Listener
	slots     chan struct{}
	perClient int
	trusted   []netip.Prefix
	closed    chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	clients map[string]int // open connections by clientKey; no entry is 0
}

func limitConns(l net.Listener, total, perClient int, trusted []netip.Prefix) *limitListener {
	return &limitListener{
		Listener: l, slots: make(chan struct{}, total), perClient: perClient, trusted: trusted,
		closed: make(chan struct{}), clients: map[string]int{},
	}
}

// Accept returns the next connection of a client that is under its bound. A
// connection over the bound is closed here and is not an error: the HTTP
// server backs off or stops on an error from Accept.
func (l *limitListener) Accept() (net.Conn, error) {
	for {
		select {
		case l.slots <- struct{}{}:
		case <-l.closed:
			// Close closed the inner listener first, so this names its error.
			c, err := l.Listener.Accept()
			if err == nil {
				c.Close()
				err = net.ErrClosed
			}
			return nil, err
		}
		c, err := l.Listener.Accept()
		if err != nil {
			<-l.slots
			return nil, err
		}
		if l.isTrusted(c.RemoteAddr()) {
			return &limitConn{Conn: c, release: func() { <-l.slots }}, nil
		}
		key := clientKey(c.RemoteAddr())
		if !l.admit(key) {
			c.Close()
			<-l.slots
			continue
		}
		return &limitConn{Conn: c, release: func() {
			l.leave(key)
			<-l.slots
		}}, nil
	}
}

func (l *limitListener) isTrusted(remote net.Addr) bool {
	ip, ok := remoteIP(remote)
	return ok && slices.ContainsFunc(l.trusted, func(p netip.Prefix) bool { return p.Contains(ip) })
}

func (l *limitListener) admit(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients[key] >= l.perClient {
		return false
	}
	l.clients[key]++
	return true
}

func (l *limitListener) leave(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients[key]--; l.clients[key] == 0 {
		delete(l.clients, key)
	}
}

// Close also ends an Accept that is waiting for a slot, which closing the
// inner listener alone would leave waiting until a connection closed.
func (l *limitListener) Close() error {
	err := l.Listener.Close()
	l.closeOnce.Do(func() { close(l.closed) })
	return err
}

// limitConn gives its slots back on the first Close. The HTTP server can
// close a connection more than once, and from more than one goroutine.
type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

// clientKey names the host behind a remote address: an IPv4 address as it
// is, also when it arrives mapped into IPv6, and an IPv6 address as its /64,
// because one host can use every address of its /64. An address that does
// not parse counts under its own text.
func clientKey(remote net.Addr) string {
	ip, ok := remoteIP(remote)
	if !ok {
		return remote.String()
	}
	if ip.Is4() {
		return ip.String()
	}
	prefix, _ := ip.Prefix(64)
	return prefix.String()
}

// remoteIP is the address of a remote host:port, unmapped and without a zone.
func remoteIP(remote net.Addr) (netip.Addr, bool) {
	ap, err := netip.ParseAddrPort(remote.String())
	if err != nil {
		return netip.Addr{}, false
	}
	return ap.Addr().Unmap().WithZone(""), true
}
