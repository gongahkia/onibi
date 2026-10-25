// Package gitmerge reads Git conflict stages and parses textual conflict regions.
// It does not choose resolutions, save worktree files or commit merges.
package gitmerge

import (
	"errors"
	"fmt"
	"strings"
)

const MaxBytes = 8 << 20
const MaxConflicts = 2000

type Hunk struct {
	Start, End, Line                                               int
	OursStart, OursEnd, BaseStart, BaseEnd, TheirsStart, TheirsEnd int
	HasBase                                                        bool
	OursLabel, BaseLabel, TheirsLabel                              string
}

func marker(line string) (byte, int, string) {
	line = strings.TrimSuffix(line, "\r")
	if len(line) < 7 {
		return 0, 0, ""
	}
	kind := line[0]
	if !strings.ContainsRune("<|=>", rune(kind)) {
		return 0, 0, ""
	}
	n := 0
	for n < len(line) && line[n] == kind {
		n++
	}
	if n < 7 {
		return 0, 0, ""
	}
	if n < len(line) && line[n] != ' ' && line[n] != '\t' {
		return 0, 0, ""
	}
	label := strings.TrimSpace(line[n:])
	if kind == '=' && label != "" {
		return 0, 0, ""
	}
	return kind, n, label
}

// Parse supports merge, diff3/zdiff3 and custom marker widths. Offsets include
// the markers and their original line endings so replacement preserves context.
func Parse(source string) ([]Hunk, error) {
	if len(source) > MaxBytes {
		return nil, errors.New("conflict inspection is limited to 8 MiB")
	}
	hunks := []Hunk{}
	state, width, line := 0, 0, 0
	var h Hunk
	for start := 0; start < len(source); line++ {
		end := len(source)
		next := end
		if at := strings.IndexByte(source[start:], '\n'); at >= 0 {
			end = start + at
			next = end + 1
		}
		kind, n, label := marker(source[start:end])
		if kind != 0 {
			if state == 0 {
				if kind == '=' {
					start = next
					continue
				} // e.g. a Markdown heading underline
				if kind != '<' {
					return nil, fmt.Errorf("unmatched conflict marker at line %d", line+1)
				}
				if len(hunks) >= MaxConflicts {
					return nil, errors.New("more than 2,000 conflict hunks")
				}
				h = Hunk{Start: start, Line: line, OursStart: next, OursLabel: label}
				width = n
				state = 1
			} else {
				if n != width {
					return nil, fmt.Errorf("inconsistent conflict marker width at line %d", line+1)
				}
				switch {
				case kind == '|' && state == 1:
					h.OursEnd = start
					h.HasBase = true
					h.BaseStart = next
					h.BaseLabel = label
					state = 2
				case kind == '=' && (state == 1 || state == 2):
					if state == 1 {
						h.OursEnd = start
					} else {
						h.BaseEnd = start
					}
					h.TheirsStart = next
					state = 3
				case kind == '>' && state == 3:
					h.TheirsEnd = start
					h.End = next
					h.TheirsLabel = label
					hunks = append(hunks, h)
					state = 0
				default:
					return nil, fmt.Errorf("nested or malformed conflict at line %d", line+1)
				}
			}
		}
		start = next
	}
	if state != 0 {
		return nil, errors.New("unterminated conflict markers; repair manually before choosing a side")
	}
	return hunks, nil
}

func (h Hunk) Resolve(source, side string) (string, error) {
	if h.Start < 0 || h.End > len(source) || h.End < h.Start {
		return "", errors.New("conflict changed")
	}
	switch side {
	case "ours":
		return source[h.OursStart:h.OursEnd], nil
	case "theirs":
		return source[h.TheirsStart:h.TheirsEnd], nil
	case "base":
		if !h.HasBase {
			return "", errors.New("this hunk has no base section; inspect the complete base version")
		}
		return source[h.BaseStart:h.BaseEnd], nil
	case "both":
		return source[h.OursStart:h.OursEnd] + source[h.TheirsStart:h.TheirsEnd], nil
	}
	return "", errors.New("unknown conflict resolution")
}
