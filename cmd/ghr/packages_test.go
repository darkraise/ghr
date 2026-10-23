package main

import (
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func samplePackages(now time.Time) model.Packages {
	at := now.Add(-3 * time.Hour)
	return model.Packages{
		IndexUpdatedAt: &at,
		Managed: []model.PackageInfo{
			{Name: "ripgrep", Version: "14.1.0-1", Candidate: "14.1.1-1", Installed: true, Upgradable: true, Bytes: 5_120_000},
			{Name: "yq", Error: "no installation candidate in the configured apt sources"},
		},
		Base:     []model.PackageInfo{{Name: "curl", Version: "8.14.1-2", Installed: true, Bytes: 500_000}},
		Detected: []model.PackageInfo{{Name: "vim", Version: "2:9.1.1230-2", Installed: true, Bytes: 4_000_000}},
	}
}

func sampleSearch() []model.PackageInfo {
	return []model.PackageInfo{
		{Name: "ripgrep", Installed: true, Managed: true, Description: "Recursively searches directories for a regex pattern"},
		{Name: "ripgrep-all", Description: "ripgrep, but also search in PDFs"},
	}
}

func TestPackageCommands(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		method, path, body string
		out                string
	}{
		{[]string{"package", "install", "ripgrep", "vim"}, "POST", "/packages", `{"names":["ripgrep","vim"]}`,
			"install: ripgrep\nadopt: vim\n" + queuedNote},
		{[]string{"package", "upgrade", "ripgrep", "jq"}, "POST", "/packages/upgrade", `{"names":["ripgrep","jq"]}`, queuedNote},
		{[]string{"package", "upgrade", "--all"}, "POST", "/packages/upgrade", `{"all":true}`, queuedNote},
		{[]string{"package", "uninstall", "ripgrep"}, "DELETE", "/packages/ripgrep?uninstall=1", "", queuedNote},
		{[]string{"package", "forget", "yq"}, "DELETE", "/packages/yq", "", "yq is no longer managed; it stays installed"},
		{[]string{"package", "refresh"}, "POST", "/packages/refresh", "", queuedNote},
	} {
		reqs := fakeDaemon(t)
		code, out, errb := runCLI(t, "", tc.args...)
		if code != 0 {
			t.Errorf("%v: code %d stderr %s", tc.args, code, errb)
			continue
		}
		if len(*reqs) != 1 {
			t.Errorf("%v: %d requests", tc.args, len(*reqs))
			continue
		}
		got := (*reqs)[0]
		if got.method != tc.method || got.path != tc.path || strings.TrimSpace(got.body) != tc.body {
			t.Errorf("%v: request %+v", tc.args, got)
		}
		if strings.TrimSpace(out) != tc.out {
			t.Errorf("%v: output %q", tc.args, out)
		}
	}
}

func TestPackageListAndSearch(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, _ := runCLI(t, "", "package", "list")
	if code != 0 {
		t.Fatalf("package list: code %d", code)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			rows[f[0]] = strings.Join(f, " ")
		}
	}
	for name, want := range map[string]string{
		"ripgrep": "ripgrep 14.1.0-1 14.1.1-1 5.1 MB",
		"yq":      "yq - - - no installation candidate in the configured apt sources",
		"curl":    "curl 8.14.1-2 500.0 kB",
		"vim":     "vim 2:9.1.1230-2 4.0 MB",
	} {
		if rows[name] != want {
			t.Errorf("row %q, want %q in:\n%s", rows[name], want, out)
		}
	}
	for _, want := range []string{"MANAGED", "apt index updated ", "BASE", "DETECTED"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	code, out, _ = runCLI(t, "", "package", "search", "rip")
	if code != 0 || !strings.Contains(out, "managed") || !strings.Contains(out, "ripgrep-all") || !strings.Contains(out, "search in PDFs") {
		t.Fatalf("package search:\n%s", out)
	}
	if got := (*reqs)[1]; got.method != "GET" || got.path != "/packages/search?q=rip" {
		t.Fatalf("search request %+v", got)
	}
}

func TestPackageCommandUsageErrors(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{
		{"package"}, {"package", "bogus"}, {"package", "list", "x"}, {"package", "search"},
		{"package", "install"}, {"package", "install", "-o"}, {"package", "upgrade"},
		{"package", "upgrade", "--all", "ripgrep"}, {"package", "uninstall"}, {"package", "forget"},
		{"package", "refresh", "now"},
	} {
		code, _, errb := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errb, "usage:") {
			t.Errorf("%v: code %d stderr %q", args, code, errb)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("a usage error sent %d requests", len(*reqs))
	}
}
