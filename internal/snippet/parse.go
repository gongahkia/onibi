// Package snippet parses and expands the LSP/VS Code snippet language without
// executing shell code. Parsing, expansion, nesting and regex work are bounded.
package snippet

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxSource = 64 << 10
const MaxExpansion = 128 << 10
const maxNodes = 2048
const maxDepth = 32

type node struct {
	ID          int
	Text        string
	Number      int
	Variable    string
	Placeholder bool
	Default     bool
	Children    []*node
	Choices     []string
	Transform   *transform
}
type Program struct {
	roots       []*node
	definitions map[int]*node
	transforms  int
}
type parser struct {
	source        string
	offset, count int
}

func Compile(source string) (*Program, error) {
	if len(source) > MaxSource || !utf8.ValidString(source) {
		return nil, fmt.Errorf("snippet must be UTF-8 and at most 64 KiB")
	}
	p := parser{source: source}
	roots, err := p.nodes(false, 0)
	if err != nil {
		return nil, err
	}
	return &Program{roots: roots}, nil
}
func (p *parser) fail(message string) error {
	return fmt.Errorf("snippet at byte %d: %s", p.offset, message)
}
func (p *parser) newNode() (*node, error) {
	p.count++
	if p.count > maxNodes {
		return nil, p.fail("too many nodes (maximum 2048)")
	}
	return &node{ID: p.count}, nil
}
func (p *parser) nodes(nested bool, depth int) ([]*node, error) {
	if depth > maxDepth {
		return nil, p.fail("nesting exceeds 32 levels")
	}
	out := []*node{}
	var plain strings.Builder
	flush := func() error {
		if plain.Len() == 0 {
			return nil
		}
		n, e := p.newNode()
		if e != nil {
			return e
		}
		n.Text = plain.String()
		plain.Reset()
		out = append(out, n)
		return nil
	}
	for p.offset < len(p.source) {
		c := p.source[p.offset]
		if nested && c == '}' {
			if e := flush(); e != nil {
				return nil, e
			}
			p.offset++
			return out, nil
		}
		if c == '\\' && p.offset+1 < len(p.source) && strings.ContainsRune("\\$}", rune(p.source[p.offset+1])) {
			plain.WriteByte(p.source[p.offset+1])
			p.offset += 2
			continue
		}
		if c != '$' || p.offset+1 >= len(p.source) || (!identifier(p.source[p.offset+1], true) && !digit(p.source[p.offset+1]) && p.source[p.offset+1] != '{') {
			plain.WriteByte(c)
			p.offset++
			continue
		}
		if e := flush(); e != nil {
			return nil, e
		}
		n, e := p.reference(depth + 1)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	if nested {
		return nil, p.fail("unclosed placeholder")
	}
	if e := flush(); e != nil {
		return nil, e
	}
	return out, nil
}
func digit(c byte) bool { return c >= '0' && c <= '9' }
func identifier(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || !first && digit(c)
}
func (p *parser) reference(depth int) (*node, error) {
	p.offset++
	braced := p.offset < len(p.source) && p.source[p.offset] == '{'
	if braced {
		p.offset++
	}
	n, e := p.newNode()
	if e != nil {
		return nil, e
	}
	start := p.offset
	if start >= len(p.source) {
		return nil, p.fail("missing reference")
	}
	if digit(p.source[start]) {
		for p.offset < len(p.source) && digit(p.source[p.offset]) {
			p.offset++
		}
		value, e := strconv.Atoi(p.source[start:p.offset])
		if e != nil || value > 1000000 {
			return nil, p.fail("tabstop number exceeds 1000000")
		}
		n.Placeholder = true
		n.Number = value
	} else if identifier(p.source[start], true) {
		for p.offset < len(p.source) && identifier(p.source[p.offset], false) {
			p.offset++
		}
		n.Variable = p.source[start:p.offset]
	} else {
		return nil, p.fail("expected a tabstop number or variable")
	}
	if !braced {
		return n, nil
	}
	if p.offset >= len(p.source) {
		return nil, p.fail("unclosed reference")
	}
	switch p.source[p.offset] {
	case '}':
		p.offset++
		return n, nil
	case ':':
		p.offset++
		n.Default = true
		n.Children, e = p.nodes(true, depth)
		return n, e
	case '|':
		if !n.Placeholder {
			return nil, p.fail("choices require a numbered tabstop")
		}
		p.offset++
		n.Default = true
		n.Choices, e = p.choices()
		return n, e
	case '/':
		n.Transform, e = p.transform(depth)
		return n, e
	default:
		return nil, p.fail("invalid reference suffix")
	}
}
func (p *parser) choices() ([]string, error) {
	out := []string{}
	var item strings.Builder
	for p.offset < len(p.source) {
		c := p.source[p.offset]
		p.offset++
		if c == '\\' && p.offset < len(p.source) && strings.ContainsRune("\\,|", rune(p.source[p.offset])) {
			item.WriteByte(p.source[p.offset])
			p.offset++
			continue
		}
		if c == '|' && p.offset < len(p.source) && p.source[p.offset] == '}' {
			p.offset++
			return append(out, item.String()), nil
		}
		if c == ',' {
			out = append(out, item.String())
			item.Reset()
			if len(out) >= 256 {
				return nil, p.fail("more than 256 choices")
			}
		} else {
			item.WriteByte(c)
		}
	}
	return nil, p.fail("unclosed choice")
}

type format struct {
	Text    string
	Group   int
	Kind    string
	Yes, No []format
}
type transform struct {
	Pattern, Flags string
	Format         []format
	re             regex
}

func (p *parser) transform(depth int) (*transform, error) {
	p.offset++
	var pattern strings.Builder
	closed := false
	inClass := false
	for p.offset < len(p.source) {
		c := p.source[p.offset]
		p.offset++
		if c == '\\' && p.offset < len(p.source) {
			pattern.WriteByte(c)
			pattern.WriteByte(p.source[p.offset])
			p.offset++
			continue
		}
		if c == '[' {
			inClass = true
		}
		if c == ']' {
			inClass = false
		}
		if c == '/' && !inClass {
			closed = true
			break
		}
		pattern.WriteByte(c)
	}
	if !closed {
		return nil, p.fail("unclosed transform regex")
	}
	f, delim, err := p.formats("/", depth+1)
	if err != nil {
		return nil, err
	}
	if delim != '/' {
		return nil, p.fail("unclosed transform format")
	}
	start := p.offset
	for p.offset < len(p.source) && p.source[p.offset] != '}' {
		p.offset++
	}
	if p.offset >= len(p.source) {
		return nil, p.fail("unclosed transform flags")
	}
	flags := p.source[start:p.offset]
	p.offset++
	result := &transform{Pattern: pattern.String(), Flags: flags, Format: f}
	if err = result.compile(); err != nil {
		return nil, err
	}
	return result, nil
}
func (p *parser) formats(end string, depth int) ([]format, byte, error) {
	if depth > maxDepth {
		return nil, 0, p.fail("transform format nesting exceeds 32")
	}
	out := []format{}
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out = append(out, format{Text: plain.String()})
			plain.Reset()
		}
	}
	for p.offset < len(p.source) {
		c := p.source[p.offset]
		if strings.ContainsRune(end, rune(c)) {
			flush()
			p.offset++
			return out, c, nil
		}
		if c == '\\' && p.offset+1 < len(p.source) && strings.ContainsRune("\\$}/:", rune(p.source[p.offset+1])) {
			plain.WriteByte(p.source[p.offset+1])
			p.offset += 2
			continue
		}
		if c != '$' {
			plain.WriteByte(c)
			p.offset++
			continue
		}
		start := p.offset
		p.offset++
		braced := p.offset < len(p.source) && p.source[p.offset] == '{'
		if braced {
			p.offset++
		}
		digits := p.offset
		for p.offset < len(p.source) && digit(p.source[p.offset]) {
			p.offset++
		}
		if digits == p.offset {
			p.offset = start + 1
			plain.WriteByte('$')
			continue
		}
		num, e := strconv.Atoi(p.source[digits:p.offset])
		if e != nil || num > 1000000 {
			return nil, 0, p.fail("capture number too large")
		}
		flush()
		f := format{Kind: "capture", Group: num}
		if braced {
			if p.offset >= len(p.source) {
				return nil, 0, p.fail("unclosed capture format")
			}
			switch p.source[p.offset] {
			case '}':
				p.offset++
			case ':':
				p.offset++
				if p.offset >= len(p.source) {
					return nil, 0, p.fail("unclosed capture modifier")
				}
				switch p.source[p.offset] {
				case '/':
					p.offset++
					from := p.offset
					for p.offset < len(p.source) && p.source[p.offset] != '}' {
						p.offset++
					}
					if p.offset >= len(p.source) {
						return nil, 0, p.fail("unclosed case modifier")
					}
					f.Kind = p.source[from:p.offset]
					p.offset++
					switch f.Kind {
					case "upcase", "downcase", "capitalize", "camelcase", "pascalcase", "snakecase", "kebabcase":
					default:
						return nil, 0, p.fail("unknown case modifier")
					}
				case '+':
					p.offset++
					f.Kind = "if"
					f.Yes, _, e = p.formats("}", depth+1)
				case '-':
					p.offset++
					f.Kind = "else"
					f.No, _, e = p.formats("}", depth+1)
				case '?':
					p.offset++
					f.Kind = "conditional"
					var delim byte
					f.Yes, delim, e = p.formats(":}", depth+1)
					if e == nil && delim == ':' {
						f.No, _, e = p.formats("}", depth+1)
					} else if e == nil {
						return nil, 0, p.fail("conditional format needs an else branch")
					}
				default:
					f.Kind = "else"
					f.No, _, e = p.formats("}", depth+1)
				}
				if e != nil {
					return nil, 0, e
				}
			default:
				return nil, 0, p.fail("invalid capture modifier")
			}
		}
		out = append(out, f)
		p.count++
		if p.count > maxNodes {
			return nil, 0, p.fail("too many snippet format nodes")
		}
	}
	return nil, 0, p.fail("unclosed transform format")
}
