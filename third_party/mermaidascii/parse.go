package mermaidascii

import (
	"regexp"
	"strings"
)

// Parse reads Mermaid flowchart source into a Graph.
//
// Recognised: `flowchart`/`graph` with a direction, node shapes, edge styles,
// edge labels in both spellings (`-->|text|` and `-- text -->`), chaining
// (`A --> B --> C`) and parallel links (`A & B --> C`).
//
// Not recognised yet: subgraphs, classDef/style, click handlers. Those lines
// are skipped rather than reported — a diagram that uses them still renders,
// it just renders without them.
func Parse(src string) (*Graph, error) {
	g := &Graph{Direction: TopDown}

	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(stripComment(line))
		if line == "" {
			continue
		}
		if i == 0 || isHeader(line) {
			if dir, ok := parseHeader(line); ok {
				g.Direction = dir
				continue
			}
		}
		if isIgnoredLine(line) {
			continue
		}
		parseEdgeLine(line, g)
	}
	return g, nil
}

// stripComment removes a `%%` comment. Mermaid only knows line comments.
func stripComment(line string) string {
	if i := strings.Index(line, "%%"); i >= 0 {
		return line[:i]
	}
	return line
}

var headerRe = regexp.MustCompile(`^(?:flowchart|graph)(?:\s+(LR|RL|TD|TB|BT))?\s*$`)

func isHeader(line string) bool { return headerRe.MatchString(line) }

// parseHeader reads the direction off the opening line.
//
// RL and BT are mirrored onto LR and TD: the layout is the same, only walked
// backwards, and mirroring at parse time keeps the layout code free of the
// distinction.
func parseHeader(line string) (Direction, bool) {
	m := headerRe.FindStringSubmatch(line)
	if m == nil {
		return TopDown, false
	}
	switch m[1] {
	case "LR", "RL":
		return LeftRight, true
	default:
		return TopDown, true
	}
}

// ignoredPrefixes are statements this parser does not model yet. They are
// skipped whole so that the rest of the diagram still renders.
var ignoredPrefixes = []string{
	"subgraph", "end", "classDef", "class ", "style ", "click ", "linkStyle",
	"direction ",
}

