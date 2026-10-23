package packages

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"ripgrep", true},
		{"g++", true},
		{"libstdc++6", true},
		{"containerd.io", true},
		{"0ad", true},
		{"python3.12-venv", true},
		{"a", false},
		{"", false},
		{"-o", false},
		{"+x", false},
		{".x", false},
		{"jq;rm", false},
		{"Ripgrep", false},
		{"jq:amd64", false},
		{"jq rm", false},
		{"a" + strings.Repeat("b", 62), true},
		{"a" + strings.Repeat("b", 63), false},
	} {
		if got := ValidName(tc.in); got != tc.want {
			t.Errorf("ValidName(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidTerm(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"rip", true},
		{"rg", true},
		{"-o", true},
		{"g++", true},
		{".n", true},
		{"r", false},
		{"", false},
		{"rip grep", false},
		{"rip*", false},
		{"Rip", false},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
	} {
		if got := ValidTerm(tc.in); got != tc.want {
			t.Errorf("ValidTerm(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAdoptable(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Info
		want bool
	}{
		{"optional", Info{Installed: true, Priority: "optional", Section: "utils"}, true},
		{"extra", Info{Installed: true, Priority: "extra", Section: "universe/utils"}, true},
		{"not installed", Info{Priority: "optional", Section: "utils"}, false},
		{"required", Info{Installed: true, Priority: "required", Section: "admin"}, false},
		{"important", Info{Installed: true, Priority: "important", Section: "admin"}, false},
		{"standard", Info{Installed: true, Priority: "standard", Section: "utils"}, false},
		{"libs", Info{Installed: true, Priority: "optional", Section: "libs"}, false},
		{"oldlibs", Info{Installed: true, Priority: "optional", Section: "oldlibs"}, false},
		{"universe libs", Info{Installed: true, Priority: "optional", Section: "universe/libs"}, false},
		{"libdevel", Info{Installed: true, Priority: "optional", Section: "libdevel"}, true},
	} {
		if got := Adoptable(tc.in); got != tc.want {
			t.Errorf("%s: Adoptable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBaseByRelease(t *testing.T) {
	debian := Base("debian", "13")
	jammy := Base("ubuntu", "22.04")
	if !slices.IsSorted(debian) || !slices.IsSorted(jammy) {
		t.Fatalf("not sorted: %v %v", debian, jammy)
	}
	for _, want := range []string{"jq", "curl", "docker-ce", "libssl3t64", "libreadline8t64"} {
		if !slices.Contains(debian, want) {
			t.Errorf("debian 13 base lacks %s", want)
		}
	}
	for _, want := range []string{"jq", "libssl3", "libreadline8"} {
		if !slices.Contains(jammy, want) {
			t.Errorf("ubuntu 22.04 base lacks %s", want)
		}
	}
	if slices.Contains(jammy, "libssl3t64") {
		t.Error("ubuntu 22.04 base has the time_t renamed libssl3t64")
	}
	if !slices.Equal(Base("ubuntu", "24.04"), debian) || !slices.Equal(Base("", ""), debian) {
		t.Error("releases other than ubuntu 22.04 must use the default array")
	}
	if n := len(debian); n != len(slices.Compact(slices.Clone(debian))) {
		t.Errorf("duplicates in %v", debian)
	}
}

func TestParseOSRelease(t *testing.T) {
	for _, tc := range []struct{ in, id, version string }{
		{"PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nNAME=\"Debian GNU/Linux\"\nVERSION_ID=\"13\"\nID=debian\n", "debian", "13"},
		{"ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"22.04\"\n", "ubuntu", "22.04"},
		{"ID='ubuntu'\nVERSION_ID='24.04'\n", "ubuntu", "24.04"},
		{"# comment\n\nNAME=x\n", "", ""},
	} {
		id, version := ParseOSRelease(tc.in)
		if id != tc.id || version != tc.version {
			t.Errorf("%q: got %q %q, want %q %q", tc.in, id, version, tc.id, tc.version)
		}
	}
}

func TestAvailableNeedsEveryTool(t *testing.T) {
	missing := ""
	a := &apt{lookPath: func(name string) (string, error) {
		if name == missing {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}}
	if !a.Available() {
		t.Fatal("unavailable with every tool on PATH")
	}
	for _, missing = range []string{"apt-get", "apt-cache", "apt-mark", "dpkg", "dpkg-query"} {
		if a.Available() {
			t.Errorf("available without %s", missing)
		}
	}
}

// TestBaseMatchesSetup keeps Base in step with what deploy/setup.sh installs:
// the packages on its apt-get install lines and its PYTHON_RUNTIME_LIBS arrays.
func TestBaseMatchesSetup(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(data), "\\\n", " ")
	var installed []string
	var libs, jammyLibs []string
	array := regexp.MustCompile(`PYTHON_RUNTIME_LIBS=\(([^)]*)\)`)
	for _, line := range strings.Split(script, "\n") {
		if _, after, ok := strings.Cut(line, "apt-get install "); ok {
			for _, tok := range strings.Fields(after) {
				if ValidName(tok) {
					installed = append(installed, tok)
				}
			}
		}
		if m := array.FindStringSubmatch(line); m != nil {
			if strings.HasPrefix(strings.TrimSpace(line), "ubuntu:22.04)") {
				jammyLibs = strings.Fields(m[1])
			} else {
				libs = strings.Fields(m[1])
			}
		}
	}
	if len(installed) == 0 || len(libs) == 0 || len(jammyLibs) == 0 {
		t.Fatalf("setup.sh parse found install %v, libs %v, jammy libs %v", installed, libs, jammyLibs)
	}
	want := func(extra []string) []string {
		out := slices.Concat(installed, extra)
		slices.Sort(out)
		return slices.Compact(out)
	}
	if got, w := Base("debian", "13"), want(libs); !slices.Equal(got, w) {
		t.Errorf("Base(debian, 13) = %v\nsetup.sh installs %v", got, w)
	}
	if got, w := Base("ubuntu", "22.04"), want(jammyLibs); !slices.Equal(got, w) {
		t.Errorf("Base(ubuntu, 22.04) = %v\nsetup.sh installs %v", got, w)
	}
}
