package packages

import (
	"cmp"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/system"
)

// CommandTimeout bounds one apt or dpkg command; an install can download
// hundreds of megabytes.
const CommandTimeout = 30 * time.Minute

// changeOpts wait up to 120 s for another frontend's lock, and keep a
// changed config file's local version without asking, as setup.sh does.
var changeOpts = []string{"-o", "DPkg::Lock::Timeout=120", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}

const queryFormat = "${Package}\t${db:Status-Status}\t${Version}\t${Installed-Size}\t${Priority}\t${Section}\t${binary:Summary}\n"

const searchLimit = 50

type apt struct {
	run      system.Runner
	lookPath func(string) (string, error)
}

var _ Manager = (*apt)(nil)

// New returns the Manager that drives apt-get, apt-cache, apt-mark and dpkg
// through run.
func New(run system.Runner) Manager { return &apt{run: run, lookPath: exec.LookPath} }

func (a *apt) Available() bool {
	for _, c := range []string{"apt-get", "apt-cache", "apt-mark", "dpkg", "dpkg-query"} {
		if _, err := a.lookPath(c); err != nil {
			return false
		}
	}
	return true
}

func (a *apt) raw(ctx context.Context, name string, args ...string) ([]byte, error) {
	return a.run(ctx, "env", append([]string{"LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", name}, args...)...)
}

func (a *apt) call(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := a.raw(ctx, name, args...)
	if err != nil {
		return out, errors.New(message(err))
	}
	return out, nil
}

var exitPrefix = regexp.MustCompile(`^exit status \d+: `)

// indexSummary is the line apt-get update ends with after the failed
// fetches it follows, which name the source.
const indexSummary = "E: Some index files failed to download"

// message is the line of a failed command's error that says what went
// wrong. system.Runner puts the command's stderr after
// "env LC_ALL=C: exit status N: ".
func message(err error) string {
	s := strings.TrimPrefix(strings.TrimSpace(err.Error()), "env LC_ALL=C: ")
	s = exitPrefix.ReplaceAllString(s, "")
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if (strings.HasPrefix(l, "E: ") && !strings.HasPrefix(l, indexSummary)) || strings.HasPrefix(l, "dpkg: error") || strings.Contains(l, "dependency problems") {
			return l
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSuffix(strings.TrimSpace(lines[i]), ":"); l != "" {
			return l
		}
	}
	return s
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (a *apt) Info(ctx context.Context, names []string, candidates bool) ([]Info, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out, err := a.raw(ctx, "dpkg-query", append([]string{"-W", "-f=" + queryFormat, "--"}, names...)...)
	// dpkg-query exits 1 when some name is unknown, and still prints the others.
	if err != nil && exitCode(err) != 1 {
		return nil, errors.New(message(err))
	}
	known := map[string]Info{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			continue
		}
		i := Info{Name: f[0], Priority: f[4], Section: f[5], Description: f[6], Installed: f[1] == "installed"}
		if i.Installed {
			i.Version = f[2]
			if kib, err := strconv.ParseInt(f[3], 10, 64); err == nil {
				i.Bytes = kib * 1024
			}
		}
		// A multi-arch package prints one line per architecture dpkg knows.
		if prev, ok := known[i.Name]; ok && prev.Installed {
			continue
		}
		known[i.Name] = i
	}
	res := make([]Info, len(names))
	for k, n := range names {
		res[k] = known[n]
		res[k].Name = n
	}
	if !candidates {
		return res, nil
	}
	cands, err := a.candidates(ctx, names)
	if err != nil {
		return nil, err
	}
	for k := range res {
		i := &res[k]
		i.Candidate = cands[i.Name]
		if i.Installed && i.Candidate != "" && i.Candidate != i.Version {
			if i.Upgradable, err = a.newer(ctx, i.Candidate, i.Version); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// candidates maps each name apt-cache policy knows to its candidate version;
// a name without one is left out.
func (a *apt) candidates(ctx context.Context, names []string) (map[string]string, error) {
	out, err := a.call(ctx, "apt-cache", append([]string{"policy", "--"}, names...)...)
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	name := ""
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" && line[0] != ' ' && strings.HasSuffix(line, ":") {
			name = strings.TrimSuffix(line, ":")
			continue
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Candidate: "); ok && name != "" && v != "(none)" {
			res[name] = v
		}
	}
	return res, nil
}

// newer reports whether version v sorts after than; dpkg exits 1 for false.
func (a *apt) newer(ctx context.Context, v, than string) (bool, error) {
	_, err := a.raw(ctx, "dpkg", "--compare-versions", v, "gt", than)
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1:
		return false, nil
	}
	return false, errors.New(message(err))
}

