package mermaidascii

import "unicode/utf8"

// renderFlowchart lays a graph out and draws it.
func renderFlowchart(g *Graph, cfg Config) string {
	if len(g.Nodes) == 0 {
		return ""
	}
	l := doLayout(g, cfg)
	routes := l.routeEdges()
	l.growForPaths(routes)
	l.toDrawing()

	cs := charsetFor(cfg)
	c := newCanvas(l.width, l.height)

	// Edges first, boxes on top: where a line runs into a box, the box wins,
	// and the junction character is set afterwards.
	for _, r := range routes {
		l.drawRoute(c, r, cs)
	}
	l.drawJunctions(c, cs)
	for _, p := range l.order {
		c.draw(drawNode(p.node.Label, p.node.Shape, p.box, cs, cfg.UseASCII), p.draw.x, p.draw.y)
	}
	for _, r := range routes {
		l.drawEnds(c, r, cs)
		l.drawLabel(c, r)
	}
	return c.String()
}

// growForPaths makes sure every grid cell an edge passes through has a size,
// so that the drawing is wide and tall enough to hold the detour.
//
// A cell no node occupies gets half the node spacing. One character would be
// enough to draw a line through, but a detour squeezed into a single column
// runs so close past the boxes that it reads as if it touched them.
func (l *layout) growForPaths(routes []*route) {
	for _, r := range routes {
		for _, p := range r.path {
			if _, ok := l.colWidth[p.x]; !ok {
				l.colWidth[p.x] = l.cfg.PaddingX / 2
			}
			if _, ok := l.rowHeight[p.y]; !ok {
				l.rowHeight[p.y] = l.cfg.PaddingY / 2
			}
		}
	}
}

// center is the character position at the middle of a grid cell.
func (l *layout) center(p gridPos) gridPos {
	return gridPos{
		x: l.charX(p.x) + l.colWidth[p.x]/2,
		y: l.charY(p.y) + l.rowHeight[p.y]/2,
	}
}

// drawRoute draws the line of one edge.
//
// After mergePath every segment runs along one axis, so the segments are
// plain runs of characters; the interesting part is the joints between them,
// where a corner character has to bridge two directions.
func (l *layout) drawRoute(c *canvas, r *route, cs charset) {
	if len(r.path) < 2 {
		return
	}
	h, v := cs.line(r.edge.Style)

	pts := make([]gridPos, len(r.path))
	for i, p := range r.path {
		pts[i] = l.center(p)
	}

	for i := 0; i < len(pts)-1; i++ {
		from, to := pts[i], pts[i+1]
		if from.y == to.y {
			for x := minInt(from.x, to.x); x <= maxInt(from.x, to.x); x++ {
				c.set(x, from.y, h)
			}
			continue
		}
		if from.x != to.x {
			// Only reachable through the straight-line fallback, when no
			// route was found at all. Draw the horizontal leg first so the
			// edge is visible rather than absent.
			for x := minInt(from.x, to.x); x <= maxInt(from.x, to.x); x++ {
				c.set(x, from.y, h)
			}
		}
		for y := minInt(from.y, to.y); y <= maxInt(from.y, to.y); y++ {
			c.set(to.x, y, v)
		}
	}

	// Corners last, over the lines that meet in them.
	for i := 1; i < len(pts)-1; i++ {
		in := bearing(pts[i-1], pts[i])
		out := bearing(pts[i], pts[i+1])
		if in != out {
			c.set(pts[i].x, pts[i].y, cornerFor(in, out, cs))
		}
	}
}

// bearing is the compass direction of a straight segment.
func bearing(from, to gridPos) dir {
	switch {
	case to.x > from.x:
		return dirRight
	case to.x < from.x:
		return dirLeft
	case to.y > from.y:
		return dirDown
	default:
		return dirUp
	}
}

