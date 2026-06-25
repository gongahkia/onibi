package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	beginMark = "<!-- BEGIN-COMMANDS -->"
	endMark   = "<!-- END-COMMANDS -->"
)

func main() {
	check := flag.Bool("check", false, "fail if README is out of date")
	flag.Parse()
	in, err := os.ReadFile("README.md")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	body := string(in)
	iBegin := strings.Index(body, beginMark)
	iEnd := strings.Index(body, endMark)
	if iBegin < 0 || iEnd < 0 || iEnd < iBegin {
		return
	}
	generated := generatedBlock(nil)
	next := body[:iBegin] + generated + body[iEnd+len(endMark):]
	if *check {
		if next != body {
			fmt.Fprintln(os.Stderr, "README is stale: run `make gen-readme`")
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile("README.md", []byte(next), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func generatedBlock(commands []string) string {
	var b strings.Builder
	b.WriteString(beginMark + "\n")
	for _, line := range commands {
		b.WriteString(formatCommand(line) + "\n")
	}
	b.WriteString(endMark)
	return b.String()
}

func formatCommand(line string) string {
	cmd, desc, ok := strings.Cut(line, " - ")
	if !ok {
		return "- `" + line + "`"
	}
	return "- `" + cmd + "` - " + desc
}
