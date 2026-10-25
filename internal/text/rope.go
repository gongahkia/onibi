// Package text owns immutable, indexed UTF-8 text and atomic editing history.
package text

import (
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const chunkSize = 4096

// Rope is a persistent piece tree. Snapshots share unchanged chunks and tree
// branches; moving a selection never copies or revalidates the document.
type Rope struct{ root *node }
type node struct {
	left, right  *node
	text         string
	breaks       []int
	priority     uint64
	bytes, lines int
	special      bool
	localSpecial bool
}

func size(n *node) int {
	if n == nil {
		return 0
	}
	return n.bytes
}
func lines(n *node) int {
	if n == nil {
		return 0
	}
	return n.lines
}
func rebuilt(n *node, l, r *node) *node {
	c := *n
	c.left, c.right = l, r
	c.bytes = size(l) + len(c.text) + size(r)
	c.lines = lines(l) + len(c.breaks) + lines(r)
	c.special = n.localSpecial || special(l) || special(r)
	return &c
}
func piece(s string, priority uint64) *node {
	n := &node{text: s, priority: priority, bytes: len(s)}
	for i := range s {
		if s[i] == '\n' {
			n.breaks = append(n.breaks, i)
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] >= 127 {
			n.localSpecial = true
			break
		}
	}
	n.special = n.localSpecial
	n.lines = len(n.breaks)
	return n
}
func merge(a, b *node) *node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.priority > b.priority {
		return rebuilt(a, a.left, merge(a.right, b))
	}
	return rebuilt(b, merge(a, b.left), b.right)
}
func split(n *node, at int) (*node, *node) {
	if n == nil {
		return nil, nil
	}
	l := size(n.left)
	if at < l {
		a, b := split(n.left, at)
		return a, rebuilt(n, b, n.right)
	}
	if at > l+len(n.text) {
		a, b := split(n.right, at-l-len(n.text))
		return rebuilt(n, n.left, a), b
	}
	i := at - l
	a, b := n.left, n.right
	if i > 0 {
		a = merge(a, piece(n.text[:i], n.priority))
	}
	if i < len(n.text) {
		b = merge(piece(n.text[i:], n.priority), b)
	}
	return a, b
}

// build uses independent pseudo-random priorities, with no mutable global state.
func build(s string, seed uint64) *node {
	var root *node
	for len(s) > 0 {
		n := min(len(s), chunkSize)
		for n < len(s) && !utf8.RuneStart(s[n]) {
			n--
		}
		seed += 0x9e3779b97f4a7c15
		z := seed
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		z ^= z >> 31
		root = merge(root, piece(s[:n], z))
		s = s[n:]
	}
	return root
}
func New(s string) Rope             { return Rope{build(s, 0)} }
func (r Rope) Len() int             { return size(r.root) }
func (r Rope) Lines() int           { return lines(r.root) + 1 }
func (r Rope) Same(other Rope) bool { return r.root == other.root }
func (r Rope) Replace(start, end int, s string, seed uint64) Rope {
	a, b := split(r.root, start)
	_, b = split(b, end-start)
	return Rope{merge(merge(a, build(s, seed)), b)}
}
func visit(n *node, start, end int, f func(string)) {
	if n == nil || start >= end {
		return
	}
	l := size(n.left)
	stop := l + len(n.text)
	if start < l {
		visit(n.left, start, min(end, l), f)
	}
	if start < stop && end > l {
		f(n.text[max(0, start-l):min(len(n.text), end-l)])
	}
	if end > stop {
		visit(n.right, max(0, start-stop), end-stop, f)
	}
}
func (r Rope) Slice(start, end int) string {
	start = max(0, min(start, r.Len()))
	end = max(start, min(end, r.Len()))
	var b strings.Builder
	b.Grow(end - start)
	visit(r.root, start, end, func(s string) { b.WriteString(s) })
	return b.String()
}
func (r Rope) String() string { return r.Slice(0, r.Len()) }
func (r Rope) WriteTo(w io.Writer) (int64, error) {
	var count int64
	var err error
	visit(r.root, 0, r.Len(), func(s string) {
		if err == nil {
			var n int
			n, err = io.WriteString(w, s)
			count += int64(n)
		}
	})
	return count, err
}
func nthBreak(n *node, line int) int {
	l := lines(n.left)
	if line < l {
		return nthBreak(n.left, line)
	}
	if line < l+len(n.breaks) {
		return size(n.left) + n.breaks[line-l]
	}
	return size(n.left) + len(n.text) + nthBreak(n.right, line-l-len(n.breaks))
}
func (r Rope) LineStart(line int) int {
	if line <= 0 {
		return 0
	}
	if line >= r.Lines() {
		return r.Len()
	}
	return nthBreak(r.root, line-1) + 1
}
func (r Rope) Line(line int) string {
	start, end := r.LineStart(line), r.LineStart(line+1)
	if line < r.Lines()-1 {
		end--
	}
	return strings.TrimSuffix(r.Slice(start, end), "\r")
}
func (r Rope) LineAt(offset int) int {
	offset = max(0, min(offset, r.Len()))
	n := r.root
	count := 0
	for n != nil {
		l := size(n.left)
		if offset < l {
			n = n.left
			continue
		}
		count += lines(n.left)
		offset -= l
		if offset <= len(n.text) {
			return count + sort.SearchInts(n.breaks, offset)
		}
		count += len(n.breaks)
		offset -= len(n.text)
		n = n.right
	}
	return count
}
func (r Rope) Boundary(offset int) bool {
	return offset >= 0 && offset <= r.Len() && (offset == r.Len() || utf8.RuneStart(r.Slice(offset, offset+1)[0]))
}

func special(n *node) bool {
	if n == nil {
		return false
	}
	return n.special
}
func plain(n *node, start, end int) bool {
	if n == nil || start >= end {
		return true
	}
	if start == 0 && end == size(n) {
		return !n.special
	}
	l := size(n.left)
	stop := l + len(n.text)
	if start < l && !plain(n.left, start, min(end, l)) {
		return false
	}
	if start < stop && end > l {
		if n.localSpecial {
			for _, b := range []byte(n.text[max(0, start-l):min(len(n.text), end-l)]) {
				if b < 32 || b >= 127 {
					return false
				}
			}
		}
	}
	return end <= stop || plain(n.right, max(0, start-stop), end-stop)
}

// Plain is indexed: an ordinary ASCII line needs no grapheme scan or copy.
func (r Rope) Plain(start, end int) bool { return plain(r.root, max(0, start), min(end, r.Len())) }
func (r Rope) LineEnd(line int) int {
	end := r.LineStart(line + 1)
	if line < r.Lines()-1 {
		end--
	}
	if end > r.LineStart(line) && r.Slice(end-1, end) == "\r" {
		end--
	}
	return end
}