// cornerFor is the corner character joining an incoming and an outgoing
// direction. The character is named after the two arms it shows: a line
// arriving from the left and leaving downwards needs arms to the left and
// down, which is ┐.
func cornerFor(in, out dir, cs charset) rune {
	switch {
	case in == dirRight && out == dirDown, in == dirUp && out == dirLeft:
		return cs.cornerTR
	case in == dirRight && out == dirUp, in == dirDown && out == dirLeft:
		return cs.cornerBR
	case in == dirLeft && out == dirDown, in == dirUp && out == dirRight:
		return cs.cornerTL
	case in == dirLeft && out == dirUp, in == dirDown && out == dirRight:
		return cs.cornerBL
	}
	return cs.cross
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// drawEnds sets the arrowheads and the junctions where an edge meets a box.
//
// The two ends are not symmetric. Where an edge leaves its source, the box
// border opens into a junction (├, ┬) — the line grows out of the box. Where
// it arrives, the border stays intact and the arrowhead sits in front of it,
// so a box is never eaten into by an incoming edge.
func (l *layout) drawEnds(c *canvas, r *route, cs charset) {
	if len(r.path) < 2 {
		return
	}
	first := l.center(r.path[0])
	last := l.center(r.path[len(r.path)-1])

	if tee := teeFor(r.startDir, cs); tee != 0 {
		c.set(first.x, first.y, tee)
	}
	if r.edge.ArrowStart {
		p := stepInside(first, r.startDir)
		c.set(p.x, p.y, arrowFor(opposite(r.startDir), cs))
	}
	if r.edge.ArrowEnd {
		p := stepInside(last, r.endDir)
		c.set(p.x, p.y, arrowFor(r.endDir, cs))
	}
}

// stepInside moves one cell away from a box, along the side the edge is
// attached to — the cell the arrowhead belongs in.
func stepInside(p gridPos, d dir) gridPos {
	switch d {
	case dirLeft:
		return gridPos{p.x - 1, p.y}
	case dirRight:
		return gridPos{p.x + 1, p.y}
	case dirUp:
		return gridPos{p.x, p.y - 1}
	case dirDown:
		return gridPos{p.x, p.y + 1}
	}
	return p
}

// arrowFor is the arrowhead that points into a box from the given side.
//
// An edge attached on the left of a box arrives travelling rightwards, so it
// is drawn with a right-pointing head.
func arrowFor(d dir, cs charset) rune {
	switch d {
	case dirLeft:
		return cs.arrowRight
	case dirRight:
		return cs.arrowLeft
	case dirUp:
		return cs.arrowDown
	case dirDown:
		return cs.arrowUp
	}
	return cs.arrowRight
}

// teeFor is the junction drawn where a line leaves or meets a box without an
// arrowhead — the box border opens towards the line.
func teeFor(d dir, cs charset) rune {
	switch d {
	case dirLeft:
		return cs.teeLeft
	case dirRight:
		return cs.teeRight
	case dirUp:
		return cs.teeUp
	case dirDown:
		return cs.teeDown
	}
	return cs.cross
}

// drawLabel writes an edge label next to its line.
//
// Which side counts as "next to" depends on how the line runs there: a
// vertical line takes the label at its side, a horizontal one above it.
// Candidates are tried in order and the first that fits wins; if none does,
// the label is left out rather than drawn over the diagram.
func (l *layout) drawLabel(c *canvas, r *route) {
	if r.edge.Label == "" || len(r.path) < 2 {
		return
	}
	i := len(r.path) / 2
	mid := l.center(r.path[i])
	n := utf8.RuneCountInString(r.edge.Label)

	var spots []gridPos
	if bearing(l.center(r.path[i-1]), mid) == dirUp || bearing(l.center(r.path[i-1]), mid) == dirDown {
		spots = []gridPos{
			{mid.x - n - 1, mid.y},
			{mid.x + 2, mid.y},
			{mid.x - n - 1, mid.y - 1},
			{mid.x + 2, mid.y - 1},
		}
	} else {
		spots = []gridPos{
			{mid.x - n/2, mid.y - 1},
			{mid.x - n/2, mid.y + 1},
			{mid.x - n/2, mid.y - 2},
		}
	}

	for _, at := range spots {
		// The inside of a box looks blank but belongs to the node — a label
		// dropped in there would read as that node's text.
		if l.overlapsNode(at.x, at.y, n) {
			continue
		}
		if c.textIfFree(at.x, at.y, r.edge.Label) {
			return
		}
	}
}

// overlapsNode reports whether a run of n characters starting at (x, y) would
// touch any node's box.
func (l *layout) overlapsNode(x, y, n int) bool {
	for _, p := range l.order {
		if y < p.draw.y || y >= p.draw.y+p.box.h {
			continue
		}
		if x < p.draw.x+p.box.w && x+n > p.draw.x {
			return true
		}
	}
	return false
}

// drawJunctions sets the character where bundled edges meet.
//
// Which one it is follows from the lines that actually arrive there, not from
// the bundle's shape: three lines meeting need a T, four a cross, and reading
// them off the canvas gets every case right without enumerating them.
func (l *layout) drawJunctions(c *canvas, cs charset) {
	for _, b := range l.bundles {
		p := l.center(b.junction)
		up, down := isLine(c.at(p.x, p.y-1), cs), isLine(c.at(p.x, p.y+1), cs)
		left, right := isLine(c.at(p.x-1, p.y), cs), isLine(c.at(p.x+1, p.y), cs)

		switch {
		case up && down && left && right:
			c.set(p.x, p.y, cs.cross)
		case up && down && right:
			c.set(p.x, p.y, orCross(cs.teeRight, cs))
		case up && down && left:
			c.set(p.x, p.y, orCross(cs.teeLeft, cs))
		case left && right && down:
			c.set(p.x, p.y, orCross(cs.teeDown, cs))
		case left && right && up:
			c.set(p.x, p.y, orCross(cs.teeUp, cs))
		}
	}
}

// orCross falls back to the cross where a character set has no T-pieces.
// ASCII has none, because a "+" cannot be told apart from a corner — but
// where lines genuinely cross, the "+" is exactly right.
func orCross(r rune, cs charset) rune {
	if r == 0 {
		return cs.cross
	}
	return r
}

// isLine reports whether a character is part of a drawn line.
func isLine(r rune, cs charset) bool {
	switch r {
	case cs.horizontal, cs.vertical, cs.dottedH, cs.dottedV, cs.thickH, cs.thickV,
		cs.cornerTL, cs.cornerTR, cs.cornerBL, cs.cornerBR,
		cs.teeUp, cs.teeDown, cs.teeLeft, cs.teeRight, cs.cross,
		cs.arrowUp, cs.arrowDown, cs.arrowLeft, cs.arrowRight:
		return r != 0
	}
	return false
}
