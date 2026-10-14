package selfupdate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const archive = "release archive bytes"

// rig is an installer over a temporary root whose release a fake Fetch,
// Extract and Run serve: the running binary is root/bin/ghr and ghr's
// folder root/opt.
type rig struct {
	in      *Installer
	root    string
	files   map[string]string // the archive's ghr/ members as Extract writes them
	symlink string            // a member Extract writes as a symlink instead
	sums    string            // checksums.txt; empty serves the right one
	version string            // what the staged binary prints for ghr version
	failGet error             // returned by every Fetch
	fetched []string
	steps   []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	g := &rig{root: root, version: "v0.1.26", files: map[string]string{
		"ghr": "new ghr", "job-started.sh": "new started", "job-completed.sh": "new completed", "config.example.yaml": "new example",
	}}
	opt := filepath.Join(root, "opt")
	for _, d := range []string{filepath.Join(opt, "hooks"), filepath.Join(root, "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{
		"bin/ghr": "old ghr", "opt/hooks/job-started.sh": "old started", "opt/hooks/job-completed.sh": "old completed",
	} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	g.in = &Installer{
		Base: DefaultBase, Opt: opt, Hooks: filepath.Join(opt, "hooks"), Binary: filepath.Join(root, "bin", "ghr"),
		Fetch: func(ctx context.Context, url, dst string) error {
			g.fetched = append(g.fetched, url)
			if g.failGet != nil {
				return g.failGet
			}
			switch path.Base(url) {
			case "ghr_linux_amd64.tar.gz":
				return os.WriteFile(dst, []byte(archive), 0o644)
			case "checksums.txt":
				sums := g.sums
				if sums == "" {
					sums = fmt.Sprintf("%x  ghr_linux_amd64.tar.gz\n", sha256.Sum256([]byte(archive)))
				}
				return os.WriteFile(dst, []byte(sums), 0o644)
			}
			return fmt.Errorf("unexpected download %s", url)
		},
		Extract: func(ctx context.Context, tarball, dir string) error {
			if got, err := os.ReadFile(tarball); err != nil || string(got) != archive {
				return fmt.Errorf("extracting %s: %q %v", tarball, got, err)
			}
			members := filepath.Join(dir, "ghr")
			if err := os.MkdirAll(members, 0o755); err != nil {
				return err
			}
			for name, body := range g.files {
				p := filepath.Join(members, name)
				if name == g.symlink {
					if err := os.Symlink(filepath.Join(root, "elsewhere"), p); err != nil {
						return err
					}
					continue
				}
				if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
					return err
				}
			}
			return nil
		},
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if filepath.Base(name) != "ghr" || !slices.Equal(args, []string{"version"}) {
				return nil, fmt.Errorf("unexpected command %s %v", name, args)
			}
			if _, ok := ctx.Deadline(); !ok {
				return nil, errors.New("the smoke test has no timeout")
			}
			return []byte(g.version + "\n"), nil
		},
	}
	return g
}

func (g *rig) stage(t *testing.T, tag string) (*Staged, error) {
	t.Helper()
	g.steps = nil
	return g.in.Stage(context.Background(), tag, func(s string) { g.steps = append(g.steps, s) })
}

func (g *rig) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(g.root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (g *rig) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(g.root, rel))
	return err == nil
}

// leftovers lists staging folders and .new files left in the root.
func (g *rig) leftovers(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{"opt/.update-*", "opt/.*.new", "opt/hooks/.*.new", "bin/.*.new"} {
		m, err := filepath.Glob(filepath.Join(g.root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m...)
	}
	return out
}

func TestStageAndSwap(t *testing.T) {
	g := newRig(t)
	s, err := g.stage(t, "v0.1.26")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.steps, []string{"download", "verify", "smoke"}) {
		t.Fatalf("steps %v", g.steps)
	}
	want := []string{
		"https://github.com/darkraise/ghr/releases/download/v0.1.26/ghr_linux_amd64.tar.gz",
		"https://github.com/darkraise/ghr/releases/download/v0.1.26/checksums.txt",
	}
	if !slices.Equal(g.fetched, want) {
		t.Fatalf("fetched %v", g.fetched)
	}
	if s.Tag != "v0.1.26" || s.Dir != filepath.Join(g.root, "opt", ".update-v0.1.26") {
		t.Fatalf("staged %+v", s)
	}
	if g.read(t, "bin/ghr") != "old ghr" {
		t.Fatal("Stage touched the binary")
	}
	if err := g.in.Swap(s); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"bin/ghr": "new ghr", "opt/ghr.previous": "old ghr", "opt/config.example.yaml": "new example",
		"opt/hooks/job-started.sh": "new started", "opt/hooks/job-completed.sh": "new completed",
	} {
		if got := g.read(t, rel); got != body {
			t.Errorf("%s: %q, want %q", rel, got, body)
		}
	}
	if runtime.GOOS != "windows" {
		for rel, mode := range map[string]os.FileMode{
			"bin/ghr": 0o755, "opt/ghr.previous": 0o755, "opt/config.example.yaml": 0o644, "opt/hooks/job-started.sh": 0o755,
		} {
			if fi, err := os.Stat(filepath.Join(g.root, rel)); err != nil || fi.Mode().Perm() != mode {
				t.Errorf("%s: mode %v %v, want %v", rel, fi.Mode().Perm(), err, mode)
			}
		}
	}
	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
	if l := g.leftovers(t); len(l) != 0 {
		t.Fatalf("left behind %v", l)
	}
}

