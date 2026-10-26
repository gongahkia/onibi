package mermaidascii

import "unicode/utf8"

// seqLayout holds the measured positions of a sequence diagram.
type seqLayout struct {
	s   *Sequence
	cfg Config
	cs  charset

	// x holds the column of each actor's lifeline.
	x []int
	// half is half the width of each actor's box, rounded up.
	half []int
	// headHeight is the height of the actor boxes at top and bottom.
	headHeight int
	// labelY and arrowY hold the rows of each message.
	labelY, arrowY []int
	// footerY is the row the closing actor boxes start on.
	footerY int

	// blockTop and blockBottom hold the frame rows of each block, divY the
	// row of each divider, keyed "block:divider".
	blockTop, blockBottom []int
	divY                  map[[2]int]int
	// notes holds the placed note boxes.
	notes []placedNote

	width, height int
}

// placedNote is a note box with its position fixed.
type placedNote struct {
	x, y, w, h int
	lines      []string
}

// boxPad is the space between an actor's name and its box border.
const boxPad = 1

// minGap is the smallest distance between two lifelines. Below this, arrows
// grow too short to carry a readable label.
const minGap = 10

// renderSequence draws a parsed sequence diagram.
func renderSequence(s *Sequence, cfg Config) string {
	if len(s.Actors) == 0 {
		return ""
	}
	l := &seqLayout{s: s, cfg: cfg, cs: charsetFor(cfg)}
	l.measure()

	c := newCanvas(l.width, l.height)
	l.drawLifelines(c)
	l.drawActorBoxes(c)
	l.drawMessages(c)
	l.drawBlocks(c)
	l.drawNotes(c)
	return c.String()
}

// measure computes every position before anything is drawn.
func (l *seqLayout) measure() {
	actors := l.s.Actors

	// Actor boxes are as wide as their name plus padding and borders; the
	// lifeline drops from the middle.
	l.half = make([]int, len(actors))
	l.headHeight = 3
	for i, a := range actors {
		w := labelWidth(a.Label) + 2*boxPad + 2
		l.half[i] = (w + 1) / 2
		if h := len(labelLines(a.Label)) + 2; h > l.headHeight {
			l.headHeight = h
		}
	}

	// A message needs room for its label between the two lifelines it
	// connects. Where it spans several actors, the requirement is shared out
	// over the gaps in between.
	need := make([]int, max(len(actors)-1, 0))
	for _, m := range l.s.Messages {
		from, to := l.index(m.From), l.index(m.To)
		if from == to || from < 0 || to < 0 {
			continue
		}
		lo, hi := minInt(from, to), maxInt(from, to)
		perGap := (labelWidth(m.Label) + 4 + (hi - lo) - 1) / (hi - lo)
		for g := lo; g < hi; g++ {
			need[g] = max(need[g], perGap)
		}
	}

	l.x = make([]int, len(actors))
	l.x[0] = l.half[0]
	for i := 1; i < len(actors); i++ {
		gap := max(l.half[i-1]+l.half[i]+2, max(need[i-1]+2, minGap))
		l.x[i] = l.x[i-1] + gap
	}

	// Vertically the diagram is a list, walked message by message: what
	// opens before a message, the message itself, what follows it.
	l.labelY = make([]int, len(l.s.Messages))
	l.arrowY = make([]int, len(l.s.Messages))
	l.blockTop = make([]int, len(l.s.Blocks))
	l.blockBottom = make([]int, len(l.s.Blocks))
	l.divY = map[[2]int]int{}

	y := l.headHeight
	for i, m := range l.s.Messages {
		for b, blk := range l.s.Blocks {
			if blk.First == i {
				y += 2
				l.blockTop[b] = y - 1
			}
		}
		for b, blk := range l.s.Blocks {
			for d, div := range blk.Dividers {
				if div.At == i {
					y++
					l.divY[[2]int{b, d}] = y
					y++
				}
			}
		}

		y++
		lines := len(labelLines(m.Label))
		if m.From == m.To {
			// A message an actor sends to itself is drawn as a loop beside
			// its lifeline: out at the top, back in at the bottom.
			l.arrowY[i] = y
			l.labelY[i] = y + 1
			y += 2 + lines
		} else {
			l.labelY[i] = y
			l.arrowY[i] = y + lines
			y += lines + 1
		}

		for _, n := range l.s.Notes {
			if n.After != i {
				continue
			}
			y++
			p := l.placeNote(n, y)
			l.notes = append(l.notes, p)
			y += p.h
		}

		for b, blk := range l.s.Blocks {
			if blk.Last == i {
				y++
				l.blockBottom[b] = y
				y++
			}
		}
	}

	l.footerY = y + 1
	l.height = l.footerY + l.headHeight
	last := len(actors) - 1
	l.width = l.x[last] + l.half[last] + 2
	// A note or a self message may reach past the last lifeline.
	for _, n := range l.notes {
		l.width = max(l.width, n.x+n.w)
	}
	for _, m := range l.s.Messages {
		if m.From != m.To {
			continue
		}
		if i := l.index(m.From); i >= 0 {
			l.width = max(l.width, l.x[i]+selfLoopWidth+2+labelWidth(m.Label)+1)
		}
	}
}

