package packages

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var envPrefix = []string{"env", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive"}

var changeFlags = []string{"-o", "DPkg::Lock::Timeout=120", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}

// fakeRun records every command line and answers through reply, which gets
// the command without the env prefix.
type fakeRun struct {
	calls [][]string
	reply func(cmd []string) ([]byte, error)
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.reply == nil {
		return nil, nil
	}
	return f.reply(args[2:])
}

func newTestApt(f *fakeRun) *apt {
	return &apt{run: f.run, lookPath: func(string) (string, error) { return "/usr/bin/x", nil }}
}

// fail builds the error system.Runner returns for a command that exited with
// code and printed stderr.
func fail(t *testing.T, code int, stderr string) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("exit %d: %v", code, err)
	}
	return fmt.Errorf("env LC_ALL=C: %w: %s", ee, strings.TrimSpace(stderr))
}

func cmdline(parts ...[]string) []string {
	var res []string
	for _, p := range parts {
		res = append(res, p...)
	}
	return res
}

func TestCommandLines(t *testing.T) {
	cases := []struct {
		name string
		do   func(a *apt) error
		want []string
	}{
		{"refresh", func(a *apt) error { return a.Refresh(context.Background()) },
			cmdline(envPrefix, []string{"apt-get", "update", "-o", "DPkg::Lock::Timeout=120", "-o", "APT::Update::Error-Mode=any"})},
		{"install", func(a *apt) error { return a.Install(context.Background(), []string{"ripgrep", "g++"}) },
			cmdline(envPrefix, []string{"apt-get", "install", "-y", "--no-install-recommends", "--no-remove"}, changeFlags, []string{"--", "ripgrep", "g++"})},
		{"upgrade", func(a *apt) error { return a.Upgrade(context.Background(), []string{"ripgrep"}) },
			cmdline(envPrefix, []string{"apt-get", "install", "-y", "--only-upgrade", "--no-install-recommends", "--no-remove"}, changeFlags, []string{"--", "ripgrep"})},
		{"simulate", func(a *apt) error { _, err := a.UpgradesOthers(context.Background(), []string{"ripgrep"}); return err },
			cmdline(envPrefix, []string{"apt-get", "install", "-y", "-s", "--no-install-recommends", "--no-remove"}, changeFlags, []string{"--", "ripgrep"})},
		{"mark manual", func(a *apt) error { return a.MarkManual(context.Background(), "jq") },
			cmdline(envPrefix, []string{"apt-mark", "manual", "--", "jq"})},
		{"remove", func(a *apt) error { return a.Remove(context.Background(), "jq") },
			cmdline(envPrefix, []string{"dpkg", "--remove", "--", "jq"})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeRun{}
			if err := c.do(newTestApt(f)); err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != 1 || !reflect.DeepEqual(f.calls[0], c.want) {
				t.Fatalf("calls %q\nwant %q", f.calls, c.want)
			}
		})
	}
}

