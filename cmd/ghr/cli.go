package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

type usageError string

func (e usageError) Error() string { return string(e) }

// flagError turns a flag parse failure into a usage error, except -h, which
// run answers with the usage text.
func flagError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	return usageError(err.Error())
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func need(args []string, n int, form string) error {
	if len(args) != n {
		return usageError("usage: ghr " + form)
	}
	return nil
}

func atoi(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, usageError("not a number: " + s)
	}
	return n, nil
}

// logPollInterval is how often `logs -f` polls.
const logPollInterval = time.Second

func cli(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out, errOut io.Writer) error {
	switch args[0] {
	case "status":
		st, err := c.Status(ctx)
		if err != nil {
			return err
		}
		printStatus(out, st)
		return nil
	case "pause", "resume":
		if err := need(args, 2, args[0]+" <repo>"); err != nil {
			return err
		}
		if args[0] == "pause" {
			return c.Pause(ctx, args[1])
		}
		return c.Resume(ctx, args[1])
	case "drain", "resume-all":
		if err := need(args, 1, args[0]); err != nil {
			return err
		}
		if args[0] == "drain" {
			return c.PauseAll(ctx)
		}
		return c.ResumeAll(ctx)
	case "set":
		return set(ctx, c, args[1:])
	case "repo":
		return repo(ctx, c, args[1:])
	case "token":
		if err := need(args, 2, "token set"); err != nil || args[1] != "set" {
			return usageError("usage: ghr token set")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024))
		if err != nil {
			return err
		}
		return c.SetToken(ctx, strings.TrimSpace(string(data)))
	case "kill":
		if err := need(args, 2, "kill <id>"); err != nil {
			return err
		}
		return c.Kill(ctx, args[1])
	case "logs":
		return logs(ctx, c, args[1:], out)
	case "history":
		return historyCmd(ctx, c, args[1:], out)
	case "runner-update":
		switch {
		case len(args) == 1:
			if err := c.QueueRunnerUpdate(ctx); err != nil {
				return err
			}
			fmt.Fprintln(out, "runner update queued; it runs when no job is running or queued")
			return nil
		case len(args) == 2 && args[1] == "--cancel":
			return c.CancelRunnerUpdate(ctx)
		}
		return usageError("usage: ghr runner-update [--cancel]")
	case "storage":
		return storageCmd(ctx, c, args[1:], out)
	case "toolchain":
		return toolchainCmd(ctx, c, args[1:], out)
	case "cache":
		return cacheCmd(ctx, c, args[1:], out)
	case "prune":
		return pruneCmd(ctx, c, args[1:], out)
	case "setup":
		return setupCmd(ctx, c, args[1:], stdin, out, errOut)
	case "web":
		const webUsage = "usage: ghr web <reset-password|set-password>"
		if len(args) != 2 {
			return usageError(webUsage)
		}
		switch args[1] {
		case "reset-password":
			if err := c.ResetWebPassword(ctx); err != nil {
				return err
			}
			fmt.Fprintln(out, "web password removed; set a new one with: ghr web set-password")
			return nil
		case "set-password":
			pw, err := readNewPassword(ctx, stdin, errOut)
			if err != nil {
				return err
			}
			if err := c.SetWebPassword(ctx, pw); err != nil {
				return err
			}
			fmt.Fprintln(out, "web password set; every browser was logged out")
			return nil
		}
		return usageError(webUsage)
	}
	return usageError("unknown command " + args[0])
}

func set(ctx context.Context, c *api.Client, args []string) error {
	if len(args) == 0 {
		return usageError("usage: ghr set <mode|global-max|max|warm> ...")
	}
	switch args[0] {
	case "mode":
		if err := need(args, 2, "set mode <queue|all>"); err != nil {
			return err
		}
		return c.PatchConfig(ctx, model.ConfigPatch{Mode: &args[1]})
	case "global-max":
		if err := need(args, 2, "set global-max <n>"); err != nil {
			return err
		}
		n, err := atoi(args[1])
		if err != nil {
			return err
		}
		return c.PatchConfig(ctx, model.ConfigPatch{GlobalMax: &n})
	case "max", "warm":
		if err := need(args, 3, "set "+args[0]+" <repo> <n>"); err != nil {
			return err
		}
		n, err := atoi(args[2])
		if err != nil {
			return err
		}
		rp := model.RepoPatch{Max: &n}
		if args[0] == "warm" {
			rp = model.RepoPatch{Warm: &n}
		}
		return c.PatchConfig(ctx, model.ConfigPatch{Repos: map[string]model.RepoPatch{args[1]: rp}})
	}
	return usageError("unknown setting " + args[0])
}

