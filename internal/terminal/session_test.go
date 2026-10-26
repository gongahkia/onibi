package terminal

import (
	"context"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"strings"
	"testing"
	"time"
)

func waitFrame(t *testing.T, s *Session, predicate func(*Frame) bool) *Frame {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("terminal timeout: %q", s.Frame().String())
		case <-tick.C:
			if f := s.Frame(); f != nil && predicate(f) {
				return f
			}
		}
	}
}
func start(t *testing.T, script string) *Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, []string{"/bin/sh", "-c", script}, t.TempDir(), 60, 15, 40, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		s.Close()
		select {
		case <-s.Done():
		case <-time.After(3 * time.Second):
			t.Error("terminal cleanup blocked")
		}
	})
	return s
}
func TestPTYInputResizeAndColor(t *testing.T) {
	s := start(t, "stty -echo; printf '\033[31mREADY\033[0m\\n'; read value; printf 'INPUT:%s\\n' \"$value\"; stty size")
	f := waitFrame(t, s, func(f *Frame) bool { return strings.Contains(f.String(), "READY") })
	if f.Cells[0].FG == -1 {
		t.Fatal("ANSI color lost")
	}
	s.Resize(70, 20)
	waitFrame(t, s, func(f *Frame) bool { return f.Width == 70 && f.Height == 20 })
	if err := s.Text("hello\r"); err != nil {
		t.Fatal(err)
	}
	f = waitFrame(t, s, func(f *Frame) bool { return f.Exited && strings.Contains(f.String(), "INPUT:hello") })
	if !strings.Contains(f.String(), "20 70") {
		t.Fatal("PTY size differs from emulation", f.String())
	}
}
func TestAlternateScreenScrollbackAndBounds(t *testing.T) {
	s := start(t, "stty -echo; i=0; while [ $i -lt 150 ]; do printf 'line %s\\n' $i; i=$((i+1)); done; printf '\033[?1049h\033[?25lALT'; read value; printf '\033[?1049lDONE\\n'; read value")
	f := waitFrame(t, s, func(f *Frame) bool { return f.Alt && strings.Contains(f.String(), "ALT") })
	if !f.Hidden {
		t.Fatal("alternate screen cursor hidden state")
	}
	s.Text("\r")
	f = waitFrame(t, s, func(f *Frame) bool { return !f.Alt && strings.Contains(f.String(), "DONE") })
	if f.History > 40 {
		t.Fatal("scrollback not bounded", f.History)
	}
	s.Scroll(10)
	f = waitFrame(t, s, func(f *Frame) bool { return f.Scroll == 10 })
	if !strings.Contains(f.String(), "line ") {
		t.Fatal("no scrollback text", f.String())
	}
	if err := s.Paste(strings.Repeat("x", maxInput+1)); err == nil || s.queued.Load() != 0 {
		t.Fatal("oversized input accepted or accounting corrupted")
	}
}
func TestCancelUnderContinuousOutput(t *testing.T) {
	s := start(t, "while :; do printf 'continuous output\\n'; done")
	waitFrame(t, s, func(f *Frame) bool { return strings.Contains(f.String(), "continuous") })
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("could not stop output flood")
	}
}

func TestPTYBracketedPasteApplicationKeysAndMouse(t *testing.T) {
	expected := "\x1b[200~你好\n\x1b[201~\x1bOA\x1b[<0;2;3M"
	script := fmt.Sprintf("stty raw -echo; printf '\033[?2004h\033[?1h\033[?1000h\033[?1006hREADY'; dd bs=1 count=%d 2>/dev/null | od -An -tx1; printf '\\nDONE\\n'", len(expected))
	s := start(t, script)
	waitFrame(t, s, func(f *Frame) bool { return f.Mouse && strings.Contains(f.String(), "READY") })
	if err := s.Paste("你好\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.Key(vt.KeyPressEvent{Code: vt.KeyUp}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mouse(vt.MouseClick(uv.Mouse{X: 1, Y: 2, Button: vt.MouseLeft})); err != nil {
		t.Fatal(err)
	}
	f := waitFrame(t, s, func(f *Frame) bool { return f.Exited && strings.Contains(f.String(), "DONE") })
	got := strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(f.String(), "READY", ""), "DONE", "")), "")
	if want := fmt.Sprintf("%x", expected); got != want {
		t.Fatalf("input bytes %s, want %s", got, want)
	}
}
func TestCancelWhenChildStopsReading(t *testing.T) {
	for _, name := range []string{"initial", "resized"} {
		t.Run(name, func(t *testing.T) {
			s := start(t, "trap '' HUP; stty raw -echo; printf READY; sleep 60")
			waitFrame(t, s, func(f *Frame) bool { return strings.Contains(f.String(), "READY") })
			if name == "resized" {
				s.Resize(70, 20)
				waitFrame(t, s, func(f *Frame) bool { return f.Width == 70 && f.Height == 20 })
			}
			if err := s.Text(strings.Repeat("x", maxInput)); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			s.Close()
			select {
			case <-s.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("input backpressure prevented shutdown")
			}
		})
	}
}
