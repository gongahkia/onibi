package main

import (
	"flag"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/editor"
	"io"
	"os"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shack:", err)
		os.Exit(1)
	}
}
func run() error {
	show := flag.Bool("version", false, "print Shack version")
	configPath := flag.String("config", "", "use a JSON configuration file")
	noConfig := flag.Bool("no-config", false, "use built-in defaults")
	noLSP := flag.Bool("no-lsp", false, "disable language servers")
	plain := flag.Bool("plain", false, "disable syntax highlighting and language servers")
	readonly := flag.Bool("readonly", false, "open without modifying files")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), "Shack — Atomweight Terminal Text Editor IDE with Sensible Day 1 Defaults\nUsage: shack [options] [files or directory...]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *show {
		fmt.Println("shack", version)
		return nil
	}
	cfg := config.Default()
	if !*noConfig {
		path := *configPath
		if path == "" {
			path = config.Path()
		}
		var err error
		cfg, err = config.Load(path, *configPath != "")
		if err != nil {
			return err
		}
	}
	if *noLSP || *plain {
		cfg.LSP = false
	}
	if *plain {
		cfg.Syntax = false
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	app, err := editor.New(screen, cfg, flag.Args(), *readonly)
	if err != nil {
		return err
	}
	if info, e := os.Stdin.Stat(); e == nil && info.Mode()&os.ModeCharDevice == 0 {
		data, e := io.ReadAll(io.LimitReader(os.Stdin, (int64(cfg.MaxFileMiB)<<20)+1))
		if e != nil {
			return e
		}
		if int64(len(data)) > int64(cfg.MaxFileMiB)<<20 {
			return fmt.Errorf("stdin exceeds file limit")
		}
		if len(data) > 0 {
			if len(flag.Args()) > 0 {
				app.NewBuffer()
			}
			if e = app.Current().Doc.Insert(string(data)); e != nil {
				return e
			}
		}
	}
	return app.Run()
}
