//go:build !onibi_rpi

// Command onibi is the entry point. Subcommands wired in internal/cli.
package main

import (
	"fmt"
	"os"

	"github.com/gongahkia/onibi/internal/cli"
)

func main() {
	root := cli.Root()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if cli.DebugEnabled(root) {
			fmt.Fprintf(os.Stderr, "debug: args=%q err_type=%T\n", os.Args[1:], err)
		}
		os.Exit(1)
	}
}
