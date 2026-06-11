// Command onibi is the entry point. Subcommands wired in internal/cli.
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
