package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// How long the update commands wait, and how often they poll; tests
// shorten them.
var (
	checkWait   = 60 * time.Second
	installWait = 2 * time.Minute
	updatePoll  = time.Second
)

const updateUsage = "usage: ghr update [--check] | ghr update ghr [--cancel] | ghr update runner [--cancel]"

// stepTexts words a ghr install's steps as `ghr update ghr` prints them.
var stepTexts = map[string]string{
	"download": "downloading",
	"verify":   "verifying the checksum and the archive",
	"smoke":    "testing the new binary",
	"waiting":  "waiting for running work to finish",
	"swap":     "installing the files",
	"restart":  "restarting ghr",
}

func updateCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	switch {
	case len(args) == 0:
		st, err := c.Status(ctx)
		if err != nil {
			return err
		}
		printUpdates(out, st)
		return nil
	case len(args) == 1 && args[0] == "--check":
		return checkUpdates(ctx, c, out)
	case args[0] == "runner":
		return runnerUpdate(ctx, c, args[1:], out, "update runner")
	case args[0] == "ghr" && len(args) == 1:
		return installGhr(ctx, c, out)
	case args[0] == "ghr" && len(args) == 2 && args[1] == "--cancel":
		return c.CancelGhrUpdate(ctx)
	}
	return usageError(updateUsage)
}

// runnerUpdate queues, or with --cancel drops, a runner update; form names
// the command in its usage line.
func runnerUpdate(ctx context.Context, c *api.Client, args []string, out io.Writer, form string) error {
	switch {
	case len(args) == 0:
		if err := c.QueueRunnerUpdate(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "runner update queued; it runs when no job is running or queued")
		return nil
	case len(args) == 1 && args[0] == "--cancel":
		return c.CancelRunnerUpdate(ctx)
	}
	return usageError("usage: ghr " + form + " [--cancel]")
}

func printUpdates(out io.Writer, st model.Status) {
	fmt.Fprintln(out, ghrLine(st))
	fmt.Fprintln(out, runnerLine(st))
	if j, ok := findJob(st.Jobs, config.JobRunnerUpdateCheck); ok && !j.Enabled {
		fmt.Fprintln(out, "warning: the runner update check is off: "+config.RunnerCheckOffReason)
	}
}

// ghrLine is the status line about ghr's own version.
func ghrLine(st model.Status) string {
	safe := stdoutLine()
	g := st.GhrUpdate
	s := "ghr " + safe(g.Installed)
	if g.Newer {
		s += " → " + safe(g.Latest)
	}
	switch {
	case g.Installing && g.Step == "waiting" && len(st.Restart.BlockedBy) > 0:
		s += "  installing: waiting for: " + safe(strings.Join(st.Restart.BlockedBy, ", "))
	case g.Installing:
		s += "  installing: " + safe(g.Step)
	case g.RestartPending != "":
		s += "  installed, restart pending"
	case g.Development:
		s += "  development build"
		if g.Latest != "" {
			s += ", newest release " + safe(g.Latest)
		}
	case g.Newer:
		s += "  update available"
	case g.CheckedAt != nil:
		s += "  up to date"
	case g.CheckError == "":
		s += "  checking…"
	}
	if g.CheckedAt != nil && !g.Installing && g.RestartPending == "" {
		s += fmt.Sprintf(" (checked %s ago)", st.Now.Sub(*g.CheckedAt).Round(time.Minute))
	}
	if g.CheckError != "" {
		s += "  last check failed: " + safe(g.CheckError)
	}
	switch g.LastOutcome {
	case "ok":
		s += "  updated"
	case "failed":
		s += "  last install failed: " + safe(g.LastError)
	case "cancelled":
		s += "  last install cancelled"
	}
	return s
}

func findJob(jobs []model.JobStatus, name string) (model.JobStatus, bool) {
	for _, j := range jobs {
		if j.Name == name {
			return j, true
		}
	}
	return model.JobStatus{}, false
}

// checkUpdates runs the update checks that are on, waits until each has run
// since the request, then prints the result.
func checkUpdates(ctx context.Context, c *api.Client, out io.Writer) error {
	before, err := c.Status(ctx)
	if err != nil {
		return err
	}
	var asked []string
	// A check already running or about to run, as both are when the daemon
	// has just started, refuses a second request; waiting for it is as good.
	joined := map[string]bool{}
	for _, name := range []string{config.JobGhrUpdateCheck, config.JobRunnerUpdateCheck} {
		if j, ok := findJob(before.Jobs, name); !ok || !j.Enabled {
			fmt.Fprintf(out, "%s is off; skipped\n", name)
			continue
		}
		if err := c.RunJob(ctx, name); err != nil {
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
				return err
			}
			joined[name] = true
		}
		asked = append(asked, name)
	}
	deadline := time.Now().Add(checkWait)
	for {
		st, err := c.Status(ctx)
		if err != nil {
			return err
		}
		var busy []string
		for _, name := range asked {
			j, _ := findJob(st.Jobs, name)
			if j.Running || j.Pending || (!joined[name] && (j.LastStart == nil || j.LastStart.Before(before.Now))) {
				busy = append(busy, name)
			}
		}
		if len(busy) == 0 {
			printUpdates(out, st)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s still running after %s", strings.Join(busy, ", "), checkWait)
		}
		if err := sleep(ctx, updatePoll); err != nil {
			return err
		}
	}
}