// placeNote fixes where a note box goes.
func (l *seqLayout) placeNote(n *Note, y int) placedNote {
	lines := labelLines(n.Text)
	w := labelWidth(n.Text) + 4
	h := len(lines) + 2

	first := l.index(n.ActorIDs[0])
	if first < 0 {
		first = 0
	}
	var x int
	switch {
	case n.Position == NoteLeft:
		x = l.x[first] - w - 1
	case n.Position == NoteRight:
		x = l.x[first] + 2
	case len(n.ActorIDs) >= 2:
		second := l.index(n.ActorIDs[1])
		if second < 0 {
			second = first
		}
		x = (l.x[first]+l.x[second])/2 - w/2
	default:
		x = l.x[first] - w/2
	}
	return placedNote{x: max(x, 0), y: y, w: w, h: h, lines: lines}
}

// index is the column of an actor, or -1.
func (l *seqLayout) index(id string) int {
	for i, a := range l.s.Actors {
		if a.ID == id {
			return i
		}
	}
	return -1
}

// drawLifelines draws the vertical line under every actor.
func (l *seqLayout) drawLifelines(c *canvas) {
	for i := range l.s.Actors {
		for y := l.headHeight; y <= l.footerY; y++ {
			c.set(l.x[i], y, l.cs.vertical)
		}
	}
}

// drawActorBoxes puts the actor's name at the top and repeats it at the
// bottom, the way the reference renderers do — on a long diagram the second
// row saves scrolling back up.
func (l *seqLayout) drawActorBoxes(c *canvas) {
	for i, a := range l.s.Actors {
		l.drawActorBox(c, l.x[i], 0, a.Label)
		l.drawActorBox(c, l.x[i], l.footerY, a.Label)

		// Where the lifeline meets a box, the border opens towards it.
		if tee := l.cs.teeDown; tee != 0 {
			c.set(l.x[i], l.headHeight-1, tee)
		}
		if tee := l.cs.teeUp; tee != 0 {
			c.set(l.x[i], l.footerY, tee)
		}
	}
}

// drawActorBox draws one box centred on the lifeline at x.
func (l *seqLayout) drawActorBox(c *canvas, x, top int, label string) {
	lines := labelLines(label)
	inner := labelWidth(label) + 2*boxPad
	w := inner + 2
	// The lifeline drops from the middle of the box: with an even width the
	// box reaches one further to the left than to the right.
	left := x - w/2
	bottom := top + l.headHeight - 1

	for i := 1; i < w-1; i++ {
		c.set(left+i, top, l.cs.horizontal)
		c.set(left+i, bottom, l.cs.horizontal)
	}
	for y := top + 1; y < bottom; y++ {
		c.set(left, y, l.cs.vertical)
		c.set(left+w-1, y, l.cs.vertical)
	}
	corner := cornersFor(Rectangle, l.cfg.UseASCII)
	c.set(left, top, corner.tl)
	c.set(left+w-1, top, corner.tr)
	c.set(left, bottom, corner.bl)
	c.set(left+w-1, bottom, corner.br)

	for i, line := range lines {
		n := utf8.RuneCountInString(line)
		c.text(left+1+boxPad+(inner-2*boxPad-n)/2, top+1+i, line)
	}
}

// drawMessages draws each exchange: the label above, the arrow below.
func (l *seqLayout) drawMessages(c *canvas) {
	for i, m := range l.s.Messages {
		from, to := l.index(m.From), l.index(m.To)
		if from < 0 || to < 0 {
			continue
		}
		if from == to {
			l.drawSelfMessage(c, m, l.x[from], l.arrowY[i])
			continue
		}
		fromX, toX := l.x[from], l.x[to]

		line := l.cs.horizontal
		if m.Dashed {
			line = l.cs.dashed()
		}

		// The label sits centred between the two lifelines.
		mid := (fromX + toX) / 2
		for j, text := range labelLines(m.Label) {
			n := utf8.RuneCountInString(text)
			c.text(mid-n/2, l.labelY[i]+j, text)
		}

		y := l.arrowY[i]
		if fromX < toX {
			for x := fromX + 1; x < toX; x++ {
				c.set(x, y, line)
			}
			c.set(toX, y, l.cs.messageHead(m.Head, true))
		} else {
			for x := toX + 1; x < fromX; x++ {
				c.set(x, y, line)
			}
			c.set(toX, y, l.cs.messageHead(m.Head, false))
		}
	}
}

