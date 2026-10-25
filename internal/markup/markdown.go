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
	md := goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList, extension.Linkify))
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
		r.mdLines(n, data, Style{Code: true})
		r.newline(2)
	case *ast.CodeBlock:
		r.newline(2)
		r.mdLines(n, data, Style{Code: true})
		r.newline(2)
	case *ast.Blockquote:
		r.newline(1)
		r.write("▎ ", Style{Dim: true})
		style.Italic = true
		r.mdChildren(n, data, style, depth)
		r.newline(2)
	case *ast.List:
		r.newline(1)
		i := n.Start
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			r.write(strings.Repeat("  ", min(8, mdListDepth(n))), Style{})
			bullet := "• "
			if n.IsOrdered() {
				bullet = fmt.Sprintf("%d. ", i)
				i++
			}
			r.write(bullet, Style{Dim: true})
			r.mdNode(c, data, style, depth+1)
			r.newline(1)
		}
		r.newline(1)
	case *ast.ListItem:
		r.mdChildren(n, data, style, depth)
	case *east.TaskCheckBox:
		if n.IsChecked {
			r.write("☑ ", Style{Dim: true})
		} else {
			r.write("☐ ", Style{Dim: true})
		}
	case *ast.Link:
		style.Link = SafeURL(decoded(n.Destination))
		style.Underline = true
		r.mdChildren(n, data, style, depth)
	case *ast.AutoLink:
		style.Link = SafeURL(string(n.URL(data)))
		style.Underline = true
		r.write(string(n.Label(data)), style)
	case *ast.Image:
		r.write("[image: ", Style{Dim: true})
		r.mdChildren(n, data, style, depth)
		r.write("]", Style{Dim: true})
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
		r.table(rows, true)
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
func mdListDepth(n ast.Node) int {
	d := 0
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.List); ok {
			d++
		}
	}
	return d
}
