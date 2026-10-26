package markup

import (
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

type TokenStyle uint8

const (
	TokenPlain TokenStyle = iota
	TokenKeyword
	TokenComment
	TokenString
	TokenNumber
	TokenName
)

// Code lexing happens once on the preview worker. Unknown/large/problematic
// blocks keep literal text, never partially tokenised or rewritten output.
func (r *renderer) codeBlock(source, language string) {
	tokens := r.codeTokens(source, language)
	if tokens == nil {
		r.write(source, Style{Code: true})
		return
	}
	for _, t := range tokens {
		s := Style{Code: true}
		switch {
		case t.Type.InCategory(chroma.Keyword):
			s.Token = TokenKeyword
		case t.Type.InCategory(chroma.Comment):
			s.Token = TokenComment
		case t.Type.InSubCategory(chroma.LiteralString):
			s.Token = TokenString
		case t.Type.InSubCategory(chroma.LiteralNumber):
			s.Token = TokenNumber
		case t.Type == chroma.NameFunction || t.Type == chroma.NameClass || t.Type == chroma.NameBuiltin:
			s.Token = TokenName
		}
		r.write(t.Value, s)
	}
}

func (r *renderer) codeTokens(source, language string) (out []chroma.Token) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	if len(source) > 64<<10 || language == "" || r.ctx.Err() != nil {
		return nil
	}
	lexer := lexers.Get(strings.ToLower(language))
	if lexer == nil {
		return nil
	}
	if r.codeDeadline.IsZero() {
		r.codeDeadline = time.Now().Add(200 * time.Millisecond)
	}
	if time.Now().After(r.codeDeadline) || r.codeBytes+len(source) > 256<<10 {
		return nil
	}
	r.codeBytes += len(source)
	deadline := minTime(time.Now().Add(50*time.Millisecond), r.codeDeadline)
	it, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, source)
	if err != nil {
		return nil
	}
	offset := 0
	for t := it(); t != chroma.EOF; t = it() {
		if r.ctx.Err() != nil || time.Now().After(deadline) || len(out) >= min(4096, max(0, MaxSpans-len(r.spans)-1024)) || !strings.HasPrefix(source[offset:], t.Value) {
			return nil
		}
		offset += len(t.Value)
		out = append(out, t)
	}
	if offset != len(source) {
		return nil
	}
	return out
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
