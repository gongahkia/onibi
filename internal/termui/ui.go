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
	width       int
	mu          sync.Mutex
}

func New(out io.Writer) *UI {
	u := &UI{Out: out, width: 80}
	if file, ok := out.(*os.File); ok {
		size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
		u.Interactive = err == nil && os.Getenv("TERM") != "dumb" && os.Getenv("CI") == "" && os.Getenv("ONIBI_PLAIN") == ""
		if err == nil && size.Col > 0 {
			u.width = int(size.Col)
		}
	}
	locale := strings.ToUpper(os.Getenv("LC_ALL") + os.Getenv("LC_CTYPE") + os.Getenv("LANG"))
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
func (u *UI) Line(format string, args ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintf(u.Out, format+"\n", args...)
}
func (u *UI) Banner(subtitle string) {
	if !u.Interactive {
		u.Line("Onibi / %s", Clean(subtitle))
		return
	}
	u.Line("")
	if u.columns() >= 42 {
		for _, line := range []string{`   ___  _  _ ___ ___ ___`, `  / _ \| \| |_ _| _ )_ _|`, " | (_) | .` || || _ \\| |", `  \___/|_|\_|___|___/___|`} {
			u.Line("%s", line)
		}
	} else {
		u.Line(" ONIBI")
	}
	u.Line("  %s\n", clip(subtitle, u.columns()-3))
}
func (u *UI) Field(label, value string) { u.Line("  %-12s %s", label, Clean(value)) }
func (u *UI) Note(value string)         { u.Line("  %s", Clean(value)) }

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
	err := task(update)
	close(done)
	wg.Wait()
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Interactive {
		fmt.Fprint(u.Out, "\r\x1b[2K")
	}
	status := "ok"
	if err != nil {
		status = "failed"
	}
	fmt.Fprintf(u.Out, "[%s] %s (%s)\n", status, Clean(label), time.Since(start).Round(time.Millisecond))
	return err
}
