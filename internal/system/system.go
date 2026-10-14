// Package system wraps the host commands ghr drives: systemd, docker, cp/chown and df.
package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Runner executes a command and returns its stdout.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// CommandTimeout bounds every command Exec runs, so a hung docker or systemctl
// cannot block the daemon loop or shutdown.
var CommandTimeout = 10 * time.Minute

// Exec is the real Runner. Errors name only the command and its first argument,
// because full argument lists can carry secrets (the JIT config).
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execute(ctx, CommandTimeout, false, name, args)
}

// ExecGroup is Exec for a command that starts children of its own, such as a
// script running apt: cancelling it kills its whole process group, not only
// the command.
func ExecGroup(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execute(ctx, CommandTimeout, true, name, args)
}

// ExecGroupFor is ExecGroup with its own timeout d in place of
// CommandTimeout, for commands that legitimately run longer, such as a
// toolchain install.
func ExecGroupFor(d time.Duration) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return execute(ctx, d, true, name, args)
	}
}

// IORunner is a Runner for a command that also reads in on stdin and gets
// extra as its open files from fd 3 on.
type IORunner func(ctx context.Context, in io.Reader, extra []*os.File, name string, args ...string) ([]byte, error)

// ExecGroupIOFor is ExecGroupFor as an IORunner.
func ExecGroupIOFor(d time.Duration) IORunner {
	return func(ctx context.Context, in io.Reader, extra []*os.File, name string, args ...string) ([]byte, error) {
		return executeIO(ctx, d, true, in, extra, name, args)
	}
}

func execute(ctx context.Context, timeout time.Duration, group bool, name string, args []string) ([]byte, error) {
	return executeIO(ctx, timeout, group, nil, nil, name, args)
}

func executeIO(ctx context.Context, timeout time.Duration, group bool, in io.Reader, extra []*os.File, name string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = in
	cmd.ExtraFiles = extra
	if group {
		killGroup(cmd)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Killing the command does not close pipes a child process inherited (a
	// docker CLI plugin, say); without WaitDelay, Output would wait on them forever.
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		first := ""
		if len(args) > 0 {
			first = " " + args[0]
		}
		return out, fmt.Errorf("%s%s: %w: %s", name, first, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

type UnitSpec struct {
	Unit    string
	User    string
	WorkDir string
	Props   []string          // e.g. "MemoryMax=6G"
	Env     map[string]string // passed with --setenv
	Command []string
}

type Systemd struct{ Run Runner }

func (s Systemd) Start(ctx context.Context, u UnitSpec) error {
	// Without a description systemd shows the command line, JIT credential
	// included, in list-units and the journal.
	desc := "ghr runner " + strings.TrimPrefix(u.Unit, "ghr-runner-")
	args := []string{"--unit=" + u.Unit, "--description=" + desc, "--uid=" + u.User, "--gid=" + u.User, "--collect", "--quiet"}
	if u.WorkDir != "" {
		args = append(args, "--working-directory="+u.WorkDir)
	}
	for _, p := range u.Props {
		args = append(args, "--property="+p)
	}
	keys := make([]string, 0, len(u.Env))
	for k := range u.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--setenv="+k+"="+u.Env[k])
	}
	args = append(args, "--")
	args = append(args, u.Command...)
	_, err := s.Run(ctx, "systemd-run", args...)
	return err
}

// Stop stops a unit. A unit systemd no longer has is stopped already;
// systemctl answers that with exit status 5 and "not loaded".
func (s Systemd) Stop(ctx context.Context, unit string) error {
	_, err := s.Run(ctx, "systemctl", "stop", unit)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 && strings.Contains(err.Error(), "not loaded") {
		return nil
	}
	return err
}

var (
	activeStates   = map[string]bool{"active": true, "activating": true, "deactivating": true, "reloading": true}
	inactiveStates = map[string]bool{"inactive": true, "failed": true, "unknown": true}
)

// Active reports whether the unit is running. It returns false only when
// systemctl prints a confirmed inactive state ("unknown" is a unit that no
// longer exists); anything else, such as a cancelled context or a bus error,
// is an error so callers never mistake a failed query for an exited runner.
func (s Systemd) Active(ctx context.Context, unit string) (bool, error) {
	out, err := s.Run(ctx, "systemctl", "is-active", unit) // exits 3 when not active
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	state := strings.TrimSpace(string(out))
	// systemctl exits non-zero for inactive units; only a normal exit confirms
	// one, so a killed or failed query is never read as an exited runner.
	var exitErr *exec.ExitError
	confirmed := err == nil || (errors.As(err, &exitErr) && exitErr.Exited())
	switch {
	case activeStates[state]:
		return true, nil
	case inactiveStates[state] && confirmed:
		return false, nil
	case err != nil:
		return false, err
	}
	return false, fmt.Errorf("systemctl is-active %s: unexpected output %q", unit, state)
}

// List returns the active units whose name starts with prefix, without ".service".
func (s Systemd) List(ctx context.Context, prefix string) ([]string, error) {
	out, err := s.Run(ctx, "systemctl", "list-units", "--all", "--plain", "--no-legend", "--type=service", prefix+"*")
	if err != nil {
		return nil, err
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !activeStates[f[2]] {
			continue
		}
		units = append(units, strings.TrimSuffix(f[0], ".service"))
	}
	return units, nil
}

type ComposeContainer struct {
	ID         string
	Project    string
	WorkingDir string
}

type NamedContainer struct {
	ID   string
	Name string
}

type ProjectContainer struct {
	ID    string
	Name  string
	Image string
	State string
}

type Docker struct{ Run Runner }

func lines(out []byte) []string {
	var res []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

func (d Docker) ComposeContainers(ctx context.Context) ([]ComposeContainer, error) {
	out, err := d.Run(ctx, "docker", "ps", "-a", "--no-trunc",
		"--filter", "label=com.docker.compose.project.working_dir",
		"--format", "{{.ID}}\t{{.Label \"com.docker.compose.project\"}}\t{{.Label \"com.docker.compose.project.working_dir\"}}")
	if err != nil {
		return nil, err
	}
	var res []ComposeContainer
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 {
			res = append(res, ComposeContainer{ID: f[0], Project: f[1], WorkingDir: f[2]})
		}
	}
	return res, nil
}

func (d Docker) Containers(ctx context.Context) ([]NamedContainer, error) {
	out, err := d.Run(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}")
	if err != nil {
		return nil, err
	}
	var res []NamedContainer
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 2)
		if len(f) == 2 {
			res = append(res, NamedContainer{ID: f[0], Name: f[1]})
		}
	}
	return res, nil
}

// ProjectContainers lists the containers of one compose project.
func (d Docker) ProjectContainers(ctx context.Context, project string) ([]ProjectContainer, error) {
	out, err := d.Run(ctx, "docker", "ps", "-a", "--no-trunc",
		"--filter", "label=com.docker.compose.project="+project,
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}")
	if err != nil {
		return nil, err
	}
	var res []ProjectContainer
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 4)
		if len(f) == 4 {
			res = append(res, ProjectContainer{ID: f[0], Name: f[1], Image: f[2], State: f[3]})
		}
	}
	return res, nil
}

