package daemon

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// fromBrowser is the context of a request the web UI forwarded: the Host
// header the browser sent and the local address its connection reached.
func fromBrowser(host, local string) context.Context {
	ctx := api.WithBrowser(context.Background(), host)
	return context.WithValue(ctx, http.LocalAddrContextKey, net.TCPAddrFromAddrPort(netip.MustParseAddrPort(local)))
}

func ipNet(t *testing.T, cidr string) net.Addr {
	t.Helper()
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip
	return n
}

// webCheckBackend has loopback, a LAN address on IPv4 and IPv6, a docker
// bridge and a second NIC, and a web listener on port 8080. Its binder finds
// port 9090 taken and records every address it is asked for.
func webCheckBackend(t *testing.T) (*Backend, *[]string) {
	t.Helper()
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:8080"}
	addrs := []net.Addr{ipNet(t, "127.0.0.1/8"), ipNet(t, "::1/128"), ipNet(t, "192.168.0.10/24"),
		ipNet(t, "fd00::10/64"), ipNet(t, "172.17.0.1/16"), ipNet(t, "10.0.0.5/24")}
	b.InterfaceAddrs = func() ([]net.Addr, error) { return addrs, nil }
	binds := &[]string{}
	b.Bind = func(addr string) error {
		*binds = append(*binds, addr)
		if strings.HasSuffix(addr, ":9090") {
			return &net.OpError{Op: "listen", Net: "tcp", Err: &os.SyscallError{Syscall: "bind", Err: errors.New("address already in use")}}
		}
		return nil
	}
	return b, binds
}

