package mermaidascii

import (
	"regexp"
	"strings"
)

// A sequence diagram reads downwards: the actors stand in a row at the top,
// each drops a lifeline, and the messages between them are the rungs of that
// ladder, in the order they were written.

// ActorKind tells how an actor is drawn.
type ActorKind int

const (
	// Participant is drawn as a box.
	Participant ActorKind = iota
	// ActorFigure is declared with `actor` and drawn as a box here too;
	// the stick figure of the reference renderers needs more than one line.
	ActorFigure
)

// Actor is one column of a sequence diagram.
type Actor struct {
	ID    string
	Label string
	Kind  ActorKind
}

// ArrowHead is the tip of a message arrow.
type ArrowHead int

const (
	// OpenHead is written `->` or `-)`.
	OpenHead ArrowHead = iota
	// FilledHead is written `->>` and is the common case.
	FilledHead
)

// Message is one exchange between two actors.
type Message struct {
	From, To string
	Label    string
	// Dashed marks the `--` spellings, conventionally a reply.
	Dashed bool
	Head   ArrowHead
	// Activate and Deactivate come from the `+` and `-` before the target
	// and mark where an actor becomes busy.
	Activate, Deactivate bool
}

// BlockKind is the keyword that opened a block.
type BlockKind int

const (
	Loop BlockKind = iota
	Alt
	Opt
	Par
	Critical
	Break
	Rect
)

// Divider is an `else` or `and` inside a block.
type Divider struct {
	// At is the message index the divider sits before.
	At    int
	Label string
}

// Block is a framed group of messages, such as loop or alt.
type Block struct {
	Kind  BlockKind
	Label string
	// First and Last are message indices, Last inclusive.
	First, Last int
	Dividers    []Divider
}

// NotePosition tells where a note sits relative to its actors.
type NotePosition int

const (
	NoteOver NotePosition = iota
	NoteLeft
	NoteRight
)

// Note is a remark attached to one or more actors.
type Note struct {
	ActorIDs []string
	Text     string
	Position NotePosition
	// After is the index of the message the note follows, -1 before the first.
	After int
}

// Sequence is a parsed sequence diagram.
type Sequence struct {
	Actors   []*Actor
	Messages []*Message
	Blocks   []*Block
	Notes    []*Note

	byID map[string]*Actor
}

// actor returns the actor for id, declaring it on first sight. Mermaid lets a
// message name an actor that was never declared.
func (s *Sequence) actor(id string) *Actor {
	if s.byID == nil {
		s.byID = make(map[string]*Actor)
	}
	if a, ok := s.byID[id]; ok {
		return a
	}
	a := &Actor{ID: id, Label: id}
	s.byID[id] = a
	s.Actors = append(s.Actors, a)
	return a
}

// Actor returns the actor with the given id, or nil.
func (s *Sequence) Actor(id string) *Actor { return s.byID[id] }

var (
	actorRe = regexp.MustCompile(`^(participant|actor)\s+(\S+?)(?:\s+as\s+(.+))?$`)
	noteRe  = regexp.MustCompile(`(?i)^Note\s+(left of|right of|over)\s+([^:]+):\s*(.+)$`)
	blockRe = regexp.MustCompile(`^(loop|alt|opt|par|critical|break|rect)\b\s*(.*)$`)
	divRe   = regexp.MustCompile(`^(else|and)\b\s*(.*)$`)
	// msgRe covers every arrow spelling, longest first so that `-->>` is not
	// read as `-->` with a stray `>`.
	msgRe = regexp.MustCompile(`^(\S+?)\s*(-->>|--x|--\)|-->|->>|-x|-\)|->)\s*([+-]?)(\S+?)\s*:\s*(.+)$`)
)

