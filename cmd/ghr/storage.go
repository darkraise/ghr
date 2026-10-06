package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

const queuedNote = "queued — follow with: ghr storage"

func storageCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	switch {
	case len(args) == 0:
		st, err := c.Storage(ctx)
		if err != nil {
			return err
		}
		printStorage(out, st)
		return nil
	case len(args) == 1 && args[0] == "refresh":
		if err := c.RefreshStorage(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "measuring — follow with: ghr storage")
		return nil
	}
	return usageError("usage: ghr storage [refresh]")
}

func toolchainCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	if len(args) == 0 {
		return usageError("usage: ghr toolchain <list|available|install|rm> ...")
	}
	switch args[0] {
	case "list":
		if err := need(args, 1, "toolchain list"); err != nil {
			return err
		}
		st, err := c.Storage(ctx)
		if err != nil {
			return err
		}
		printToolchains(out, st)
		return nil
	case "available":
		if err := need(args, 2, "toolchain available <tool>"); err != nil {
			return err
		}
		cs, err := c.AvailableToolchains(ctx, args[1])
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "SPEC\tVERSION\tLTS")
		for _, ch := range cs {
			lts := ""
			if ch.LTS {
				lts = "lts"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", ch.Spec, ch.Version, lts)
		}
		return w.Flush()
	case "install":
		var err error
		switch {
		case len(args) == 3 && args[1] == "--preset":
			err = c.InstallPreset(ctx, args[2])
		case len(args) == 3 && !strings.HasPrefix(args[1], "-"):
			err = c.InstallToolchain(ctx, args[1], args[2])
		default:
			return usageError("usage: ghr toolchain install <tool> <version> | --preset popular")
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	case "rm":
		if err := need(args, 3, "toolchain rm <tool> <version>"); err != nil {
			return err
		}
		if err := c.RemoveToolchain(ctx, args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	}
	return usageError("unknown toolchain command " + args[0])
}

func cacheCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	if len(args) == 0 {
		return usageError("usage: ghr cache <list|clear> ...")
	}
	switch args[0] {
	case "list":
		if err := need(args, 1, "cache list"); err != nil {
			return err
		}
		st, err := c.Storage(ctx)
		if err != nil {
			return err
		}
		printCaches(out, st)
		fmt.Fprintln(out, measuredLine(st))
		return nil
	case "clear":
		if err := need(args, 2, "cache clear <name>"); err != nil {
			return err
		}
		if err := c.ClearCache(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	}
	return usageError("unknown cache command " + args[0])
}

func pruneCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scope := fs.String("scope", "", "")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if fs.NArg() > 0 {
		return usageError("unexpected argument " + fs.Arg(0))
	}
	var err error
	if *scope == "" {
		err = c.Prune(ctx)
	} else {
		err = c.PruneScope(ctx, *scope)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "prune started — follow with: ghr storage")
	return nil
}

func stamp(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func printStorage(out io.Writer, st model.Storage) {
	fmt.Fprintf(out, "DOCKER DISK (disk %d%%)\n", st.Docker.DiskPct)
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tCOUNT\tACTIVE\tSIZE\tRECLAIMABLE")
	for _, r := range st.Docker.Rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", r.Type, r.Count, r.Active, model.HumanBytes(r.Bytes), model.HumanBytes(r.Reclaimable))
		if r.Type == "Build Cache" {
			for _, ct := range st.Docker.BuildCacheTypes {
				fmt.Fprintf(w, "  %s\t%d\t-\t%s\t%s\n", ct.Type, ct.Count, model.HumanBytes(ct.Bytes), model.HumanBytes(ct.Reclaimable))
			}
		}
	}
	w.Flush()
	fmt.Fprintln(out, pruneLine(st.LastPrune))
	fmt.Fprintln(out)
	printToolchains(out, st)
	fmt.Fprintln(out)
	printCaches(out, st)
	fmt.Fprintln(out, measuredLine(st))
	fmt.Fprintln(out)
	printOperations(out, st.Operations)
}

func pruneLine(lp *model.LastPrune) string {
	if lp == nil {
		return "last prune: none since ghr started"
	}
	head := fmt.Sprintf("last prune: %s · %s · %s", lp.Trigger, lp.Scope, stamp(lp.StartedAt))
	if lp.FinishedAt == nil {
		return head + " · pruning…"
	}
	var steps []string
	for _, s := range lp.Steps {
		if s.Error != "" {
			steps = append(steps, s.Name+" failed: "+s.Error)
		} else {
			steps = append(steps, s.Name+" "+model.HumanBytes(s.Freed))
		}
	}
	line := head + " · " + lp.Outcome
	if len(steps) > 0 {
		line += " — " + strings.Join(steps, ", ")
	}
	return line
}

// printToolchains prints the installed toolchains and the tool-cache
// folders no installer owns, which have no Remove.
func printToolchains(out io.Writer, st model.Storage) {
	fmt.Fprintln(out, "TOOLCHAINS")
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tVERSION\tSIZE\tINSTALLED")
	for _, tc := range st.Toolchains {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", tc.Tool, tc.Version, model.HumanBytes(tc.Bytes), stamp(tc.InstalledAt))
	}
	w.Flush()
	if len(st.OtherToolCache) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "OTHER TOOL CACHE")
	w = tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE")
	for _, f := range st.OtherToolCache {
		fmt.Fprintf(w, "%s\t%s\n", f.Name, model.HumanBytes(f.Bytes))
	}
	w.Flush()
}

func printCaches(out io.Writer, st model.Storage) {
	fmt.Fprintln(out, "PACKAGE CACHES")
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE\tFILES\tLAST WRITTEN\tPATH")
	for _, pc := range st.PackageCaches {
		paths := strings.Join(pc.Paths, ", ")
		if !pc.Present {
			fmt.Fprintf(w, "%s\tnot present\t\t\t%s\n", pc.Name, paths)
			continue
		}
		last := "-"
		if pc.LastWritten != nil {
			last = stamp(*pc.LastWritten)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", pc.Name, model.HumanBytes(pc.Bytes), pc.Files, last, paths)
	}
	w.Flush()
}

func measuredLine(st model.Storage) string {
	var s string
	switch {
	case st.Measuring:
		s = "measuring…"
	case st.MeasuredAt == nil:
		s = "not measured yet"
	default:
		s = "measured " + stamp(*st.MeasuredAt)
	}
	if st.MeasureError != "" {
		s += "  measure error: " + st.MeasureError
	}
	return s
}

func printOperations(out io.Writer, ops model.Operations) {
	if c := ops.Current; c != nil {
		line := "running: " + c.Kind + " " + c.Target
		if c.Progress != "" {
			line += " — " + c.Progress
		}
		fmt.Fprintf(out, "%s (%d queued)\n", line, ops.Queued)
	} else {
		fmt.Fprintf(out, "no operation running (%d queued)\n", ops.Queued)
	}
	if len(ops.Recent) == 0 {
		return
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "FINISHED\tKIND\tTARGET\tOUTCOME\tMESSAGE")
	for _, o := range ops.Recent {
		finished := "-"
		if o.FinishedAt != nil {
			finished = stamp(*o.FinishedAt)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", finished, o.Kind, o.Target, o.Outcome, o.Message)
	}
	w.Flush()
}
