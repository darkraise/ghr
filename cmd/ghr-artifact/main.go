// Command ghr-artifact uploads and downloads GitHub Actions artifacts on the
// S3 store ghr set up for a repository. ghr's upload and download actions run it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/s3"
)

var version = "dev"

// newClient and now are replaced in tests.
var (
	newClient = func(st artifact.Settings) (*s3.Client, error) {
		return s3.New(st.Endpoint, st.Bucket, st.Region, s3.Credentials{AccessKeyID: st.KeyID, SecretAccessKey: st.Secret})
	}
	now = time.Now
)

// GitHub-hosted jobs stop after 6 hours, so no transfer outlives that.
const opTimeout = 6 * time.Hour

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// usageError exits 2; an empty message means flag parsing already printed it.
type usageError string

func (e usageError) Error() string { return string(e) }

func run(args []string, env func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var err error
	switch args[0] {
	case "upload":
		err = upload(ctx, args[1:], env, stdout, stderr)
	case "download":
		err = download(ctx, args[1:], env, stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		err = usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
	var ue usageError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errMissing):
		return 3
	case errors.As(err, &ue):
		if ue != "" {
			fmt.Fprintln(stderr, "ghr-artifact:", ue)
			fmt.Fprint(stderr, usage)
		}
		return 2
	}
	fmt.Fprintln(stderr, "ghr-artifact:", err)
	return 1
}

const settingsEnv = "GHR_ARTIFACT_SETTINGS"

func newFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("ghr-artifact "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	settings := fs.String("settings", "", "ghr's artifact store settings file (default $"+settingsEnv+")")
	return fs, settings
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError("")
	}
	if fs.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	return nil
}

// job is what a command needs from the Actions environment.
type job struct {
	settings  artifact.Settings
	owner     string
	repo      string
	runID     int64
	attempt   int
	workspace string
	temp      string
}

// loadSettings reads the settings file ghr wrote for its runner. Its errors
// name the path but never quote the file, which holds the job key's secret.
func loadSettings(path string) (artifact.Settings, error) {
	if path == "" {
		return artifact.Settings{}, errors.New("no settings file: pass --settings or set GHR_ARTIFACT_SETTINGS; ghr's upload and download actions pass the file ghr wrote for its runner")
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return artifact.Settings{}, fmt.Errorf("ghr's artifact store settings %s do not exist; this job did not run on a ghr runner switched on for self-hosted storage", path)
	}
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return artifact.Settings{}, fmt.Errorf("cannot read ghr's artifact store settings %s: %v", path, err)
	}
	st, err := artifact.ParseSettings(b)
	if err != nil {
		return artifact.Settings{}, fmt.Errorf("ghr's artifact store settings %s: %w", path, err)
	}
	return st, nil
}

func loadJob(env func(string) string, settingsFlag string) (job, error) {
	path := settingsFlag
	if path == "" {
		path = env(settingsEnv)
	}
	st, err := loadSettings(path)
	if err != nil {
		return job{}, err
	}
	j := job{settings: st}
	owner, repo, ok := strings.Cut(env("GITHUB_REPOSITORY"), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return job{}, fmt.Errorf("GITHUB_REPOSITORY %q is not owner/repo; run ghr-artifact in a GitHub Actions job", env("GITHUB_REPOSITORY"))
	}
	j.owner, j.repo = owner, repo
	if j.runID, err = strconv.ParseInt(env("GITHUB_RUN_ID"), 10, 64); err != nil || j.runID <= 0 {
		return job{}, fmt.Errorf("GITHUB_RUN_ID %q is not a run ID; run ghr-artifact in a GitHub Actions job", env("GITHUB_RUN_ID"))
	}
	j.attempt = 1
	if a := env("GITHUB_RUN_ATTEMPT"); a != "" {
		if j.attempt, err = strconv.Atoi(a); err != nil || j.attempt < 1 {
			return job{}, fmt.Errorf("GITHUB_RUN_ATTEMPT %q is not a run attempt", a)
		}
	}
	if j.workspace = env("GITHUB_WORKSPACE"); j.workspace == "" {
		if j.workspace, err = os.Getwd(); err != nil {
			return job{}, err
		}
	}
	if j.temp = env("RUNNER_TEMP"); j.temp == "" {
		j.temp = os.TempDir()
	}
	return j, nil
}

// describe words a store error for the person reading the job log. It never
// includes the key: only the endpoint, the bucket and the store's own message.
func describe(err error, st artifact.Settings) error {
	var se *s3.Error
	if errors.As(err, &se) {
		detail := se.Code
		if se.Message != "" {
			detail += ": " + se.Message
		}
		if detail == "" {
			detail = "HTTP " + strconv.Itoa(se.Status)
		}
		switch {
		case se.Code == "NoSuchBucket":
			return fmt.Errorf("bucket %s does not exist on %s; create it, or fix the bucket in ghr's Settings", st.Bucket, st.Endpoint)
		case se.Status == 403 && se.Code == "":
			return fmt.Errorf("%s answered HTTP 403 without an S3 error; a proxy in front of the store may have blocked the request, such as Cloudflare's challenge for runner addresses", st.Endpoint)
		case se.Status == 403 || se.Code == "AccessDenied" || se.Code == "InvalidAccessKeyId" || se.Code == "SignatureDoesNotMatch":
			return fmt.Errorf("%s refused the job key (%s); check the job key's policy on the store, or replace the job key in ghr", st.Endpoint, detail)
		}
		return fmt.Errorf("%s answered %s", st.Endpoint, detail)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("stopped by a signal, as when the job is cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("gave up after %s", opTimeout)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("cannot reach %s: %w", st.Endpoint, ue.Err)
	}
	return err
}

func writeOutputs(path string, kv ...string) error {
	if path == "" {
		return nil
	}
	var b strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		if strings.ContainsAny(kv[i+1], "\r\n") {
			return fmt.Errorf("output %s contains a line break", kv[i])
		}
		b.WriteString(kv[i] + "=" + kv[i+1] + "\n")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// command escapes a value for a GitHub Actions workflow command.
func command(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

const usage = `usage: ghr-artifact <command> [flags]

  upload --name <name> --path <paths> [--retention-days n] [--if-no-files-found warn|error|ignore]
         [--overwrite] [--compression-level 0-9] [--include-hidden-files]
  download [--name <name> | --pattern <glob>] [--merge-multiple] [--path <dir>] [--run-id <id>]
           exits 3 when nothing matched
  version

upload and download take --settings <file>, the artifact store settings ghr writes for its
runners, which defaults to $GHR_ARTIFACT_SETTINGS. ghr's upload and download actions pass it.
`