func (d Docker) ContainerIDsByLabel(ctx context.Context, label string) ([]string, error) {
	out, err := d.Run(ctx, "docker", "ps", "-aq", "--no-trunc", "--filter", "label="+label)
	return lines(out), err
}

func (d Docker) RemoveContainers(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.Run(ctx, "docker", append([]string{"rm", "-f", "-v"}, ids...)...)
	return err
}

func (d Docker) RemoveNetworksByLabel(ctx context.Context, label string) error {
	out, err := d.Run(ctx, "docker", "network", "ls", "-q", "--filter", "label="+label)
	if err != nil || len(lines(out)) == 0 {
		return err
	}
	_, err = d.Run(ctx, "docker", append([]string{"network", "rm"}, lines(out)...)...)
	return err
}

func (d Docker) RemoveVolumesByLabel(ctx context.Context, label string) error {
	out, err := d.Run(ctx, "docker", "volume", "ls", "-q", "--filter", "label="+label)
	if err != nil || len(lines(out)) == 0 {
		return err
	}
	_, err = d.Run(ctx, "docker", append([]string{"volume", "rm", "-f"}, lines(out)...)...)
	return err
}

// DiskUsage is the data-root filesystem's use: the percentage as df rounds
// it, bytes, and the data root path.
type DiskUsage struct {
	Pct   int
	Used  int64
	Total int64
	Root  string
}

// DataRootUsage returns the used percentage of the filesystem holding Docker's data root.
func (d Docker) DataRootUsage(ctx context.Context) (int, error) {
	u, err := d.DataRootBytes(ctx)
	return u.Pct, err
}

