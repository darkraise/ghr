package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

func packageCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	if len(args) == 0 {
		return usageError("usage: ghr package <list|search|install|upgrade|uninstall|forget|refresh> ...")
	}
	switch args[0] {
	case "list":
		if err := need(args, 1, "package list"); err != nil {
			return err
		}
		p, err := c.Packages(ctx)
		if err != nil {
			return err
		}
		printPackages(out, p)
		return nil
	case "search":
		if err := need(args, 2, "package search <term>"); err != nil {
			return err
		}
		ps, err := c.SearchPackages(ctx, args[1])
		if err != nil {
			return err
		}
		printPackageSearch(out, ps)
		return nil
	case "install":
		if len(args) < 2 || hasFlag(args[1:]) {
			return usageError("usage: ghr package install <name>...")
		}
		res, err := c.AddPackages(ctx, args[1:])
		if err != nil {
			return err
		}
		if len(res.Install) > 0 {
			fmt.Fprintln(out, "install: "+strings.Join(res.Install, ", "))
		}
		if len(res.Adopt) > 0 {
			fmt.Fprintln(out, "adopt: "+strings.Join(res.Adopt, ", "))
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	case "upgrade":
		var err error
		switch {
		case len(args) == 2 && args[1] == "--all":
			err = c.UpgradePackages(ctx, nil, true)
		case len(args) >= 2 && !hasFlag(args[1:]):
			err = c.UpgradePackages(ctx, args[1:], false)
		default:
			return usageError("usage: ghr package upgrade <name>... | --all")
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	case "uninstall":
		if err := need(args, 2, "package uninstall <name>"); err != nil {
			return err
		}
		if err := c.UninstallPackage(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	case "forget":
		if err := need(args, 2, "package forget <name>"); err != nil {
			return err
		}
		if err := c.ForgetPackage(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(out, args[1]+" is no longer managed; it stays installed")
		return nil
	case "refresh":
		if err := need(args, 1, "package refresh"); err != nil {
			return err
		}
		if err := c.RefreshPackageIndex(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	}
	return usageError("unknown package command " + args[0])
}

// hasFlag reports whether any argument starts with "-", which no package
// name does.
func hasFlag(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return true
		}
	}
	return false
}

func printPackages(out io.Writer, p model.Packages) {
	safe := stdoutLine()
	fmt.Fprintln(out, "MANAGED")
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tUPGRADE\tSIZE\tERROR")
	for _, i := range p.Managed {
		version, size, upgrade := "-", "-", "-"
		if i.Installed {
			version, size = safe(i.Version), model.HumanBytes(i.Bytes)
		}
		if i.Upgradable {
			upgrade = safe(i.Candidate)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", safe(i.Name), version, upgrade, size, safe(i.Error))
	}
	w.Flush()
	if p.IndexUpdatedAt != nil {
		fmt.Fprintln(out, "apt index updated "+stamp(*p.IndexUpdatedAt))
	} else {
		fmt.Fprintln(out, "apt index never updated")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "BASE (installed by setup.sh)")
	printInstalled(out, p.Base)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "DETECTED (installed, not managed; add with ghr package install)")
	printInstalled(out, p.Detected)
}

func printInstalled(out io.Writer, ps []model.PackageInfo) {
	safe := stdoutLine()
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tSIZE")
	for _, i := range ps {
		fmt.Fprintf(w, "%s\t%s\t%s\n", safe(i.Name), safe(i.Version), model.HumanBytes(i.Bytes))
	}
	w.Flush()
}

func printPackageSearch(out io.Writer, ps []model.PackageInfo) {
	safe := stdoutLine()
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tDESCRIPTION")
	for _, i := range ps {
		state := "-"
		switch {
		case i.Managed:
			state = "managed"
		case i.Installed:
			state = "installed"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", safe(i.Name), state, safe(i.Description))
	}
	w.Flush()
}
