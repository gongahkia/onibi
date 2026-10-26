// Package terminal hosts a real PTY behind an actor-owned terminal emulator.
// The editor consumes immutable snapshots and never waits for subprocess I/O.
package terminal

import (
	"context"
	"errors"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"image/color"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const maxInput = 1 << 20
const maxCells = 20000
const scrollbackBytes = 4 << 20

type Cell struct {
	Text                    string
	FG, BG                  int32
	Attrs, Underline, Width uint8
}
type Frame struct {
	Width, Height, CursorX, CursorY, Scroll, History int
	Hidden, Alt, Mouse, Exited                       bool
	Title, Exit                                      string
	Cells                                            []Cell
}

func (f *Frame) String() string {
	if f == nil {
		return ""
	}
	var out strings.Builder
	for y := 0; y < f.Height; y++ {
		var row strings.Builder
		for x := 0; x < f.Width; x++ {
			c := f.Cells[y*f.Width+x]
			if c.Width == 0 {
				continue
			}
			if c.Text == "" {
				row.WriteByte(' ')
			} else {
				row.WriteString(c.Text)
			}
		}
		out.WriteString(strings.TrimRight(row.String(), " "))
		out.WriteByte('\n')
	}
	return out.String()
}

type action struct {
	Kind   string
	Key    vt.KeyPressEvent
	Mouse  vt.Mouse
	Text   string
	Number int
}
type Session struct {
	cancel   context.CancelFunc
	commands chan action
	sizes    chan [2]int
	latest   atomic.Pointer[Frame]
	queued   atomic.Int64
	exited   atomic.Bool
	done     chan struct{}
}

func dimensions(w, h int) (int, int) {
	w = max(2, min(300, w))
	h = max(2, min(200, h))
	h = min(h, maxCells/w)
	return w, h
}
func (s *Session) Frame() *Frame         { return s.latest.Load() }
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) Close()                { s.cancel() }
func (s *Session) send(a action) error {
	if a.Kind != "scroll" && s.exited.Load() {
		return errors.New("terminal process exited")
	}
	n := int64(len(a.Text))
	if n > maxInput {
		return errors.New("terminal paste exceeds 1 MiB")
	}
	if s.queued.Add(n) > maxInput {
		s.queued.Add(-n)
		return errors.New("terminal input queue exceeds 1 MiB")
	}
	select {
	case <-s.done:
		s.queued.Add(-n)
		return errors.New("terminal closed")
	case s.commands <- a:
		return nil
	default:
		s.queued.Add(-n)
		return errors.New("terminal input queue is full; process may not be reading")
	}
}
func (s *Session) Key(key vt.KeyPressEvent) error { return s.send(action{Kind: "key", Key: key}) }
func (s *Session) Mouse(mouse vt.Mouse) error     { return s.send(action{Kind: "mouse", Mouse: mouse}) }
func (s *Session) Paste(value string) error       { return s.send(action{Kind: "paste", Text: value}) }
func (s *Session) Text(value string) error        { return s.send(action{Kind: "text", Text: value}) }
func (s *Session) Scroll(delta int) error         { return s.send(action{Kind: "scroll", Number: delta}) }
func (s *Session) Resize(w, h int) {
	w, h = dimensions(w, h)
	select {
	case <-s.sizes:
	default:
	}
	select {
	case s.sizes <- [2]int{w, h}:
	default:
	}
}
func Start(parent context.Context, argv []string, root string, w, h, history int, notify func()) (*Session, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, errors.New("no terminal command")
	}
	w, h = dimensions(w, h)
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "TERM", "COLORTERM", "TERM_PROGRAM", "LINES", "COLUMNS":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=shack")
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	if err != nil {
		cancel()
		return nil, err
	}
	raw, err := master.SyscallConn()
	if err == nil && runtime.GOOS == "linux" {
		// creack/pty's setup ioctls switch the master to blocking mode.
		// Restore nonblocking I/O so Close interrupts backpressured writes.
		var nonblockErr error
		err = raw.Control(func(fd uintptr) { nonblockErr = unix.SetNonblock(int(fd), true) })
		if err == nil {
			err = nonblockErr
		}
	}
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = master.Close()
		return nil, err
	}
	s := &Session{cancel: cancel, commands: make(chan action, 64), sizes: make(chan [2]int, 1), done: make(chan struct{})}
	emulator := vt.NewEmulator(w, h)
	pipe := emulator.InputPipe().(*io.PipeWriter)
	output := make(chan []byte, 4)
	input := make(chan []byte, 16)
	readDone := make(chan struct{})
	writerDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	processDone := make(chan error, 1)
	waitDone := make(chan struct{})
	go func() {
		defer close(output)
		buffer := make([]byte, 16384)
		for {
			n, e := master.Read(buffer)
			if n > 0 {
				data := append([]byte(nil), buffer[:n]...)
				select {
				case output <- data:
				case <-ctx.Done():
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	// Read's only emulator access is its immutable closed flag and its io.Pipe.
	// The actor closes that pipe, waits for this reader, then closes the emulator.
	go func() {
		defer close(readDone)
		buffer := make([]byte, 16384)
		for {
			n, e := emulator.Read(buffer)
			if n > 0 {
				data := append([]byte(nil), buffer[:n]...)
				select {
				case input <- data:
				case <-ctx.Done():
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-input:
				for len(data) > 0 {
					n, e := master.Write(data)
					if e != nil {
						return
					}
					data = data[n:]
				}
			}
		}
	}()
	go func() { e := cmd.Wait(); s.exited.Store(true); processDone <- e; close(waitDone) }()
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		foreground := 0
		_ = raw.Control(func(fd uintptr) { foreground, _ = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) })
		if !s.exited.Load() {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
		}
		if foreground > 0 {
			_ = syscall.Kill(-foreground, syscall.SIGHUP)
		}
		_ = pipe.CloseWithError(io.EOF)
		_ = master.Close()
		timer := time.NewTimer(200 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		if !s.exited.Load() {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		if foreground > 0 {
			_ = syscall.Kill(-foreground, syscall.SIGKILL)
		}
	}()
	go func() {
		defer close(s.done)
		defer func() {
			cancel()
			_ = pipe.CloseWithError(io.EOF)
			<-readDone
			<-writerDone
			<-shutdownDone
			<-waitDone
			_ = emulator.Close()
		}()
		scroll, title, exit := 0, "", ""
		maxWidth := w
		history = max(1, min(2000, history))
		modes := map[ansi.Mode]bool{}
		emulator.SetCallbacks(vt.Callbacks{Title: func(v string) {
			if len(v) > 256 {
				v = v[:256]
			}
			title = v
		}, EnableMode: func(mode ansi.Mode) { modes[mode] = true }, DisableMode: func(mode ansi.Mode) { delete(modes, mode) }})
		boundHistory := func() {
			limit := min(history, max(1, scrollbackBytes/(maxWidth*int(unsafe.Sizeof(uv.Cell{})))))
			rows := emulator.Scrollback().Lines()
			if len(rows) > limit {
				clear(rows[:len(rows)-limit])
			}
			emulator.SetScrollbackSize(limit)
		}
		boundHistory()
		snapshot := func() {
			scroll = min(max(0, scroll), emulator.ScrollbackLen())
			if emulator.IsAltScreen() {
				scroll = 0
			}
			cursor := emulator.CursorPosition()
			frame := &Frame{Width: emulator.Width(), Height: emulator.Height(), CursorX: cursor.X, CursorY: cursor.Y, Hidden: emulator.CursorHidden() || scroll > 0, Alt: emulator.IsAltScreen(), Title: title, Exit: exit, Exited: s.exited.Load(), Scroll: scroll, History: emulator.ScrollbackLen()}
			frame.Mouse = modes[ansi.ModeMouseX10] || modes[ansi.ModeMouseNormal] || modes[ansi.ModeMouseButtonEvent] || modes[ansi.ModeMouseAnyEvent]
			frame.Cells = make([]Cell, frame.Width*frame.Height)
			for y := 0; y < frame.Height; y++ {
				for x := 0; x < frame.Width; x++ {
					var cell *uv.Cell
					if scroll > y {
						cell = emulator.ScrollbackCellAt(x, frame.History-scroll+y)
					} else {
						cell = emulator.CellAt(x, y-scroll)
					}
					value := Cell{Text: " ", FG: -1, BG: -1, Width: 1}
					if cell != nil {
						value = Cell{cell.Content, rgb(cell.Style.Fg), rgb(cell.Style.Bg), cell.Style.Attrs, uint8(cell.Style.Underline), uint8(cell.Width)}
					}
					frame.Cells[y*frame.Width+x] = value
				}
			}
			s.latest.Store(frame)
			if notify != nil {
				notify()
			}
		}
		snapshot()
		var timer *time.Timer
		var frameC <-chan time.Time
		dirty := func() {
			if frameC == nil {
				if timer == nil {
					timer = time.NewTimer(time.Second / 30)
				} else {
					timer.Reset(time.Second / 30)
				}
				frameC = timer.C
			}
		}
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-output:
				if !ok {
					output = nil
				} else {
					_, _ = emulator.Write(data)
				}
				dirty()
			case e := <-processDone:
				s.exited.Store(true)
				if e != nil {
					exit = e.Error()
				} else {
					exit = "exited successfully"
				}
				processDone = nil
				dirty()
			case size := <-s.sizes:
				emulator.Resize(size[0], size[1])
				maxWidth = max(maxWidth, size[0])
				boundHistory()
				// Fd (including pty.Setsize) would restore blocking mode on Linux.
				_ = raw.Control(func(fd uintptr) {
					_ = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(size[0]), Row: uint16(size[1])})
				})
				dirty()
			case a := <-s.commands:
				switch a.Kind {
				case "key":
					scroll = 0
					emulator.SendKey(a.Key)
				case "mouse":
					emulator.SendMouse(a.Mouse)
				case "paste":
					scroll = 0
					emulator.Paste(a.Text)
				case "text":
					scroll = 0
					emulator.SendText(a.Text)
				case "scroll":
					if a.Number == 0 {
						scroll = 0
					} else {
						scroll += a.Number
					}
				}
				s.queued.Add(-int64(len(a.Text)))
				dirty()
			case <-frameC:
				frameC = nil
				snapshot()
			}
		}
	}()
	return s, nil
}
func rgb(c color.Color) int32 {
	if c == nil {
		return -1
	}
	r, g, b, _ := c.RGBA()
	return int32((r>>8)<<16 | (g>>8)<<8 | b>>8)
}
