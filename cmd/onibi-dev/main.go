// Command onibi-dev is the small source-checkout helper used by make.
package main

import (
	"context"
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
		return fmt.Errorf("usage: onibi-dev build")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	verbose := flags.Bool("verbose", false, "stream compiler output")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
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
	tool := devtool.Tool{Root: cwd, UI: ui, Verbose: *verbose}
	switch os.Args[1] {
	case "build":
		return tool.Build(ctx)
	default:
		return fmt.Errorf("unknown operation %q", os.Args[1])
	}
}
