package mermaidascii

import "testing"

func TestParseSequenceActorsAndMessages(t *testing.T) {
	s, err := ParseSequence(`sequenceDiagram
    participant U as Nutzer
    participant M as mdpdf
    U->>M: render doku.md
    M-->>U: doku.pdf`)
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Actors) != 2 {
		t.Fatalf("got %d actors, want 2", len(s.Actors))
	}
	if s.Actor("U").Label != "Nutzer" || s.Actor("M").Label != "mdpdf" {
		t.Errorf("labels = %q/%q", s.Actor("U").Label, s.Actor("M").Label)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(s.Messages))
	}
	if m := s.Messages[0]; m.From != "U" || m.To != "M" || m.Label != "render doku.md" || m.Dashed {
		t.Errorf("first message = %+v", m)
	}
	if m := s.Messages[1]; !m.Dashed || m.Head != FilledHead {
		t.Errorf("reply should be dashed with a filled head: %+v", m)
	}
}

func TestParseSequenceArrowSpellings(t *testing.T) {
	s, _ := ParseSequence(`sequenceDiagram
    A->B: one
    A-->B: two
    A->>B: three
    A-->>B: four
    A-)B: five
    A--)B: six
    A-xB: seven`)

	want := []struct {
		dashed bool
		head   ArrowHead
	}{
		{false, OpenHead},
		{true, OpenHead},
		{false, FilledHead},
		{true, FilledHead},
		{false, OpenHead},
		{true, OpenHead},
		{false, FilledHead},
	}
	if len(s.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d", len(s.Messages), len(want))
	}
	for i, w := range want {
		m := s.Messages[i]
		if m.Dashed != w.dashed || m.Head != w.head {
			t.Errorf("message %d = dashed:%v head:%v, want dashed:%v head:%v",
				i, m.Dashed, m.Head, w.dashed, w.head)
		}
	}
}

func TestParseSequenceActivation(t *testing.T) {
	s, _ := ParseSequence(`sequenceDiagram
    A->>+B: request
    B-->>-A: reply`)

	if !s.Messages[0].Activate {
		t.Error("the + before the target should activate it")
	}
	if !s.Messages[1].Deactivate {
		t.Error("the - before the target should deactivate")
	}
}

func TestParseSequenceBlocks(t *testing.T) {
	s, _ := ParseSequence(`sequenceDiagram
    A->>B: ask
    alt happy
        B-->>A: yes
    else sad
        B-->>A: no
    end
    A->>B: bye`)

	if len(s.Blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(s.Blocks))
	}
	b := s.Blocks[0]
	if b.Kind != Alt || b.Label != "happy" {
		t.Errorf("block = %v/%q, want Alt/happy", b.Kind, b.Label)
	}
	if b.First != 1 || b.Last != 2 {
		t.Errorf("block spans %d..%d, want 1..2", b.First, b.Last)
	}
	if len(b.Dividers) != 1 || b.Dividers[0].Label != "sad" || b.Dividers[0].At != 2 {
		t.Errorf("dividers = %+v", b.Dividers)
	}
}

func TestParseSequenceNotes(t *testing.T) {
	s, _ := ParseSequence(`sequenceDiagram
    Alice->>Bob: Hallo
    Note right of Bob: denkt nach
    Note over Alice,Bob: beide
    Bob-->>Alice: Antwort`)

	if len(s.Notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(s.Notes))
	}
	if n := s.Notes[0]; n.Position != NoteRight || n.Text != "denkt nach" || n.After != 0 {
		t.Errorf("first note = %+v", n)
	}
	if n := s.Notes[1]; n.Position != NoteOver || len(n.ActorIDs) != 2 {
		t.Errorf("second note = %+v", n)
	}
}

// TestParseSequenceUnclosedBlock covers a forgotten `end`: the block should
// still appear, closed at the last message, rather than vanishing.
func TestParseSequenceUnclosedBlock(t *testing.T) {
	s, _ := ParseSequence(`sequenceDiagram
    loop forever
        A->>B: tick`)

	if len(s.Blocks) != 1 || s.Blocks[0].Kind != Loop {
		t.Fatalf("blocks = %+v", s.Blocks)
	}
	if s.Blocks[0].Last != 0 {
		t.Errorf("block ends at %d, want 0", s.Blocks[0].Last)
	}
}

func TestIsSequence(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"sequenceDiagram\n A->>B: x", true},
		{"%% comment\nsequenceDiagram\n A->>B: x", true},
		{"  sequencediagram  \n", true},
		{"flowchart TD\n A --> B", false},
		{"", false},
	} {
		if got := isSequence(tc.src); got != tc.want {
			t.Errorf("isSequence(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}
