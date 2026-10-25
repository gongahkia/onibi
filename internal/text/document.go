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
	text             Rope
	selections       []Selection
	changes, reverse [][]Edit
}
type Document struct {
	Text        Rope
	Selections  []Selection
	Version     uint64
	LastChanges [][]Edit
	saved       Rope
	undo, redo  []snapshot
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
	return snapshot{text: d.Text, selections: append([]Selection(nil), d.Selections...)}
}
func (d *Document) restore(s snapshot) {
	d.Text = s.text
	d.Selections = s.selections
	d.Version++
	d.LastChanges = s.changes
}
func (d *Document) Undo() bool {
	if len(d.undo) == 0 {
		return false
	}
	target := d.undo[len(d.undo)-1]
	back := d.state()
	back.changes, back.reverse = target.reverse, target.changes
	d.redo = append(d.redo, back)
	d.restore(target)
	d.undo = d.undo[:len(d.undo)-1]
	return true
}
func (d *Document) Redo() bool {
	if len(d.redo) == 0 {
		return false
	}
	target := d.redo[len(d.redo)-1]
	back := d.state()
	back.changes, back.reverse = target.reverse, target.changes
	d.undo = append(d.undo, back)
	d.restore(target)
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
	changed := false
	for _, e := range edits {
		if d.Text.Slice(e.Start, e.End) != e.Text {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	before := d.state()
	inverse := make([]Edit, len(edits))
	deltaBefore := 0
	for i, e := range edits {
		inverse[i] = Edit{Start: e.Start + deltaBefore, End: e.Start + deltaBefore + len(e.Text), Text: d.Text.Slice(e.Start, e.End)}
		deltaBefore += len(e.Text) - (e.End - e.Start)
	}
	before.changes = [][]Edit{inverse}
	before.reverse = [][]Edit{edits}
	next := d.Text
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		next = next.Replace(e.Start, e.End, e.Text, (d.Version+1)*997+uint64(i))
	}
	d.undo = append(d.undo, before)
	if len(d.undo) > 256 {
		d.undo = append([]snapshot(nil), d.undo[len(d.undo)-256:]...)
	}
	d.redo = nil
	d.Text = next
	d.LastChanges = [][]Edit{edits}
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

// Coalesce joins adjacent typing transactions without flattening shared text.
func (d *Document) Coalesce() {
	if len(d.undo) < 2 {
		return
	}
	n := len(d.undo)
	older, newer := d.undo[n-2], d.undo[n-1]
	if newer.text.Same(d.saved) || len(older.changes) >= 128 {
		return
	}
	older.changes = append(append([][]Edit(nil), newer.changes...), older.changes...)
	older.reverse = append(append([][]Edit(nil), older.reverse...), newer.reverse...)
	d.undo[n-2] = older
	d.undo = d.undo[:n-1]
}

// Clone can be staged independently; no mutable history slice aliases its source.
func (d *Document) Clone() *Document {
	c := *d
	c.undo = append([]snapshot(nil), d.undo...)
	c.redo = append([]snapshot(nil), d.redo...)
	c.Selections = append([]Selection(nil), d.Selections...)
	return &c
}
func Rebase(p int, edits []Edit) int {
	delta := 0
	for _, e := range edits {
		if p < e.Start {
			break
		}
		if p <= e.End {
			return e.Start + delta + len(e.Text)
		}
		delta += len(e.Text) - (e.End - e.Start)
	}
	return p + delta
}
