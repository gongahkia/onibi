// Package markup renders Markdown and Org into terminal-independent text and
// style ranges. It never executes document code, expands includes or fetches URLs.
package markup

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const (
	MaxSource    = 1 << 20
	MaxOrgSource = 256 << 10
	MaxOutput    = 2 << 20
	MaxSpans     = 32768
	maxNodes     = 100000
	maxDepth     = 64
)

type Style struct {
	Bold, Italic, Underline, Strike, Code, Heading, Dim bool
	Link                                                string
}
type Span struct {
	Start, End int
	Style      Style
}
type Heading struct {
	Offset, Level int
	Title         string
}
type Document struct {
	Text     string
	Spans    []Span
	Headings []Heading
}
type renderer struct {
	ctx        context.Context
	out        strings.Builder
	spans      []Span
	headings   []Heading
	nodes      int
	err        error
	trailing   int
	prefix     string
	blockStart int // suppress leading block spacing immediately after a list marker
}

func Format(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return "markdown"
	case ".org":
		return "org"
	}
	return ""
}

// Render bounds retained output and traversal. Parsing runs on the caller's
// worker: parser calls cannot be interrupted halfway through; cancellation is
// checked before parsing and throughout output construction.
func Render(ctx context.Context, source, format string) (doc Document, err error) {
	defer func() {
		if p := recover(); p != nil {
			doc = Document{}
			err = fmt.Errorf("document rendering failed: %v", p)
		}
	}()
	limit := MaxSource
	if format == "org" {
		limit = MaxOrgSource
	}
	if len(source) > limit {
		return doc, fmt.Errorf("%s preview limit is %d KiB; source remains editable", format, limit>>10)
	}
	if !utf8.ValidString(source) {
		return doc, fmt.Errorf("preview requires UTF-8")
	}
	if err = ctx.Err(); err != nil {
		return doc, err
	}
	if err = preflight(source, format); err != nil {
		return doc, err
	}
	r := &renderer{ctx: ctx}
	switch format {
	case "markdown":
		r.markdown(source)
	case "org":
		r.org(source)
	default:
		return doc, fmt.Errorf("choose Markdown or Org preview")
	}
	if r.err != nil {
		return doc, r.err
	}
	return Document{r.out.String(), r.spans, r.headings}, nil
}

// Reject extreme nesting before handing input to third-party recursive parsers.
// This conservative ceiling also applies to deeply indented source examples.
func preflight(source, format string) error {
	lines, blocks := 0, 0
	for line := range strings.SplitSeq(source, "\n") {
		lines++
		if lines > 20000 || len(line) > 60000 {
			return fmt.Errorf("preview limit: 20,000 lines, 60,000 bytes per line")
		}
		lead := 0
		for _, c := range line {
			if c == ' ' {
				lead++
			} else if c == '\t' {
				lead += 4
			} else if c == '>' || c == '*' {
				lead += 4
			} else {
				break
			}
		}
		if lead > maxDepth*4 {
			return fmt.Errorf("preview nesting exceeds %d levels", maxDepth)
		}
		if format == "org" {
			l := strings.ToUpper(strings.TrimSpace(line))
			if strings.HasPrefix(l, "#+BEGIN_") {
				blocks++
			}
			if strings.HasPrefix(l, "#+END_") {
				blocks = max(0, blocks-1)
			}
			if blocks > maxDepth {
				return fmt.Errorf("Org block nesting exceeds %d levels", maxDepth)
			}
		}
	}
	return nil
}
func (r *renderer) visit(depth int) bool {
	if r.err != nil {
		return false
	}
	r.nodes++
	if depth > maxDepth || r.nodes > maxNodes {
		r.err = fmt.Errorf("preview exceeds nesting or node limit")
		return false
	}
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return false
	}
	return true
}
func clean(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			if c == '\r' {
				return -1
			}
			return '�'
		}
		return c
	}, s)
}

