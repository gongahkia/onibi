package text

import (
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
)

type Selection struct{ Anchor, Head int }

func (s Selection) Start() int { return min(s.Anchor, s.Head) }
func (s Selection) End() int   { return max(s.Anchor, s.Head) }

type Edit struct {
	Start, End int
	Text       string
}
type snapshot struct {
	text       Rope
	selections []Selection
}
type Document struct {
	Text       Rope
	Selections []Selection
	Version    uint64
	saved      Rope
	undo, redo []snapshot
}

func NewDocument(s string) (*Document, error) {
	if !utf8.ValidString(s) {
		return nil, errors.New("file is not valid UTF-8")
	}
	r := New(s)
	return &Document{Text: r, saved: r, Selections: []Selection{{}}}, nil
}
func (d *Document) Dirty() bool { return !d.Text.Same(d.saved) }
func (d *Document) MarkSaved()  { d.saved = d.Text }
func (d *Document) state() snapshot {
	return snapshot{d.Text, append([]Selection(nil), d.Selections...)}
}
func (d *Document) restore(s snapshot) { d.Text = s.text; d.Selections = s.selections; d.Version++ }
func (d *Document) Undo() bool {
	if len(d.undo) == 0 {
		return false
	}
	d.redo = append(d.redo, d.state())
	d.restore(d.undo[len(d.undo)-1])
	d.undo = d.undo[:len(d.undo)-1]
	return true
}
func (d *Document) Redo() bool {
	if len(d.redo) == 0 {
		return false
	}
	d.undo = append(d.undo, d.state())
	d.restore(d.redo[len(d.redo)-1])
	d.redo = d.redo[:len(d.redo)-1]
	return true
}

// Apply validates an entire batch against one version before publishing it.
// Offsets are UTF-8 bytes in the original snapshot, not successive mutations.
func (d *Document) Apply(version uint64, edits []Edit) error {
	if version != d.Version {
		return errors.New("document changed; open a fresh preview")
	}
	if len(edits) == 0 {
		return nil
	}
	edits = append([]Edit(nil), edits...)
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].Start < edits[j].Start })
	previous := -1
	for i, e := range edits {
		if e.Start > e.End || !d.Text.Boundary(e.Start) || !d.Text.Boundary(e.End) || !utf8.ValidString(e.Text) {
			return fmt.Errorf("invalid UTF-8 edit %d", i+1)
		}
		if e.Start < previous || (i > 0 && e.Start == edits[i-1].Start) {
			return errors.New("overlapping edits")
		}
		previous = e.End
	}
	next := d.Text
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		next = next.Replace(e.Start, e.End, e.Text, (d.Version+1)*997+uint64(i))
	}
	d.undo = append(d.undo, d.state())
	if len(d.undo) > 256 {
		d.undo = append([]snapshot(nil), d.undo[len(d.undo)-256:]...)
	}
	d.redo = nil
	d.Text = next
	d.Version++
	d.Selections = nil
	delta := 0
	for _, e := range edits {
		p := e.Start + delta + len(e.Text)
		d.Selections = append(d.Selections, Selection{p, p})
		delta += len(e.Text) - (e.End - e.Start)
	}
	return nil
}
func (d *Document) Insert(s string) error {
	edits := make([]Edit, 0, len(d.Selections))
	for _, sel := range d.Selections {
		edits = append(edits, Edit{sel.Start(), sel.End(), s})
	}
	return d.Apply(d.Version, edits)
}