func repo(ctx context.Context, c *api.Client, args []string) error {
	if len(args) < 2 {
		return usageError("usage: ghr repo <add|rm> <name>")
	}
	switch args[0] {
	case "rm":
		if err := need(args, 2, "repo rm <name>"); err != nil {
			return err
		}
		return c.RemoveRepo(ctx, args[1])
	case "add":
		fs := flag.NewFlagSet("repo add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		max := fs.Int("max", 0, "")
		allowPublic := fs.Bool("allow-public", false, "")
		var labels stringList
		fs.Var(&labels, "label", "")
		if err := fs.Parse(args[2:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 {
			return usageError("unexpected argument " + fs.Arg(0))
		}
		req := model.AddRepoRequest{Name: args[1], Labels: labels, AllowPublic: *allowPublic}
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "max" {
				req.Max = max
			}
		})
		if req.Max != nil && *req.Max < 0 {
			return usageError("--max must be >= 0 (0 = unlimited)")
		}
		return c.AddRepo(ctx, req)
	}
	return usageError("unknown repo command " + args[0])
}

func logs(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	follow := false
	var id string
	for _, a := range args {
		if a == "-f" {
			follow = true
		} else {
			id = a
		}
	}
	if id == "" {
		return usageError("usage: ghr logs <id> [-f]")
	}
	cursor := ""
	for {
		chunk, err := c.Log(ctx, id, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if _, err := io.WriteString(out, chunk.Data); err != nil {
			return err
		}
		cursor = chunk.Next
		if !follow {
			// One response is capped, so read until the daemon has nothing more.
			if chunk.Data == "" {
				return nil
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(logPollInterval):
		}
	}
}

func historyCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "")
	conclusion := fs.String("conclusion", "", "")
	limit := fs.Int("limit", 20, "")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	h, err := c.History(ctx, *repo, *conclusion, time.Time{}, *limit)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "FINISHED\tREPO\tRUN\tJOB\tRESULT\tDURATION")
	for _, e := range h {
		fmt.Fprintf(w, "%s\t%s\t#%s\t%s\t%s\t%s\n", e.FinishedAt.Local().Format("2006-01-02 15:04"), e.Repo, e.RunNumber, e.JobName,
			e.Conclusion, e.FinishedAt.Sub(e.StartedAt).Round(time.Second))
	}
	return w.Flush()
}

func maxText(n int) string {
	if n == 0 {
		return "∞"
	}
	return strconv.Itoa(n)
}

func printStatus(out io.Writer, st model.Status) {
	if st.Unconfigured {
		fmt.Fprintln(out, "warning: ghr is not configured. Run: ghr setup github --owner <owner>, or open the web UI")
	}
	if st.WebSetupRequired {
		fmt.Fprintln(out, "warning: the web UI has no password. Run: ghr web set-password")
	}
	if st.Unconfigured {
		return
	}
	running := 0
	for _, i := range st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	global := fmt.Sprintf("%d/%d", running, st.GlobalMax)
	if st.Mode == "all" {
		global = fmt.Sprintf("%d/∞", running)
	}
	fmt.Fprintf(out, "mode %s  global %s  api %d  disk %d%%\n", st.Mode, global, st.RateRemaining, st.DiskPct)
	fmt.Fprintln(out, runnerLine(st))
	if st.Degraded {
		fmt.Fprintf(out, "DEGRADED: %s\n", st.DegradedReason)
	}
	fmt.Fprintln(out)
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tSTATE\tRUN\tQUEUE\tLAST JOB")
	for _, r := range st.Repos {
		state := "active"
		if r.Paused {
			state = "paused"
		}
		if r.Error != "" {
			state = "error: " + r.Error
		}
		last := "-"
		if r.LastJob != nil {
			last = fmt.Sprintf("%s #%s %s (%s ago)", r.LastJob.Conclusion, r.LastJob.RunNumber, r.LastJob.JobName,
				st.Now.Sub(r.LastJob.FinishedAt).Round(time.Minute))
		}
		fmt.Fprintf(w, "%s\t%s\t%d/%s\t%d\t%s\n", r.Name, state, r.Active, maxText(r.Max), r.Queued, last)
	}
	w.Flush()
	if len(st.Instances) == 0 {
		return
	}
	fmt.Fprintln(out)
	w = tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "RUNNER\tREPO\tSTATE\tJOB\tELAPSED")
	for _, i := range st.Instances {
		job := "-"
		if i.Job != nil {
			job = fmt.Sprintf("%s #%s", i.Job.Name, i.Job.RunNumber)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.Repo, i.State, job, st.Now.Sub(i.Since).Round(time.Second))
	}
	w.Flush()
}

// runnerLine is the status line about the GitHub Actions runner version.
func runnerLine(st model.Status) string {
	u := st.RunnerUpdate
	if u.Installed == "" {
		s := "runner version unknown (no dist/current)"
		if u.CheckError != "" {
			s += "  last check failed: " + u.CheckError
		}
		return s
	}
	s := "runner " + u.Installed
	switch {
	case u.Deadline != nil:
		s += fmt.Sprintf(" → %s  update available, update by %s (%s)", u.Latest, u.Deadline.Local().Format(time.DateOnly), daysLeft(*u.Deadline, st.Now))
	case u.CheckedAt != nil:
		s += fmt.Sprintf("  up to date (checked %s ago)", st.Now.Sub(*u.CheckedAt).Round(time.Minute))
	case u.CheckError == "":
		s += "  checking…"
	}
	switch {
	case u.Running:
		s += "  updating"
	case u.Queued:
		s += "  queued: runs when no job is running or queued"
	}
	if u.CheckError != "" {
		s += "  last check failed: " + u.CheckError
	}
	return s
}

// daysLeft is the time to deadline in whole days, or "overdue".
func daysLeft(deadline, now time.Time) string {
	left := deadline.Sub(now)
	if left < 0 {
		return "overdue"
	}
	return fmt.Sprintf("%d days", int(left.Hours()/24))
}