// installGhr starts installing the newest ghr release and follows it until
// the daemon runs it. The daemon goes away while it restarts, and with no work
// to wait for that can fall between two polls, so once the install has
// started a failed status read is the restart, not an error.
func installGhr(ctx context.Context, c *api.Client, out io.Writer) error {
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	tag := st.GhrUpdate.Latest
	if err := c.StartGhrUpdate(ctx); err != nil {
		return err
	}
	fmt.Fprintf(out, "installing ghr %s\n", tag)
	safe := stdoutLine()
	shown := ""
	var restarting time.Time
	for {
		st, err := c.Status(ctx)
		switch {
		case err != nil:
			if restarting.IsZero() {
				restarting = time.Now()
			}
		default:
			g := st.GhrUpdate
			if g.Installed == tag {
				fmt.Fprintf(out, "ghr %s is running\n", tag)
				return nil
			}
			if g.Step != "" && g.Step != shown {
				shown = g.Step
				fmt.Fprintln(out, stepTexts[g.Step])
			}
			if restarting.IsZero() && (g.Step == "swap" || g.Step == "restart" || g.RestartPending != "") {
				restarting = time.Now()
			}
			// Work that started during the swap sends the install back to
			// waiting; the two minutes count from the restart, not the swap.
			if g.Installing && g.Step == "waiting" && g.RestartPending != "" {
				restarting = time.Now()
			}
			if !g.Installing {
				switch g.LastOutcome {
				case "failed":
					return errors.New("ghr update failed: " + safe(g.LastError))
				case "cancelled":
					return errors.New("ghr update cancelled")
				}
			}
		}
		if !restarting.IsZero() && time.Since(restarting) > installWait {
			return errors.New("restart requested; the daemon is not back")
		}
		if err := sleep(ctx, updatePoll); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

const jobsUsage = "usage: ghr jobs | ghr jobs run <name> | ghr jobs set <name> [--every <duration>] [--on | --off]"

func jobsCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	switch {
	case len(args) == 0:
		st, err := c.Status(ctx)
		if err != nil {
			return err
		}
		printJobs(out, st)
		return nil
	case args[0] == "run" && len(args) == 2:
		return c.RunJob(ctx, args[1])
	case args[0] == "set" && len(args) >= 2:
		return setJob(ctx, c, args[1], args[2:])
	}
	return usageError(jobsUsage)
}

func setJob(ctx context.Context, c *api.Client, name string, args []string) error {
	fs := flag.NewFlagSet("jobs set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	every := fs.String("every", "", "")
	on := fs.Bool("on", false, "")
	off := fs.Bool("off", false, "")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if fs.NArg() != 0 || (*on && *off) || (*every == "" && !*on && !*off) {
		return usageError(jobsUsage)
	}
	var p model.JobPatch
	if *every != "" {
		p.Every = every
	}
	if *on || *off {
		p.Enabled = on
	}
	return c.PatchConfig(ctx, model.ConfigPatch{Jobs: map[string]model.JobPatch{name: p}})
}

func printJobs(out io.Writer, st model.Status) {
	safe := stdoutLine()
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tON\tEVERY\tLAST RUN\tNEXT")
	for _, j := range st.Jobs {
		on := "on"
		if !j.Enabled {
			on = "off"
		}
		last := "never"
		switch {
		case j.Running:
			last = "running"
		case j.LastFinish != nil && j.LastError != "":
			last = fmt.Sprintf("failed %s ago: %s", st.Now.Sub(*j.LastFinish).Round(time.Minute), safe(j.LastError))
		case j.LastFinish != nil:
			last = fmt.Sprintf("ok %s ago", st.Now.Sub(*j.LastFinish).Round(time.Minute))
		}
		next := "-"
		switch {
		case !j.Enabled:
			next = "off"
		case j.Pending:
			next = "pending"
		case j.Next != nil && !j.Next.After(st.Now):
			next = "due"
		case j.Next != nil:
			next = "in " + j.Next.Sub(st.Now).Round(time.Minute).String()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", safe(j.Name), on, safe(j.Every), last, next)
	}
	w.Flush()
}
