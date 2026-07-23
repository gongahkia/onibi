package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

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
	ansiBlue   = "\x1b[34m"
)

type cliStyle struct {
	color bool
}

var tableWidthForWriter = func(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil || cols <= 0 {
		return 0
	}
	return cols
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
func (s cliStyle) blue(text string) string   { return s.paint(ansiBlue, text) }

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

func uiWidth(w io.Writer) int {
	if width := tableWidthForWriter(w); width > 0 {
		return width
	}
	return 96
}

func uiCompact(w io.Writer) bool { return uiWidth(w) < 50 }

func renderPanel(w io.Writer, style cliStyle, title string, lines ...string) error {
	width := uiWidth(w)
	if width < 50 {
		if title != "" {
			if _, err := fmt.Fprintln(w, style.bold(title)); err != nil {
				return err
			}
		}
		for _, line := range lines {
			for _, wrapped := range wrapTableCell(line, max(20, width)) {
				if _, err := fmt.Fprintln(w, wrapped); err != nil {
					return err
				}
			}
		}
		return nil
	}
	inner := min(94, width-4)
	if inner < 38 {
		inner = 38
	}
	heading := " " + strings.TrimSpace(title) + " "
	if visibleLen(heading) > inner {
		heading, _ = takeVisiblePrefix(heading, inner)
	}
	if _, err := fmt.Fprintln(w, style.dim("╭"+heading+strings.Repeat("─", max(0, inner-visibleLen(heading)))+"╮")); err != nil {
		return err
	}
	for _, line := range lines {
		for _, wrapped := range wrapTableCell(line, inner-2) {
			if _, err := fmt.Fprintln(w, style.dim("│")+" "+wrapped+strings.Repeat(" ", max(0, inner-1-visibleLen(wrapped)))+style.dim("│")); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(w, style.dim("╰"+strings.Repeat("─", inner)+"╯"))
	return err
}

func renderKeyValuePanel(cmd *cobra.Command, title string, rows [][]string) error {
	style := styleFor(cmd)
	width := uiWidth(cmd.OutOrStdout())
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		if len(row) == 1 || width < 64 {
			lines = append(lines, strings.Join(row, " "))
			continue
		}
		label := strings.TrimSpace(row[0])
		value := strings.TrimSpace(strings.Join(row[1:], "  "))
		lines = append(lines, style.dim(label)+"  "+value)
	}
	return renderPanel(cmd.OutOrStdout(), style, title, lines...)
}

func renderMenuPanel(cmd *cobra.Command, title string, rows [][]string) error {
	style := styleFor(cmd)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}
		key := style.cyan("[" + row[0] + "]")
		lines = append(lines, key+" "+style.bold(row[1])+"  "+style.dim(row[2]))
	}
	return renderPanel(cmd.OutOrStdout(), style, title, lines...)
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
	terminalWidth := tableWidthForWriter(w)
	if terminalWidth > 0 {
		if terminalWidth < 80 && tableHeaderRow(rows[0]) {
			return renderStackedTable(w, rows)
		}
		contentWidth := terminalWidth - (3*len(widths) + 1)
		if contentWidth > 0 {
			widths = fitTableWidths(widths, contentWidth+2*(len(widths)-1))
			for sumInts(widths) > contentWidth {
				widest := -1
				for i, width := range widths {
					if width > 1 && (widest < 0 || width > widths[widest]) {
						widest = i
					}
				}
				if widest < 0 {
					break
				}
				widths[widest]--
			}
		}
		return renderBoxTable(w, rows, widths)
	}
	for _, row := range rows {
		wrapped := make([][]string, len(widths))
		lines := 1
		for i := range widths {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			wrapped[i] = wrapTableCell(cell, widths[i])
			if len(wrapped[i]) > lines {
				lines = len(wrapped[i])
			}
		}
		for line := 0; line < lines; line++ {
			for i, cellLines := range wrapped {
				if i > 0 {
					if _, err := io.WriteString(w, "  "); err != nil {
						return err
					}
				}
				cell := ""
				if line < len(cellLines) {
					cell = cellLines[line]
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
	}
	return nil
}

func renderStackedTable(w io.Writer, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	headers := rows[0]
	for rowIndex, row := range rows[1:] {
		if rowIndex > 0 {
			if _, err := fmt.Fprintln(w, "─"); err != nil {
				return err
			}
		}
		for i, header := range headers {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			for _, line := range wrapTableCell(value, max(20, tableWidthForWriter(w)-visibleLen(stripANSI(header))-3)) {
				if _, err := fmt.Fprintf(w, "%s: %s\n", header, line); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func renderBoxTable(w io.Writer, rows [][]string, widths []int) error {
	if len(widths) == 0 {
		return nil
	}
	border := func(left, join, right string) string {
		parts := make([]string, len(widths))
		for i, width := range widths {
			parts[i] = strings.Repeat("─", width+2)
		}
		return left + strings.Join(parts, join) + right
	}
	if _, err := fmt.Fprintln(w, border("╭", "┬", "╮")); err != nil {
		return err
	}
	for rowIndex, row := range rows {
		wrapped := make([][]string, len(widths))
		lines := 1
		for i := range widths {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			wrapped[i] = wrapTableCell(cell, widths[i])
			if len(wrapped[i]) > lines {
				lines = len(wrapped[i])
			}
		}
		for line := 0; line < lines; line++ {
			if _, err := io.WriteString(w, "│"); err != nil {
				return err
			}
			for i, cellLines := range wrapped {
				cell := ""
				if line < len(cellLines) {
					cell = cellLines[line]
				}
				if _, err := io.WriteString(w, " "+cell+strings.Repeat(" ", widths[i]-visibleLen(cell))+" │"); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		if rowIndex == 0 && tableHeaderRow(row) {
			if _, err := fmt.Fprintln(w, border("├", "┼", "┤")); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(w, border("╰", "┴", "╯"))
	return err
}

func tableHeaderRow(row []string) bool {
	if len(row) == 0 {
		return false
	}
	for _, cell := range row {
		plain := strings.TrimSpace(stripANSI(cell))
		if plain == "" || plain != strings.ToUpper(plain) {
			return false
		}
	}
	return true
}

func stripANSI(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func fitTableWidths(widths []int, maxWidth int) []int {
	if len(widths) == 0 {
		return widths
	}
	gap := 2 * (len(widths) - 1)
	if maxWidth <= gap+len(widths) {
		return widths
	}
	out := append([]int(nil), widths...)
	target := maxWidth - gap
	for sumInts(out) > target {
		col := -1
		margin := 0
		for i, width := range out {
			minWidth := minTableColumnWidth(i, len(out), width)
			if width-minWidth > margin {
				col = i
				margin = width - minWidth
			}
		}
		if col < 0 {
			break
		}
		out[col]--
	}
	return out
}

func minTableColumnWidth(i, cols, natural int) int {
	if natural <= 1 {
		return natural
	}
	if i == 0 {
		return 1
	}
	if i == cols-1 && natural >= 22 {
		return 22
	}
	if natural < 12 {
		return natural
	}
	return 12
}

func sumInts(vals []int) int {
	n := 0
	for _, v := range vals {
		n += v
	}
	return n
}

func wrapTableCell(cell string, width int) []string {
	if width <= 0 {
		return []string{cell}
	}
	paragraphs := strings.Split(cell, "\n")
	var out []string
	for _, p := range paragraphs {
		out = append(out, wrapTableLine(p, width)...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func wrapTableLine(line string, width int) []string {
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	current := ""
	for _, word := range words {
		if current == "" {
			pieces := splitTableWord(word, width)
			if len(pieces) == 0 {
				continue
			}
			out = append(out, pieces[:len(pieces)-1]...)
			current = pieces[len(pieces)-1]
			continue
		}
		if visibleLen(current)+1+visibleLen(word) <= width {
			current += " " + word
			continue
		}
		out = append(out, current)
		pieces := splitTableWord(word, width)
		if len(pieces) == 0 {
			current = ""
			continue
		}
		out = append(out, pieces[:len(pieces)-1]...)
		current = pieces[len(pieces)-1]
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func splitTableWord(word string, width int) []string {
	if visibleLen(word) <= width {
		return []string{word}
	}
	var out []string
	for visibleLen(word) > width {
		part, n := takeVisiblePrefix(word, width)
		if part == "" || n <= 0 {
			break
		}
		out = append(out, part)
		word = word[n:]
	}
	if word != "" {
		out = append(out, word)
	}
	return out
}

func takeVisiblePrefix(s string, width int) (string, int) {
	seen := 0
	i := 0
	for i < len(s) && seen < width {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		i += size
		seen++
	}
	return s[:i], i
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
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		i += size - 1
		n++
	}
	return n
}
