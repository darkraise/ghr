package toolchain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/darkraise/ghr/internal/system"
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

// host fakes the commands an Env runs: chown is recorded, runuser runs the
// fake registered for the script it names.
type host struct {
	mu          sync.Mutex
	calls       []string
	scripts     map[string]func(dir string, env map[string]string, args []string) error
	failExtract error
}

func (h *host) run(_ context.Context, name string, args ...string) ([]byte, error) {
	h.mu.Lock()
	h.calls = append(h.calls, name+" "+strings.Join(args, " "))
	h.mu.Unlock()
	switch name {
	case "chown":
		return nil, nil
	case "runuser":
		return nil, h.runAs(args)
	}
	return nil, fmt.Errorf("unexpected command %s", name)
}

// runAs reads the command line Env.runAs builds:
// runuser -u USER -- env -u AGENT_TOOLSDIRECTORY K=V... bash -c CMD DIR SCRIPT ARGS...
func (h *host) runAs(args []string) error {
	e, b := slices.Index(args, "env"), slices.Index(args, "bash")
	if e < 0 || b < e+3 || len(args) < b+5 {
		return fmt.Errorf("unexpected runuser arguments %q", args)
	}
	env := map[string]string{}
	for _, kv := range args[e+3 : b] {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	f, ok := h.scripts[args[b+4]]
	if !ok {
		return fmt.Errorf("no fake for %s", args[b+4])
	}
	return f(args[b+3], env, args[b+5:])
}

func (h *host) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

// fixture is an Env whose sources point at a local HTTP server and whose
// commands go to a fake host.
type fixture struct {
	*Env
	host  *host
	url   string
	mu    sync.Mutex
	files map[string][]byte // by request URI
	hits  map[string]int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{host: &host{scripts: map[string]func(string, map[string]string, []string) error{}},
		files: map[string][]byte{}, hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		b, ok := f.files[r.URL.RequestURI()]
		f.hits[r.URL.RequestURI()]++
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	n := 0
	f.Env = &Env{
		Root: t.TempDir(),
		Home: "/home/ghrunner",
		User: "ghrunner",
		Sources: Sources{
			NodeManifest:   srv.URL + "/node.json",
			GoManifest:     srv.URL + "/go.json",
			PythonManifest: srv.URL + "/python.json",
			GoReleases:     srv.URL + "/godl.json",
			GoDownload:     srv.URL + "/dl/",
			Adoptium:       srv.URL,
			DotnetIndex:    srv.URL + "/dotnet-index.json",
			DotnetScript:   srv.URL + "/dotnet-install.sh",
		},
		Get:   testGet,
		Fetch: system.Download,
		Run:   f.host.run,
		Extract: func(_ context.Context, archive, dir string) error {
			if f.host.failExtract != nil {
				return f.host.failExtract
			}
			return extractTarGz(archive, dir)
		},
		OpID: func() string { n++; return fmt.Sprintf("op%d", n) },
	}
	return f
}

func (f *fixture) serve(uri string, body []byte) {
	f.mu.Lock()
	f.files[uri] = body
	f.mu.Unlock()
}

func (f *fixture) hitCount(uri string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[uri]
}

func testGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// tarGz builds a .tar.gz holding files (path → content), parents implied.
func tarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(files[n])), Typeflag: tar.TypeReg})
		tw.Write([]byte(files[n]))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func extractTarGz(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(h.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o755); err != nil {
			return err
		}
	}
}

func sum256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func tmpEntries(t *testing.T, root string) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}
