package editor

import (
	"sort"

	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
)

// Index only grapheme boundaries, never arbitrary byte offsets. Checkpoints are
// sparse and compact as a line grows; the total cache is bounded across panes
// and buffers. No source text is flattened or retained outside the current rope.
const layoutEntries = 16
const layoutPoints = 2048
const layoutStride = 512

type layoutPoint struct {
	Offset, Column           int
	Row, RowStart, RowColumn int
}
type lineLayout struct {
	buffer                               *Buffer
	doc                                  *text.Document
	rope                                 text.Rope
	version                              uint64
	line, start, end, width, tab, stride int
	used                                 uint64
	points                               []layoutPoint
}
type layoutCache struct {
	entries []*lineLayout
	clock   uint64
}

func (c *layoutCache) remove(i int) {
	copy(c.entries[i:], c.entries[i+1:])
	c.entries[len(c.entries)-1] = nil
	c.entries = c.entries[:len(c.entries)-1]
}
func (c *layoutCache) forget(b *Buffer) {
	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].buffer == b {
			c.remove(i)
		}
	}
}
func (c *layoutCache) line(b *Buffer, line, width, tab int) *lineLayout {
	c.clock++
	d := b.Doc
	for i := len(c.entries) - 1; i >= 0; i-- {
		l := c.entries[i]
		if l.buffer != b || (l.doc == d && l.rope.Same(d.Text)) {
			continue
		}
		// LastChanges describes the last transaction, including grouped undo. If
		// several transactions passed without a lookup, discard rather than guess.
		earliest := 0
		if l.doc == d && d.Version == l.version+1 && len(d.LastChanges) > 0 {
			earliest = d.Text.Len()
			for _, batch := range d.LastChanges {
				for _, e := range batch {
					earliest = min(earliest, e.Start)
				}
			}
		}
		if earliest <= l.start {
			c.remove(i)
			continue
		}
		l.rope, l.version = d.Text, d.Version
		l.end = d.Text.LineEnd(l.line)
		// A combining character inserted at a boundary can merge with its preceding
		// cluster. Retain only checkpoints strictly before the first changed byte.
		n := sort.Search(len(l.points), func(i int) bool { return l.points[i].Offset >= earliest })
		l.points = l.points[:max(1, n)]
	}
	for _, l := range c.entries {
		if l.buffer == b && l.line == line && l.width == width && l.tab == tab {
			l.used = c.clock
			return l
		}
	}
	l := &lineLayout{buffer: b, doc: d, rope: d.Text, version: d.Version, line: line,
		start: d.Text.LineStart(line), end: d.Text.LineEnd(line), width: width, tab: max(1, tab), stride: layoutStride, used: c.clock}
	l.points = []layoutPoint{{Offset: l.start, RowStart: l.start}}
	if len(c.entries) == layoutEntries {
		oldest := 0
		for i := range c.entries {
			if c.entries[i].used < c.entries[oldest].used {
				oldest = i
			}
		}
		c.entries[oldest] = l
	} else {
		c.entries = append(c.entries, l)
	}
	return l
}

// graphemeReader reads small UTF-8 windows. The final cluster in a window is
// retried with more lookahead: a rope chunk boundary must not split combining
// marks, a regional-indicator pair or a ZWJ sequence. An unusually long single
// cluster grows the window geometrically, avoiding quadratic copying.
type graphemeReader struct {
	rope                text.Rope
	offset, end, loaded int
	rest                string
	state               int
}