// drawBlocks frames the messages of loop, alt and their kin.
func (l *seqLayout) drawBlocks(c *canvas) {
	corner := cornersFor(Rectangle, l.cfg.UseASCII)

	for b, blk := range l.s.Blocks {
		top, bottom := l.blockTop[b], l.blockBottom[b]
		if top == 0 || bottom == 0 {
			continue
		}

		// The frame reaches around the lifelines the block talks to, plus a
		// little air so that arrows do not touch its sides.
		left, right := l.width, 0
		for i := blk.First; i <= blk.Last && i < len(l.s.Messages); i++ {
			m := l.s.Messages[i]
			from, to := l.index(m.From), l.index(m.To)
			if from < 0 || to < 0 {
				continue
			}
			left = minInt(left, l.x[minInt(from, to)])
			right = maxInt(right, l.x[maxInt(from, to)])
		}
		if right == 0 {
			continue
		}
		left = max(left-4, 0)
		right = minInt(right+4, l.width-1)

		for x := left + 1; x < right; x++ {
			c.set(x, top, l.cs.horizontal)
			c.set(x, bottom, l.cs.horizontal)
		}
		for y := top + 1; y < bottom; y++ {
			c.set(left, y, l.cs.vertical)
			c.set(right, y, l.cs.vertical)
		}
		c.set(left, top, corner.tl)
		c.set(right, top, corner.tr)
		c.set(left, bottom, corner.bl)
		c.set(right, bottom, corner.br)

		// The keyword sits in the top border, so the frame costs no extra row.
		header := blockName(blk.Kind)
		if blk.Label != "" {
			header += " [" + blk.Label + "]"
		}
		c.text(left+1, top, header)

		for d, div := range blk.Dividers {
			y, ok := l.divY[[2]int{b, d}]
			if !ok {
				continue
			}
			for x := left + 1; x < right; x++ {
				c.set(x, y, l.cs.divider())
			}
			c.set(left, y, orCross(l.cs.teeRight, l.cs))
			c.set(right, y, orCross(l.cs.teeLeft, l.cs))
			if div.Label != "" {
				c.text(left+1, y, "["+div.Label+"]")
			}
		}
	}
}

// drawNotes draws the remark boxes.
func (l *seqLayout) drawNotes(c *canvas) {
	corner := cornersFor(Rectangle, l.cfg.UseASCII)

	for _, n := range l.notes {
		bottom := n.y + n.h - 1
		for x := 1; x < n.w-1; x++ {
			c.set(n.x+x, n.y, l.cs.horizontal)
			c.set(n.x+x, bottom, l.cs.horizontal)
		}
		for i, line := range n.lines {
			y := n.y + 1 + i
			c.set(n.x, y, l.cs.vertical)
			c.set(n.x+n.w-1, y, l.cs.vertical)
			c.text(n.x+2, y, line)
		}
		c.set(n.x, n.y, corner.tl)
		c.set(n.x+n.w-1, n.y, corner.tr)
		c.set(n.x, bottom, corner.bl)
		c.set(n.x+n.w-1, bottom, corner.br)
	}
}

// blockName is the keyword a block is drawn with.
func blockName(k BlockKind) string {
	switch k {
	case Alt:
		return "alt"
	case Opt:
		return "opt"
	case Par:
		return "par"
	case Critical:
		return "critical"
	case Break:
		return "break"
	case Rect:
		return "rect"
	default:
		return "loop"
	}
}

// selfLoopWidth is how far a self message reaches out from its lifeline.
const selfLoopWidth = 4

// drawSelfMessage draws the loop of a message an actor sends to itself.
func (l *seqLayout) drawSelfMessage(c *canvas, m *Message, x, y int) {
	line := l.cs.horizontal
	if m.Dashed {
		line = l.cs.dashed()
	}
	corner := cornersFor(Rectangle, l.cfg.UseASCII)

	// Out at the top.
	if tee := l.cs.teeRight; tee != 0 {
		c.set(x, y, tee)
	}
	for i := 1; i < selfLoopWidth; i++ {
		c.set(x+i, y, line)
	}
	c.set(x+selfLoopWidth, y, corner.tr)

	// Down the side, with the label beside it.
	c.set(x+selfLoopWidth, y+1, l.cs.vertical)
	c.text(x+selfLoopWidth+2, y+1, m.Label)

	// Back in at the bottom.
	c.set(x, y+2, l.cs.messageHead(m.Head, false))
	for i := 1; i < selfLoopWidth; i++ {
		c.set(x+i, y+2, line)
	}
	c.set(x+selfLoopWidth, y+2, corner.br)
}
