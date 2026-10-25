package text

import "testing"

func TestLongEditSequenceKeepsGroupedAndEarlierUndo(t *testing.T) {
	d, _ := NewDocument("start")
	d.Insert("prior ")
	version := d.Version
	batches := [][]Edit{}
	for i := 0; i < 300; i++ {
		batches = append(batches, []Edit{{Start: 0, End: 0, Text: "😀"}})
	}
	if err := d.ApplySequence(version, batches); err != nil {
		t.Fatal(err)
	}
	if d.Version != version+300 || len(d.LastChanges) != 300 || d.Text.Len() != len("prior start")+300*4 {
		t.Fatal("sequence was not one publication")
	}
	if !d.Undo() || d.Text.String() != "prior start" {
		t.Fatal("sequence split into multiple undo entries")
	}
	if !d.Redo() || d.Text.Len() != len("prior start")+300*4 {
		t.Fatal("redo")
	}
	d.Undo()
	if !d.Undo() || d.Text.String() != "start" {
		t.Fatal("earlier history was lost")
	}
}
func TestEditSequenceRejectsLateInvalidBatchWithoutPublishing(t *testing.T) {
	d, _ := NewDocument("😀x")
	if err := d.ApplySequence(d.Version, [][]Edit{{{Start: 4, End: 5, Text: "y"}}, {{Start: 1, End: 2, Text: "bad"}}}); err == nil {
		t.Fatal("split UTF-8 accepted")
	}
	if d.Text.String() != "😀x" || d.Version != 0 || d.Undo() {
		t.Fatal("partial sequence published")
	}
}
