package editor

import (
	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
	"time"
	"unicode"
	"unicode/utf8"
)

type miniJob struct {
	Buffer             *Buffer
	Version            uint64
	Text               text.Rope
	Width, Height, Tab int
}
type miniImage struct {
	Job    miniJob
	Rows   []string
	Height int
}

func (a *App) minimapWidth() int {
	w, _ := a.Screen.Size()
	if !a.minimapVisible || w-a.sidebarWidth() < 65 || a.Current().Transient {
		return 0
	}
	return 13
}
func (a *App) minimapRect() rectangle {
	w, h := a.Screen.Size()
	width := a.minimapWidth()
	return rectangle{w - width, 2, width, max(0, h-4)}
}
func (a *App) wantMinimap() {
	r := a.minimapRect()
	if r.W == 0 || r.H == 0 {
		return
	}
	b := a.Current()
	job := miniJob{b, b.Doc.Version, b.Doc.Text, r.W - 2, r.H, a.Config.TabSize}
	same := func(j miniJob) bool {
		return j.Buffer == b && j.Version == b.Doc.Version && j.Width == job.Width && j.Height == job.Height && j.Tab == job.Tab
	}
	if (a.miniCache != nil && same(a.miniCache.Job)) || same(a.miniWanted) {
		return
	}
	a.miniWanted = job
	if a.miniTimer == nil {
		a.miniTimer = time.NewTimer(100 * time.Millisecond)
	} else {
		a.miniTimer.Stop()
		a.miniTimer.Reset(100 * time.Millisecond)
	}
	a.miniC = a.miniTimer.C
}
func (a *App) runMinimap() {
	a.miniC = nil
	if !a.minimapVisible {
		return
	}
	if a.miniJobs == nil {
		a.miniJobs = make(chan miniJob, 1)
		go func() {
			for {
				select {
				case <-a.ctx.Done():
					return
				case job := <-a.miniJobs:
					image := renderMini(job)
					a.post(func() {
						if a.minimapVisible && job.Buffer == a.Current() && job.Version == job.Buffer.Doc.Version && job.Width == a.minimapRect().W-2 && job.Height == a.minimapRect().H {
							a.miniCache = image
						}
					})
				}
			}
		}()
	}
	select {
	case <-a.miniJobs:
	default:
	}
	select {
	case a.miniJobs <- a.miniWanted:
	default:
	}
}

// At most four short line samples per screen row. No tokenization, no full-file
// materialization, and no retained document-scale bitmap.
func renderMini(job miniJob) *miniImage {
	height := min(job.Height, (job.Text.Lines()+3)/4)
	height = max(1, height)
	cells := make([][]rune, height)
	for i := range cells {
		cells[i] = make([]rune, max(1, job.Width))
	}
	dots := [2][4]rune{{1, 2, 4, 64}, {8, 16, 32, 128}}
	samples := min(job.Text.Lines(), height*4)
	for sample := 0; sample < samples; sample++ {
		line := sample * job.Text.Lines() / samples
		start := job.Text.LineStart(line)
		end := min(job.Text.LineEnd(line), start+512)
		for end > start && !job.Text.Boundary(end) {
			end--
		}
		g := uniseg.NewGraphemes(job.Text.Slice(start, end))
		col := 0
		for g.Next() {
			value := g.Str()
			r, _ := utf8.DecodeRuneInString(value)
			width := g.Width()
			if value == "\t" {
				width = job.Tab - col%job.Tab
			}
			if col/4 >= job.Width*2 {
				break
			}
			if !unicode.IsSpace(r) && !unicode.IsControl(r) {
				for c := col / 4; c <= min(job.Width*2-1, (col+max(1, width)-1)/4); c++ {
					cells[sample/4][c/2] |= dots[c%2][sample%4]
				}
			}
			col += width
		}
	}
	image := &miniImage{Job: job, Height: height}
	image.Job.Text = text.Rope{}
	for _, row := range cells {
		for i, bits := range row {
			if bits == 0 {
				row[i] = ' '
			} else {
				row[i] = 0x2800 + bits
			}
		}
		image.Rows = append(image.Rows, string(row))
	}
	return image
}
func (a *App) drawMinimap(p palette) {
	r := a.minimapRect()
	if r.W == 0 {
		return
	}
	a.wantMinimap()
	a.put(r.X+1, 1, r.W-1, "Map", p.dim)
	for y := 1; y < r.Y+r.H; y++ {
		a.paintRune(r.X, y, '│', nil, p.dim)
	}
	image := a.miniCache
	if image == nil || image.Job.Buffer != a.Current() {
		return
	}
	b := a.Current()
	line := b.Doc.Text.LineAt(b.Doc.Selections[0].Head)
	rows := max(1, image.Height)
	total := max(1, b.Doc.Text.Lines())
	top, bottom := b.Top, b.Top+a.rect(a.focus).H
	if pane := a.panes[a.focus]; len(pane.Rows) > 0 {
		bottom = pane.Rows[len(pane.Rows)-1].Line + 1
	}
	for y, value := range image.Rows {
		if y >= r.H {
			break
		}
		style := p.dim
		from, to := y*total/rows, (y+1)*total/rows
		if from < bottom && to >= top {
			style = p.selection
		}
		if line*rows/total == y {
			style = p.accent.Bold(true)
		}
		a.fill(r.X+1, r.Y+y, r.W-1, style)
		a.put(r.X+1, r.Y+y, r.W-2, value, style)
	}
}
func (a *App) minimapJump(y int, extend bool) {
	r := a.minimapRect()
	b := a.Current()
	height := min(r.H, max(1, (b.Doc.Text.Lines()+3)/4))
	line := min(b.Doc.Text.Lines()-1, max(0, y-r.Y)*b.Doc.Text.Lines()/max(1, height))
	offset := b.Doc.Text.LineStart(line)
	anchor := offset
	if extend {
		anchor = b.Doc.Selections[0].Anchor
	}
	b.Doc.Selections = []text.Selection{{Anchor: anchor, Head: offset}}
	b.Top = max(0, line-a.rect(a.focus).H/2)
	b.WrapTop = 0
	a.reveal()
}
