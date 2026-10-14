package daemon

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

type textAddr string

func (a textAddr) Network() string { return "tcp" }
func (a textAddr) String() string  { return string(a) }

// scriptedConn is a connection with a chosen remote address that counts its
// Close calls. Nothing under test reads or writes it.
type scriptedConn struct {
	net.Conn
	remote textAddr
	closed atomic.Int32
}

func (c *scriptedConn) RemoteAddr() net.Addr { return c.remote }
func (c *scriptedConn) Close() error         { c.closed.Add(1); return nil }

var errScriptClosed = errors.New("scripted listener closed")

// scriptedListener returns from Accept only what the test sends it, so the
// test knows when the listener under test is inside Accept and when it is not.
type scriptedListener struct {
	arrive chan *scriptedConn
	fail   chan error
	closed chan struct{}
	once   sync.Once
	calls  atomic.Int32 // Accept calls, including the one still waiting
}

func newScriptedListener() *scriptedListener {
	return &scriptedListener{arrive: make(chan *scriptedConn), fail: make(chan error), closed: make(chan struct{})}
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	select {
	case c := <-l.arrive:
		return c, nil
	case err := <-l.fail:
		return nil, err
	case <-l.closed:
		return nil, errScriptClosed
	}
}

func (l *scriptedListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *scriptedListener) Addr() net.Addr { return textAddr("192.0.2.1:8080") }

// dial makes a connection from remote arrive at the listener and returns it.
func (l *scriptedListener) dial(remote string) *scriptedConn {
	c := &scriptedConn{remote: textAddr(remote)}
	l.arrive <- c
	return c
}

// acceptLoop calls Accept the way a server does. It sends every connection on
// the first channel and, when Accept fails, the error on the second.
func acceptLoop(ln net.Listener) (<-chan net.Conn, <-chan error) {
	conns, failed := make(chan net.Conn), make(chan error, 1)
	go func() {
		defer close(conns)
		for {
			c, err := ln.Accept()
			if err != nil {
				failed <- err
				return
			}
			conns <- c
		}
	}()
	return conns, failed
}

// endLoop closes ln and takes what its accept loop still holds. A bubble must
// not end with that loop blocked, also when an assertion has failed.
func endLoop(ln net.Listener, conns <-chan net.Conn) {
	ln.Close()
	for range conns {
	}
}

// closeTwiceAtOnce closes c from two goroutines and waits for both.
func closeTwiceAtOnce(c net.Conn) {
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { c.Close() })
	}
	wg.Wait()
}

func TestLimitListenerHoldsTheGlobalCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 2, 2, nil)
		conns, failed := acceptLoop(ln)
		defer endLoop(ln, conns)

		inner.dial("192.0.2.10:5000")
		first := <-conns
		inner.dial("192.0.2.11:5000")
		<-conns
		synctest.Wait()
		if n := inner.calls.Load(); n != 2 {
			t.Fatalf("with 2 connections open the inner Accept was called %d times", n)
		}

		// A connection closed twice, from two goroutines, frees one slot.
		closeTwiceAtOnce(first)
		inner.dial("192.0.2.12:5000")
		<-conns
		synctest.Wait()
		if n := inner.calls.Load(); n != 3 {
			t.Fatalf("after one connection closed the inner Accept was called %d times, want 3", n)
		}

		// Close ends the Accept that is waiting for a slot, with the inner
		// listener's own error.
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-failed; !errors.Is(err, errScriptClosed) {
			t.Fatalf("Accept after Close: %v", err)
		}
	})
}

// A failed Accept of the inner listener is returned as it is and keeps no slot.
func TestLimitListenerFreesTheSlotOfAFailedAccept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 1, 1, nil)
		boom := errors.New("accept: too many open files")
		_, failed := acceptLoop(ln)
		inner.fail <- boom
		if err := <-failed; err != boom {
			t.Fatalf("Accept returned %v", err)
		}
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)
		inner.dial("192.0.2.10:5000")
		<-conns
	})
}

func TestLimitListenerBoundsEachClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 3, 2, nil)
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)
		clients := func() int {
			ln.mu.Lock()
			defer ln.mu.Unlock()
			return len(ln.clients)
		}

		inner.dial("192.0.2.10:5000")
		first := <-conns
		inner.dial("192.0.2.10:5001")
		second := <-conns

		// A third connection of the same client is closed at once and Accept
		// goes on. Only one global slot is left, and the next client gets it,
		// so the refused connection gave its slot back.
		refused := inner.dial("192.0.2.10:5002")
		inner.dial("192.0.2.11:5000")
		other := <-conns
		if n := refused.closed.Load(); n != 1 {
			t.Fatalf("the connection over the client's bound was closed %d times", n)
		}
		if n := clients(); n != 2 {
			t.Fatalf("%d clients counted, want 2", n)
		}

		// A connection closed twice, from two goroutines, gives the client
		// one place back: its next connection is taken, the one after is not.
		closeTwiceAtOnce(first)
		inner.dial("192.0.2.10:5003")
		third := <-conns
		other.Close()
		if n := clients(); n != 1 {
			t.Fatalf("%d clients counted after the second one left, want 1", n)
		}
		again := inner.dial("192.0.2.10:5004")
		synctest.Wait()
		if n := again.closed.Load(); n != 1 {
			t.Fatalf("after one close the client holds 2 again, but its next connection was closed %d times", n)
		}

		// A client with nothing open is forgotten.
		second.Close()
		third.Close()
		if n := clients(); n != 0 {
			t.Fatalf("%d clients still counted with nothing open", n)
		}
	})
}

// A client at its bound that keeps connecting takes nothing from the others:
// every connection it is refused gives its global slot back, so a second
// client still gets all of its own.
func TestLimitListenerRefusalsCostOtherClientsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 4, 2, nil)
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)

		inner.dial("192.0.2.10:5000")
		<-conns
		inner.dial("192.0.2.10:5001")
		<-conns
		hammer := func() {
			t.Helper()
			for port := range 50 {
				c := inner.dial(net.JoinHostPort("192.0.2.10", strconv.Itoa(6000+port)))
				synctest.Wait()
				if n := c.closed.Load(); n != 1 {
					t.Fatalf("refused connection %d was closed %d times", port, n)
				}
			}
		}
		hammer()
		inner.dial("192.0.2.11:5000")
		<-conns
		hammer()
		inner.dial("192.0.2.11:5001")
		<-conns
		// Both clients are at their bound and every global slot is taken, so
		// the inner Accept is not called again: 4 kept, 100 refused.
		synctest.Wait()
		if n := inner.calls.Load(); n != 104 {
			t.Fatalf("the inner Accept was called %d times, want 104", n)
		}
	})
}

// A trusted proxy carries the connections of many clients, so only the
// global cap holds it.
func TestLimitListenerExemptsATrustedProxy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 6, 2, []netip.Prefix{netip.MustParsePrefix("192.0.2.2/32")})
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)

		var proxied []net.Conn
		for _, remote := range []string{"192.0.2.2:5000", "192.0.2.2:5001", "192.0.2.2:5002", "[::ffff:192.0.2.2]:5003"} {
			inner.dial(remote)
			proxied = append(proxied, <-conns)
		}
		ln.mu.Lock()
		counted := len(ln.clients)
		ln.mu.Unlock()
		if counted != 0 {
			t.Fatalf("%d clients counted for the trusted proxy's connections", counted)
		}

		// A neighbour of the proxy is still held to its bound, and the global
		// cap holds the proxy too.
		inner.dial("192.0.2.3:5000")
		<-conns
		inner.dial("192.0.2.3:5001")
		<-conns
		synctest.Wait()
		if n := inner.calls.Load(); n != 6 {
			t.Fatalf("with every slot taken the inner Accept was called %d times, want 6", n)
		}
		for _, c := range proxied {
			c.Close()
		}
		refused := inner.dial("192.0.2.3:5002")
		synctest.Wait()
		if n := refused.closed.Load(); n != 1 {
			t.Fatalf("the neighbour's connection over its bound was closed %d times", n)
		}

		// The proxy's closed connections gave their slots back.
		for port := range 4 {
			inner.dial(net.JoinHostPort("192.0.2.2", strconv.Itoa(6000+port)))
			<-conns
		}
	})
}

func TestClientKey(t *testing.T) {
	for remote, want := range map[string]string{
		"192.0.2.10:5000":                         "192.0.2.10",
		"[::ffff:192.0.2.10]:5000":                "192.0.2.10",
		"[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:5000": "2001:db8:1:2::/64",
		"[2001:db8:1:2::1]:6000":                  "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:6000":                  "2001:db8:1:3::/64",
		"[fe80::1%eth0]:5000":                     "fe80::/64",
		"@":                                       "@",
		"192.0.2.10":                              "192.0.2.10",
	} {
		if got := clientKey(textAddr(remote)); got != want {
			t.Errorf("clientKey(%q) = %q, want %q", remote, got, want)
		}
	}
}
