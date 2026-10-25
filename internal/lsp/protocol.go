// Package lsp owns the optional language-server boundary. Positions on the wire
// use UTF-16; the editor and text core use UTF-8 byte offsets.
package lsp

import (
	"errors"
	"github.com/gongahkia/shack/internal/text"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}
type TextEdit struct {
	Range        Range  `json:"range"`
	NewText      string `json:"newText"`
	AnnotationID string `json:"annotationId,omitempty"`
}
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
}
type Location struct {
	URI                  string `json:"uri"`
	Range                Range  `json:"range"`
	TargetURI            string `json:"targetUri"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}
type CompletionEdit struct {
	Range   *Range `json:"range"`
	Insert  *Range `json:"insert"`
	Replace *Range `json:"replace"`
	NewText string `json:"newText"`
}

func (e CompletionEdit) Edit() (TextEdit, error) {
	r := e.Range
	if r == nil {
		r = e.Replace
	}
	if r == nil {
		r = e.Insert
	}
	if r == nil {
		return TextEdit{}, errors.New("completion has no valid edit range")
	}
	return TextEdit{Range: *r, NewText: e.NewText}, nil
}

type Completion struct {
	Label               string          `json:"label"`
	Detail              string          `json:"detail"`
	InsertText          string          `json:"insertText"`
	InsertTextFormat    int             `json:"insertTextFormat"`
	TextEdit            *CompletionEdit `json:"textEdit"`
	AdditionalTextEdits []TextEdit      `json:"additionalTextEdits"`
}

func URI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
func Path(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(filepath.FromSlash(u.Path)) {
		return "", errors.New("definition is not a local file")
	}
	return filepath.FromSlash(u.Path), nil
}
func PositionAt(source string, offset int) Position {
	offset = max(0, min(offset, len(source)))
	prefix := source[:offset]
	line := strings.Count(prefix, "\n")
	start := strings.LastIndexByte(prefix, '\n') + 1
	units := 0
	for _, r := range prefix[start:] {
		units += utf16.RuneLen(r)
	}
	return Position{line, units}
}
func Offset(source string, p Position) (int, error) {
	if p.Line < 0 || p.Character < 0 {
		return 0, errors.New("negative LSP position")
	}
	start := 0
	for line := 0; line < p.Line; line++ {
		i := strings.IndexByte(source[start:], '\n')
		if i < 0 {
			return 0, errors.New("LSP line out of bounds")
		}
		start += i + 1
	}
	end := len(source)
	if i := strings.IndexByte(source[start:], '\n'); i >= 0 {
		end = start + i
	}
	if end > start && source[end-1] == '\r' {
		end--
	}
	units := 0
	for at := start; at < end; {
		if units == p.Character {
			return at, nil
		}
		r, n := utf8.DecodeRuneInString(source[at:])
		units += utf16.RuneLen(r)
		if units > p.Character {
			return 0, errors.New("LSP position splits a surrogate pair")
		}
		at += n
	}
	if units == p.Character {
		return end, nil
	}
	return 0, errors.New("LSP column out of bounds")
}
func Edits(source string, edits []TextEdit) ([]text.Edit, error) {
	out := make([]text.Edit, 0, len(edits))
	for _, e := range edits {
		start, err := Offset(source, e.Range.Start)
		if err != nil {
			return nil, err
		}
		end, err := Offset(source, e.Range.End)
		if err != nil {
			return nil, err
		}
		if end < start {
			return nil, errors.New("reversed LSP edit")
		}
		out = append(out, text.Edit{Start: start, End: end, Text: e.NewText})
	}
	return out, nil
}
func Change(before, after string) map[string]any {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	for prefix > 0 && prefix < len(before) && !utf8.RuneStart(before[prefix]) {
		prefix--
	}
	oldEnd, newEnd := len(before), len(after)
	for oldEnd > prefix && newEnd > prefix && before[oldEnd-1] == after[newEnd-1] {
		oldEnd--
		newEnd--
	}
	for oldEnd < len(before) && !utf8.RuneStart(before[oldEnd]) {
		oldEnd++
		newEnd++
	}
	// CRLF is a single line break in the protocol.
	if prefix > 0 && prefix < len(before) && before[prefix-1] == '\r' && before[prefix] == '\n' {
		prefix--
	}
	if oldEnd > 0 && oldEnd < len(before) && before[oldEnd-1] == '\r' && before[oldEnd] == '\n' {
		oldEnd++
		newEnd++
	}
	return map[string]any{"range": Range{PositionAt(before, prefix), PositionAt(before, oldEnd)}, "text": after[prefix:newEnd]}
}
