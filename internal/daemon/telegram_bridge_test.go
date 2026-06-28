package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/tmux"
)

func TestSendSessionTextAndCaptureTmux(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte("$ ls\nREADME.md\nTODO.md\n"),
		[]byte("$ ls\nREADME.md\nTODO.md\n"),
		[]byte("$ ls\nREADME.md\nTODO.md\n"),
	}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	d := New(Options{})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	out, err := d.SendSessionTextAndCapture(context.Background(), "s1", "ls", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "README.md") || !strings.Contains(out, "TODO.md") {
		t.Fatalf("out = %q", out)
	}
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "ls") {
		t.Fatalf("missing text send: %#v", r.calls)
	}
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "Enter") {
		t.Fatalf("missing enter send: %#v", r.calls)
	}
}

func TestNewTelegramPairCodeSixDigits(t *testing.T) {
	code, err := NewTelegramPairCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code = %q", code)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("code = %q", code)
		}
	}
}
