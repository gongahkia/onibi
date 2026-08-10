package main

import (
	"os"

	"github.com/gongahkia/kaypoh/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.DefaultExecute(version))
}
