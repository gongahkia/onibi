package termui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rivo/uniseg"
)

// Live owns a single activity row underneath append-only log records. All
// writes and redraws share the UI lock; no alternate screen or stdin handling.
// Its owner must Close before returning to the shell.
type Live struct {
	ui                       *UI
	done                     chan struct{}
	wg                       sync.WaitGroup
	started, closed, visible bool
	startedAt                time.Time
	label, pending           string
	cells                    int
	err                      error
	records                  atomic.Uint64
}

func NewLive(ui *UI) *Live      { return &Live{ui: ui, done: make(chan struct{}), startedAt: time.Now()} }
func (l *Live) Records() uint64 { return l.records.Load() }
func (l *Live) Start(status func(time.Duration) string) {
	u := l.ui
	u.mu.Lock()
	if l.started || l.closed || !u.Interactive {
		u.mu.Unlock()
		return
	}
	l.started = true
	l.wg.Add(1)
	u.mu.Unlock()
	go func() {
		defer l.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			label := Clean(status(time.Since(l.startedAt)))
			u.mu.Lock()
			l.label = label
			if !l.closed {
				l.redrawLocked()
			}
			u.mu.Unlock()
			select {
			case <-l.done:
				return
			case <-ticker.C:
			}
		}
	}()
}
func (l *Live) clearLocked() {
	if !l.visible {
		return
	}
	// Ghostty reflows the one unfinished row when its viewport shrinks.
	rows := (l.cells + max(1, l.ui.columns()) - 1) / max(1, l.ui.columns())
	_, l.err = io.WriteString(l.ui.Out, "\r\x1b[2K")
	for i := 1; i < rows && l.err == nil; i++ {
		_, l.err = io.WriteString(l.ui.Out, "\x1b[1A\r\x1b[2K")
	}
	l.visible = false
}
func (l *Live) redrawLocked() {
	if l.err != nil {
		return
	}
	l.clearLocked()
	if l.label == "" || l.closed || l.err != nil {
		return
	}
	rattle := WaveRows
	if l.ui.ASCII {
		rattle = SimpleDots
	}
	line := clip("  "+rattle.Frame(time.Since(l.startedAt))+"  "+l.label, l.ui.columns()-4)
	l.cells = uniseg.StringWidth(line)
	if !l.ui.NoStyle {
		line = "\x1b[2m" + line + "\x1b[0m"
	}
	_, l.err = io.WriteString(l.ui.Out, line)
	l.visible = l.err == nil
}
func (l *Live) eventLocked(line string) {
	indent := "                  "
	if l.ui.columns() < 60 {
		indent = "    "
	}
	// A timestamp and severity anchor the first row; continuation metadata is
	// indented, never sliced mid-cell or allowed to overwrite the activity row.
	first := "  "
	width := max(1, l.ui.columns()-1-uniseg.StringWidth(first))
	parts := wrapCells(line, width)
	if len(parts) > 0 {
		_, l.err = fmt.Fprintln(l.ui.Out, first+parts[0])
	}
	if len(parts) > 1 {
		rest := strings.Join(parts[1:], " ")
		for _, row := range wrapCells(rest, max(1, l.ui.columns()-1-len(indent))) {
			if l.err != nil {
				return
			}
			_, l.err = fmt.Fprintln(l.ui.Out, indent+row)
		}
	}
	l.records.Add(1)
}
func (l *Live) Write(p []byte) (int, error) {
	u := l.ui
	u.mu.Lock()
	defer u.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	if l.err != nil {
		return 0, l.err
	}
	if !u.Interactive {
		n, err := u.Out.Write(p)
		l.records.Add(uint64(strings.Count(string(p[:n]), "\n")))
		return n, err
	}
	l.clearLocked()
	l.pending += string(p)
	for {
		i := strings.IndexByte(l.pending, '\n')
		if i < 0 {
			break
		}
		l.eventLocked(l.pending[:i])
		l.pending = l.pending[i+1:]
		if l.err != nil {
			return 0, l.err
		}
	}
	// Current telemetry is small. Bound an accidental unterminated write too.
	if len(l.pending) > 64<<10 {
		l.eventLocked(l.pending[:64<<10])
		l.pending = l.pending[64<<10:]
	}
	l.redrawLocked()
	if l.err != nil {
		return 0, l.err
	}
	return len(p), nil
}
func (l *Live) Close() error {
	u := l.ui
	u.mu.Lock()
	if l.closed {
		err := l.err
		u.mu.Unlock()
		return err
	}
	l.closed = true
	close(l.done)
	u.mu.Unlock()
	l.wg.Wait()
	u.mu.Lock()
	defer u.mu.Unlock()
	l.clearLocked()
	if l.pending != "" {
		l.eventLocked(l.pending)
		l.pending = ""
	}
	return l.err
}
