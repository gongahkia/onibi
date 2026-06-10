// Command onibi is the entry point. Subcommands wired in internal/cli.
//
// See TODO-10-JUN.md (§4 repo layout, §8 phase plan) for the design.
package main

import (
	"fmt"
	"os"

	"github.com/gongahkia/onibi/internal/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
