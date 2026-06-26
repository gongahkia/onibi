package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestStartManagedTmuxSessionStoresTmuxMetadata(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{nil, []byte("ready\n")}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	d := New(Options{Paths: config.Paths{Socket: "/tmp/onibi.sock"}})
	s, err := d.StartManagedTmuxSession(context.Background(), "zsh", "shell", "/bin/zsh", []string{"-il"}, "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" || s.Host != nil || s.CWD != "/tmp/repo" {
		t.Fatalf("session = %#v", s)
	}
	if len(r.calls) < 2 || !containsStringArg(r.calls[0], "ONIBI_SESSION_ID="+s.ID) || !containsStringArg(r.calls[0], "ONIBI_SOCK=/tmp/onibi.sock") {
		t.Fatalf("tmux calls = %#v", r.calls)
	}
}

func TestHandoverMacClosesWebAttachAndReturnsAttachHint(t *testing.T) {
	d := New(Options{TerminalDefault: "none"})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	closed := false
	d.webAttachHosts[s.ID] = pty.NewVirtualHost(nil, func() error {
		closed = true
		return nil
	}, nil)

	msg, err := d.HandoverSession(context.Background(), s.ID, "mac")
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("web attach host was not closed")
	}
	if !strings.Contains(msg, "tmux attach-session -t onibi-s1") {
		t.Fatalf("message = %q", msg)
	}
}

type tmuxRunner struct {
	calls   [][]string
	results [][]byte
}

func (r *tmuxRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(r.results) == 0 {
		return nil, nil
	}
	out := r.results[0]
	r.results = r.results[1:]
	return out, nil
}

func containsStringArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
