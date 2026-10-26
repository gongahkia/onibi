package markup

import (
	"fmt"
	"html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func (r *renderer) markdown(source string) {
	data := []byte(source)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote, extension.DefinitionList))
	root := md.Parser().Parse(gtext.NewReader(data))
	r.mdNode(root, data, Style{}, 0)
}
func decoded(s []byte) string { return html.UnescapeString(string(util.UnescapePunctuations(s))) }
func (r *renderer) mdChildren(n ast.Node, data []byte, style Style, depth int) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.mdNode(c, data, style, depth+1)
		if r.err != nil {
			return
		}
	}
}
func (r *renderer) mdNode(n ast.Node, data []byte, style Style, depth int) {
	if !r.visit(depth) {
		return
	}
	switch n := n.(type) {
	case *ast.Document:
		r.mdChildren(n, data, style, depth)
	case *ast.Heading:
		r.heading(n.Level, func(s Style) { r.mdChildren(n, data, s, depth) })
	case *ast.Paragraph:
		r.mdChildren(n, data, style, depth)
		r.newline(2)
	case *ast.TextBlock:
		r.mdChildren(n, data, style, depth)
		r.newline(1)
	case *ast.Text:
		value := string(n.Segment.Value(data))
		if !n.IsRaw() && !style.Code {
			value = decoded(n.Segment.Value(data))
		}
		r.write(value, style)
		if n.HardLineBreak() {
			r.write("\n", style)
		} else if n.SoftLineBreak() {
			r.write(" ", style)
		}
	case *ast.String:
		r.write(string(n.Value), style)
	case *ast.Emphasis:
		if n.Level == 2 {
			style.Bold = true
		} else {
			style.Italic = true
		}
		r.mdChildren(n, data, style, depth)
	case *east.Strikethrough:
		style.Strike = true
		r.mdChildren(n, data, style, depth)
	case *ast.CodeSpan:
		style.Code = true
		var s strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*ast.Text); ok {
				s.Write(t.Segment.Value(data))
			}
		}
		v := strings.ReplaceAll(s.String(), "\n", " ")
		if len(v) >= 2 && v[0] == ' ' && v[len(v)-1] == ' ' && strings.TrimSpace(v) != "" {
			v = v[1 : len(v)-1]
		}
		r.write(v, style)
	case *ast.FencedCodeBlock:
		r.newline(2)
		if lang := string(n.Language(data)); lang != "" {
			r.write("┌ "+lang+"\n", Style{Dim: true})
		}
		source, language := string(n.Lines().Value(data)), string(n.Language(data))
		if language == "mermaid" && r.options.Media {
			r.diagram(source)
		} else {
			r.codeBlock(source, language)
		}
		r.newline(2)
	case *ast.CodeBlock:
		r.newline(2)
		r.mdLines(n, data, Style{Code: true})
		r.newline(2)
	case *ast.Blockquote:
		r.newline(1)
		if r.out.Len() > 0 && r.out.Len() == r.blockStart {
			r.write("▎ ", Style{Dim: true})
			r.blockStart = r.out.Len()
		}
		prefix := r.prefix
		r.prefix += "▎ "
		r.mdChildren(n, data, style, depth)
		r.prefix = prefix
		r.newline(2)
	case *ast.List:
		if r.out.Len() > 0 && r.out.Len() == r.blockStart {
			r.blockStart = -1
		}
		r.newline(1)
		i := n.Start
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			bullet := "• "
			if n.IsOrdered() {
				bullet = fmt.Sprintf("%d. ", i)
				i++
			}
			r.mdItem(bullet, func() { r.mdNode(c, data, style, depth+1) })
		}
		r.newline(1)
	case *ast.ListItem:
		r.mdChildren(n, data, style, depth)
	case *east.FootnoteLink:
		r.linePrefix()
		r.setAnchor(fmt.Sprintf("reference:%d:%d", n.Index, n.RefIndex), r.out.Len())
		r.write(fmt.Sprintf("[%d]", n.Index), Style{Underline: true, Anchor: fmt.Sprintf("footnote:%d", n.Index)})
	case *east.FootnoteBacklink:
		r.write(" ↩", Style{Underline: true, Anchor: fmt.Sprintf("reference:%d:%d", n.Index, n.RefIndex)})
	case *east.FootnoteList:
		r.newline(2)
		r.write("Footnotes", Style{Bold: true})
		r.newline(2)
		r.mdChildren(n, data, style, depth)
	case *east.Footnote:
		r.newline(1)
		r.linePrefix()
		r.setAnchor(fmt.Sprintf("footnote:%d", n.Index), r.out.Len())
		r.mdItem(fmt.Sprintf("[%d] ", n.Index), func() { r.mdChildren(n, data, style, depth) })
	case *east.DefinitionList:
		r.newline(2)
		r.mdChildren(n, data, style, depth)
		r.newline(2)
	case *east.DefinitionTerm:
		r.newline(1)
		style.Bold = true
		r.mdChildren(n, data, style, depth)
		r.newline(1)
	case *east.DefinitionDescription:
		r.mdItem("  ", func() { r.mdChildren(n, data, style, depth) })
	case *east.TaskCheckBox:
		if n.IsChecked {
			r.write("☑ ", Style{Dim: true})
		} else {
			r.write("☐ ", Style{Dim: true})
		}
	case *ast.Link:
		style.Target = LinkTarget(decoded(n.Destination))
		style.Link = SafeURL(style.Target)
		style.Underline = true
		r.mdChildren(n, data, style, depth)
	case *ast.AutoLink:
		style.Target = LinkTarget(string(n.URL(data)))
		style.Link = SafeURL(style.Target)
		style.Underline = true
		r.write(string(n.Label(data)), style)
	case *ast.Image:
		if style.Target == "" && style.Anchor == "" {
			style.Target = LinkTarget(decoded(n.Destination))
			style.Link = SafeURL(style.Target)
		}
		labelStyle := style
		labelStyle.Dim = true
		r.write("[image: ", labelStyle)
		r.mdChildren(n, data, style, depth)
		r.write("]", labelStyle)
		if len(n.Destination) > 0 {
			r.write(" ("+decoded(n.Destination)+")", labelStyle)
		}
		if r.options.Media {
			r.image(decoded(n.Destination))
		}
	case *ast.ThematicBreak:
		r.newline(1)
		r.write("────────────────────────\n", Style{Dim: true})
		r.newline(2)
	case *east.Table:
		var rows [][]*renderer
		cells := 0
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			var out []*renderer
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells++
				if cells > 4096 {
					r.err = fmt.Errorf("preview table exceeds 4096 cells")
					return
				}
				child := &renderer{ctx: r.ctx}
				s := style
				if row.Kind() == east.KindTableHeader {
					s.Bold = true
				}
				child.mdChildren(cell, data, s, depth+2)
				out = append(out, child)
			}
			rows = append(rows, out)
		}
		alignments := make([]alignment, len(n.Alignments))
		for i, a := range n.Alignments {
			switch a {
			case east.AlignRight:
				alignments[i] = alignRight
			case east.AlignCenter:
				alignments[i] = alignCenter
			}
		}
		r.table(rows, true, alignments)
	case *ast.HTMLBlock:
		r.newline(1)
		r.mdLines(n, data, Style{Dim: true})
		if n.HasClosure() {
			v := n.ClosureLine
			r.write(string(v.Value(data)), Style{Dim: true})
		}
		r.newline(2)
	case *ast.RawHTML:
		for i := 0; i < n.Segments.Len(); i++ {
			s := n.Segments.At(i)
			r.write(string(s.Value(data)), Style{Dim: true})
		}
	default:
		r.mdChildren(n, data, style, depth)
	}
}
func (r *renderer) mdLines(n ast.Node, data []byte, style Style) {
	for i := 0; i < n.Lines().Len(); i++ {
		s := n.Lines().At(i)
		r.write(string(s.Value(data)), style)
	}
}

// Keep each container's continuation lines aligned with its content. Blocks
// such as headings/code can start immediately after a marker without an empty
// bullet line; nested containers inherit the accumulated continuation prefix.
func (r *renderer) mdItem(marker string, content func()) {
	r.newline(1)
	r.write(marker, Style{Dim: true})
	prefix, start := r.prefix, r.blockStart
	r.prefix += strings.Repeat(" ", len([]rune(marker)))
	r.blockStart = r.out.Len()
	content()
	r.prefix, r.blockStart = prefix, start
	r.newline(1)
}