func TestNoNames(t *testing.T) {
	f := &fakeRun{}
	a := newTestApt(f)
	ctx := context.Background()
	if err := a.Install(ctx, nil); !errors.Is(err, ErrNoNames) {
		t.Fatalf("install: %v", err)
	}
	if err := a.Upgrade(ctx, nil); !errors.Is(err, ErrNoNames) {
		t.Fatalf("upgrade: %v", err)
	}
	if _, err := a.UpgradesOthers(ctx, nil); !errors.Is(err, ErrNoNames) {
		t.Fatalf("simulate: %v", err)
	}
	if res, err := a.Info(ctx, nil, true); err != nil || res != nil {
		t.Fatalf("info: %v %v", res, err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("ran %q", f.calls)
	}
}

const queryOut = "base-files\tinstalled\t13ubuntu10.5\t426\trequired\tadmin\tDebian base system miscellaneous files\n" +
	"jq\tinstalled\t1.7.1-3build1\t113\toptional\tutils\tlightweight and flexible command-line JSON processor\n" +
	"libjq1\tinstalled\t1.7.1-3ubuntu0.24.04.2\t359\toptional\tutils\tlightweight and flexible command-line JSON processor - shared library\n" +
	"cron\tconfig-files\t3.0pl1-184ubuntu2\t\timportant\tadmin\tprocess scheduling daemon\n"

const policyOut = `jq:
  Installed: 1.7.1-3build1
  Candidate: 1.7.1-3ubuntu0.24.04.2
  Version table:
     1.7.1-3ubuntu0.24.04.2 500
        500 http://archive.ubuntu.com/ubuntu noble-updates/main amd64 Packages
 *** 1.7.1-3build1 500
        100 /var/lib/dpkg/status
libjq1:
  Installed: 1.7.1-3ubuntu0.24.04.2
  Candidate: 1.7.1-3build1
  Version table:
 *** 1.7.1-3ubuntu0.24.04.2 100
        100 /var/lib/dpkg/status
     1.7.1-3build1 500
        500 http://archive.ubuntu.com/ubuntu noble/main amd64 Packages
ripgrep:
  Installed: (none)
  Candidate: 14.1.0-1
  Version table:
     14.1.0-1 500
        500 http://archive.ubuntu.com/ubuntu noble/universe amd64 Packages
cron:
  Installed: (none)
  Candidate: (none)
  Version table:
`

func infoReply(t *testing.T) func(cmd []string) ([]byte, error) {
	return func(cmd []string) ([]byte, error) {
		switch cmd[0] {
		case "dpkg-query":
			return []byte(queryOut), fail(t, 1, "dpkg-query: no packages found matching ripgrep\ndpkg-query: no packages found matching nosuchpkg")
		case "apt-cache":
			return []byte(policyOut), nil
		case "dpkg":
			if reflect.DeepEqual(cmd, []string{"dpkg", "--compare-versions", "1.7.1-3ubuntu0.24.04.2", "gt", "1.7.1-3build1"}) {
				return nil, nil
			}
			return nil, fail(t, 1, "")
		}
		return nil, fmt.Errorf("unexpected %q", cmd)
	}
}

func TestInfo(t *testing.T) {
	f := &fakeRun{reply: infoReply(t)}
	names := []string{"jq", "libjq1", "ripgrep", "cron", "nosuchpkg", "base-files"}
	got, err := newTestApt(f).Info(context.Background(), names, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{
		{Name: "jq", Version: "1.7.1-3build1", Candidate: "1.7.1-3ubuntu0.24.04.2", Description: "lightweight and flexible command-line JSON processor",
			Priority: "optional", Section: "utils", Installed: true, Upgradable: true, Bytes: 113 * 1024},
		{Name: "libjq1", Version: "1.7.1-3ubuntu0.24.04.2", Candidate: "1.7.1-3build1", Description: "lightweight and flexible command-line JSON processor - shared library",
			Priority: "optional", Section: "utils", Installed: true, Bytes: 359 * 1024},
		{Name: "ripgrep", Candidate: "14.1.0-1"},
		{Name: "cron", Description: "process scheduling daemon", Priority: "important", Section: "admin"},
		{Name: "nosuchpkg"},
		{Name: "base-files", Version: "13ubuntu10.5", Description: "Debian base system miscellaneous files",
			Priority: "required", Section: "admin", Installed: true, Bytes: 426 * 1024},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	wantCalls := [][]string{
		cmdline(envPrefix, []string{"dpkg-query", "-W", "-f=" + queryFormat, "--"}, names),
		cmdline(envPrefix, []string{"apt-cache", "policy", "--"}, names),
		cmdline(envPrefix, []string{"dpkg", "--compare-versions", "1.7.1-3ubuntu0.24.04.2", "gt", "1.7.1-3build1"}),
		cmdline(envPrefix, []string{"dpkg", "--compare-versions", "1.7.1-3build1", "gt", "1.7.1-3ubuntu0.24.04.2"}),
	}
	if !reflect.DeepEqual(f.calls, wantCalls) {
		t.Fatalf("calls %q\nwant %q", f.calls, wantCalls)
	}
}

func TestInfoMatchesCandidatesToTheExactName(t *testing.T) {
	// apt-cache policy reads a name no package has as a regular expression
	// and prints the blocks of the packages it matches.
	f := &fakeRun{reply: func(cmd []string) ([]byte, error) {
		switch cmd[0] {
		case "dpkg-query":
			return nil, fail(t, 1, "dpkg-query: no packages found matching ripgre.\ndpkg-query: no packages found matching ripgrep-")
		case "apt-cache":
			return []byte("ripgrep:\n  Installed: (none)\n  Candidate: 14.1.0-1\n  Version table:\n     14.1.0-1 500\n"), nil
		}
		return nil, fmt.Errorf("unexpected %q", cmd)
	}}
	got, err := newTestApt(f).Info(context.Background(), []string{"ripgre.", "ripgrep-"}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{{Name: "ripgre."}, {Name: "ripgrep-"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestInfoWithoutCandidates(t *testing.T) {
	f := &fakeRun{reply: infoReply(t)}
	got, err := newTestApt(f).Info(context.Background(), []string{"jq"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Candidate != "" || got[0].Upgradable || !got[0].Installed {
		t.Fatalf("got %+v", got)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls %q", f.calls)
	}
}

func TestInfoPrefersInstalledArchitecture(t *testing.T) {
	f := &fakeRun{reply: func([]string) ([]byte, error) {
		return []byte("zlib1g\tinstalled\t1:1.3.dfsg-3.1ubuntu2.2\t164\trequired\tlibs\tcompression library - runtime\n" +
			"zlib1g\tnot-installed\t\t\toptional\tlibs\tcompression library - runtime\n"), nil
	}}
	got, err := newTestApt(f).Info(context.Background(), []string{"zlib1g"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Installed || got[0].Version != "1:1.3.dfsg-3.1ubuntu2.2" {
		t.Fatalf("got %+v", got)
	}
}

func TestInfoFails(t *testing.T) {
	f := &fakeRun{reply: func([]string) ([]byte, error) {
		return nil, fail(t, 2, "dpkg-query: error: --show needs a valid format")
	}}
	_, err := newTestApt(f).Info(context.Background(), []string{"jq"}, false)
	if err == nil || err.Error() != "dpkg-query: error: --show needs a valid format" {
		t.Fatalf("err %v", err)
	}
}

func TestCompareVersionsFails(t *testing.T) {
	f := &fakeRun{reply: func(cmd []string) ([]byte, error) {
		switch cmd[0] {
		case "dpkg-query":
			return []byte("jq\tinstalled\t1.7.1\t113\toptional\tutils\tJSON\n"), nil
		case "apt-cache":
			return []byte("jq:\n  Installed: 1.7.1\n  Candidate: not a version\n"), nil
		}
		return nil, fail(t, 2, "dpkg: error: version 'not a version' has bad syntax: version string has embedded spaces")
	}}
	_, err := newTestApt(f).Info(context.Background(), []string{"jq"}, true)
	if err == nil || !strings.Contains(err.Error(), "has bad syntax") {
		t.Fatalf("err %v", err)
	}
}

func TestSearch(t *testing.T) {
	f := &fakeRun{reply: func(cmd []string) ([]byte, error) {
		switch cmd[0] {
		case "apt-cache":
			return []byte("librip-dev - rip library\nripgrep-all - ripgrep, but also search in PDFs\nrip - rip tool\nripgrep - Recursively searches directories for a regex pattern\n"), nil
		case "dpkg-query":
			return []byte("ripgrep\tinstalled\t14.1.0-1\t5000\toptional\tutils\tRecursively searches directories for a regex pattern\n"), fail(t, 1, "dpkg-query: no packages found matching rip")
		}
		return nil, fmt.Errorf("unexpected %q", cmd)
	}}
	got, err := newTestApt(f).Search(context.Background(), "rip")
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{
		{Name: "rip", Description: "rip tool"},
		{Name: "ripgrep", Version: "14.1.0-1", Description: "Recursively searches directories for a regex pattern", Priority: "optional", Section: "utils", Installed: true, Bytes: 5000 * 1024},
		{Name: "ripgrep-all", Description: "ripgrep, but also search in PDFs"},
		{Name: "librip-dev", Description: "rip library"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if want := cmdline(envPrefix, []string{"apt-cache", "search", "--names-only", "--", "rip"}); !reflect.DeepEqual(f.calls[0], want) {
		t.Fatalf("search call %q", f.calls[0])
	}
	if want := cmdline(envPrefix, []string{"dpkg-query", "-W", "-f=" + queryFormat, "--", "rip", "ripgrep", "ripgrep-all", "librip-dev"}); !reflect.DeepEqual(f.calls[1], want) {
		t.Fatalf("info call %q", f.calls[1])
	}
}

func TestSearchEscapesAndLimits(t *testing.T) {
	var lines strings.Builder
	for i := range 60 {
		fmt.Fprintf(&lines, "g++-%02d - GNU C++ compiler\n", i)
	}
	f := &fakeRun{reply: func(cmd []string) ([]byte, error) {
		if cmd[0] == "apt-cache" {
			return []byte(lines.String()), nil
		}
		return nil, fail(t, 1, "dpkg-query: no packages found matching g++-00")
	}}
	got, err := newTestApt(f).Search(context.Background(), "g++")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 || got[0].Name != "g++-00" || got[49].Name != "g++-49" {
		t.Fatalf("got %d results, first %+v", len(got), got[0])
	}
	if f.calls[0][len(f.calls[0])-1] != `g\+\+` {
		t.Fatalf("pattern %q", f.calls[0])
	}
}

func TestSearchRefusesInvalidTerm(t *testing.T) {
	for _, term := range []string{"r", "Rip", "rip grep", "^rip", ""} {
		f := &fakeRun{}
		if _, err := newTestApt(f).Search(context.Background(), term); !errors.Is(err, ErrInvalidTerm) {
			t.Fatalf("%q: %v", term, err)
		}
		if len(f.calls) != 0 {
			t.Fatalf("%q ran %q", term, f.calls)
		}
	}
}

func TestDetected(t *testing.T) {
	f := &fakeRun{reply: func(cmd []string) ([]byte, error) {
		switch cmd[0] {
		case "apt-mark":
			return []byte("apt\nbase-files\njq\nlibjq1\nlibfoo:i386\nripgrep\nvim\n"), nil
		case "dpkg-query":
			return []byte("apt\tinstalled\t2.7.14\t4000\timportant\tadmin\tcommandline package manager\n" +
				"base-files\tinstalled\t13ubuntu10.5\t426\trequired\tadmin\tDebian base system miscellaneous files\n" +
				"jq\tinstalled\t1.7.1\t113\toptional\tutils\tJSON processor\n" +
				"libjq1\tinstalled\t1.7.1\t359\toptional\tlibs\tJSON processor - shared library\n" +
				"vim\tinstalled\t2:9.1.0016-1ubuntu7\t4000\toptional\teditors\tVi IMproved\n"), nil
		}
		return nil, fmt.Errorf("unexpected %q", cmd)
	}}
	got, err := newTestApt(f).Detected(context.Background(), []string{"ripgrep"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range got {
		names = append(names, i.Name)
	}
	if !slices.Equal(names, []string{"jq", "vim"}) {
		t.Fatalf("detected %q", names)
	}
	if want := cmdline(envPrefix, []string{"dpkg-query", "-W", "-f=" + queryFormat, "--", "apt", "base-files", "jq", "libjq1", "vim"}); !reflect.DeepEqual(f.calls[1], want) {
		t.Fatalf("info call %q", f.calls[1])
	}
}

func TestUpgradesOthers(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"new packages only", "Inst libjq1 (1.7.1-3ubuntu0.24.04.2 Ubuntu:24.04/noble-updates [amd64])\nInst jq (1.7.1-3ubuntu0.24.04.2 Ubuntu:24.04/noble-updates [amd64])\nConf jq (1.7.1-3ubuntu0.24.04.2 Ubuntu:24.04/noble-updates [amd64])\n", false},
		{"upgrades the request", "Inst jq [1.7.1-3build1] (1.7.1-3ubuntu0.24.04.2 Ubuntu:24.04/noble-updates [amd64])\n", false},
		{"upgrades another", "Inst liblzma5 [5.6.1+really5.4.5-1ubuntu0.3] (5.6.1+really5.4.5-1ubuntu0.4 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])\nInst jq (1.7.1-3ubuntu0.24.04.2 Ubuntu:24.04/noble-updates [amd64])\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeRun{reply: func([]string) ([]byte, error) { return []byte(c.out), nil }}
			got, err := newTestApt(f).UpgradesOthers(context.Background(), []string{"jq"})
			if err != nil || got != c.want {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestMessage(t *testing.T) {
	cases := []struct {
		name, stderr, want string
	}{
		{"apt", "E: Packages need to be removed but remove is disabled.",
			"E: Packages need to be removed but remove is disabled."},
		{"apt lock", "E: Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend), is another process using it?",
			"E: Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend), is another process using it?"},
		{"apt warning after error", "E: Unable to locate package nosuchpkg\nW: Some index files failed to download. They have been ignored, or old ones used instead.",
			"E: Unable to locate package nosuchpkg"},
		{"failed source", "E: Failed to fetch http://nonexistent.invalid/debian/dists/stable/InRelease  Could not resolve 'nonexistent.invalid'\nE: Some index files failed to download. They have been ignored, or old ones used instead.",
			"E: Failed to fetch http://nonexistent.invalid/debian/dists/stable/InRelease  Could not resolve 'nonexistent.invalid'"},
		{"dpkg dependents", "dpkg: dependency problems prevent removal of libjq1:amd64:\n jq depends on libjq1 (= 1.7.1-3ubuntu0.24.04.2).\n\ndpkg: error processing package libjq1:amd64 (--remove):\n dependency problems - not removing\nErrors were encountered while processing:\n libjq1:amd64",
			"dependency problems - not removing"},
		{"dpkg lock", "dpkg: error: dpkg frontend lock was locked by another process with pid 247\nNote: removing the lock file is always wrong, can damage the locked area\nand the entire system. See <https://wiki.debian.org/Teams/Dpkg/FAQ#db-lock>.",
			"dpkg: error: dpkg frontend lock was locked by another process with pid 247"},
		{"other", "first\nsecond:", "second"},
		{"no stderr", "", "exit status 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := 1
			if strings.HasPrefix(c.stderr, "E: ") {
				code = 100
			}
			if got := message(fail(t, code, c.stderr)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestRefreshFailsWhenASourceFails(t *testing.T) {
	f := &fakeRun{reply: func([]string) ([]byte, error) {
		return []byte("Err:1 http://nonexistent.invalid/debian stable InRelease\n"), fail(t, 100,
			"E: Failed to fetch http://nonexistent.invalid/debian/dists/stable/InRelease  Could not resolve 'nonexistent.invalid'\nE: Some index files failed to download. They have been ignored, or old ones used instead.")
	}}
	err := newTestApt(f).Refresh(context.Background())
	if err == nil || err.Error() != "E: Failed to fetch http://nonexistent.invalid/debian/dists/stable/InRelease  Could not resolve 'nonexistent.invalid'" {
		t.Fatalf("err %v", err)
	}
}

func TestFailureMessage(t *testing.T) {
	f := &fakeRun{reply: func([]string) ([]byte, error) {
		return nil, fail(t, 100, "E: Packages need to be removed but remove is disabled.")
	}}
	err := newTestApt(f).Install(context.Background(), []string{"chrony"})
	if err == nil || err.Error() != "E: Packages need to be removed but remove is disabled." {
		t.Fatalf("err %v", err)
	}
}