func isIgnoredLine(line string) bool {
	for _, p := range ignoredPrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// nodePattern matches one node declaration. Order matters: multi-character
// delimiters must be tried before single-character ones, or `A[[text]]` would
// be read as a rectangle holding `[text]`.
type nodePattern struct {
	re    *regexp.Regexp
	shape Shape
}

var nodePatterns = []nodePattern{
	{regexp.MustCompile(`^([\w-]+)\(\(\((.+?)\)\)\)`), Circle},  // A(((text)))
	{regexp.MustCompile(`^([\w-]+)\(\[(.+?)\]\)`), Stadium},     // A([text])
	{regexp.MustCompile(`^([\w-]+)\(\((.+?)\)\)`), Circle},      // A((text))
	{regexp.MustCompile(`^([\w-]+)\[\[(.+?)\]\]`), Subroutine},  // A[[text]]
	{regexp.MustCompile(`^([\w-]+)\[\((.+?)\)\]`), Cylinder},    // A[(text)]
	{regexp.MustCompile(`^([\w-]+)\[/(.+?)\\\]`), Trapezoid},    // A[/text\]
	{regexp.MustCompile(`^([\w-]+)\[\\(.+?)/\]`), Trapezoid},    // A[\text/]
	{regexp.MustCompile(`^([\w-]+)\[/(.+?)/\]`), Parallelogram}, // A[/text/]
	{regexp.MustCompile(`^([\w-]+)\{\{(.+?)\}\}`), Hexagon},     // A{{text}}
	{regexp.MustCompile(`^([\w-]+)\[(.+?)\]`), Rectangle},       // A[text]
	{regexp.MustCompile(`^([\w-]+)\((.+?)\)`), Rounded},         // A(text)
	{regexp.MustCompile(`^([\w-]+)\{(.+?)\}`), Diamond},         // A{text}
}

var (
	bareNodeRe = regexp.MustCompile(`^([\w-]+)`)
	classRe    = regexp.MustCompile(`^:::[\w][\w-]*`)
	// arrowRe covers the compact spelling: an optional back-arrow, the
	// operator, and an optional |label|.
	arrowRe = regexp.MustCompile(`^(<)?(-->|-\.->|==>|---|-\.-|===)(?:\|([^|]*)\|)?`)
	// textArrowRe covers the spread spelling `-- text -->`, where the label
	// sits between the two halves of the arrow.
	textArrowRe = regexp.MustCompile(`^(<)?(--|-\.|==)\s+(.+?)\s+(-->|---|\.->|-\.-|==>|===)`)
)

// parseEdgeLine reads one line of node groups joined by arrows.
func parseEdgeLine(line string, g *Graph) {
	rest := strings.TrimSpace(line)

	from, rest, ok := consumeNodeGroup(rest, g)
	if !ok {
		return
	}

	for rest != "" {
		var (
			label      string
			style      EdgeStyle
			arrowStart bool
			arrowEnd   bool
		)

		if m := arrowRe.FindStringSubmatch(rest); m != nil {
			arrowStart = m[1] != ""
			style = styleFromOp(m[2])
			arrowEnd = strings.HasSuffix(m[2], ">")
			label = normalizeBreaks(strings.TrimSpace(m[3]))
			rest = strings.TrimSpace(rest[len(m[0]):])
		} else if m := textArrowRe.FindStringSubmatch(rest); m != nil {
			arrowStart = m[1] != ""
			style = styleFromOps(m[2], m[4])
			arrowEnd = strings.HasSuffix(m[4], ">")
			label = normalizeBreaks(strings.TrimSpace(m[3]))
			rest = strings.TrimSpace(rest[len(m[0]):])
		} else {
			return
		}

		to, remainder, ok := consumeNodeGroup(rest, g)
		if !ok {
			return
		}
		rest = remainder

		// Parallel links join every source to every target:
		// `A & B --> C & D` is four edges.
		for _, s := range from {
			for _, t := range to {
				g.Edges = append(g.Edges, &Edge{
					From: s, To: t, Label: label, Style: style,
					ArrowStart: arrowStart, ArrowEnd: arrowEnd,
				})
			}
		}
		from = to
	}
}

// consumeNodeGroup reads one or more node declarations joined by `&`.
func consumeNodeGroup(text string, g *Graph) (ids []string, rest string, ok bool) {
	id, rest, ok := consumeNode(text, g)
	if !ok {
		return nil, text, false
	}
	ids = []string{id}

	for strings.HasPrefix(rest, "&") {
		next := strings.TrimSpace(rest[1:])
		id, remainder, ok := consumeNode(next, g)
		if !ok {
			break
		}
		ids = append(ids, id)
		rest = remainder
	}
	return ids, rest, true
}

// consumeNode reads one node declaration from the start of text.
//
// A declaration with brackets sets label and shape; a bare reference only
// names a node that may be declared elsewhere on another line.
func consumeNode(text string, g *Graph) (id, rest string, ok bool) {
	for _, p := range nodePatterns {
		m := p.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		n := g.node(m[1])
		n.Label = normalizeBreaks(unquote(m[2]))
		n.Shape = p.shape
		return n.ID, trimClass(text[len(m[0]):]), true
	}

	m := bareNodeRe.FindStringSubmatch(text)
	if m == nil {
		return "", text, false
	}
	n := g.node(m[1])
	return n.ID, trimClass(text[len(m[0]):]), true
}

// trimClass drops a `:::className` suffix, which styles a node. Styling has
// no meaning without colour, so the name is read and discarded.
func trimClass(rest string) string {
	rest = strings.TrimSpace(rest)
	if m := classRe.FindString(rest); m != "" {
		rest = strings.TrimSpace(rest[len(m):])
	}
	return rest
}

// unquote removes the quotes around a label. Mermaid needs them when a label
// contains brackets; they are not part of the text.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

var brRe = regexp.MustCompile(`(?i)<br\s*/?>`)

// normalizeBreaks turns the HTML line break Mermaid allows in labels into a
// real newline.
func normalizeBreaks(s string) string {
	return brRe.ReplaceAllString(s, "\n")
}

func styleFromOp(op string) EdgeStyle {
	switch op {
	case "-.->", "-.-":
		return Dotted
	case "==>", "===":
		return Thick
	default:
		return Solid
	}
}

// styleFromOps reads the style off the spread spelling, where either half may
// carry it: `-- text ==>` is thick.
func styleFromOps(open, close string) EdgeStyle {
	if open == "==" || close == "==>" || close == "===" {
		return Thick
	}
	if open == "-." || close == ".->" || close == "-.-" {
		return Dotted
	}
	return Solid
}
