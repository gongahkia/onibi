package mermaidascii

// Edges are routed on the same grid the nodes sit on. An edge leaves its
// source at one of the eight points around it, crosses free cells, and
// arrives at a point around its target. Which points those are is decided
// first, the way between them by A*.

// dir is an offset into a node's 3x3 block, naming where an edge attaches.
type dir struct{ x, y int }

var (
	dirUp         = dir{1, 0}
	dirDown       = dir{1, 2}
	dirLeft       = dir{0, 1}
	dirRight      = dir{2, 1}
	dirUpperRight = dir{2, 0}
	dirUpperLeft  = dir{0, 0}
	dirLowerRight = dir{2, 2}
	dirLowerLeft  = dir{0, 2}
	dirMiddle     = dir{1, 1}
)

// opposite is where an edge should arrive when it left in direction d.
func opposite(d dir) dir {
	switch d {
	case dirUp:
		return dirDown
	case dirDown:
		return dirUp
	case dirLeft:
		return dirRight
	case dirRight:
		return dirLeft
	case dirUpperRight:
		return dirLowerLeft
	case dirUpperLeft:
		return dirLowerRight
	case dirLowerRight:
		return dirUpperLeft
	case dirLowerLeft:
		return dirUpperRight
	}
	return dirMiddle
}

// towards is the rough compass bearing from one grid position to another.
func towards(from, to gridPos) dir {
	switch {
	case from.x == to.x:
		if from.y < to.y {
			return dirDown
		}
		return dirUp
	case from.y == to.y:
		if from.x < to.x {
			return dirRight
		}
		return dirLeft
	case from.x < to.x:
		if from.y < to.y {
			return dirLowerRight
		}
		return dirUpperRight
	default:
		if from.y < to.y {
			return dirLowerLeft
		}
		return dirUpperLeft
	}
}

// attachments are the two candidate routes for an edge: a preferred pair of
// attachment points and an alternative. Both are tried, and the shorter path
// wins.
type attachments struct {
	startPref, endPref dir
	startAlt, endAlt   dir
}

// chooseAttachments decides where an edge leaves and enters.
//
// The rules encode what looks right rather than what is shortest. An edge
// that has to run backwards against the flow, for instance, leaves downwards
// and comes back rather than cutting straight through the row of boxes.
func chooseAttachments(from, to gridPos, flow Direction, selfEdge bool) attachments {
	if selfEdge {
		if flow == LeftRight {
			return attachments{dirRight, dirDown, dirDown, dirRight}
		}
		return attachments{dirDown, dirRight, dirRight, dirDown}
	}

	d := towards(from, to)
	lr := flow == LeftRight

	backwards := (lr && (d == dirLeft || d == dirUpperLeft || d == dirLowerLeft)) ||
		(!lr && (d == dirUp || d == dirUpperLeft || d == dirUpperRight))

	switch {
	case d == dirLowerRight:
		if lr {
			return attachments{dirDown, dirLeft, dirRight, dirUp}
		}
		return attachments{dirRight, dirUp, dirDown, dirLeft}

	case d == dirUpperRight:
		if lr {
			return attachments{dirUp, dirLeft, dirRight, dirDown}
		}
		return attachments{dirRight, dirDown, dirUp, dirLeft}

	case d == dirLowerLeft:
		if lr {
			return attachments{dirDown, dirDown, dirLeft, dirUp}
		}
		return attachments{dirLeft, dirUp, dirDown, dirRight}

	case d == dirUpperLeft:
		if lr {
			return attachments{dirDown, dirDown, dirLeft, dirDown}
		}
		return attachments{dirRight, dirRight, dirUp, dirRight}

	case backwards && lr && d == dirLeft:
		return attachments{dirDown, dirDown, dirLeft, dirRight}

	case backwards && !lr && d == dirUp:
		return attachments{dirRight, dirRight, dirUp, dirDown}
	}

	// Straight along the flow: leave and enter the natural way.
	o := opposite(d)
	return attachments{d, o, d, o}
}

// offset moves a grid position by an attachment direction.
func offset(p gridPos, d dir) gridPos { return gridPos{p.x + d.x, p.y + d.y} }

// route is the finished way of one edge across the grid.
type route struct {
	edge     *Edge
	path     []gridPos
	startDir dir
	endDir   dir
}

// routeEdges finds a way for every edge.
//
// Bundles first: where several edges run into the same node they share the
// last stretch of the way. Whatever is left over is routed on its own.
func (l *layout) routeEdges() []*route {
	routes := make([]*route, 0, len(l.g.Edges))
	taken := map[*Edge]bool{}

	for _, b := range l.analyzeBundles() {
		l.bundles = append(l.bundles, b)
		routes = append(routes, l.routeBundle(b)...)
		for _, e := range b.edges {
			taken[e] = true
		}
	}

	for _, e := range l.g.Edges {
		if taken[e] {
			continue
		}
		from, ok := l.nodes[e.From]
		if !ok {
			continue
		}
		to, ok := l.nodes[e.To]
		if !ok {
			continue
		}
		routes = append(routes, l.routeEdge(e, from, to))
	}
	return routes
}

