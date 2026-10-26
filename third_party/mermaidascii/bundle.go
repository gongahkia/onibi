package mermaidascii

// When several edges run into the same node, drawing each one separately
// leaves a comb of parallel lines. Instead they meet at a common point and
// share the last stretch of the way — the way one would draw it by hand.

// bundleKind tells the two directions edges can be gathered in.
type bundleKind int

const (
	// fanIn: many sources, one shared target (A & B --> C).
	fanIn bundleKind = iota
	// fanOut: one source, many targets (A --> B & C).
	fanOut
)

// bundle is a group of edges that share a stretch of path.
type bundle struct {
	kind   bundleKind
	edges  []*Edge
	shared *placed
	others []*placed
	// junction is where the ways meet.
	junction gridPos
	// sharedPath is the common stretch between junction and shared node.
	sharedPath []gridPos
}

// analyzeBundles finds the groups of edges that may be gathered.
//
// Only for downward flow: left to right, edges already converge on a shared
// column and a junction would buy nothing.
func (l *layout) analyzeBundles() []*bundle {
	if l.g.Direction != TopDown {
		return nil
	}

	taken := map[*Edge]bool{}
	var bundles []*bundle

	// Converging edges first, diverging ones after: an edge belongs to at
	// most one bundle, and a shared target is the more common and the more
	// conspicuous case.
	for _, id := range l.uniqueOrder(func(e *Edge) string { return e.To }) {
		edges := l.edgesTo(id)
		if !bundleable(edges, taken) {
			continue
		}
		b := &bundle{kind: fanIn, edges: edges, shared: l.nodes[id]}
		for _, e := range edges {
			b.others = append(b.others, l.nodes[e.From])
			taken[e] = true
		}
		bundles = append(bundles, b)
	}

	for _, id := range l.uniqueOrder(func(e *Edge) string { return e.From }) {
		edges := l.edgesFrom(id)
		if !bundleable(edges, taken) {
			continue
		}
		b := &bundle{kind: fanOut, edges: edges, shared: l.nodes[id]}
		for _, e := range edges {
			b.others = append(b.others, l.nodes[e.To])
			taken[e] = true
		}
		bundles = append(bundles, b)
	}

	return bundles
}

// bundleable reports whether a group of edges may be gathered into one.
func bundleable(edges []*Edge, taken map[*Edge]bool) bool {
	if len(edges) < 2 {
		return false
	}
	for _, e := range edges {
		if taken[e] {
			return false
		}
		// Labelled edges stay apart: at the junction their labels would sit
		// on top of each other, and which belongs to which edge would no
		// longer be readable.
		if e.Label != "" {
			return false
		}
		// Merging different line styles would produce a line that changes
		// its appearance halfway.
		if e.Style != edges[0].Style {
			return false
		}
		if e.From == e.To {
			return false
		}
	}
	return true
}

// edgesTo collects the edges pointing at id.
func (l *layout) edgesTo(id string) []*Edge {
	var out []*Edge
	for _, e := range l.g.Edges {
		if e.To == id && e.From != e.To {
			out = append(out, e)
		}
	}
	return out
}

// edgesFrom collects the edges leaving id.
func (l *layout) edgesFrom(id string) []*Edge {
	var out []*Edge
	for _, e := range l.g.Edges {
		if e.From == id && e.From != e.To {
			out = append(out, e)
		}
	}
	return out
}

// uniqueOrder lists node ids in order of first appearance. Ranging over a map
// instead would group the bundles differently from run to run and make the
// drawing unpredictable.
func (l *layout) uniqueOrder(key func(*Edge) string) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range l.g.Edges {
		k := key(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// routeBundle fixes the meeting point and lays the ways towards it.
func (l *layout) routeBundle(b *bundle) []*route {
	shared := b.shared.pos

	// The junction sits in the shared node's column, close in front of it:
	// one row above for converging edges, just below for diverging ones.
	if b.kind == fanIn {
		b.junction = gridPos{x: shared.x + 1, y: shared.y - 1}
	} else {
		b.junction = gridPos{x: shared.x + 1, y: shared.y + 3}
	}

	top := gridPos{x: shared.x + 1, y: shared.y}
	bottom := gridPos{x: shared.x + 1, y: shared.y + 2}

	var routes []*route
	if b.kind == fanIn {
		b.sharedPath = l.pathOrLine(b.junction, top)
		for i, e := range b.edges {
			exit := gridPos{x: b.others[i].pos.x + 1, y: b.others[i].pos.y + 2}
			path := append(l.pathOrLine(exit, b.junction), b.sharedPath[1:]...)
			routes = append(routes, &route{edge: e, path: path, startDir: dirDown, endDir: dirUp})
		}
		return routes
	}

	b.sharedPath = l.pathOrLine(bottom, b.junction)
	for i, e := range b.edges {
		entry := gridPos{x: b.others[i].pos.x + 1, y: b.others[i].pos.y}
		path := append(append([]gridPos{}, b.sharedPath...), l.pathOrLine(b.junction, entry)[1:]...)
		routes = append(routes, &route{edge: e, path: path, startDir: dirDown, endDir: dirUp})
	}
	return routes
}

// pathOrLine searches for a way and falls back to the straight connection.
func (l *layout) pathOrLine(from, to gridPos) []gridPos {
	if p := mergePath(l.findPath(from, to)); p != nil {
		return p
	}
	return []gridPos{from, to}
}
