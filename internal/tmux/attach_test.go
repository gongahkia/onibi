package tmux

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls [][]string
	out   []byte
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.out, f.err
}

func TestSendTextUsesLiteralThenEnter(t *testing.T) {
	r := &fakeRunner{}
	if err := NewWithRunner(r).SendText(t.Context(), "%1", "one\ntwo", true); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"tmux", "send-keys", "-t", "%1", "-l", "--", "one\ntwo"}, {"tmux", "send-keys", "-t", "%1", "Enter"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestStartSessionBuildsTmuxCommand(t *testing.T) {
	r := &fakeRunner{}
	err := NewWithRunner(r).StartSession(t.Context(), "onibi-abc", StartOptions{WindowName: "shell", CWD: "/tmp/repo", Env: []string{"ONIBI_SESSION_ID=abc"}, Command: "/bin/echo", Args: []string{"hello world"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "new-session", "-d", "-s", "onibi-abc", "-n", "shell", "-c", "/tmp/repo", "-e", "ONIBI_SESSION_ID=abc", "sh", "-lc", "exec '/bin/echo' 'hello world'"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestListSessionsParsesUniqueNames(t *testing.T) {
	r := &fakeRunner{out: []byte("onibi-a\nonibi-a\nother\n")}
	sessions, err := NewWithRunner(r).ListSessions(t.Context())
	if err != nil || len(sessions) != 2 || sessions[1].Name != "other" {
		t.Fatalf("sessions=%#v err=%v", sessions, err)
	}
}

func TestRunHintsWhenTmuxMissing(t *testing.T) {
	_, err := NewWithRunner(&fakeRunner{err: exec.ErrNotFound}).Capture(t.Context(), "%1", 50)
	if err == nil || !strings.Contains(err.Error(), "set ONIBI_TMUX_BIN") {
		t.Fatalf("err = %v", err)
	}
}

func TestCaptureWrapsTmuxError(t *testing.T) {
	_, err := NewWithRunner(&fakeRunner{out: []byte("missing"), err: errors.New("exit status 1")}).Capture(t.Context(), "%missing", 50)
	if err == nil {
		t.Fatal("expected error")
	}
}
