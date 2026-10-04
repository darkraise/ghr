package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
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