func TestStageFailures(t *testing.T) {
	for _, c := range []struct {
		name    string
		set     func(g *rig)
		step    string
		want    string
		symlink bool
	}{
		{"download", func(g *rig) { g.failGet = errors.New("GET: 404 Not Found") }, "download", "404 Not Found", false},
		{"checksum mismatch", func(g *rig) { g.sums = strings.Repeat("0", 64) + "  ghr_linux_amd64.tar.gz\n" }, "verify", "SHA-256 mismatch", false},
		{"no checksum line", func(g *rig) { g.sums = strings.Repeat("0", 64) + "  other.tar.gz\n" }, "verify", "checksums.txt lists no ghr_linux_amd64.tar.gz", false},
		{"missing hook", func(g *rig) { delete(g.files, "job-completed.sh") }, "verify", "the release has no ghr/job-completed.sh", false},
		{"symlinked binary", func(g *rig) { g.symlink = "ghr" }, "verify", "ghr/ghr is not a regular file", true},
		{"symlinked example", func(g *rig) { g.symlink = "config.example.yaml" }, "verify", "ghr/config.example.yaml is not a regular file", true},
		{"smoke mismatch", func(g *rig) { g.version = "v0.1.25" }, "smoke", `reports version "v0.1.25"`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.symlink && runtime.GOOS == "windows" {
				t.Skip("symlinks need privileges on Windows")
			}
			g := newRig(t)
			c.set(g)
			_, err := g.stage(t, "v0.1.26")
			var se *StepError
			if !errors.As(err, &se) || se.Step != c.step || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want step %s with %q", err, c.step, c.want)
			}
			if l := g.leftovers(t); len(l) != 0 {
				t.Fatalf("left behind %v", l)
			}
			if g.read(t, "bin/ghr") != "old ghr" || g.exists("opt/ghr.previous") {
				t.Fatal("a failed stage touched the install")
			}
		})
	}
}

func TestStageRemovesLeftovers(t *testing.T) {
	g := newRig(t)
	for _, rel := range []string{"opt/.update-v0.1.20/ghr/ghr", "opt/hooks/.job-started.sh.new", "opt/.config.example.yaml.new", "bin/.ghr.new"} {
		p := filepath.Join(g.root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("left"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := g.stage(t, "v0.1.26")
	if err != nil {
		t.Fatal(err)
	}
	if l := g.leftovers(t); !slices.Equal(l, []string{s.Dir}) {
		t.Fatalf("left behind %v", l)
	}
}

func TestStageRefusesABadTag(t *testing.T) {
	g := newRig(t)
	for _, tag := range []string{"latest", "../x", "v1.2", ""} {
		var se *StepError
		if _, err := g.stage(t, tag); !errors.As(err, &se) || se.Step != "download" {
			t.Errorf("%q: %v", tag, err)
		}
	}
	if len(g.fetched) != 0 {
		t.Fatalf("fetched %v", g.fetched)
	}
}

func TestStageStopsWhenCancelled(t *testing.T) {
	g := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.in.Stage(ctx, "v0.1.26", func(string) {})
	if !errors.Is(err, context.Canceled) || len(g.fetched) != 0 {
		t.Fatalf("err %v, fetched %v", err, g.fetched)
	}
}

func TestSwapStopsBeforeTheBinary(t *testing.T) {
	g := newRig(t)
	s, err := g.stage(t, "v0.1.26")
	if err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(g.root, "opt", "hooks")
	if err := os.RemoveAll(hooks); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.in.Swap(s); err == nil {
		t.Fatal("Swap succeeded without a hooks folder")
	}
	if g.read(t, "bin/ghr") != "old ghr" || g.exists("opt/ghr.previous") {
		t.Fatal("a failed swap reached the binary")
	}
}

func TestReleaseWithoutAnExample(t *testing.T) {
	g := newRig(t)
	delete(g.files, "config.example.yaml")
	s, err := g.stage(t, "v0.1.26")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.in.Swap(s); err != nil {
		t.Fatal(err)
	}
	if g.exists("opt/config.example.yaml") || g.read(t, "bin/ghr") != "new ghr" {
		t.Fatal("a release without an example was installed wrong")
	}
}
