// Command ghr manages native GitHub Actions runners: `ghr daemon` runs the supervisor,
// every other subcommand talks to it over its Unix socket.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/darkraise/ghr/internal/api"
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

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
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

const usage = `usage: ghr <command>

  daemon                          run the supervisor (systemd runs this)
  tui                             interactive dashboard
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
  version
`
