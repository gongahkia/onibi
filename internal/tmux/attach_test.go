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

func TestCapturePreservesTerminalStyles(t *testing.T) {
	r := &fakeRunner{out: []byte("screen")}
	if _, err := NewWithRunner(r).Capture(t.Context(), "%1", 80); err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "capture-pane", "-e", "-p", "-t", "%1", "-S", "-80"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestHasSession(t *testing.T) {
	r := &fakeRunner{}
	c := NewWithRunner(r)
	live, err := c.HasSession(t.Context(), "onibi-a")
	if err != nil || !live {
		t.Fatalf("live=%t err=%v", live, err)
	}
	want := []string{"tmux", "has-session", "-t", "onibi-a"}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("calls=%#v", r.calls)
	}
}

func TestBellCounterUsesItsOwnHookSlot(t *testing.T) {
	r := &fakeRunner{}
	c := NewWithRunner(r)
	if err := c.InstallBellCounter(t.Context(), "onibi-a"); err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "set-hook", "-t", "onibi-a", "alert-bell[86]", `set-option -F -t onibi-a @onibi_bells "#{e|+:#{?@onibi_bells,#{@onibi_bells},0},1}"`}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("calls=%#v", r.calls)
	}
	if err := c.InstallBellCounter(t.Context(), "user session;kill-server"); err == nil {
		t.Fatal("accepted a session name that would change the hook command")
	}
}

func TestAlertsParsesCounterAndFlag(t *testing.T) {
	for _, tc := range []struct {
		out              string
		bells            int
		counted, flagged bool
	}{{"3 1\n", 3, true, true}, {"- 1\n", 0, false, true}, {"- 0\n", 0, false, false}} {
		bells, counted, flag, err := NewWithRunner(&fakeRunner{out: []byte(tc.out)}).Alerts(t.Context(), "onibi-a")
		if err != nil || bells != tc.bells || counted != tc.counted || flag != tc.flagged {
			t.Fatalf("out=%q bells=%d counted=%t flag=%t err=%v", tc.out, bells, counted, flag, err)
		}
	}
	if _, _, _, err := NewWithRunner(&fakeRunner{out: []byte("x 1")}).Alerts(t.Context(), "onibi-a"); err == nil {
		t.Fatal("accepted malformed counter")
	}
}

func TestHasSessionTreatsMissingTargetAsNotLive(t *testing.T) {
	live, err := NewWithRunner(&fakeRunner{out: []byte("can't find session: onibi-a"), err: errors.New("exit status 1")}).HasSession(t.Context(), "onibi-a")
	if err != nil || live {
		t.Fatalf("live=%t err=%v", live, err)
	}
}
