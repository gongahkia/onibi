// Package termui provides line-oriented terminal presentation. It never takes
// over the screen or alters stdin; redirected output remains ordinary text.
package termui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rivo/uniseg"
	"golang.org/x/sys/unix"
)

type UI struct {
	Out         io.Writer
	Interactive bool
	ASCII       bool
	NoStyle     bool
	LabelWidth  int
	bannerShown bool
	width       int
	mu          sync.Mutex
}

func New(out io.Writer) *UI {
	u := &UI{Out: out, width: 80, NoStyle: os.Getenv("NO_COLOR") != ""}
	if file, ok := out.(*os.File); ok {
		size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
		u.Interactive = err == nil && os.Getenv("TERM") != "dumb" && os.Getenv("CI") == "" && os.Getenv("ONIBI_PLAIN") == ""
		if err == nil && size.Col > 0 {
			u.width = int(size.Col)
		}
	}
	locale := ""
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(name); value != "" {
			locale = strings.ToUpper(value)
			break
		}
	}
	u.ASCII = !strings.Contains(locale, "UTF")
	return u
}
func Clean(value string) string {
	return strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) {
			return -1
		}
		return ch
	}, value)
}
func clip(value string, width int) string {
	value = Clean(value)
	if uniseg.StringWidth(value) <= width {
		return value
	}
	if width < 4 {
		return strings.Repeat(".", max(width, 0))
	}
	var out strings.Builder
	used := 0
	g := uniseg.NewGraphemes(value)
	for g.Next() {
		w := g.Width()
		if used+w > width-3 {
			break
		}
		out.WriteString(g.Str())
		used += w
	}
	return out.String() + "..."
}
func (u *UI) columns() int {
	if f, ok := u.Out.(*os.File); ok && u.Interactive {
		if size, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ); err == nil && size.Col > 0 {
			return int(size.Col)
		}
	}
	return u.width
}
func (u *UI) lineLocked(format string, args ...any) { fmt.Fprintf(u.Out, format+"\n", args...) }
func (u *UI) Line(format string, args ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lineLocked(format, args...)
}

// wrapCells keeps grapheme clusters intact and prefers word boundaries. It is
// used only for human text; copyable pairing URLs and JSON bypass wrapping.
func wrapCells(value string, width int) []string {
	value = Clean(value)
	width = max(1, width)
	var rows []string
	for uniseg.StringWidth(value) > width {
		g := uniseg.NewGraphemes(value)
		used, end, lastSpace := 0, 0, 0
		for g.Next() {
			if used+g.Width() > width {
				break
			}
			used += g.Width()
			_, end = g.Positions()
			if g.Str() == " " {
				lastSpace = end
			}
		}
		if end == 0 {
			g = uniseg.NewGraphemes(value)
			g.Next()
			_, end = g.Positions()
			rows = append(rows, "?")
			value = value[end:]
			continue
		}
		if lastSpace > 0 {
			end = lastSpace
		}
		rows = append(rows, strings.TrimRight(value[:end], " "))
		value = strings.TrimLeft(value[end:], " ")
	}
	return append(rows, value)
}
func (u *UI) textLocked(value, first, rest string) {
	if !u.Interactive {
		u.lineLocked("%s%s", first, Clean(value))
		return
	}
	width := max(1, u.columns()-1-max(uniseg.StringWidth(first), uniseg.StringWidth(rest)))
	for i, row := range wrapCells(value, width) {
		prefix := rest
		if i == 0 {
			prefix = first
		}
		u.lineLocked("%s%s", prefix, row)
	}
}
func (u *UI) Field(label, value string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	prefix := fmt.Sprintf("  %-*s ", max(14, u.LabelWidth), Clean(label))
	if u.Interactive && u.columns() < 48 {
		u.textLocked(label, "  ", "  ")
		u.textLocked(value, "    ", "    ")
		return
	}
	u.textLocked(value, prefix, strings.Repeat(" ", uniseg.StringWidth(prefix)))
}
func (u *UI) Note(value string) { u.mu.Lock(); defer u.mu.Unlock(); u.textLocked(value, "  ", "  ") }

// Step uses an indeterminate rattle: elapsed time and actual operation details,
// never an invented completion percentage. The task owns cancellation.
func (u *UI) Step(label string, task func(update func(string)) error) error {
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	var detail string
	var detailMu sync.Mutex
	update := func(s string) { detailMu.Lock(); detail = Clean(s); detailMu.Unlock() }
	if u.Interactive {
		render := func() {
			detailMu.Lock()
			d := detail
			detailMu.Unlock()
			rattle := WaveRows
			if u.ASCII {
				rattle = SimpleDots
			}
			line := fmt.Sprintf("  %s %s  %s", rattle.Frame(time.Since(start)), label, time.Since(start).Truncate(time.Second))
			if d != "" {
				line += " / " + d
			}
			u.mu.Lock()
			fmt.Fprintf(u.Out, "\r\x1b[2K%s", clip(line, u.columns()-1))
			u.mu.Unlock()
		}
		render()
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(90 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					render()
				}
			}
		}()
	} else {
		u.Line("[start] %s", label)
	}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			if u.Interactive {
				u.mu.Lock()
				fmt.Fprint(u.Out, "\r\x1b[2K")
				u.mu.Unlock()
			}
		})
	}
	defer finish()
	err := task(update)
	finish()
	u.mu.Lock()
	defer u.mu.Unlock()
	status := "ok"
	if err != nil {
		status = "failed"
	}
	fmt.Fprintf(u.Out, "[%s] %s (%s)\n", status, Clean(label), time.Since(start).Round(time.Millisecond))
	return err
}
