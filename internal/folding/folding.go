// Package folding finds bounded, language-independent structural fold candidates.
// It is a heuristic fallback, not a language parser: source text is never altered.
package folding

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const MaxSource = 16 << 20
const MaxRanges = 20000

// Range includes the header line and ends at the start of the following line
// (or EOF). Folding hides everything after the header through the last line.
type Range struct{ Start, End int }
type line struct {
	start, end int
	value      string
}

func Find(ctx context.Context, source, path string) ([]Range, error) {
	if len(source) > MaxSource {
		return nil, fmt.Errorf("fold scanning is limited to 16 MiB")
	}
	lines := []line{}
	offset := 0
	for s := range strings.SplitSeq(source, "\n") {
		if len(lines)%256 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if len(lines) >= 500000 {
			return nil, fmt.Errorf("fold scanning is limited to 500,000 lines")
		}
		end := min(len(source), offset+len(s)+1)
		lines = append(lines, line{offset, end, strings.TrimSuffix(s, "\r")})
		offset = end
	}
	out := []Range{}
	add := func(start, end int) {
		if len(out) <= MaxRanges && start >= 0 && end > start && end < len(lines) {
			out = append(out, Range{lines[start].start, lines[end].end})
		}
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown", ".mdown", ".mkd", ".org":
		markup(ctx, lines, ext == ".org", add)
	default:
		code(ctx, lines, ext, add)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(out) > MaxRanges {
		return nil, fmt.Errorf("fold scanning exceeds %d regions", MaxRanges)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start == out[j].Start {
			return out[i].End > out[j].End
		}
		return out[i].Start < out[j].Start
	})
	// Keep the widest candidate per header, drop crossing regions. Nested ranges
	// remain available even while their parents are collapsed.
	clean := out[:0]
	stack := []Range{}
	for _, r := range out {
		if len(clean) > 0 && clean[len(clean)-1].Start == r.Start {
			continue
		}
		for len(stack) > 0 && r.Start >= stack[len(stack)-1].End {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && r.End > stack[len(stack)-1].End {
			continue
		}
		clean = append(clean, r)
		stack = append(stack, r)
	}
	return clean, nil
}
func markup(ctx context.Context, lines []line, org bool, add func(int, int)) {
	type heading struct{ line, level int }
	heads := []heading{}
	blockStart := -1
	marker := ""
	for i, l := range lines {
		if i%256 == 0 && ctx.Err() != nil {
			return
		}
		v := strings.TrimSpace(l.value)
		if blockStart >= 0 {
			close := false
			if org {
				close = strings.HasPrefix(strings.ToUpper(v), "#+END_"+marker)
			} else {
				run := len(v) - len(strings.TrimLeft(v, string(marker[0])))
				close = run >= len(marker) && strings.TrimSpace(v[run:]) == ""
			}
			if close {
				add(blockStart, i)
				blockStart = -1
			}
			continue
		}
		if org && strings.HasPrefix(strings.ToUpper(v), "#+BEGIN_") {
			blockStart = i
			fields := strings.Fields(strings.TrimPrefix(strings.ToUpper(v), "#+BEGIN_"))
			if len(fields) == 0 {
				blockStart = -1
				continue
			}
			marker = fields[0]
			continue
		}
		if !org && (strings.HasPrefix(v, "```") || strings.HasPrefix(v, "~~~")) {
			run := len(v) - len(strings.TrimLeft(v, string(v[0])))
			blockStart = i
			marker = v[:run]
			continue
		}
		level := 0
		if org {
			level = len(l.value) - len(strings.TrimLeft(l.value, "*"))
			if level == 0 || level >= len(l.value) || l.value[level] != ' ' {
				level = 0
			}
		} else {
			level = len(v) - len(strings.TrimLeft(v, "#"))
			if level > 6 || level == 0 || (level < len(v) && v[level] != ' ' && v[level] != '\t') {
				level = 0
			}
			if level == 0 && i+1 < len(lines) && v != "" {
				next := strings.TrimSpace(lines[i+1].value)
				if len(next) > 0 && strings.Trim(next, "=") == "" {
					level = 1
				} else if len(next) > 0 && strings.Trim(next, "-") == "" {
					level = 2
				}
			}
		}
		if level > 0 {
			for len(heads) > 0 && heads[len(heads)-1].level >= level {
				h := heads[len(heads)-1]
				heads = heads[:len(heads)-1]
				add(h.line, i-1)
			}
			heads = append(heads, heading{i, level})
		}
	}
	last := len(lines) - 1
	if last > 0 && lines[last].value == "" {
		last--
	}
	if blockStart >= 0 {
		add(blockStart, last)
	}
	for _, h := range heads {
		add(h.line, last)
	}
}
func code(ctx context.Context, lines []line, ext string, add func(int, int)) {
	type opening struct {
		char byte
		line int
	}
	stack := []opening{}
	python := ext == ".py" || ext == ".pyi"
	indentLanguage := python || ext == ".yaml" || ext == ".yml" || ext == ".toml" || ext == ".sh" || ext == ".rb" || ext == ".txt" || ext == ""
	hashComments := indentLanguage || ext == ".conf"
	ocaml := ext == ".ml" || ext == ".mli"
	quote := byte(0)
	triple := false
	block := false
	blockStart := 0
	commentDepth := 0
	quoteStart := 0
	type indented struct{ line, col int }
	indents := []indented{}
	previous, previousIndent := -1, 0
	lastContent := -1
	commentStart := -1
	flushComment := func(end int) {
		if commentStart >= 0 {
			add(commentStart, end)
			commentStart = -1
		}
	}
	for i, l := range lines {
		if i%256 == 0 && ctx.Err() != nil {
			return
		}
		v := l.value
		trim := strings.TrimSpace(v)
		fullComment := quote == 0 && !block && (strings.HasPrefix(trim, "//") || hashComments && strings.HasPrefix(trim, "#"))
		if fullComment {
			if commentStart < 0 {
				commentStart = i
			}
		} else {
			flushComment(i - 1)
		}
		if indentLanguage && trim != "" && !fullComment && !block && quote == 0 {
			col := 0
			for _, c := range v {
				if c == ' ' {
					col++
				} else if c == '\t' {
					col += 4 - col%4
				} else {
					break
				}
			}
			for len(indents) > 0 && col <= indents[len(indents)-1].col {
				top := indents[len(indents)-1]
				indents = indents[:len(indents)-1]
				add(top.line, lastContent)
			}
			if previous >= 0 && col > previousIndent {
				indents = append(indents, indented{previous, previousIndent})
			}
			previous, previousIndent = i, col
		}
		if trim != "" {
			lastContent = i
		}
		for j := 0; j < len(v); j++ {
			c := v[j]
			if block {
				if j+1 < len(v) && ((!ocaml && v[j:j+2] == "*/") || (ocaml && v[j:j+2] == "*)")) {
					commentDepth--
					j++
					if commentDepth == 0 {
						block = false
						add(blockStart, i)
					}
				} else if ocaml && j+1 < len(v) && v[j:j+2] == "(*" {
					commentDepth++
					j++
				}
				continue
			}
			if quote != 0 {
				if c == '\\' && quote != '`' {
					j++
					continue
				}
				if c == quote {
					if triple {
						if j+2 < len(v) && v[j+1] == quote && v[j+2] == quote {
							add(quoteStart, i)
							quote = 0
							triple = false
							j += 2
						}
					} else {
						if quote == '`' {
							add(quoteStart, i)
						}
						quote = 0
					}
				}
				continue
			}
			if j+1 < len(v) && ((!ocaml && v[j:j+2] == "/*") || (ocaml && v[j:j+2] == "(*")) {
				block = true
				blockStart = i
				commentDepth = 1
				j++
				continue
			}
			if !ocaml && j+1 < len(v) && v[j:j+2] == "//" || hashComments && c == '#' {
				break
			}
			if c == '"' || c == '\'' || c == '`' {
				// Rust lifetimes and OCaml type variables are not quoted strings.
				if c == '\'' && (ext == ".rs" || ocaml) && !strings.Contains(v[j+1:], "'") {
					continue
				}
				quote = c
				quoteStart = i
				triple = python && j+2 < len(v) && v[j+1] == c && v[j+2] == c
				if triple {
					j += 2
				}
				continue
			}
			switch c {
			case '{', '[', '(':
				stack = append(stack, opening{c, i})
			case '}', ']', ')':
				if len(stack) > 0 {
					top := stack[len(stack)-1]
					if top.char == '{' && c == '}' || top.char == '[' && c == ']' || top.char == '(' && c == ')' {
						stack = stack[:len(stack)-1]
						add(top.line, i)
					}
				}
			}
		}
		// Ordinary strings in these languages do not span physical lines; backtick
		// and Python triple-quoted strings do. Incomplete quotes must not hide EOF.
		if quote != '`' && !triple {
			quote = 0
		}
		if len(stack) > 4096 {
			stack = stack[:4096]
		}
	}
	flushComment(lastContent)
	for _, top := range indents {
		add(top.line, lastContent)
	}
}
