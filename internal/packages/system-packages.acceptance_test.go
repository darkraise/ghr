package packages_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/packages"
)

// setupScript reads the apt-get install lines and the PYTHON_RUNTIME_LIBS
// arrays of deploy/setup.sh: the names the lines install, and the default and
// the Ubuntu 22.04 arrays.
func setupScript(t *testing.T) (lineNames, defaultLibs, jammyLibs []string) {
	t.Helper()
	raw, err := os.ReadFile("../../deploy/setup.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	arrays := regexp.MustCompile(`PYTHON_RUNTIME_LIBS=\(([^)]*)\)`).FindAllStringSubmatch(text, -1)
	if len(arrays) != 2 {
		t.Fatalf("found %d PYTHON_RUNTIME_LIBS assignments in setup.sh, expected the default and the 22.04 one", len(arrays))
	}
	defaultLibs, jammyLibs = strings.Fields(arrays[0][1]), strings.Fields(arrays[1][1])
	// Only the lines setup.sh runs unconditionally name what it installs.
	logical := strings.ReplaceAll(text, "\\\n", " ")
	for _, line := range strings.Split(logical, "\n") {
		if !strings.Contains(line, "apt-get install") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		f := strings.Fields(line[strings.Index(line, "apt-get install")+len("apt-get install"):])
		for i := 0; i < len(f); i++ {
			switch {
			case f[i] == "-o":
				i++
			case strings.HasPrefix(f[i], "-"):
			case strings.Contains(f[i], "PYTHON_RUNTIME_LIBS"):
			default:
				lineNames = append(lineNames, strings.Trim(f[i], `"'`))
			}
		}
	}
	return lineNames, defaultLibs, jammyLibs
}

func sortedUnique(in ...[]string) []string {
	out := slices.Concat(in...)
	slices.Sort(out)
	return slices.Compact(out)
}

func TestA15_BaseEqualsWhatSetupShInstalls(t *testing.T) {
	lineNames, defaultLibs, jammyLibs := setupScript(t)
	for _, want := range []string{"ca-certificates", "curl", "gnupg", "docker-ce", "docker-buildx-plugin", "git", "jq", "unzip", "build-essential"} {
		if !slices.Contains(lineNames, want) {
			t.Fatalf("the setup.sh parse missed %s: %q", want, lineNames)
		}
	}
	if slices.Equal(defaultLibs, jammyLibs) {
		t.Fatal("setup.sh's two PYTHON_RUNTIME_LIBS arrays are the same")
	}
	for _, tc := range []struct {
		id, version string
		libs        []string
	}{
		{"debian", "13", defaultLibs},
		{"ubuntu", "22.04", jammyLibs},
	} {
		want := sortedUnique(lineNames, tc.libs)
		got := sortedUnique(packages.Base(tc.id, tc.version))
		if !slices.Equal(got, want) {
			t.Errorf("%s %s: Base is %q, setup.sh installs %q", tc.id, tc.version, got, want)
		}
	}
}
