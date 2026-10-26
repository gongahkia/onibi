package mermaidascii

// The layout works on a coarse grid, not on characters. Every node occupies a
// 3x3 block of grid cells: the middle cell holds the label, the eight around
// it are the border and the attachment points for edges. Levels sit four grid
// units apart, which leaves one free cell ring between neighbours for edges to
// travel through.
//
// Only afterwards is the grid measured in characters: each grid column gets
// the width of the widest thing in it, each row the tallest. That is what
// makes columns line up even when the boxes differ in size.

// gridPos is a cell in the layout grid.
type gridPos struct{ x, y int }

// placed is a node with its position on the grid.
type placed struct {
	node *Node
	pos  gridPos
	box  box
	// draw is the top left corner in character coordinates.
	draw gridPos
}

// layout is the result of placing a graph on the grid.
type layout struct {
	g         *Graph
	cfg       Config
	nodes     map[string]*placed
	order     []*placed
	occupied  map[gridPos]string
	colWidth  map[int]int
	rowHeight map[int]int
	// width and height are the size of the finished drawing in characters.
	width, height int
	// bundles are the edge groups that share a stretch of path.
	bundles []*bundle
}

// levelStep is the grid distance between two levels. Three cells for a node
// plus one free cell for edges to pass through.
const levelStep = 4

// doLayout places every node of g on the grid and measures the result.
func doLayout(g *Graph, cfg Config) *layout {
	l := &layout{
		g: g, cfg: cfg,
		nodes:     make(map[string]*placed, len(g.Nodes)),
		occupied:  make(map[gridPos]string),
		colWidth:  make(map[int]int),
		rowHeight: make(map[int]int),
	}

	for _, n := range g.Nodes {
		p := &placed{node: n, box: measure(n.Label, cfg.Padding)}
		l.nodes[n.ID] = p
		l.order = append(l.order, p)
	}

	l.place()
	l.measureGrid()
	l.toDrawing()
	return l
}

// place assigns a grid position to every node.
func (l *layout) place() {
	// nextFree[level] is the next unused position across the flow, so that
	// two nodes on the same level do not land on each other.
	nextFree := map[int]int{}
	done := map[string]bool{}

	for _, p := range l.roots() {
		l.reserve(p, l.atLevel(0, nextFree[0]))
		nextFree[0] += levelStep
		done[p.node.ID] = true
	}

	// Walk the graph outwards. Repeat until nothing moves: the source order
	// of a Mermaid file is not topological, so a child may be reached only
	// after its parent has been placed in a later pass.
	for len(done) < len(l.order) {
		progress := false
		for _, p := range l.order {
			if !done[p.node.ID] {
				continue
			}
			for _, child := range l.children(p.node.ID) {
				if done[child.node.ID] {
					continue
				}
				level := l.levelOf(p) + levelStep
				l.reserve(child, l.atLevel(level, nextFree[level]))
				nextFree[level] += levelStep
				done[child.node.ID] = true
				progress = true
			}
		}
		if progress {
			continue
		}
		// What is left has no path from any root — a cycle with no entry, or
		// a node nothing points at. Start a new run at level 0 for the first
		// of them and try again.
		for _, p := range l.order {
			if !done[p.node.ID] {
				l.reserve(p, l.atLevel(0, nextFree[0]))
				nextFree[0] += levelStep
				done[p.node.ID] = true
				progress = true
				break
			}
		}
		if !progress {
			break
		}
	}
}

// atLevel builds a position from a level along the flow and an offset across
// it. Which of the two is x and which is y depends on the direction.
func (l *layout) atLevel(level, across int) gridPos {
	if l.g.Direction == LeftRight {
		return gridPos{x: level, y: across}
	}
	return gridPos{x: across, y: level}
}

// levelOf reads the level back off a position.
func (l *layout) levelOf(p *placed) int {
	if l.g.Direction == LeftRight {
		return p.pos.x
	}
	return p.pos.y
}

