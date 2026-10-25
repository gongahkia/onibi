// Command onibi-dev is the small source-checkout helper used by make.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gongahkia/onibi/internal/devtool"
	"github.com/gongahkia/onibi/internal/termui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[failed]", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: onibi-dev build|install|update|uninstall|status|help [options]")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	prefix := flags.String("prefix", "", "installation directory (default ~/.local/bin)")
	dryRun := flags.Bool("dry-run", false, "preview lifecycle changes")
	pull := flags.Bool("pull", false, "fetch and fast-forward a clean main checkout before updating")
	restart := flags.Bool("restart-web", false, "restart the web login service after a successful update")
	verbose := flags.Bool("verbose", false, "stream compiler output")
	if err := flags.Parse(os.Args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if os.Args[1] != "update" && (*pull || *restart) {
		return errors.New("--pull and --restart-web require update")
	}
	if (os.Args[1] == "build" || os.Args[1] == "status") && *dryRun {
		return errors.New("--dry-run applies to install, update, and uninstall")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ui := termui.New(os.Stderr)
	if *verbose {
		ui.Interactive = false
	}
	tool := devtool.Tool{Root: cwd, Prefix: *prefix, UI: ui, Verbose: *verbose}
	opts := devtool.LifecycleOptions{DryRun: *dryRun, Pull: *pull, RestartWeb: *restart}
	switch os.Args[1] {
	case "help":
		tool.Help()
		return nil
	case "build":
		return tool.Build(ctx)
	case "install":
		return tool.Install(ctx, false, opts)
	case "update":
		return tool.Install(ctx, true, opts)
	case "uninstall":
		return tool.Uninstall(ctx, opts)
	case "status":
		return tool.Status(ctx)
	default:
		return fmt.Errorf("unknown operation %q", os.Args[1])
	}
}
