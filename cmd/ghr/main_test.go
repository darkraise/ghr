package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/tui"
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

// stubTUI replaces runTUI with fn for one test and restores it afterwards.
func stubTUI(t *testing.T, fn func(tui.Client) error) {
	t.Helper()
	old := runTUI
	runTUI = fn
	t.Cleanup(func() { runTUI = old })
}

func TestBareGhrOpensTUIOnTerminal(t *testing.T) {
	calls := 0
	stubTUI(t, func(tui.Client) error { calls++; return nil })
	stubTerminal(t, true)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 0 || calls != 1 {
		t.Fatalf("bare ghr: exit %d, tui calls %d, stderr %q", code, calls, errb.String())
	}
	if code := run([]string{"tui"}, strings.NewReader(""), &out, &errb); code != 0 || calls != 2 {
		t.Fatalf("ghr tui: exit %d, tui calls %d", code, calls)
	}
	stubTUI(t, func(tui.Client) error { return errors.New("terminal gone") })
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "ghr: terminal gone") {
		t.Fatalf("tui error: exit %d, stderr %q", code, errb.String())
	}
}

// The production detector needs both stdin and stdout on a terminal; a
// detector that always says false would never open the TUI.
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

func TestBareGhrPrintsUsageWithoutTerminal(t *testing.T) {
	calls := 0
	stubTUI(t, func(tui.Client) error { calls++; return nil })
	stubTerminal(t, false)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 || calls != 0 || !strings.Contains(errb.String(), "usage: ghr") {
		t.Fatalf("exit %d, tui calls %d, stderr %q", code, calls, errb.String())
	}
}
