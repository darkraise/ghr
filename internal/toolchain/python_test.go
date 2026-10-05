package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func pythonManifest(url string) []byte {
	return []byte(fmt.Sprintf(`[
 {"version":"3.14.0","stable":true,"files":[
   {"filename":"python-3.14.0-linux-22.04-x64.tar.gz","arch":"x64","platform":"linux","platform_version":"22.04","download_url":"%[1]s/old.tar.gz"}]},
 {"version":"3.13.7","stable":true,"files":[
   {"filename":"python-3.13.7-linux-24.04-x64-freethreaded.tar.gz","arch":"x64-freethreaded","platform":"linux","platform_version":"24.04","download_url":"%[1]s/ft.tar.gz"},
   {"filename":"python-3.13.7-linux-24.04-x64.tar.gz","arch":"x64","platform":"linux","platform_version":"24.04","download_url":"%[1]s/py-3.13.7.tar.gz"}]}
]`, url))
}

// fakeSetup acts like python-versions' setup.sh: it installs into
// $RUNNER_TOOL_CACHE/Python/<ver>/x64 and writes the marker last.
func fakeSetup(ver string, seen *map[string]string, fail bool) func(string, map[string]string, []string) error {
	return func(dir string, env map[string]string, _ []string) error {
		*seen = env
		if !exists(filepath.Join(dir, "setup.sh")) {
			return errors.New("setup.sh not in the working directory")
		}
		x64 := filepath.Join(env["RUNNER_TOOL_CACHE"], "Python", ver, "x64")
		if err := os.MkdirAll(filepath.Join(x64, "bin"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(x64, "bin", "python3"), nil, 0o755); err != nil {
			return err
		}
		if fail {
			return errors.New("ensurepip failed")
		}
		return os.WriteFile(x64+".complete", nil, 0o644)
	}
}

func pythonFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.serve("/python.json", pythonManifest(f.url))
	f.serve("/py-3.13.7.tar.gz", tarGz(map[string]string{"setup.sh": "#!/bin/bash", "python": "", "lib/libpython3.13.so.1.0": ""}))
	return f
}

func TestPythonResolveUsesUbuntu2404X64Files(t *testing.T) {
	f := pythonFixture(t)
	p := newPython(f.Env)
	rel, err := p.Resolve(context.Background(), "3")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Release{Tool: "python", Version: "3.13.7", Folder: "3.13.7", URL: f.url + "/py-3.13.7.tar.gz"}); rel != want {
		t.Fatalf("resolved %+v", rel)
	}
	if _, err := p.Resolve(context.Background(), "3.14"); err == nil {
		t.Fatal("3.14 has no 24.04 build here and must not resolve")
	}
}

func TestPythonInstallRunsSetupShAsTheRunnerUser(t *testing.T) {
	f := pythonFixture(t)
	var env map[string]string
	f.host.scripts["setup.sh"] = fakeSetup("3.13.7", &env, false)
	p := newPython(f.Env)
	rel, _ := p.Resolve(context.Background(), "3.13")
	var steps []string
	if err := p.Install(context.Background(), rel, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if got := toolCacheFind(f.Root, "Python", "3.13"); got != filepath.Join(f.Root, "Python", "3.13.7", "x64") {
		t.Fatalf("setup-python would find %q", got)
	}
	if got := toolCacheFind(f.Root, "Python", "3.13.7"); got == "" {
		t.Fatal("setup-python with 3.13.7 would find nothing")
	}
	extracted := filepath.Join(f.Root, ".tmp", "op1", "x")
	want := map[string]string{"RUNNER_TOOL_CACHE": f.Root, "LD_LIBRARY_PATH": filepath.Join(extracted, "lib"), "HOME": "/home/ghrunner"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	cmds := f.host.commands()
	chown := slices.Index(cmds, "chown -R -h ghrunner:ghrunner "+filepath.Join(f.Root, ".tmp", "op1"))
	run := slices.IndexFunc(cmds, func(c string) bool {
		return strings.HasPrefix(c, "runuser -u ghrunner -- env -u AGENT_TOOLSDIRECTORY ")
	})
	if chown < 0 || run < 0 || chown > run {
		t.Fatalf("commands %q: the operation directory must go to ghrunner before setup.sh runs without AGENT_TOOLSDIRECTORY", cmds)
	}
	if mk := slices.Index(cmds, "chown -h ghrunner:ghrunner "+filepath.Join(f.Root, "Python")); mk < 0 || mk > run {
		t.Fatalf("commands %q: <Root>/Python must exist, owned by ghrunner, before setup.sh runs", cmds)
	}
	if !slices.Equal(steps, []string{"downloading", "extracting", "running setup.sh"}) {
		t.Fatalf("progress %v", steps)
	}
	if err := p.Install(context.Background(), rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install: %v", err)
	}
}

func TestPythonFailedSetupLeavesNoVersion(t *testing.T) {
	for name, fail := range map[string]bool{"script error": true, "no marker": false} {
		t.Run(name, func(t *testing.T) {
			f := pythonFixture(t)
			var env map[string]string
			setup := fakeSetup("3.13.7", &env, fail)
			if !fail {
				setup = func(dir string, e map[string]string, args []string) error {
					return os.MkdirAll(filepath.Join(e["RUNNER_TOOL_CACHE"], "Python", "3.13.7", "x64"), 0o755)
				}
			}
			f.host.scripts["setup.sh"] = setup
			p := newPython(f.Env)
			rel, _ := p.Resolve(context.Background(), "3.13")
			if err := p.Install(context.Background(), rel, func(string) {}); err == nil {
				t.Fatal("no error")
			}
			if exists(filepath.Join(f.Root, "Python", "3.13.7")) {
				t.Fatal("a failed install left its version folder behind")
			}
			if got := tmpEntries(t, f.Root); len(got) != 0 {
				t.Fatalf(".tmp holds %v", got)
			}
		})
	}
}