// routeEdge tries both candidate routes and keeps the shorter one.
func (l *layout) routeEdge(e *Edge, from, to *placed) *route {
	a := chooseAttachments(from.pos, to.pos, l.g.Direction, from == to)

	prefFrom, prefTo := offset(from.pos, a.startPref), offset(to.pos, a.endPref)
	altFrom, altTo := offset(from.pos, a.startAlt), offset(to.pos, a.endAlt)

	pref := mergePath(l.findPath(prefFrom, prefTo))
	alt := mergePath(l.findPath(altFrom, altTo))

	switch {
	case pref != nil && alt != nil:
		if len(pref) <= len(alt) {
			return &route{edge: e, path: pref, startDir: a.startPref, endDir: a.endPref}
		}
		return &route{edge: e, path: alt, startDir: a.startAlt, endDir: a.endAlt}
	case pref != nil:
		return &route{edge: e, path: pref, startDir: a.startPref, endDir: a.endPref}
	case alt != nil:
		return &route{edge: e, path: alt, startDir: a.startAlt, endDir: a.endAlt}
	}

	// Nothing got through — the boxes stand too close for a clean route.
	// Draw the edge straight anyway: a line through a box is easier to read
	// than a diagram with an edge silently missing.
	return &route{
		edge: e, path: []gridPos{prefFrom, prefTo},
		startDir: a.startPref, endDir: a.endPref,
	}
}

// findPath is A* over free grid cells, four-way movement only.
//
// The heuristic charges one extra unit whenever the remaining distance needs
// both a horizontal and a vertical leg. That makes the search prefer straight
// lines and few corners, which is what a reader follows most easily.
func (l *layout) findPath(from, to gridPos) []gridPos {
	// Shack: an enclosed target otherwise searches an infinite positive grid.
	// Keep a margin around all nodes/endpoints and bound retained search state.
	maxX, maxY := max(from.x, to.x), max(from.y, to.y)
	for p := range l.occupied {
		maxX = max(maxX, p.x)
		maxY = max(maxY, p.y)
	}
	maxX += 8
	maxY += 8
	open := &minHeap{}
	open.push(heapItem{pos: from, priority: 0})

	cost := map[gridPos]int{from: 0}
	cameFrom := map[gridPos]gridPos{}
	seen := map[gridPos]bool{from: true}

	for open.len() > 0 {
		if len(cost) > 8192 {
			return nil
		}
		current := open.pop().pos

		if current == to {
			var path []gridPos
			for c := current; ; {
				path = append([]gridPos{c}, path...)
				prev, ok := cameFrom[c]
				if !ok {
					break
				}
				c = prev
			}
			return path
		}

		for _, step := range [4]gridPos{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			next := gridPos{current.x + step.x, current.y + step.y}
			if next.x < 0 || next.y < 0 || next.x > maxX || next.y > maxY {
				continue
			}
			if next != to && !l.free(next) {
				continue
			}
			newCost := cost[current] + 1
			if old, ok := cost[next]; ok && newCost >= old {
				continue
			}
			cost[next] = newCost
			cameFrom[next] = current
			open.push(heapItem{pos: next, priority: newCost + estimate(next, to)})
			seen[next] = true
		}
	}
	return nil
}

// free reports whether a grid cell can be crossed. Cells inside a node are
// blocked, and negative coordinates lie outside the drawing.
func (l *layout) free(p gridPos) bool {
	if p.x < 0 || p.y < 0 {
		return false
	}
	_, taken := l.occupied[p]
	return !taken
}

// estimate is the Manhattan distance plus a penalty for needing a corner.
func estimate(a, b gridPos) int {
	dx, dy := abs(a.x-b.x), abs(a.y-b.y)
	if dx == 0 || dy == 0 {
		return dx + dy
	}
	return dx + dy + 1
}

// mergePath drops waypoints that only continue a straight line, leaving the
// corners. Everything downstream draws segments, so fewer points is less work
// and fewer chances to disagree about a cell.
func mergePath(path []gridPos) []gridPos {
	if len(path) <= 2 {
		return path
	}
	out := []gridPos{path[0]}
	for i := 1; i < len(path)-1; i++ {
		before := gridPos{path[i].x - path[i-1].x, path[i].y - path[i-1].y}
		after := gridPos{path[i+1].x - path[i].x, path[i+1].y - path[i].y}
		if before != after {
			out = append(out, path[i])
		}
	}
	return append(out, path[len(path)-1])
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// heapItem is one entry of the A* frontier.
type heapItem struct {
	pos      gridPos
	priority int
}

// minHeap is a binary heap over heapItem, smallest priority first.
//
// Hand-rolled rather than container/heap: the reference implementation uses
// its own heap, and two heaps that break ties differently would pick
// different, equally short paths — and every diagram would come out subtly
// unlike the original.
type minHeap struct{ items []heapItem }

func (h *minHeap) len() int { return len(h.items) }

func (h *minHeap) push(it heapItem) {
	h.items = append(h.items, it)
	i := len(h.items) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if h.items[i].priority >= h.items[parent].priority {
			break
		}
		h.items[i], h.items[parent] = h.items[parent], h.items[i]
		i = parent
	}
}

func (h *minHeap) pop() heapItem {
	top := h.items[0]
	last := len(h.items) - 1
	h.items[0] = h.items[last]
	h.items = h.items[:last]

	i, n := 0, len(h.items)
	for {
		smallest, left, right := i, 2*i+1, 2*i+2
		if left < n && h.items[left].priority < h.items[smallest].priority {
			smallest = left
		}
		if right < n && h.items[right].priority < h.items[smallest].priority {
			smallest = right
		}
		if smallest == i {
			break
		}
		h.items[i], h.items[smallest] = h.items[smallest], h.items[i]
		i = smallest
	}
	return top
}
