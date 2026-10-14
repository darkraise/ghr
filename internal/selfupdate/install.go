package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/system"
)

const (
	archiveName   = "ghr_linux_amd64.tar.gz"
	checksumsName = "checksums.txt"
	exampleName   = "config.example.yaml"
	smokeTimeout  = 10 * time.Second
)

// hookNames are the job hooks a release carries beside the binary.
var hookNames = []string{"job-started.sh", "job-completed.sh"}

// Installer stages a ghr release and swaps it in for the running one, as
// deploy/setup.sh installs it.
type Installer struct {
	// Base is the repository the release comes from, as for Latest.
	Base string
	// Opt is ghr's folder: the staging folder, ghr.previous and
	// config.example.yaml go there.
	Opt string
	// Hooks is the folder of the job hooks.
	Hooks string
	// Binary is the running binary, resolved through symlinks.
	Binary  string
	Fetch   func(ctx context.Context, url, dst string) error
	Extract func(ctx context.Context, tarball, dir string) error
	Run     system.Runner
}

// Staged is a release downloaded, verified and smoke-tested; its files are
// in Dir/ghr.
type Staged struct {
	Tag string
	Dir string
}

// Discard removes the staging folder.
func (s *Staged) Discard() error { return os.RemoveAll(s.Dir) }

// StepError names the install step that failed.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

// Stage downloads the release tag into Opt/.update-<tag>, checks the
// archive against the release's checksums.txt, extracts it, checks its
// members and runs the new binary's version command. step is called as each
// step begins. Any failure removes the staging folder and is a *StepError.
func (in *Installer) Stage(ctx context.Context, tag string, step func(string)) (*Staged, error) {
	if _, ok := ParseVersion(tag); !ok {
		return nil, &StepError{"download", fmt.Errorf("refusing release tag %q", tag)}
	}
	dir := filepath.Join(in.Opt, ".update-"+tag)
	fail := func(s string, err error) (*Staged, error) {
		os.RemoveAll(dir)
		return nil, &StepError{s, err}
	}
	begin := func(s string) error {
		step(s)
		return ctx.Err()
	}
	if err := begin("download"); err != nil {
		return fail("download", err)
	}
	if err := in.clean(); err != nil {
		return fail("download", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail("download", err)
	}
	from := strings.TrimSuffix(in.Base, "/") + "/releases/download/" + tag + "/"
	tarball, sums := filepath.Join(dir, archiveName), filepath.Join(dir, checksumsName)
	for _, f := range [][2]string{{from + archiveName, tarball}, {from + checksumsName, sums}} {
		if err := in.Fetch(ctx, f[0], f[1]); err != nil {
			return fail("download", err)
		}
	}

	if err := begin("verify"); err != nil {
		return fail("verify", err)
	}
	want, err := checksum(sums, archiveName)
	if err != nil {
		return fail("verify", err)
	}
	got, err := system.FileSHA256(tarball)
	if err != nil {
		return fail("verify", err)
	}
	if got != want {
		return fail("verify", fmt.Errorf("SHA-256 mismatch: %s says %s, the download is %s", checksumsName, want, got))
	}
	if err := in.Extract(ctx, tarball, dir); err != nil {
		return fail("verify", fmt.Errorf("extract: %w", err))
	}
	members := filepath.Join(dir, "ghr")
	for _, name := range append([]string{"ghr"}, hookNames...) {
		if err := regular(members, name, false); err != nil {
			return fail("verify", err)
		}
	}
	if err := regular(members, exampleName, true); err != nil {
		return fail("verify", err)
	}

	if err := begin("smoke"); err != nil {
		return fail("smoke", err)
	}
	sctx, cancel := context.WithTimeout(ctx, smokeTimeout)
	out, err := in.Run(sctx, filepath.Join(members, "ghr"), "version")
	cancel()
	if err != nil {
		return fail("smoke", err)
	}
	if v := strings.TrimSpace(string(out)); v != tag {
		return fail("smoke", fmt.Errorf("the new binary reports version %q, want %s", v, tag))
	}
	return &Staged{Tag: tag, Dir: dir}, nil
}

// targets are the files a swap replaces, in the order it replaces them.
func (in *Installer) targets() []string {
	var out []string
	for _, name := range hookNames {
		out = append(out, filepath.Join(in.Hooks, name))
	}
	return append(out, filepath.Join(in.Opt, exampleName), filepath.Join(in.Opt, "ghr.previous"), in.Binary)
}

// clean removes what an earlier install left: staging folders, and the .new
// files of a swap cut short.
func (in *Installer) clean() error {
	left, err := filepath.Glob(filepath.Join(in.Opt, ".update-*"))
	if err != nil {
		return err
	}
	for _, t := range in.targets() {
		left = append(left, newPath(t))
	}
	for _, p := range left {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("remove leftover %s: %w", p, err)
		}
	}
	return nil
}

// checksum reads name's SHA-256 from a sha256sum listing.
func checksum(path, name string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s lists no %s", checksumsName, name)
}

// regular checks that the archive member name in dir is a regular file, not
// a symlink; an optional member may be missing.
func regular(dir, name string, optional bool) error {
	fi, err := os.Lstat(filepath.Join(dir, name))
	if optional && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the release has no ghr/%s: %w", name, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("the release's ghr/%s is not a regular file", name)
	}
	return nil
}

// Swap installs a staged release: the hooks, the example config when the
// release has one, a copy of the running binary as ghr.previous, then the
// binary. Each file is written beside its target and renamed over it, so
// every target is whole, old or new; the binary goes last, so a failure
// before it leaves the old daemon complete. Swap takes no context: once
// begun it runs to its end.
func (in *Installer) Swap(s *Staged) error {
	members := filepath.Join(s.Dir, "ghr")
	for _, name := range hookNames {
		if err := place(filepath.Join(members, name), filepath.Join(in.Hooks, name), 0o755); err != nil {
			return err
		}
	}
	example := filepath.Join(members, exampleName)
	if _, err := os.Lstat(example); err == nil {
		if err := place(example, filepath.Join(in.Opt, exampleName), 0o644); err != nil {
			return err
		}
	}
	if err := place(in.Binary, filepath.Join(in.Opt, "ghr.previous"), 0o755); err != nil {
		return err
	}
	return place(filepath.Join(members, "ghr"), in.Binary, 0o755)
}

// newPath is the file a swap writes before renaming it over target.
func newPath(target string) string {
	return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".new")
}

// place copies src over dst through newPath(dst), synced before the rename.
func place(src, dst string, mode os.FileMode) error {
	tmp := newPath(dst)
	err := copyFile(src, tmp, mode)
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("install %s: %w", dst, err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// OpenFile's mode is masked by the umask and ignored for a file that exists.
		err = os.Chmod(dst, mode)
	}
	return err
}
