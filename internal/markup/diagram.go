package markup

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/coreequip/mermaidascii"
)

func (r *renderer) diagram(source string) {
	r.diagrams++
	var drawing string
	var err error
	if r.diagrams > 8 {
		err = fmt.Errorf("limit: eight diagrams per preview")
	} else {
		drawing, err = diagramText(source)
	}
	if err != nil {
		r.write("[Mermaid source: "+err.Error()+"]\n", Style{Dim: true})
		r.write(source, Style{Code: true})
		return
	}
	r.hasMedia = true
	r.write(drawing, Style{Code: true})
}

func diagramText(source string) (out string, err error) {
	defer func() {
		if recover() != nil {
			out = ""
			err = fmt.Errorf("renderer could not lay out this diagram")
		}
	}()
	if len(source) > 4096 || strings.Count(source, "\n") > 64 {
		return "", fmt.Errorf("limit: 4 KiB / 64 lines")
	}
	lines := []string{}
	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		if strings.Contains(line, "@{") {
			return "", fmt.Errorf("extended node/actor metadata is not supported")
		}
		if len(line) > 160 {
			return "", fmt.Errorf("limit: 160 bytes per line")
		}
		for _, c := range line {
			if c > unicode.MaxASCII {
				return "", fmt.Errorf("non-ASCII labels are not supported by the diagram layout")
			}
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("empty diagram")
	}
	source = strings.Join(lines, "\n")
	header := strings.Fields(lines[0])
	if header[0] == "sequenceDiagram" {
		if len(header) != 1 {
			return "", fmt.Errorf("unsupported sequence header")
		}
		depth := 0
		for _, line := range lines[1:] {
			if line == "end" {
				depth--
				if depth < 0 {
					return "", fmt.Errorf("unmatched end")
				}
				continue
			}
			if sequenceBlock.MatchString(line) {
				depth++
				continue
			}
			if sequenceDivider.MatchString(line) {
				if depth == 0 {
					return "", fmt.Errorf("divider outside a block")
				}
				continue
			}
			if !sequenceStatement.MatchString(line) {
				return "", fmt.Errorf("unsupported sequence statement")
			}
		}
		if depth != 0 {
			return "", fmt.Errorf("unclosed sequence block")
		}
		s, e := mermaidascii.ParseSequence(source)
		if e != nil {
			return "", e
		}
		if len(s.Actors) == 0 {
			return "", fmt.Errorf("no sequence actors")
		}
		if len(s.Actors) > 8 || len(s.Messages) > 24 || len(s.Blocks) > 8 || len(s.Notes) > 8 {
			return "", fmt.Errorf("sequence limit: 8 actors / 24 messages / 8 blocks / 8 notes")
		}
	} else if header[0] == "flowchart" || header[0] == "graph" {
		if len(header) > 2 || len(header) == 2 && header[1] != "TD" && header[1] != "TB" && header[1] != "LR" {
			return "", fmt.Errorf("supported directions: TD, TB, LR")
		}
		for _, line := range lines[1:] {
			for _, keyword := range []string{"subgraph", "end", "classDef", "class ", "style ", "click ", "linkStyle", "direction "} {
				if strings.HasPrefix(line, keyword) {
					return "", fmt.Errorf("unsupported statement: %s", strings.Fields(line)[0])
				}
			}
			if strings.ContainsAny(line, ";@") || strings.Contains(line, ":::") {
				return "", fmt.Errorf("use one basic node/edge statement per line")
			}
		}
		// The library tolerates unrecognised suffixes and its bare-node scanner
		// can consume an unspaced arrow as part of an ID. Validate the complete
		// statement and separate tokens before handing it to the layout engine.
		for i := 1; i < len(lines); i++ {
			line, ok := normalizeFlowLine(lines[i])
			if !ok {
				return "", fmt.Errorf("unsupported or incomplete flowchart statement")
			}
			lines[i] = line
		}
		source = strings.Join(lines, "\n")
		g, e := mermaidascii.Parse(source)
		if e != nil {
			return "", e
		}
		if len(g.Nodes) == 0 {
			return "", fmt.Errorf("no flowchart nodes")
		}
		if len(g.Nodes) > 16 || len(g.Edges) > 32 {
			return "", fmt.Errorf("flowchart limit: 16 nodes / 32 edges")
		}
	} else {
		return "", fmt.Errorf("supported types: flowchart, graph and sequenceDiagram")
	}
	out, err = mermaidascii.Render(source, mermaidascii.WithPadding(0), mermaidascii.WithSpacing(3, 2))
	if len(out) > 128<<10 {
		return "", fmt.Errorf("diagram output exceeds 128 KiB")
	}
	return out, err
}

var sequenceBlock = regexp.MustCompile(`^(loop|alt|opt|par|critical|break|rect)\b`)
var sequenceDivider = regexp.MustCompile(`^(else|and)\b`)
var sequenceStatement = regexp.MustCompile(`^(?:(?:participant|actor)\s+\S+(?:\s+as\s+.+)?|(?i:note)\s+(?i:left of|right of|over)\s+[^:]+:\s*.+|\S+?\s*(?:-->>|--x|--\)|-->|->>|-x|-\)|->)\s*[+-]?\S+?\s*:\s*.+)$`)

var flowCompactEdge = regexp.MustCompile(`^<?(?:-->|-\.->|==>|---|-\.-|===)(?:\|[^|]*\|)?`)
var flowTextEdge = regexp.MustCompile(`^<?(?:--|-\.|==)\s+.+?\s+(?:-->|---|\.->|-\.-|==>|===)`)

func normalizeFlowLine(line string) (string, bool) {
	line = strings.TrimSpace(strings.SplitN(line, "%%", 2)[0])
	if line == "" {
		return "", true
	}
	var out []string
	group := func() bool {
		for {
			line = strings.TrimSpace(line)
			end := 0
			word := func(c byte) bool {
				return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
			}
			for end < len(line) {
				if word(line[end]) || end > 0 && line[end] == '-' && end+1 < len(line) && word(line[end+1]) {
					end++
					continue
				}
				break
			}
			if end == 0 {
				return false
			}
			for _, pair := range [][2]string{{"(((", ")))"}, {"([", "])"}, {"((", "))"}, {"[[", "]]"}, {"[(", ")]"}, {"{{", "}}"}, {"[/", "\\]"}, {"[\\", "/]"}, {"[/", "/]"}, {"[", "]"}, {"(", ")"}, {"{", "}"}} {
				if !strings.HasPrefix(line[end:], pair[0]) {
					continue
				}
				start := end + len(pair[0])
				close := strings.Index(line[start:], pair[1])
				if close <= 0 {
					continue
				}
				end = start + close + len(pair[1])
				break
			}
			out = append(out, line[:end])
			line = strings.TrimSpace(line[end:])
			if !strings.HasPrefix(line, "&") {
				return true
			}
			out = append(out, "&")
			line = line[1:]
		}
	}
	if !group() {
		return "", false
	}
	for line != "" {
		edge := flowTextEdge.FindString(line)
		if edge == "" {
			edge = flowCompactEdge.FindString(line)
		}
		if edge == "" {
			return "", false
		}
		out = append(out, edge)
		line = line[len(edge):]
		if !group() {
			return "", false
		}
	}
	return strings.Join(out, " "), true
}
