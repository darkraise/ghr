// Command ghr manages native GitHub Actions runners: `ghr daemon` runs the supervisor,
// every other subcommand talks to it over its Unix socket.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/daemon"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func socketPath() string {
	if s := os.Getenv("GHR_SOCKET"); s != "" {
		return s
	}
	return api.DefaultSocket
}

// newClient and interruptContext are replaced in tests. interruptContext
// returns the context Ctrl-C cancels.
var (
	newClient        = func() *api.Client { return api.NewUnixClient(socketPath()) }
	interruptContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	}
)

// isTerminal and fdIsTerminal are replaced in tests.
var (
	isTerminal   = stdioIsTerminal
	fdIsTerminal = func(fd uintptr) bool { return term.IsTerminal(int(fd)) }
)

// stdioIsTerminal reports whether both stdin and stdout are terminals.
func stdioIsTerminal() bool {
	return fdIsTerminal(os.Stdin.Fd()) && fdIsTerminal(os.Stdout.Fd())
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// Scripts and pipes keep the old usage-and-exit-2 behaviour.
		if !isTerminal() {
			fmt.Fprint(stderr, usage)
			return 2
		}
		args = []string{"status"}
	}
	switch args[0] {
	case "daemon":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := daemon.Run(ctx, daemonOptions()); err != nil {
			fmt.Fprintln(stderr, "ghr daemon:", err)
			return 1
		}
		return 0
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	ctx, stop := interruptContext()
	defer stop()
	if err := cli(ctx, newClient(), args, stdin, stdout, stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "ghr: interrupted")
			return 1
		}
		fmt.Fprintln(stderr, "ghr:", err)
		if _, ok := err.(usageError); ok {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return 1
	}
	return 0
}

// daemonOptions is how ghr daemon runs: the default paths, and this
// binary's version for the ghr update check.
func daemonOptions() daemon.Options {
	o := daemon.DefaultOptions()
	o.Version = version
	return o
}

const usage = `usage: ghr [command]

  (no command)                    same as status, in a terminal
  daemon                          run the supervisor (systemd runs this)
  status                          repos, runners and health
  pause <repo> | resume <repo>    stop/start new runners for a repo
  drain | resume-all              pause/resume every repo
  set mode <queue|all>
  set global-max <n>
  set max <repo> <n>              0 = unlimited
  set warm <repo> <n>
  repo add <name> [--max n] [--label l]... [--allow-public]
  repo rm <name>                  removed once its runners finish
  token set                       set a new PAT (prompts, or reads stdin)
  kill <id>                       stop a runner
  logs <id> [-f]                  runner diagnostic log
  history [--repo r] [--conclusion c] [--limit n]
  update                          ghr and runner versions and their state
  update --check                  run the update checks that are on, wait, then show
  update ghr [--cancel]           install the newest ghr release (or cancel the install)
  update runner [--cancel]        queue (or cancel) a runner update
  runner-update [--cancel]        same as update runner
  jobs                            background tasks and their state
  jobs run <name>                 run a background task now
  jobs set <name> [--every <duration>] [--on | --off]
  storage [refresh]               disk use: Docker, toolchains, package caches
  toolchain list | available <tool>
  toolchain install <tool> <version> | --preset popular
  toolchain rm <tool> <version>   refused while a runner has a job or starts;
                                  idle runners stop meanwhile (needs GitHub)
  cache list | clear <name>       clear is held the same way as toolchain rm
  package list | search <term>    apt packages: managed, setup.sh's base set, others installed
  package install <name>...       install from the host's apt sources, or adopt an installed one
  package upgrade <name>... | --all
                                  held the same way as toolchain rm
  package uninstall <name>        dpkg --remove; held the same way as toolchain rm
  package forget <name>           stop managing a package; it stays installed
  package refresh                 refresh the apt package index
  prune [--scope <scope>]         standard (default), build-cache-keep,
                                  build-cache-all, dangling-images, unused-volumes
  artifact-store                  self-hosted artifact storage: keys, usage, repositories
  artifact-store set-key admin|job
                                  replace a key (prompts, or reads the ID and the
                                  secret as two lines of stdin)
  code                            web UI setup code, until a password is set
  web reset-password              forget the web UI password
  web set-password                set the web UI password (prompts, or reads stdin)
  setup                           first-run state: configured, wizard, owner
  setup github [--owner <owner>]  set the GitHub owner and token (prompts, or reads stdin)
  setup finish [--toolchains popular|none]
  version
`