// DataRootBytes returns the used percentage, used bytes and size of the
// filesystem holding Docker's data root, and the data root path.
func (d Docker) DataRootBytes(ctx context.Context) (DiskUsage, error) {
	out, err := d.Run(ctx, "docker", "info", "--format", "{{.DockerRootDir}}")
	if err != nil {
		return DiskUsage{}, err
	}
	root := strings.TrimSpace(string(out))
	out, err = d.Run(ctx, "df", "-B1", "--output=pcent,used,size", root)
	if err != nil {
		return DiskUsage{}, err
	}
	l := lines(out)
	var f []string
	if len(l) >= 2 {
		f = strings.Fields(l[len(l)-1])
	}
	if len(f) != 3 {
		return DiskUsage{}, fmt.Errorf("unexpected df output %q", out)
	}
	pct, err1 := strconv.Atoi(strings.TrimSuffix(f[0], "%"))
	used, err2 := strconv.ParseInt(f[1], 10, 64)
	total, err3 := strconv.ParseInt(f[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return DiskUsage{}, fmt.Errorf("unexpected df output %q", out)
	}
	return DiskUsage{Pct: pct, Used: used, Total: total, Root: root}, nil
}

func (d Docker) PruneBuildCacheOlderThan(ctx context.Context, hours int) (string, error) {
	out, err := d.Run(ctx, "docker", "builder", "prune", "-f", "--filter", fmt.Sprintf("until=%dh", hours))
	return reclaimed(out), err
}

// PruneBuildCacheTo prunes the build cache down to keep (e.g. "20GB"). The
// flag name differs between CLI versions and between the classic builder and
// buildx (which "docker builder" may alias), so it is read from the installed
// command's help: --reserved-space when offered, else --keep-storage.
func (d Docker) PruneBuildCacheTo(ctx context.Context, keep string) (string, error) {
	help, err := d.Run(ctx, "docker", "builder", "prune", "--help")
	if err != nil {
		return "", err
	}
	flag := "--keep-storage"
	if strings.Contains(string(help), "--reserved-space") {
		flag = "--reserved-space"
	}
	out, err := d.Run(ctx, "docker", "builder", "prune", "-f", flag, keep)
	return reclaimed(out), err
}

func (d Docker) PruneDanglingImages(ctx context.Context) (string, error) {
	out, err := d.Run(ctx, "docker", "image", "prune", "-f")
	return reclaimed(out), err
}

func reclaimed(out []byte) string {
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "Total reclaimed space:") || strings.HasPrefix(l, "Total:") {
			return strings.TrimSpace(l[strings.Index(l, ":")+1:])
		}
	}
	return "0B"
}

type Host struct {
	Run Runner
	// Script runs scripts that start children of their own; nil uses Run.
	Script Runner
}

// CopyTree copies src to dst (which must not exist), preserving modes and symlinks.
func (h Host) CopyTree(ctx context.Context, src, dst string) error {
	_, err := h.Run(ctx, "cp", "-a", src, dst)
	return err
}

func (h Host) ChownR(ctx context.Context, path, user string) error {
	_, err := h.Run(ctx, "chown", "-R", "-h", user+":"+user, path)
	return err
}

// Extract unpacks a .tar.gz into dir. Run by root, tar restores the archive's
// owner ids unless told not to; the runner tarball's are 1001, which may be a
// real user on this host, so the files are made root's.
func (h Host) Extract(ctx context.Context, tarball, dir string) error {
	_, err := h.Run(ctx, "tar", "--no-same-owner", "-xzf", tarball, "-C", dir)
	return err
}

// RunScript runs the script at path inside dir, as setup.sh runs
// bin/installdependencies.sh.
func (h Host) RunScript(ctx context.Context, dir, path string) error {
	run := h.Script
	if run == nil {
		run = h.Run
	}
	_, err := run(ctx, filepath.Join(dir, path))
	return err
}

// ReadLink resolves path through every symlink.
func (Host) ReadLink(path string) (string, error) { return filepath.EvalSymlinks(path) }

// SwitchLink points the symlink at path to target with one rename, so path
// never goes missing; a leftover path+".tmp" is replaced.
func (Host) SwitchLink(target, path string) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DownloadTimeout bounds one Download.
var DownloadTimeout = 10 * time.Minute

// HTTPSRedirect is the redirect rule for what the daemon unpacks or runs: a
// redirect to anything but https is refused, so the body cannot be swapped on
// the way. It keeps http.Client's own limit of ten redirects, which setting a
// rule replaces.
func HTTPSRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing a redirect to %s://%s", req.URL.Scheme, req.URL.Host)
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

// maxDownload caps one download; the largest archives ghr fetches, a .NET
// SDK or a JDK, are a few hundred megabytes.
var maxDownload int64 = 2 << 30

var downloadClient = &http.Client{CheckRedirect: HTTPSRedirect}

// Download saves url to dst, following redirects to https URLs. It sends no
// credentials: it fetches public release assets.
func Download(ctx context.Context, url, dst string) error {
	return download(ctx, DownloadTimeout, url, dst)
}

// DownloadFor is Download with its own timeout d in place of
// DownloadTimeout, writing to w, which the caller has opened.
func DownloadFor(d time.Duration) func(ctx context.Context, url string, w io.Writer) error {
	return func(ctx context.Context, url string, w io.Writer) error {
		return fetchTo(ctx, d, url, func() (io.Writer, error) { return w, nil })
	}
}

func download(ctx context.Context, timeout time.Duration, url, dst string) error {
	var f *os.File
	err := fetchTo(ctx, timeout, url, func() (io.Writer, error) {
		var err error
		f, err = os.Create(dst)
		return f, err
	})
	if f != nil {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

// fetchTo copies url's body to what open returns, calling open only once
// the response is known to be one to keep.
func fetchTo(ctx context.Context, timeout time.Duration, url string, open func() (io.Writer, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if resp.ContentLength > maxDownload {
		return fmt.Errorf("GET %s: download is larger than %d bytes", url, maxDownload)
	}
	w, err := open()
	if err != nil {
		return err
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxDownload+1))
	if err == nil && n > maxDownload {
		err = fmt.Errorf("GET %s: download is larger than %d bytes", url, maxDownload)
	}
	return err
}
