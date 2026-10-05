// Command ghr manages native GitHub Actions runners: `ghr daemon` runs the supervisor,
// every other subcommand talks to it over its Unix socket.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/daemon"
	"github.com/darkraise/ghr/internal/tui"
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

// newClient is replaced in tests.
var newClient = func() *api.Client { return api.NewUnixClient(socketPath()) }

// runTUI, isTerminal and fdIsTerminal are replaced in tests.
var (
	runTUI       = tui.Run
	isTerminal   = stdioIsTerminal
	fdIsTerminal = term.IsTerminal
)

// stdioIsTerminal reports whether both stdin and stdout are terminals.
func stdioIsTerminal() bool {
	return fdIsTerminal(os.Stdin.Fd()) && fdIsTerminal(os.Stdout.Fd())
}

// openTUI runs the dashboard and returns the process exit code.
func openTUI(stderr io.Writer) int {
	if err := runTUI(newClient()); err != nil {
		fmt.Fprintln(stderr, "ghr:", err)
		return 1
	}
	return 0
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// Scripts and pipes keep the old usage-and-exit-2 behaviour.
		if isTerminal() {
			return openTUI(stderr)
		}
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "daemon":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := daemon.Run(ctx, daemon.DefaultOptions()); err != nil {
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
	case "tui":
		return openTUI(stderr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli(ctx, newClient(), args, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "ghr:", err)
		if _, ok := err.(usageError); ok {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return 1
	}
	return 0
}

const usage = `usage: ghr [command]

  (no command)                    open the interactive dashboard
  daemon                          run the supervisor (systemd runs this)
  tui                             same as no command
  status                          repos, runners and health
  pause <repo> | resume <repo>    stop/start new runners for a repo
  drain | resume-all              pause/resume every repo
  set mode <queue|all>
  set global-max <n>
  set max <repo> <n>              0 = unlimited
  set warm <repo> <n>
  repo add <name> [--max n] [--label l]... [--allow-public]
  repo rm <name>                  removed once its runners finish
  token set                       read a new PAT from stdin
  kill <id>                       stop a runner
  logs <id> [-f]                  runner diagnostic log
  history [--repo r] [--conclusion c] [--limit n]
  runner-update [--cancel]        queue (or cancel) a runner update
  version
`
