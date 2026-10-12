package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/darkraise/ghr/internal/api"
)

func TestVersionAndUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "dev" {
		t.Fatalf("version: exit %d out %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errb); code != 0 || !strings.Contains(out.String(), "usage: ghr") {
		t.Fatalf("help: exit %d", code)
	}
	stubTerminal(t, false)
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("no args: exit %d", code)
	}
}

func TestSubcommandHelpPrintsUsage(t *testing.T) {
	for _, args := range [][]string{{"prune", "-h"}, {"history", "--help"}, {"repo", "add", "x", "-h"}} {
		var out, errb bytes.Buffer
		code := run(args, strings.NewReader(""), &out, &errb)
		if code != 0 || !strings.Contains(out.String(), "usage: ghr") || errb.Len() != 0 {
			t.Errorf("%v: exit %d stdout %d bytes stderr %q", args, code, out.Len(), errb.String())
		}
	}
}

func TestDaemonFailsWithoutConfig(t *testing.T) {
	if _, err := os.Stat("/etc/ghr/config.yaml"); err == nil {
		t.Skip("a real ghr config exists on this machine; not starting a daemon from a test")
	}
	var out, errb bytes.Buffer
	if code := run([]string{"daemon"}, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "ghr daemon:") {
		t.Fatalf("exit %d stderr %q", code, errb.String())
	}
}

// stubTerminal fixes isTerminal for one test and restores it afterwards.
func stubTerminal(t *testing.T, on bool) {
	t.Helper()
	old := isTerminal
	isTerminal = func() bool { return on }
	t.Cleanup(func() { isTerminal = old })
}

// The production detector needs both stdin and stdout on a terminal.
func TestStdioIsTerminalNeedsBoth(t *testing.T) {
	old := fdIsTerminal
	t.Cleanup(func() { fdIsTerminal = old })
	in, out := os.Stdin.Fd(), os.Stdout.Fd()
	for _, tc := range []struct{ stdin, stdout, want bool }{
		{true, true, true}, {true, false, false}, {false, true, false}, {false, false, false},
	} {
		fdIsTerminal = func(fd uintptr) bool {
			switch fd {
			case in:
				return tc.stdin
			case out:
				return tc.stdout
			}
			t.Fatalf("unexpected fd %d", fd)
			return false
		}
		if got := stdioIsTerminal(); got != tc.want {
			t.Fatalf("stdin terminal %v, stdout terminal %v: got %v, want %v", tc.stdin, tc.stdout, got, tc.want)
		}
	}
}

func TestBareGhrRunsStatusOnTerminal(t *testing.T) {
	reqs := fakeDaemon(t)
	stubTerminal(t, true)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 0 || !strings.Contains(out.String(), "mode queue  global 1/3") {
		t.Fatalf("bare ghr: exit %d out %q err %q", code, out.String(), errb.String())
	}
	if last := (*reqs)[len(*reqs)-1]; last.path != "/status" {
		t.Fatalf("request %+v", last)
	}
}

func TestBareGhrReportsAnUnreachableDaemon(t *testing.T) {
	old := newClient
	t.Cleanup(func() { newClient = old })
	newClient = func() *api.Client { return api.NewUnixClient(filepath.Join(t.TempDir(), "missing.sock")) }
	stubTerminal(t, true)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "ghr: ghr daemon unreachable") {
		t.Fatalf("exit %d err %q", code, errb.String())
	}
}

// hungDaemon holds every request for an hour, or until the request's context
// ends, as a transport does. onRequest runs when a request arrives.
type hungDaemon struct{ onRequest func() }

func (d hungDaemon) RoundTrip(r *http.Request) (*http.Response, error) {
	d.onRequest()
	select {
	case <-time.After(time.Hour):
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

// The bubble's clock is fake, so the daemon's hour and the client's limits
// pass at once.
func TestRunNamesWhyACallEndedEarly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		ctrlC  bool
		stderr string
	}{
		{"Ctrl-C", []string{"kill", "a3f9c1"}, true, "ghr: interrupted\n"},
		{"no answer", []string{"status"}, false, "ghr: ghr daemon did not answer within 30s; the request may still be running\n"},
		{"no answer to a kill", []string{"kill", "a3f9c1"}, false, "ghr: ghr daemon did not answer within 100s; the request may still be running\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				oldClient, oldContext := newClient, interruptContext
				t.Cleanup(func() { newClient, interruptContext = oldClient, oldContext })
				interruptContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
				newClient = func() *api.Client {
					return &api.Client{Base: "http://ghr", HTTP: &http.Client{Transport: hungDaemon{func() {
						if tc.ctrlC {
							cancel()
						}
					}}}}
				}
				var out, errb bytes.Buffer
				if code := run(tc.args, strings.NewReader(""), &out, &errb); code != 1 || errb.String() != tc.stderr || out.Len() != 0 {
					t.Fatalf("exit %d stdout %q stderr %q, want exit 1 and stderr %q", code, out.String(), errb.String(), tc.stderr)
				}
			})
		})
	}
}

func TestBareGhrPrintsUsageWithoutTerminal(t *testing.T) {
	stubTerminal(t, false)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage: ghr") || out.Len() != 0 {
		t.Fatalf("exit %d out %q err %q", code, out.String(), errb.String())
	}
}

func TestTuiIsAnUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"tui"}, strings.NewReader(""), &out, &errb); code != 2 || !strings.Contains(errb.String(), "ghr: unknown command tui") {
		t.Fatalf("exit %d err %q", code, errb.String())
	}
	out.Reset()
	run([]string{"help"}, strings.NewReader(""), &out, &errb)
	if strings.Contains(out.String(), "tui") || !strings.Contains(out.String(), "same as status, in a terminal") {
		t.Fatalf("usage:\n%s", out.String())
	}
}