func newGraphemeReader(r text.Rope, start, end int) graphemeReader {
	return graphemeReader{rope: r, offset: start, end: end, state: -1}
}
func (g *graphemeReader) next() (string, int, int, bool) {
	if g.offset >= g.end {
		return "", 0, 0, false
	}
	window := 4096
	for {
		if len(g.rest) == 0 {
			g.loaded = min(g.end, g.offset+window)
			for g.loaded < g.end && !g.rope.Boundary(g.loaded) {
				g.loaded++
			}
			g.rest = g.rope.Slice(g.offset, g.loaded)
		}
		cluster, rest, width, state := uniseg.FirstGraphemeClusterInString(g.rest, g.state)
		if len(rest) == 0 && g.loaded < g.end {
			window = max(window*2, len(g.rest)*2)
			g.rest = ""
			continue
		}
		from := g.offset
		g.offset += len(cluster)
		g.rest, g.state = rest, state
		return cluster, from, width, true
	}
}
func (l *lineLayout) remember(p layoutPoint) {
	last := l.points[len(l.points)-1]
	if p.Offset <= last.Offset || (p.Offset-last.Offset < l.stride && p.Offset != l.end) {
		return
	}
	if len(l.points) == layoutPoints {
		n := 1
		for i := 2; i < len(l.points); i += 2 {
			l.points[n] = l.points[i]
			n++
		}
		l.points = l.points[:n]
		l.stride *= 2
	}
	if len(l.points) == cap(l.points) {
		points := make([]layoutPoint, len(l.points), min(layoutPoints, max(8, 2*cap(l.points))))
		copy(points, l.points)
		l.points = points
	}
	l.points = append(l.points, p)
}
func (l *lineLayout) checkpoint(after func(layoutPoint) bool) layoutPoint {
	i := sort.Search(len(l.points), func(i int) bool { return after(l.points[i]) })
	return l.points[max(0, i-1)]
}

// step returns the cluster's actual row and advances to the next boundary.
// Overflow is decided before emitting a cluster; an exactly full row is closed
// afterward, leaving the same trailing empty row as ordinary ASCII wrapping.
func (l *lineLayout) step(p *layoutPoint, g *graphemeReader) (layoutPoint, int, bool) {
	cluster, from, w, ok := g.next()
	if !ok {
		return *p, 0, false
	}
	if cluster == "\t" {
		w = l.tab - p.Column%l.tab
	}
	if l.width > 0 && p.Column-p.RowColumn+w > l.width && from > p.RowStart {
		p.Row++
		p.RowStart = from
		p.RowColumn = p.Column
	}
	before := *p
	p.Offset = g.offset
	p.Column += w
	if l.width > 0 && p.Column-p.RowColumn >= l.width {
		p.Row++
		p.RowStart = p.Offset
		p.RowColumn = p.Column
	}
	l.remember(*p)
	return before, w, true
}
func (l *lineLayout) atOffset(offset int) (layoutPoint, layoutPoint) {
	offset = max(l.start, min(offset, l.end))
	p := l.checkpoint(func(p layoutPoint) bool { return p.Offset > offset })
	g := newGraphemeReader(l.rope, p.Offset, l.end)
	for {
		before, _, ok := l.step(&p, &g)
		if !ok {
			return p, p
		}
		if p.Offset > offset {
			return before, p
		}
	}
}
func (l *lineLayout) column(offset int) int {
	before, _ := l.atOffset(offset)
	return before.Column
}
func (l *lineLayout) offset(column int) int {
	p := l.checkpoint(func(p layoutPoint) bool { return p.Column >= column })
	g := newGraphemeReader(l.rope, p.Offset, l.end)
	for {
		before, _, ok := l.step(&p, &g)
		if !ok {
			return p.Offset
		}
		if p.Column > column {
			return before.Offset
		}
		if p.Column == column {
			return p.Offset
		}
	}
}
func (l *lineLayout) rowAt(offset int) int {
	before, _ := l.atOffset(offset)
	return before.Row
}
func (l *lineLayout) segment(row int) (segment, int) {
	row = max(0, row)
	p := l.checkpoint(func(p layoutPoint) bool { return p.Row > row })
	g := newGraphemeReader(l.rope, p.Offset, l.end)
	for {
		old := p
		before, _, ok := l.step(&p, &g)
		if !ok {
			return segment{p.RowStart, l.end, p.RowColumn}, p.Row
		}
		if before.Row > row {
			return segment{old.RowStart, before.Offset, old.RowColumn}, old.Row
		}
		if p.Row > row {
			return segment{before.RowStart, p.Offset, before.RowColumn}, before.Row
		}
	}
}
func (l *lineLayout) count() int {
	p, _ := l.atOffset(l.end)
	return p.Row + 1
}

