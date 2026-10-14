//go:build unix

package system

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A cancelled script takes the children it started with it.
func TestExecGroupKillsTheScriptsChildren(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := ExecGroup(ctx, "sh", "-c", "sleep 30 & echo $! > "+pidFile+"; wait"); err == nil {
		t.Fatal("a cancelled script must fail")
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the cancelled script", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A command can read what it gets on stdin and from fd 3 on, with no path
// naming either.
func TestExecGroupIOForPassesStdinAndExtraFiles(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	extra := filepath.Join(t.TempDir(), "extra")
	if err := os.WriteFile(extra, []byte(" and fd 3"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(extra)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, err := ExecGroupIOFor(time.Minute)(context.Background(), strings.NewReader("stdin"), []*os.File{f}, "sh", "-c", "cat && cat <&3")
	if err != nil || string(out) != "stdin and fd 3" {
		t.Fatalf("got %q, %v", out, err)
	}
}
