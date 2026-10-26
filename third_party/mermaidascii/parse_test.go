package mermaidascii

import "testing"

func TestParseShapes(t *testing.T) {
	g, err := Parse(`flowchart LR
    A[Rectangle] --> B(Rounded)
    B --> C([Stadium])
    C --> D[(Cylinder)]
    D --> E{{Hexagon}}
    E --> F{Diamond}
    F --> G((Circle))
    G --> H[[Subroutine]]`)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		id    string
		label string
		shape Shape
	}{
		{"A", "Rectangle", Rectangle},
		{"B", "Rounded", Rounded},
		{"C", "Stadium", Stadium},
		{"D", "Cylinder", Cylinder},
		{"E", "Hexagon", Hexagon},
		{"F", "Diamond", Diamond},
		{"G", "Circle", Circle},
		{"H", "Subroutine", Subroutine},
	}
	for _, w := range want {
		n := g.Node(w.id)
		if n == nil {
			t.Errorf("node %s missing", w.id)
			continue
		}
		if n.Label != w.label || n.Shape != w.shape {
			t.Errorf("node %s = %q/%v, want %q/%v", w.id, n.Label, n.Shape, w.label, w.shape)
		}
	}
	if len(g.Edges) != 7 {
		t.Errorf("got %d edges, want 7", len(g.Edges))
	}
	if g.Direction != LeftRight {
		t.Errorf("direction = %v, want LeftRight", g.Direction)
	}
}

// TestParseDiamondAfterArrow guards the bug that made this port worthwhile:
// mermaid-ascii reads `B{Frontmatter?}` on the right of an arrow as a node
// literally named "B{Frontmatter?}", and so ends up with two nodes.
func TestParseDiamondAfterArrow(t *testing.T) {
	g, err := Parse(`flowchart TD
    A[Start] --> B{Decision}
    B -->|yes| C[Yes]
    B -->|no| D[No]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 4 {
		var ids []string
		for _, n := range g.Nodes {
			ids = append(ids, n.ID)
		}
		t.Fatalf("got %d nodes %v, want 4", len(g.Nodes), ids)
	}
	b := g.Node("B")
	if b == nil || b.Shape != Diamond || b.Label != "Decision" {
		t.Errorf("B = %+v, want a diamond labelled Decision", b)
	}
	if g.Edges[1].Label != "yes" || g.Edges[2].Label != "no" {
		t.Errorf("edge labels = %q/%q, want yes/no", g.Edges[1].Label, g.Edges[2].Label)
	}
}

func TestParseEdgeStyles(t *testing.T) {
	g, _ := Parse(`flowchart LR
    A --> B
    B -.-> C
    C ==> D
    D --- E
    E <--> F`)

	want := []struct {
		style                EdgeStyle
		arrowStart, arrowEnd bool
	}{
		{Solid, false, true},
		{Dotted, false, true},
		{Thick, false, true},
		{Solid, false, false},
		{Solid, true, true},
	}
	if len(g.Edges) != len(want) {
		t.Fatalf("got %d edges, want %d", len(g.Edges), len(want))
	}
	for i, w := range want {
		e := g.Edges[i]
		if e.Style != w.style || e.ArrowStart != w.arrowStart || e.ArrowEnd != w.arrowEnd {
			t.Errorf("edge %d = %v/%v/%v, want %v/%v/%v",
				i, e.Style, e.ArrowStart, e.ArrowEnd, w.style, w.arrowStart, w.arrowEnd)
		}
	}
}

func TestParseTextArrowLabels(t *testing.T) {
	g, _ := Parse(`flowchart LR
    A -- yes --> B
    B -. maybe .-> C
    C == sure ==> D`)

	want := []struct {
		label string
		style EdgeStyle
	}{
		{"yes", Solid},
		{"maybe", Dotted},
		{"sure", Thick},
	}
	if len(g.Edges) != len(want) {
		t.Fatalf("got %d edges, want %d", len(g.Edges), len(want))
	}
	for i, w := range want {
		if g.Edges[i].Label != w.label || g.Edges[i].Style != w.style {
			t.Errorf("edge %d = %q/%v, want %q/%v", i, g.Edges[i].Label, g.Edges[i].Style, w.label, w.style)
		}
	}
}

func TestParseChainingAndParallel(t *testing.T) {
	g, _ := Parse(`flowchart LR
    A --> B --> C
    D & E --> F & G`)

	if len(g.Edges) != 6 {
		t.Fatalf("got %d edges, want 6 (2 chained + 4 parallel)", len(g.Edges))
	}
	pairs := map[string]bool{}
	for _, e := range g.Edges {
		pairs[e.From+"->"+e.To] = true
	}
	for _, want := range []string{"A->B", "B->C", "D->F", "D->G", "E->F", "E->G"} {
		if !pairs[want] {
			t.Errorf("edge %s missing", want)
		}
	}
}

func TestParseIgnoresUnsupportedLines(t *testing.T) {
	g, _ := Parse(`flowchart TD
    %% a comment
    classDef big fill:#f9f
    A[Start]:::big --> B[End]
    style A stroke:#333`)

	if len(g.Nodes) != 2 || len(g.Edges) != 1 {
		t.Fatalf("got %d nodes / %d edges, want 2 / 1", len(g.Nodes), len(g.Edges))
	}
	if g.Node("A").Label != "Start" {
		t.Errorf("A = %q, want Start", g.Node("A").Label)
	}
}

func TestParseDirections(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want Direction
	}{
		{"flowchart LR\n A --> B", LeftRight},
		{"flowchart RL\n A --> B", LeftRight},
		{"flowchart TD\n A --> B", TopDown},
		{"flowchart TB\n A --> B", TopDown},
		{"flowchart BT\n A --> B", TopDown},
		{"graph LR\n A --> B", LeftRight},
		{"graph\n A --> B", TopDown},
	} {
		g, _ := Parse(tc.src)
		if g.Direction != tc.want {
			t.Errorf("%q: direction = %v, want %v", tc.src, g.Direction, tc.want)
		}
	}
}
