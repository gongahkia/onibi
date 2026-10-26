package editor

import "unicode/utf8"

// wordChunk visits a class run in one bounded window. Forward scans can stop
// immediately; backward scans summarize the final run of a checkpoint window.
// Repeated Ctrl+Arrow/word selection never restarts at the beginning of a line
// for every character, including a megabyte-long identifier.
func (a *App) wordChunk(b *Buffer, offset int, forward bool, classify func(rune) int) (int, int) {
	r := b.Doc.Text
	line := r.LineAt(offset)
	if forward {
		end := r.LineStart(line + 1)
		g := newGraphemeReader(r, offset, end)
		class := -1
		for {
			s, from, _, ok := g.next()
			if !ok {
				return end, class
			}
			ch, _ := utf8.DecodeRuneInString(s)
			next := classify(ch)
			if class >= 0 && next != class {
				return from, class
			}
			class = next
			if g.offset-offset >= 4096 {
				return g.offset, class
			}
		}
	}
	if offset == r.LineStart(line) {
		line--
	}
	start, end := r.LineStart(line), r.LineEnd(line)
	if offset > end {
		return end, classify('\n')
	}
	from := max(start, offset-4096)
	if !r.Plain(start, end) {
		l := a.lineLayouts.line(b, line, 0, a.Config.TabSize)
		if l.points[len(l.points)-1].Offset < offset {
			l.atOffset(offset)
		}
		from = l.checkpoint(func(p layoutPoint) bool { return p.Offset >= offset }).Offset
	}
	g := newGraphemeReader(r, from, offset)
	class, run := -1, from
	for {
		s, p, _, ok := g.next()
		if !ok {
			return run, class
		}
		ch, _ := utf8.DecodeRuneInString(s)
		next := classify(ch)
		if class != next {
			class, run = next, p
		}
	}
}
func (a *App) wordOffset(b *Buffer, p, delta int) int {
	if delta > 0 {
		class := -1
		spaces := false
		for p < b.Doc.Text.Len() {
			next, c := a.wordChunk(b, p, true, wordClass)
			if class < 0 {
				class = c
			}
			if c != class {
				if c != 0 {
					return p
				}
				spaces = true
			}
			if spaces && c != 0 {
				return p
			}
			p = next
		}
		return p
	}
	class := -1
	for p > 0 {
		previous, c := a.wordChunk(b, p, false, wordClass)
		if class < 0 {
			if c != 0 {
				class = c
			}
		} else if c != class {
			return p
		}
		p = previous
	}
	return p
}