func (a *App) columnAt(b *Buffer, offset int) int {
	r := b.Doc.Text
	line := r.LineAt(offset)
	start := r.LineStart(line)
	if r.Plain(start, offset) {
		return offset - start
	}
	return a.lineLayouts.line(b, line, 0, a.Config.TabSize).column(offset)
}
func (a *App) lineOffset(b *Buffer, line, column int) int {
	r := b.Doc.Text
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		return max(start, min(end, start+column))
	}
	return a.lineLayouts.line(b, line, 0, a.Config.TabSize).offset(column)
}
func (a *App) rowAt(b *Buffer, line, offset, width int) int {
	r := b.Doc.Text
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		return (offset - start) / max(1, width)
	}
	return a.lineLayouts.line(b, line, max(1, width), a.Config.TabSize).rowAt(offset)
}
func (a *App) rowCount(b *Buffer, line, width int) int {
	r := b.Doc.Text
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		return (end-start)/max(1, width) + 1
	}
	return a.lineLayouts.line(b, line, max(1, width), a.Config.TabSize).count()
}
func (a *App) rowSegment(b *Buffer, line, row, width int) (segment, int) {
	r := b.Doc.Text
	start, end := r.LineStart(line), r.LineEnd(line)
	width = max(1, width)
	if r.Plain(start, end) {
		row = max(0, min(row, (end-start)/width))
		from := start + row*width
		return segment{from, min(end, from+width), row * width}, row
	}
	return a.lineLayouts.line(b, line, width, a.Config.TabSize).segment(row)
}
func (a *App) hasRow(b *Buffer, line, row, width int) bool {
	_, actual := a.rowSegment(b, line, row, width)
	return actual == row
}

func cursorBoundary(r text.Rope, offset int, forward bool, layout func(int) *lineLayout) int {
	offset = max(0, min(offset, r.Len()))
	line := r.LineAt(offset)
	start, end := r.LineStart(line), r.LineEnd(line)
	if forward {
		if offset >= end {
			return r.LineStart(line + 1)
		}
		// Two printable ASCII bytes guarantee the boundary after the first. Check
		// the preceding byte too: U+0600 + A is one cluster, not two characters.
		if (offset == start || r.Plain(offset-1, offset)) && r.Plain(offset, min(r.Len(), offset+2)) {
			return offset + 1
		}
		_, after := layout(line).atOffset(offset)
		return after.Offset
	}
	if offset <= 0 {
		return 0
	}
	if offset == start {
		return r.LineEnd(line - 1)
	}
	if offset > end {
		return end
	}
	if r.Plain(max(start, offset-2), offset) {
		return offset - 1
	}
	before, _ := layout(line).atOffset(offset - 1)
	return before.Offset
}
func (a *App) previous(b *Buffer, offset int) int {
	return cursorBoundary(b.Doc.Text, offset, false, func(line int) *lineLayout { return a.lineLayouts.line(b, line, 0, a.Config.TabSize) })
}
func (a *App) next(b *Buffer, offset int) int {
	return cursorBoundary(b.Doc.Text, offset, true, func(line int) *lineLayout { return a.lineLayouts.line(b, line, 0, a.Config.TabSize) })
}
func uncachedLayout(r text.Rope, line int) *lineLayout {
	start := r.LineStart(line)
	return &lineLayout{rope: r, start: start, end: r.LineEnd(line), tab: 4, stride: layoutStride, points: []layoutPoint{{Offset: start, RowStart: start}}}
}
