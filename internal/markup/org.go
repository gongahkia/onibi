package markup

import (
	"fmt"
	"strings"

	"github.com/niklasfasching/go-org/org"
)

func (r *renderer) org(source string) {
	cfg := org.New().Silent()
	cfg.ReadFile = func(string) ([]byte, error) {
		return nil, fmt.Errorf("includes and setup files are not expanded in previews")
	}
	d := cfg.Parse(strings.NewReader(source), "")
	if d.Error != nil {
		r.err = d.Error
		return
	}
	r.orgNodes(d.Nodes, Style{}, 0)
}
func (r *renderer) orgNodes(nodes []org.Node, s Style, depth int) {
	for _, n := range nodes {
		r.orgNode(n, s, depth+1)
		if r.err != nil {
			return
		}
	}
}
func (r *renderer) orgNode(node org.Node, s Style, depth int) {
	if !r.visit(depth) {
		return
	}
	switch n := node.(type) {
	case org.Headline:
		r.heading(n.Lvl, func(s Style) {
			if n.Status != "" {
				r.write(n.Status+" ", s)
			}
			if n.Priority != "" {
				r.write("[#"+n.Priority+"] ", s)
			}
			r.orgNodes(n.Title, s, depth)
			if len(n.Tags) > 0 {
				r.write("  :"+strings.Join(n.Tags, ":")+":", Style{Dim: true})
			}
		})
		r.orgNodes(n.Children, s, depth)
	case org.Paragraph:
		r.orgNodes(n.Children, s, depth)
		if !s.Code {
			r.newline(2)
		}
	case org.Text:
		r.write(n.Content, s)
	case org.LineBreak:
		if s.Code {
			r.write(strings.Repeat("\n", max(1, n.Count)), s)
		} else {
			r.write(" ", s)
		}
	case org.ExplicitLineBreak:
		r.write("\n", s)
	case org.Emphasis:
		switch n.Kind {
		case "*":
			s.Bold = true
		case "/":
			s.Italic = true
		case "_":
			s.Underline = true
		case "+":
			s.Strike = true
		case "=", "~":
			s.Code = true
		}
		r.orgNodes(n.Content, s, depth)
	case org.RegularLink:
		s.Link = SafeURL(n.URL)
		s.Underline = true
		if len(n.Description) > 0 {
			r.orgNodes(n.Description, s, depth)
		} else {
			r.write(n.URL, s)
		}
	case org.Block:
		r.newline(2)
		if n.Name == "SRC" || n.Name == "EXAMPLE" || n.Name == "EXPORT" {
			label := strings.ToLower(n.Name)
			if len(n.Parameters) > 0 {
				label += " " + strings.Join(n.Parameters, " ")
			}
			r.write("┌ "+label+"\n", Style{Dim: true})
			s.Code = true
		} else if n.Name == "QUOTE" {
			r.write("▎ ", Style{Dim: true})
			s.Italic = true
		}
		r.orgNodes(n.Children, s, depth)
		r.newline(2)
	case org.Example:
		s.Code = true
		r.newline(1)
		r.orgNodes(n.Children, s, depth)
		r.newline(2)
	case org.InlineBlock:
		s.Code = true
		r.orgNodes(n.Children, s, depth)
	case org.List:
		r.newline(1)
		r.orgNodes(n.Items, s, depth)
		r.newline(1)
	case org.ListItem:
		bullet := "•"
		if n.Bullet != "-" && n.Bullet != "+" && n.Bullet != "*" {
			bullet = n.Bullet
		}
		r.write(bullet+" ", Style{Dim: true})
		if n.Status != "" {
			mark := "☐ "
			if n.Status == "X" {
				mark = "☑ "
			} else if n.Status == "-" {
				mark = "◩ "
			}
			r.write(mark, Style{Dim: true})
		}
		r.orgNodes(n.Children, s, depth)
		r.newline(1)
	case org.DescriptiveListItem:
		r.write("• ", Style{Dim: true})
		bold := s
		bold.Bold = true
		r.orgNodes(n.Term, bold, depth)
		r.write(" — ", s)
		r.orgNodes(n.Details, s, depth)
		r.newline(1)
	case org.Table:
		var rows [][]*renderer
		cells := 0
		for _, row := range n.Rows {
			if len(row.Columns) == 0 {
				continue
			}
			var out []*renderer
			for _, col := range row.Columns {
				cells++
				if cells > 4096 {
					r.err = fmt.Errorf("preview table exceeds 4096 cells")
					return
				}
				child := &renderer{ctx: r.ctx}
				child.orgNodes(col.Children, s, depth)
				out = append(out, child)
			}
			rows = append(rows, out)
		}
		r.table(rows, len(n.SeparatorIndices) > 0)
	case org.HorizontalRule:
		r.newline(1)
		r.write("────────────────────────\n", Style{Dim: true})
		r.newline(2)
	case org.Keyword:
		if n.Key == "TITLE" {
			r.heading(1, func(s Style) { r.write(n.Value, s) })
		} else if n.Key == "AUTHOR" || n.Key == "DATE" {
			r.write(n.Value+"\n", Style{Dim: true})
		} else {
			r.write("#+"+n.Key+": "+n.Value+"\n", Style{Dim: true})
		}
	case org.Include:
		r.write("#+"+n.Key+": "+n.Value+" (not expanded)\n", Style{Dim: true})
	case org.Comment:
		r.write("# "+n.Content+"\n", Style{Dim: true})
	case org.NodeWithMeta:
		r.orgNode(n.Node, s, depth+1)
	case org.NodeWithName:
		r.orgNode(n.Node, s, depth+1)
	case org.Drawer:
		r.write(":"+n.Name+":\n", Style{Dim: true})
		r.orgNodes(n.Children, s, depth)
		r.newline(1)
	case org.PropertyDrawer:
		for _, p := range n.Properties {
			r.write(strings.Join(p, ": ")+"\n", Style{Dim: true})
		}
	case org.Result:
		r.orgNode(n.Node, s, depth+1)
	case org.StatisticToken:
		r.write(n.Content, s)
	case org.Timestamp:
		r.write(n.Time.Format("2006-01-02 Mon 15:04")+n.Interval, s)
	case org.Macro:
		r.write("{{{"+n.Name+"("+strings.Join(n.Parameters, ",")+")}}}", Style{Dim: true})
	case org.LatexFragment:
		r.write(n.OpeningPair, s)
		r.orgNodes(n.Content, s, depth)
		r.write(n.ClosingPair, s)
	case org.LatexBlock:
		r.orgNodes(n.Content, Style{Code: true}, depth)
		r.newline(2)
	case org.FootnoteLink:
		r.write("["+n.Name+"]", Style{Underline: true})
		if n.Definition != nil && n.Definition.Inline {
			r.orgNodes(n.Definition.Children, s, depth)
		}
	case org.FootnoteDefinition:
		r.write("["+n.Name+"] ", s)
		r.orgNodes(n.Children, s, depth)
		r.newline(2)
	}
}