func (a *apt) Search(ctx context.Context, term string) ([]Info, error) {
	if !ValidTerm(term) {
		return nil, ErrInvalidTerm
	}
	// apt-cache reads the term as a regular expression.
	pattern := strings.NewReplacer(".", `\.`, "+", `\+`).Replace(term)
	out, err := a.call(ctx, "apt-cache", "search", "--names-only", "--", pattern)
	if err != nil {
		return nil, err
	}
	summary := map[string]string{}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		n, s, ok := strings.Cut(line, " - ")
		if _, seen := summary[n]; !ok || seen || !ValidName(n) {
			continue
		}
		summary[n] = s
		names = append(names, n)
	}
	rank := func(n string) int {
		switch {
		case n == term:
			return 0
		case strings.HasPrefix(n, term):
			return 1
		}
		return 2
	}
	slices.SortFunc(names, func(x, y string) int {
		return cmp.Or(cmp.Compare(rank(x), rank(y)), strings.Compare(x, y))
	})
	if len(names) > searchLimit {
		names = names[:searchLimit]
	}
	res, err := a.Info(ctx, names, false)
	if err != nil {
		return nil, err
	}
	for k := range res {
		if res[k].Description == "" {
			res[k].Description = summary[res[k].Name]
		}
	}
	return res, nil
}

func (a *apt) Detected(ctx context.Context, exclude []string) ([]Info, error) {
	out, err := a.call(ctx, "apt-mark", "showmanual")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, n := range strings.Fields(string(out)) {
		if ValidName(n) && !slices.Contains(exclude, n) {
			names = append(names, n)
		}
	}
	infos, err := a.Info(ctx, names, false)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(infos, func(i Info) bool { return !Adoptable(i) }), nil
}

// Refresh fails when any source fails to download; without
// APT::Update::Error-Mode=any, apt-get update exits 0 with only warnings.
func (a *apt) Refresh(ctx context.Context) error {
	_, err := a.call(ctx, "apt-get", "update", "-o", "DPkg::Lock::Timeout=120", "-o", "APT::Update::Error-Mode=any")
	return err
}

// installArgs is the apt-get install command line. --no-remove makes apt
// abort rather than remove any package to satisfy the request.
func installArgs(names []string, upgrade, simulate bool) []string {
	args := []string{"install", "-y"}
	if simulate {
		args = append(args, "-s")
	}
	if upgrade {
		args = append(args, "--only-upgrade")
	}
	args = append(args, "--no-install-recommends", "--no-remove")
	args = append(args, changeOpts...)
	return append(append(args, "--"), names...)
}

func (a *apt) UpgradesOthers(ctx context.Context, names []string) (bool, error) {
	if len(names) == 0 {
		return false, ErrNoNames
	}
	out, err := a.call(ctx, "apt-get", installArgs(names, false, true)...)
	if err != nil {
		return false, err
	}
	// "Inst <name> [<installed version>] (<new version> ...)" upgrades a
	// package; a new package's line has no bracketed version.
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "Inst" && strings.HasPrefix(f[2], "[") && !slices.Contains(names, f[1]) {
			return true, nil
		}
	}
	return false, nil
}

func (a *apt) Install(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return ErrNoNames
	}
	_, err := a.call(ctx, "apt-get", installArgs(names, false, false)...)
	return err
}

func (a *apt) Upgrade(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return ErrNoNames
	}
	_, err := a.call(ctx, "apt-get", installArgs(names, true, false)...)
	return err
}

func (a *apt) MarkManual(ctx context.Context, name string) error {
	_, err := a.call(ctx, "apt-mark", "manual", "--", name)
	return err
}

// Remove uninstalls one package with dpkg, which refuses while another
// installed package depends on it; apt-get remove would remove those too.
func (a *apt) Remove(ctx context.Context, name string) error {
	_, err := a.call(ctx, "dpkg", "--remove", "--", name)
	return err
}