func TestPatchConfigChecksWebSettings(t *testing.T) {
	lan := fromBrowser("192.168.0.10:8080", "192.168.0.10:8080")
	byName := fromBrowser("ghr.lan:8080", "192.168.0.10:8080")
	v6 := fromBrowser("[fd00::10]:8080", "[fd00::10]:8080")
	mapped := fromBrowser("192.168.0.10:8080", "[::ffff:192.168.0.10]:8080")
	proxy := fromBrowser("ghr.example.com", "127.0.0.1:8080")
	viaLocalhost := fromBrowser("localhost:8080", "127.0.0.1:8080")
	noLocal := api.WithBrowser(context.Background(), "ghr.lan:8080")
	socket := context.Background()
	listen := func(s string) *model.WebPatch { return &model.WebPatch{Listen: &s} }
	hosts := func(h ...string) *model.WebPatch {
		if h == nil {
			h = []string{}
		}
		return &model.WebPatch{Hosts: &h}
	}
	lockout := func(local, host string) string {
		return "web.listen: you reached ghr on " + local + "; listening on " + host + " would lock this browser out"
	}
	const (
		off      = "web.listen: the web UI cannot turn itself off; clear it in config.yaml to turn the UI off"
		shape    = "web.listen: web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080"
		taken    = "web.listen: 0.0.0.0:9090 is not free: address already in use"
		lanUsing = "web.hosts: you are using ghr.lan; removing it would lock this browser out"
	)
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		web   *model.WebPatch
		want  string // "" accepts the patch
		binds []string
	}{
		{"empty listen from a browser", lan, listen(""), off, nil},
		{"empty listen over the socket", socket, listen(""), off, nil},
		{"no port", socket, listen("0.0.0.0"), shape, nil},
		{"port 0", lan, listen("0.0.0.0:0"), shape, nil},
		{"port 70000", socket, listen("0.0.0.0:70000"), shape, nil},
		{"a named port", lan, listen("0.0.0.0:http"), shape, nil},

		{"LAN browser: 0.0.0.0", lan, listen("0.0.0.0:8080"), "", nil},
		{"LAN browser: ::", lan, listen("[::]:8080"), "", nil},
		{"LAN browser: its own address", lan, listen("192.168.0.10:8080"), "", nil},
		{"LAN browser: an empty host", lan, listen(":8080"), "", nil},
		{"LAN browser: the docker bridge", lan, listen("172.17.0.1:8080"), lockout("192.168.0.10", "172.17.0.1"), nil},
		{"LAN browser: another NIC", lan, listen("10.0.0.5:8080"), lockout("192.168.0.10", "10.0.0.5"), nil},
		{"LAN browser: localhost", lan, listen("localhost:8080"), lockout("192.168.0.10", "localhost"), nil},
		{"LAN browser: a loopback address", lan, listen("127.0.0.1:8080"), lockout("192.168.0.10", "127.0.0.1"), nil},

		{"IPv6 browser: 0.0.0.0", v6, listen("0.0.0.0:8080"), lockout("fd00::10", "0.0.0.0"), nil},
		{"IPv6 browser: ::", v6, listen("[::]:8080"), "", nil},
		{"IPv6 browser: its own address", v6, listen("[fd00::10]:8080"), "", nil},
		{"IPv6 browser: an IPv4 address", v6, listen("192.168.0.10:8080"), lockout("fd00::10", "192.168.0.10"), nil},
		{"IPv4-mapped browser: 0.0.0.0", mapped, listen("0.0.0.0:8080"), "", nil},
		{"IPv4-mapped browser: its own address", mapped, listen("192.168.0.10:8080"), "", nil},

		{"loopback browser: localhost", proxy, listen("localhost:8080"), "", nil},
		{"loopback browser: 127.0.0.1", proxy, listen("127.0.0.1:8080"), "", nil},
		{"loopback browser: ::1", proxy, listen("[::1]:8080"), "", nil},
		{"loopback browser: 0.0.0.0", proxy, listen("0.0.0.0:8080"), "", nil},
		{"loopback browser: a LAN address", proxy, listen("192.168.0.10:8080"), lockout("127.0.0.1", "192.168.0.10"), nil},

		{"socket: an own address", socket, listen("10.0.0.5:8080"), "", nil},
		{"socket: an own IPv6 address", socket, listen("[fd00::10]:8080"), "", nil},
		{"socket: 0.0.0.0", socket, listen("0.0.0.0:8080"), "", nil},
		{"socket: ::", socket, listen("[::]:8080"), "", nil},
		{"socket: localhost", socket, listen("localhost:8080"), "", nil},
		{"socket: an empty host", socket, listen(":8080"), "", nil},
		{"socket: a foreign address", socket, listen("203.0.113.7:8080"), "web.listen: 203.0.113.7 is not an address of this host", nil},
		{"socket: a hostname", socket, listen("ghr.lan:8080"), "web.listen: ghr.lan is not an address of this host", nil},
		{"a browser without a local address is checked as the socket", noLocal, listen("203.0.113.7:8080"),
			"web.listen: 203.0.113.7 is not an address of this host", nil},

		{"a free port", lan, listen("0.0.0.0:9091"), "", []string{"0.0.0.0:9091"}},
		{"a taken port", lan, listen("0.0.0.0:9090"), taken, []string{"0.0.0.0:9090"}},
		{"the held port on another address", lan, listen("192.168.0.10:8080"), "", nil},
		{"a taken port over the socket", socket, listen("10.0.0.5:9090"), "web.listen: 10.0.0.5:9090 is not free: address already in use",
			[]string{"10.0.0.5:9090"}},
		{"a lockout is reported before the port", lan, listen("172.17.0.1:9090"), lockout("192.168.0.10", "172.17.0.1"), nil},

		{"hostname browser: its name kept in another case", byName, hosts("other.lan", "GHR.LAN"), "", nil},
		{"hostname browser: its name removed", byName, hosts("other.lan"), lanUsing, nil},
		{"hostname browser: every name removed", byName, hosts(), lanUsing, nil},
		{"hostname browser behind a proxy", proxy, hosts("ghr.lan"),
			"web.hosts: you are using ghr.example.com; removing it would lock this browser out", nil},
		{"IP browser: every name removed", lan, hosts(), "", nil},
		{"IPv6 browser: every name removed", v6, hosts(), "", nil},
		{"localhost browser: every name removed", viaLocalhost, hosts(), "", nil},
		{"socket: every name removed", socket, hosts(), "", nil},
		{"listen is checked before hosts", byName, &model.WebPatch{Listen: listen("172.17.0.1:8080").Listen, Hosts: hosts().Hosts},
			lockout("192.168.0.10", "172.17.0.1"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, binds := webCheckBackend(t)
			before, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			err = b.PatchConfig(tc.ctx, model.ConfigPatch{Web: tc.web})
			if !slices.Equal(*binds, tc.binds) {
				t.Errorf("binds %q, want %q", *binds, tc.binds)
			}
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if w := b.Store.Config().Web; tc.web.Listen != nil && w.Listen != *tc.web.Listen {
					t.Fatalf("listen %q not saved", w.Listen)
				}
				return
			}
			if apiStatus(err) != 400 || err.Error() != tc.want {
				t.Fatalf("err %v (status %d), want 400 %q", err, apiStatus(err), tc.want)
			}
			if msg := err.Error(); !strings.HasPrefix(msg, "web.listen: ") && !strings.HasPrefix(msg, "web.hosts: ") {
				t.Fatalf("%q does not start with its field key", msg)
			}
			after, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("a refused patch changed config.yaml:\n%s", after)
			}
		})
	}
}

// Without the seams the checks list this host's own addresses and bind for
// real: a port held elsewhere is refused, and accepted once it is released.
func TestPatchConfigWebChecksUseTheHost(t *testing.T) {
	b, _, _ := newBackend(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	err = b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &addr}})
	if want := "web.listen: " + addr + " is not free: address already in use"; err == nil || err.Error() != want {
		l.Close()
		t.Fatalf("err %v, want %q", err, want)
	}
	l.Close()
	if err := b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &addr}}); err != nil {
		t.Fatalf("a released port: %v", err)
	}
	foreign := "203.0.113.7:8080"
	err = b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &foreign}})
	if want := "web.listen: 203.0.113.7 is not an address of this host"; err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
}
