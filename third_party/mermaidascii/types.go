package mermaidascii

// Shape is the outline drawn around a node's label.
type Shape int

const (
	Rectangle     Shape = iota
	Rounded             // (text)
	Stadium             // ([text])
	Cylinder            // [(text)]
	Circle              // ((text))
	Diamond             // {text}
	Hexagon             // {{text}}
	Parallelogram       // [/text/]
	Trapezoid           // [/text\]
	Subroutine          // [[text]]
)

// EdgeStyle is the line style of an edge.
type EdgeStyle int

const (
	Solid  EdgeStyle = iota // -->
	Dotted                  // -.->
	Thick                   // ==>
)

// Direction is the layout direction of a flowchart.
type Direction int

const (
	LeftRight Direction = iota // LR, RL is mirrored onto this
	TopDown                    // TD/TB, BT is mirrored onto this
)

// Node is a box in a flowchart.
type Node struct {
	// ID is the identity used by edges, e.g. "A" in "A[Label] --> B".
	ID string
	// Label is the text drawn inside the box. Defaults to ID.
	Label string
	Shape Shape
	// Index is the order of first appearance, which decides layout ties.
	Index int
}

// Edge connects two nodes.
type Edge struct {
	From, To string
	// Label is drawn next to the line, e.g. "yes" in "A -->|yes| B".
	Label string
	Style EdgeStyle
	// ArrowStart and ArrowEnd tell which ends carry an arrowhead.
	// "A --- B" has neither, "A <--> B" has both.
	ArrowStart, ArrowEnd bool
}

// Graph is a parsed flowchart.
type Graph struct {
	Direction Direction
	Nodes     []*Node
	Edges     []*Edge
	// byID resolves an ID to its node in constant time. Nodes may be
	// referenced before they are declared, so the parser creates them on
	// first sight and fills in label and shape later.
	byID map[string]*Node
}

// node returns the node for id, creating it on first sight.
func (g *Graph) node(id string) *Node {
	if g.byID == nil {
		g.byID = make(map[string]*Node)
	}
	if n, ok := g.byID[id]; ok {
		return n
	}
	n := &Node{ID: id, Label: id, Index: len(g.Nodes)}
	g.byID[id] = n
	g.Nodes = append(g.Nodes, n)
	return n
}

// Node returns the node with the given id, or nil.
func (g *Graph) Node(id string) *Node { return g.byID[id] }
