package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
)

const (
	beginMark = "<!-- BEGIN-TELEGRAM-COMMANDS -->"
	endMark   = "<!-- END-TELEGRAM-COMMANDS -->"
)

func main() {
	check := flag.Bool("check", false, "fail if README is out of date")
	flag.Parse()
	commands, err := readHelpCommands("internal/daemon/commands.go")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	in, err := os.ReadFile("README.md")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	body := string(in)
	iBegin := strings.Index(body, beginMark)
	iEnd := strings.Index(body, endMark)
	if iBegin < 0 || iEnd < 0 || iEnd < iBegin {
		fmt.Fprintln(os.Stderr, "README missing BEGIN/END markers")
		os.Exit(2)
	}
	generated := generatedBlock(commands)
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

func readHelpCommands(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "helpText" || fn.Body == nil {
			continue
		}
		var commands []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) == 0 {
				return true
			}
			call, ok := ret.Results[0].(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range lit.Elts {
				basic, ok := elt.(*ast.BasicLit)
				if !ok || basic.Kind != token.STRING {
					continue
				}
				line, err := strconv.Unquote(basic.Value)
				if err == nil && strings.HasPrefix(line, "/") {
					commands = append(commands, line)
				}
			}
			return false
		})
		if len(commands) == 0 {
			return nil, fmt.Errorf("helpText command list not found in %s", path)
		}
		return commands, nil
	}
	return nil, fmt.Errorf("helpText not found in %s", path)
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
