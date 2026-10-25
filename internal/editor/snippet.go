package editor

import (
	"errors"
	"github.com/gongahkia/shack/internal/text"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type snippetStop struct {
	Number int
	Ranges []text.Selection
}
type snippetState struct {
	Buffer  *Buffer
	Version uint64
	Stops   []snippetStop
	Index   int
}

// parseSnippet accepts numbered tabstops, plain defaults, choices and escapes.
// Nested placeholders, variables and transforms are rejected, never inserted raw.
func parseSnippet(source string) (string, []snippetStop, error) {
	var out strings.Builder
	ranges := map[int][]text.Selection{}
	values := map[int]string{}
	fail := func() (string, []snippetStop, error) {
		return "", nil, errors.New("completion uses an unsupported snippet variable, nested placeholder or transform")
	}
	for i := 0; i < len(source); {
		if source[i] == '\\' && i+1 < len(source) {
			out.WriteByte(source[i+1])
			i += 2
			continue
		}
		if source[i] != '$' {
			r, n := utf8.DecodeRuneInString(source[i:])
			out.WriteRune(r)
			i += n
			continue
		}
		i++
		braced := i < len(source) && source[i] == '{'
		if braced {
			i++
		}
		start := i
		for i < len(source) && source[i] >= '0' && source[i] <= '9' {
			i++
		}
		if i == start {
			return fail()
		}
		num, err := strconv.Atoi(source[start:i])
		if err != nil {
			return fail()
		}
		value, defined := values[num]
		if braced {
			if i >= len(source) {
				return fail()
			}
			switch source[i] {
			case '}':
				i++
			case ':', '|':
				choice := source[i] == '|'
				i++
				var body strings.Builder
				first := true
				closed := false
				for i < len(source) {
					c := source[i]
					if c == '\\' && i+1 < len(source) {
						if first {
							body.WriteByte(source[i+1])
						}
						i += 2
						continue
					}
					if c == '$' || c == '{' {
						return fail()
					}
					if (!choice && c == '}') || (choice && c == '|' && i+1 < len(source) && source[i+1] == '}') {
						i++
						if choice {
							i++
						}
						closed = true
						break
					}
					if choice && c == ',' {
						first = false
					} else if first {
						body.WriteByte(c)
					}
					i++
				}
				if !closed {
					return fail()
				}
				value = body.String()
				values[num] = value
				defined = true
			default:
				return fail()
			}
		}
		if !defined {
			value = ""
		}
		from := out.Len()
		out.WriteString(value)
		ranges[num] = append(ranges[num], text.Selection{Anchor: from, Head: out.Len()})
	}
	numbers := []int{}
	for n := range ranges {
		if n != 0 {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	if _, ok := ranges[0]; !ok {
		ranges[0] = []text.Selection{{Anchor: out.Len(), Head: out.Len()}}
	}
	numbers = append(numbers, 0)
	stops := []snippetStop{}
	for _, n := range numbers {
		stops = append(stops, snippetStop{n, ranges[n]})
	}
	return out.String(), stops, nil
}
func (a *App) snippetTab(back bool) bool {
	s := a.snippet
	if s == nil || s.Buffer != a.Current() || s.Version != a.Current().Doc.Version {
		a.snippet = nil
		return false
	}
	if back {
		s.Index = max(0, s.Index-1)
	} else {
		s.Index++
	}
	if s.Index >= len(s.Stops) {
		a.snippet = nil
		return false
	}
	a.selectSnippetStop()
	return true
}
func (a *App) selectSnippetStop() {
	s := a.snippet
	if s == nil {
		return
	}
	stop := s.Stops[s.Index]
	s.Buffer.Doc.Selections = append([]text.Selection(nil), stop.Ranges...)
	a.normalizeSelections()
	if stop.Number == 0 {
		a.snippet = nil
	}
	a.reveal()
}
func rebaseLeft(p int, edits []text.Edit) int {
	delta := 0
	for _, e := range edits {
		if p < e.Start {
			break
		}
		if p <= e.End {
			return e.Start + delta
		}
		delta += len(e.Text) - (e.End - e.Start)
	}
	return p + delta
}
func (a *App) rebaseSnippet() {
	s := a.snippet
	if s == nil {
		return
	}
	b := a.Current()
	if b != s.Buffer || b.Doc.Version != s.Version+1 {
		a.snippet = nil
		return
	}
	for _, edits := range b.Doc.LastChanges {
		for i := range s.Stops {
			for j, r := range s.Stops[i].Ranges {
				if i == s.Index {
					r.Anchor = rebaseLeft(r.Anchor, edits)
				} else {
					r.Anchor = text.Rebase(r.Anchor, edits)
				}
				r.Head = text.Rebase(r.Head, edits)
				s.Stops[i].Ranges[j] = r
			}
		}
	}
	s.Version = b.Doc.Version
}