// reserve claims the 3x3 block at pos, moving across the flow until it finds
// free ground.
func (l *layout) reserve(p *placed, pos gridPos) {
	for l.taken(pos) {
		if l.g.Direction == LeftRight {
			pos.y += levelStep
		} else {
			pos.x += levelStep
		}
	}
	for dx := 0; dx < 3; dx++ {
		for dy := 0; dy < 3; dy++ {
			l.occupied[gridPos{pos.x + dx, pos.y + dy}] = p.node.ID
		}
	}
	p.pos = pos
}

func (l *layout) taken(pos gridPos) bool {
	_, ok := l.occupied[pos]
	return ok
}

// roots are the nodes no edge points at — where the drawing starts.
//
// A graph made only of cycles has none; place() then falls back to starting
// wherever it can, so that such a graph still renders.
func (l *layout) roots() []*placed {
	isTarget := make(map[string]bool, len(l.g.Edges))
	for _, e := range l.g.Edges {
		isTarget[e.To] = true
	}
	var out []*placed
	for _, p := range l.order {
		if !isTarget[p.node.ID] {
			out = append(out, p)
		}
	}
	return out
}

// children are the targets of the edges leaving id, in source order.
func (l *layout) children(id string) []*placed {
	var out []*placed
	seen := map[string]bool{}
	for _, e := range l.g.Edges {
		if e.From != id || e.To == id || seen[e.To] {
			continue
		}
		seen[e.To] = true
		if p, ok := l.nodes[e.To]; ok {
			out = append(out, p)
		}
	}
	return out
}

// measureGrid gives every grid column a width and every row a height.
//
// A node contributes three columns — border, content, border — and the cell
// before it carries the spacing to its neighbour. Taking the maximum per
// column is what aligns boxes of different sizes into columns.
func (l *layout) measureGrid() {
	for _, p := range l.order {
		widths := [3]int{1, p.box.w - 2, 1}
		heights := [3]int{1, p.box.h - 2, 1}
		for i := 0; i < 3; i++ {
			l.colWidth[p.pos.x+i] = max(l.colWidth[p.pos.x+i], widths[i])
			l.rowHeight[p.pos.y+i] = max(l.rowHeight[p.pos.y+i], heights[i])
		}
		if p.pos.x > 0 {
			l.colWidth[p.pos.x-1] = max(l.colWidth[p.pos.x-1], l.cfg.PaddingX)
		}
		if p.pos.y > 0 {
			l.rowHeight[p.pos.y-1] = max(l.rowHeight[p.pos.y-1], l.cfg.PaddingY)
		}
	}
}

// toDrawing turns grid positions into character positions and sizes the canvas.
//
// A box is redrawn to the size of the three grid columns and rows it occupies,
// which is at least the size it asked for. That is what lines boxes up: two
// nodes in the same column both grow to the width of the wider one, so their
// borders sit flush and the edges between them run straight.
func (l *layout) toDrawing() {
	maxCol, maxRow := 0, 0
	for _, p := range l.order {
		p.draw = gridPos{x: l.charX(p.pos.x), y: l.charY(p.pos.y)}
		p.box = box{
			w: l.colWidth[p.pos.x] + l.colWidth[p.pos.x+1] + l.colWidth[p.pos.x+2],
			h: l.rowHeight[p.pos.y] + l.rowHeight[p.pos.y+1] + l.rowHeight[p.pos.y+2],
		}
		maxCol = max(maxCol, p.draw.x+p.box.w)
		maxRow = max(maxRow, p.draw.y+p.box.h)
	}
	l.width, l.height = maxCol, maxRow
}

// charX is the first character column of grid column col.
func (l *layout) charX(col int) int {
	x := 0
	for i := 0; i < col; i++ {
		x += l.colWidth[i]
	}
	return x
}

// charY is the first character row of grid row row.
func (l *layout) charY(row int) int {
	y := 0
	for i := 0; i < row; i++ {
		y += l.rowHeight[i]
	}
	return y
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
