package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

// setupPoll and setupWait bound how long `setup github` waits for the daemon
// to serve once owner and token are saved; tests shorten them.
var (
	setupPoll = time.Second
	setupWait = 60 * time.Second
)

const setupUsage = "usage: ghr setup [github [--owner <owner>] | finish [--toolchains popular|none]]"

func setupCmd(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		st, err := c.SetupState(ctx)
		if err != nil {
			return err
		}
		printSetupState(out, st)
		return nil
	}
	switch args[0] {
	case "github":
		fs := flag.NewFlagSet("setup github", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		owner := fs.String("owner", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 {
			return usageError(setupUsage)
		}
		if *owner == "" {
			st, err := c.SetupState(ctx)
			if err != nil {
				return err
			}
			if st.Owner == "" {
				return usageError("ghr setup github needs --owner <owner>: config.yaml names none")
			}
		}
		token, err := readToken(ctx, stdin, errOut)
		if err != nil {
			return err
		}
		if err := c.SetupGitHub(ctx, *owner, token); err != nil {
			return err
		}
		if err := waitConfigured(ctx, c); err != nil {
			return err
		}
		fmt.Fprintln(out, "GitHub owner and token set; ghr is running")
		return nil
	case "finish":
		fs := flag.NewFlagSet("setup finish", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		toolchains := fs.String("toolchains", "none", "")
		if err := fs.Parse(args[1:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 || (*toolchains != "popular" && *toolchains != "none") {
			return usageError(setupUsage)
		}
		if err := c.SetupFinish(ctx, *toolchains); err != nil {
			return err
		}
		fmt.Fprintln(out, "first-run setup finished")
		return nil
	}
	return usageError(setupUsage)
}

func printSetupState(out io.Writer, st model.SetupState) {
	configured := "no"
	switch {
	case st.Configured:
		configured = "yes"
	case st.Starting:
		configured = "starting"
	}
	wizard := "done"
	if st.SetupPending {
		wizard = "pending"
	}
	fmt.Fprintf(out, "configured: %s\nwizard: %s\nowner: %s\nweb: %s\n", configured, wizard, orDash(st.Owner), orDash(st.WebListen))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// readToken asks once without echo on a terminal, or reads a pipe; the token
// is trimmed either way.
func readToken(ctx context.Context, stdin io.Reader, prompts io.Writer) (string, error) {
	if !stdinIsTerminal() {
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	tok, err := askPassword(ctx, prompts, "GitHub token: ")
	return strings.TrimSpace(tok), err
}

// waitConfigured polls until the full API serves. A connection error counts
// as still starting: systemd restarts a daemon whose start failed.
func waitConfigured(ctx context.Context, c *api.Client) error {
	deadline := time.Now().Add(setupWait)
	for {
		if st, err := c.SetupState(ctx); err == nil && st.Configured {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("owner and token saved, but ghr is still starting; check: journalctl -u ghr -n 50")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(setupPoll):
		}
	}
}