// ParseSequence reads Mermaid sequence diagram source.
func ParseSequence(src string) (*Sequence, error) {
	s := &Sequence{}

	// open blocks, innermost last
	var open []*Block

	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(stripComment(line))
		if line == "" || (i == 0 && strings.EqualFold(line, "sequenceDiagram")) {
			continue
		}

		switch {
		case parseActorLine(s, line):
		case parseNoteLine(s, line):
		case parseBlockLine(s, line, &open):
		case parseDividerLine(s, line, open):
		case parseEndLine(s, line, &open):
		case parseMessageLine(s, line):
		}
	}

	// A block left open at the end is closed at the last message, so that a
	// forgotten `end` costs the frame's lower edge and nothing else.
	for len(open) > 0 {
		b := open[len(open)-1]
		open = open[:len(open)-1]
		b.Last = max(len(s.Messages)-1, b.First)
		s.Blocks = append(s.Blocks, b)
	}
	return s, nil
}

func parseActorLine(s *Sequence, line string) bool {
	m := actorRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	a := s.actor(m[2])
	if m[3] != "" {
		a.Label = normalizeBreaks(strings.TrimSpace(m[3]))
	}
	if m[1] == "actor" {
		a.Kind = ActorFigure
	}
	return true
}

func parseNoteLine(s *Sequence, line string) bool {
	m := noteRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	var ids []string
	for _, id := range strings.Split(m[2], ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		s.actor(id)
		ids = append(ids, id)
	}
	pos := NoteOver
	switch strings.ToLower(m[1]) {
	case "left of":
		pos = NoteLeft
	case "right of":
		pos = NoteRight
	}
	s.Notes = append(s.Notes, &Note{
		ActorIDs: ids,
		Text:     normalizeBreaks(strings.TrimSpace(m[3])),
		Position: pos,
		After:    len(s.Messages) - 1,
	})
	return true
}

func parseBlockLine(s *Sequence, line string, open *[]*Block) bool {
	m := blockRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	*open = append(*open, &Block{
		Kind:  blockKind(m[1]),
		Label: normalizeBreaks(strings.TrimSpace(m[2])),
		First: len(s.Messages),
	})
	return true
}

func parseDividerLine(s *Sequence, line string, open []*Block) bool {
	m := divRe.FindStringSubmatch(line)
	if m == nil || len(open) == 0 {
		return false
	}
	b := open[len(open)-1]
	b.Dividers = append(b.Dividers, Divider{
		At:    len(s.Messages),
		Label: normalizeBreaks(strings.TrimSpace(m[2])),
	})
	return true
}

func parseEndLine(s *Sequence, line string, open *[]*Block) bool {
	if line != "end" || len(*open) == 0 {
		return false
	}
	b := (*open)[len(*open)-1]
	*open = (*open)[:len(*open)-1]
	b.Last = max(len(s.Messages)-1, b.First)
	s.Blocks = append(s.Blocks, b)
	return true
}

func parseMessageLine(s *Sequence, line string) bool {
	m := msgRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	s.actor(m[1])
	s.actor(m[4])

	arrow := m[2]
	msg := &Message{
		From:  m[1],
		To:    m[4],
		Label: normalizeBreaks(strings.TrimSpace(m[5])),
		// Two leading dashes make the line dashed, which by convention
		// marks a reply.
		Dashed: strings.HasPrefix(arrow, "--"),
		Head:   OpenHead,
	}
	if strings.Contains(arrow, ">>") || strings.Contains(arrow, "x") {
		msg.Head = FilledHead
	}
	switch m[3] {
	case "+":
		msg.Activate = true
	case "-":
		msg.Deactivate = true
	}
	s.Messages = append(s.Messages, msg)
	return true
}

func blockKind(s string) BlockKind {
	switch s {
	case "alt":
		return Alt
	case "opt":
		return Opt
	case "par":
		return Par
	case "critical":
		return Critical
	case "break":
		return Break
	case "rect":
		return Rect
	default:
		return Loop
	}
}

// isSequence reports whether the source opens a sequence diagram.
func isSequence(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(stripComment(line))
		if line == "" {
			continue
		}
		return strings.EqualFold(line, "sequenceDiagram")
	}
	return false
}
