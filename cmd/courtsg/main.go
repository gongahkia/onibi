package main

import (
	"os"

	"github.com/gongahkia/courtsg/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.DefaultExecute(version))
}
