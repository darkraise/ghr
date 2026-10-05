package toolchain

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// mkInstall creates <root>/<toolDir>/<folder>/x64 holding one file, with the
// .complete marker when complete is true.
func mkInstall(t *testing.T, root, toolDir, folder string, complete bool) string {
	t.Helper()
	dir := filepath.Join(root, toolDir, folder, "x64")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if complete {
		if err := os.WriteFile(dir+".complete", nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// semver is the part of node-semver the ported finders need.
type semver struct {
	nums       [3]int
	pre, build string
}

// parseSemver accepts what node-semver's valid(clean(s)) accepts for the
// folder names in question: three numeric parts without leading zeros, an
// optional -prerelease and an optional +build.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var v semver
	if i := strings.IndexByte(s, '+'); i >= 0 {
		v.build, s = s[i+1:], s[:i]
		if v.build == "" {
			return semver{}, false
		}
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre, s = s[i+1:], s[:i]
		if v.pre == "" {
			return semver{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || p == "" || (len(p) > 1 && p[0] == '0') || n < 0 {
			return semver{}, false
		}
		v.nums[i] = n
	}
	return v, true
}

// satisfies is node-semver's satisfies(v, spec) for an x-range spec such as
// "22", "3.13", "8.0.x" or "1.25.1": a prerelease never satisfies it and
// build metadata is ignored.
func satisfies(v semver, spec string) bool {
	if v.pre != "" {
		return false
	}
	for i, p := range strings.Split(spec, ".") {
		if p == "x" || p == "X" || p == "*" {
			return true
		}
		n, err := strconv.Atoi(p)
		if err != nil || i > 2 || v.nums[i] != n {
			return false
		}
	}
	return true
}

func semverLess(a, b semver) bool {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			return a.nums[i] < b.nums[i]
		}
	}
	return a.pre != "" && b.pre == ""
}

func dirNames(dir string) []string {
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// toolCacheFind ports @actions/tool-cache find(tool, spec, "x64"): an
// explicit version is looked up directly; otherwise the highest complete
// cached version satisfying spec wins. It returns "" when nothing matches.
func toolCacheFind(root, tool, spec string) string {
	if _, ok := parseSemver(spec); ok {
		p := filepath.Join(root, tool, spec, "x64")
		if exists(p) && exists(p+".complete") {
			return p
		}
		return ""
	}
	best, bestName := semver{}, ""
	for _, name := range dirNames(filepath.Join(root, tool)) {
		v, ok := parseSemver(name)
		p := filepath.Join(root, tool, name, "x64")
		if !ok || !exists(p) || !exists(p+".complete") || !satisfies(v, spec) {
			continue
		}
		if bestName == "" || semverLess(best, v) {
			best, bestName = v, name
		}
	}
	if bestName == "" {
		return ""
	}
	return filepath.Join(root, tool, bestName, "x64")
}

// javaFind ports setup-java's findInToolcache for Temurin: tool-cache's
// findAllVersions, then the folder's first "-" turned back into "+" before
// isVersionSatisfies.
func javaFind(root, spec string) string {
	tool := "Java_Temurin-Hotspot_jdk"
	best, bestName := semver{}, ""
	for _, name := range dirNames(filepath.Join(root, tool)) {
		p := filepath.Join(root, tool, name, "x64")
		if _, ok := parseSemver(name); !ok || !exists(p) || !exists(p+".complete") {
			continue
		}
		v, ok := parseSemver(strings.Replace(name, "-", "+", 1))
		if !ok || !satisfies(v, spec) {
			continue
		}
		if bestName == "" || semverLess(best, v) {
			best, bestName = v, name
		}
	}
	if bestName == "" {
		return ""
	}
	return filepath.Join(root, tool, bestName, "x64")
}

// dotnetFind ports setup-dotnet's local SDK scan: an SDK under
// <dotnetDir>/sdk counts when it holds dotnet.dll and satisfies spec.
func dotnetFind(dotnetDir, spec string) string {
	for _, name := range dirNames(filepath.Join(dotnetDir, "sdk")) {
		v, ok := parseSemver(name)
		if ok && satisfies(v, spec) && exists(filepath.Join(dotnetDir, "sdk", name, "dotnet.dll")) {
			return filepath.Join(dotnetDir, "sdk", name)
		}
	}
	return ""
}
