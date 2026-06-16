package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

type cliStyle struct {
	color bool
}

func styleFor(cmd *cobra.Command) cliStyle {
	mode := colorMode(cmd)
	switch mode {
	case "always":
		return cliStyle{color: true}
	case "never":
		return cliStyle{}
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0" {
		return cliStyle{}
	}
	if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		return cliStyle{color: true}
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return cliStyle{color: true}
	}
	return cliStyle{}
}

func colorMode(cmd *cobra.Command) string {
	flag := cmd.Flag("color")
	if flag == nil && cmd.Root() != nil {
		flag = cmd.Root().Flag("color")
	}
	if flag == nil {
		return "auto"
	}
	switch flag.Value.String() {
	case "always", "never", "auto":
		return flag.Value.String()
	default:
		return "auto"
	}
}

func (s cliStyle) paint(code, text string) string {
	if !s.color || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (s cliStyle) bold(text string) string   { return s.paint(ansiBold, text) }
func (s cliStyle) dim(text string) string    { return s.paint(ansiDim, text) }
func (s cliStyle) red(text string) string    { return s.paint(ansiRed, text) }
func (s cliStyle) green(text string) string  { return s.paint(ansiGreen, text) }
func (s cliStyle) yellow(text string) string { return s.paint(ansiYellow, text) }
func (s cliStyle) cyan(text string) string   { return s.paint(ansiCyan, text) }

func (s cliStyle) status(status any) string {
	text := strings.TrimSpace(fmt.Sprint(status))
	label := "[" + text + "]"
	switch text {
	case "PASS":
		return s.green(label)
	case "WARN":
		return s.yellow(label)
	case "FAIL":
		return s.red(label)
	default:
		return s.cyan(label)
	}
}

func (s cliStyle) fix(ok bool) string {
	if ok {
		return s.green("[FIX]")
	}
	return s.red("[FIX-FAIL]")
}

func (s cliStyle) bool(v bool) string {
	if v {
		return s.green("true")
	}
	return s.dim("false")
}

func (s cliStyle) installed(v bool) string {
	if v {
		return s.green("true")
	}
	return s.yellow("false")
}

func renderTable(w io.Writer, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if n := visibleLen(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				if _, err := io.WriteString(w, "  "); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, cell); err != nil {
				return err
			}
			if i < len(widths)-1 {
				if _, err := io.WriteString(w, strings.Repeat(" ", widths[i]-visibleLen(cell))); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func tableHeader(s cliStyle, cols ...string) []string {
	out := make([]string, len(cols))
	for i, col := range cols {
		out[i] = s.bold(col)
	}
	return out
}

func visibleLen(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		n++
	}
	return n
}