// SafeURL permits only inert web/mail links. Local and custom schemes are shown
// as text, never handed to a shell or terminal as executable links.
func SafeURL(s string) string {
	if len(s) > 2048 || strings.ContainsAny(s, "\x1b\x07\r\n\t") {
		return ""
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return ""
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host != "" {
			return s
		}
	case "mailto":
		if u.Opaque != "" {
			return s
		}
	}
	return ""
}
func (r *renderer) write(s string, style Style) {
	if r.err != nil || s == "" {
		return
	}
	s = clean(s)
	if s == "" {
		return
	}
	if r.out.Len()+len(s) > MaxOutput {
		r.err = fmt.Errorf("rendered preview exceeds 2 MiB")
		return
	}
	if r.prefix == "" {
		r.writeRaw(s, style)
		return
	}
	for len(s) > 0 {
		end := strings.IndexByte(s, '\n')
		if end < 0 {
			end = len(s)
		} else {
			end++
		}
		if s[0] != '\n' {
			r.linePrefix()
		}
		r.writeRaw(s[:end], style)
		s = s[end:]
	}
}
func (r *renderer) linePrefix() {
	if r.prefix != "" && (r.out.Len() == 0 || r.trailing > 0) {
		r.writeRaw(r.prefix, Style{Dim: true})
	}
}
func (r *renderer) writeRaw(s string, style Style) {
	if r.err != nil || s == "" {
		return
	}
	if r.out.Len()+len(s) > MaxOutput {
		r.err = fmt.Errorf("rendered preview exceeds 2 MiB")
		return
	}
	start := r.out.Len()
	r.out.WriteString(s)
	for _, c := range s {
		if c == '\n' {
			r.trailing++
		} else {
			r.trailing = 0
		}
	}
	if style == (Style{}) {
		return
	}
	if len(r.spans) > 0 && r.spans[len(r.spans)-1].End == start && r.spans[len(r.spans)-1].Style == style {
		r.spans[len(r.spans)-1].End = r.out.Len()
		return
	}
	if len(r.spans) >= MaxSpans {
		r.err = fmt.Errorf("preview exceeds %d style ranges", MaxSpans)
		return
	}
	r.spans = append(r.spans, Span{start, r.out.Len(), style})
}
func (r *renderer) newline(n int) {
	if r.out.Len() > 0 && r.out.Len() != r.blockStart && r.trailing < n {
		r.write(strings.Repeat("\n", n-r.trailing), Style{})
	}
}
func (r *renderer) heading(level int, content func(Style)) {
	r.newline(2)
	r.linePrefix()
	start := r.out.Len()
	content(Style{Bold: true, Heading: true})
	r.headings = append(r.headings, Heading{start, level, r.out.String()[start:]})
	r.newline(2)
}
func (r *renderer) append(child *renderer) {
	if child.err != nil {
		r.err = child.err
		return
	}
	s := child.out.String()
	pos := 0
	for _, span := range child.spans {
		r.write(s[pos:span.Start], Style{})
		r.write(s[span.Start:span.End], span.Style)
		pos = span.End
	}
	r.write(s[pos:], Style{})
	r.nodes += child.nodes
	if r.nodes > maxNodes {
		r.err = fmt.Errorf("preview exceeds node limit")
	}
}

type alignment uint8

const (
	alignLeft alignment = iota
	alignRight
	alignCenter
)

// Tables preserve every cell's text. Padding is capped so a very long cell does
// not multiply its width across every other row. Such cells can scroll/wrap.
func (r *renderer) table(rows [][]*renderer, header bool, alignmentOptions ...[]alignment) {
	r.newline(2)
	widths := []int{}
	for _, row := range rows {
		for c, cell := range row {
			for len(widths) <= c {
				widths = append(widths, 0)
			}
			widths[c] = min(80, max(widths[c], uniseg.StringWidth(cell.out.String())))
		}
	}
	for i, row := range rows {
		r.write("│ ", Style{Dim: true})
		for c, cell := range row {
			pad := max(0, widths[c]-uniseg.StringWidth(cell.out.String()))
			left := 0
			if len(alignmentOptions) > 0 && c < len(alignmentOptions[0]) {
				switch alignmentOptions[0][c] {
				case alignRight:
					left = pad
				case alignCenter:
					left = pad / 2
				}
			}
			r.write(strings.Repeat(" ", left), Style{Dim: true})
			r.append(cell)
			r.write(strings.Repeat(" ", pad-left)+" │ ", Style{Dim: true})
		}
		r.newline(1)
		if i == 0 && header {
			r.write("├", Style{Dim: true})
			for c, w := range widths {
				r.write(strings.Repeat("─", w+2), Style{Dim: true})
				if c < len(widths)-1 {
					r.write("┼", Style{Dim: true})
				}
			}
			r.write("┤\n", Style{Dim: true})
		}
	}
	r.newline(2)
}
