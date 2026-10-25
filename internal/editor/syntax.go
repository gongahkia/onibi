package editor

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"time"
)

type span struct {
	Start, End int
	Color      tcell.Color
}
type syntaxJob struct {
	Buffer      *Buffer
	Version     uint64
	Text        text.Rope
	Path, Theme string
}

func (a *App) services() {
	b := a.Current()
	if !a.Config.Syntax || b.Doc.Text.Len() > a.Config.ServiceMaxBytes {
		return
	}
	job := syntaxJob{b, b.Doc.Version, b.Doc.Text, b.File.Path, a.Config.Theme}
	select {
	case <-a.syntaxJobs:
	default:
	}
	select {
	case a.syntaxJobs <- job:
	default:
	}
}
func (a *App) syntaxWorker() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case job := <-a.syntaxJobs:
			spans := highlight(a.ctx.Done(), job)
			a.post(func() {
				if job.Buffer.Doc.Version == job.Version {
					job.Buffer.Spans = spans
					job.Buffer.SyntaxVersion = job.Version
					job.Buffer.SyntaxReady = true
				}
			})
		}
	}
}
func highlight(done <-chan struct{}, job syntaxJob) (out []span) {
	// Some lexers are third-party ports; a lexer failure must not end editing.
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	lexer := lexers.Match(job.Path)
	if lexer == nil {
		return nil
	}
	source := job.Text.String()
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, source)
	if err != nil {
		return nil
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	offset := 0
	for token := iterator(); token != chroma.EOF; token = iterator() {
		select {
		case <-done:
			return nil
		default:
		}
		if time.Now().After(deadline) {
			return nil
		}
		end := offset + len(token.Value)
		color := tcell.ColorDefault
		t := token.Type
		switch {
		case t.InCategory(chroma.Keyword):
			color = tcell.NewHexColor(0xc9a3f5)
		case t.InCategory(chroma.Comment):
			color = tcell.NewHexColor(0x8c9aab)
		case t.InSubCategory(chroma.LiteralString):
			color = tcell.NewHexColor(0x9dd59b)
		case t.InSubCategory(chroma.LiteralNumber):
			color = tcell.NewHexColor(0xe4bd7a)
		case t == chroma.NameFunction || t == chroma.NameClass:
			color = tcell.NewHexColor(0x86c7ef)
		}
		if job.Theme == "light" {
			switch {
			case t.InCategory(chroma.Keyword):
				color = tcell.NewHexColor(0x7543a8)
			case t.InCategory(chroma.Comment):
				color = tcell.NewHexColor(0x647182)
			case t.InSubCategory(chroma.LiteralString):
				color = tcell.NewHexColor(0x267536)
			case t.InSubCategory(chroma.LiteralNumber):
				color = tcell.NewHexColor(0x946208)
			case t == chroma.NameFunction || t == chroma.NameClass:
				color = tcell.NewHexColor(0x155f9a)
			}
		}
		if color != tcell.ColorDefault {
			out = append(out, span{offset, end, color})
		}
		offset = end
	}
	return out
}
