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
	calls   [][]string
	out     []byte
	err     error
	results []fakeResult
}

type fakeResult struct {
	out []byte
	err error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if len(f.results) > 0 {
		res := f.results[0]
		f.results = f.results[1:]
		return res.out, res.err
	}
	return f.out, f.err
}

func TestSendTextUsesLiteralThenEnter(t *testing.T) {
	r := &fakeRunner{}
	c := NewWithRunner(r)
	if err := c.SendText(context.Background(), "%1", "hello", true); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "send-keys", "-t", "%1", "-l", "--", "hello"},
		{"tmux", "send-keys", "-t", "%1", "Enter"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestSendTextMultilineVerifiesCaptureSuccess(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{},
		{},
		{out: []byte("one\nfinal line\n")},
	}}
	c := NewWithRunner(r)
	if err := c.SendText(context.Background(), "%1", "one\nfinal line", true); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "send-keys", "-t", "%1", "-l", "--", "one\nfinal line"},
		{"tmux", "send-keys", "-t", "%1", "Enter"},
		{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-50"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestSendTextMultilineIgnoresCaptureError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{},
		{},
		{err: errors.New("capture failed")},
	}}
	c := NewWithRunner(r)
	if err := c.SendText(context.Background(), "%1", "one\ntwo", true); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "send-keys", "-t", "%1", "-l", "--", "one\ntwo"},
		{"tmux", "send-keys", "-t", "%1", "Enter"},
		{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-50"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestSendTextMultilineRetriesWhenFinalLineMissing(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{},
		{},
		{out: []byte("one\n")},
		{},
	}}
	c := NewWithRunner(r)
	if err := c.SendText(context.Background(), "%1", "one\ntwo", true); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "send-keys", "-t", "%1", "-l", "--", "one\ntwo"},
		{"tmux", "send-keys", "-t", "%1", "Enter"},
		{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-50"},
		{"tmux", "send-keys", "-t", "%1", "Enter"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestCaptureWrapsTmuxError(t *testing.T) {
	r := &fakeRunner{out: []byte("can't find pane"), err: errors.New("exit status 1")}
	c := NewWithRunner(r)
	_, err := c.Capture(context.Background(), "%missing", 50)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCapturePreservesEscapeSequences(t *testing.T) {
	r := &fakeRunner{out: []byte("\x1b[31mred\x1b[0m\n")}
	c := NewWithRunner(r)
	got, err := c.Capture(context.Background(), "%1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got != "\x1b[31mred\x1b[0m" {
		t.Fatalf("capture = %q", got)
	}
	want := [][]string{{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-50"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestStartSessionBuildsTmuxCommand(t *testing.T) {
	r := &fakeRunner{}
	c := NewWithRunner(r)
	if err := c.StartSession(context.Background(), "onibi-abc", StartOptions{
		WindowName: "codex",
		CWD:        "/tmp/repo",
		Env:        []string{"ONIBI_SESSION_ID=abc"},
		Command:    "/bin/echo",
		Args:       []string{"hello world"},
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "new-session", "-d", "-s", "onibi-abc", "-n", "codex", "-c", "/tmp/repo", "-e", "ONIBI_SESSION_ID=abc", "sh", "-lc", "exec '/bin/echo' 'hello world'"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestCopyModePageKeys(t *testing.T) {
	r := &fakeRunner{}
	c := NewWithRunner(r)
	if err := c.CopyModePageUp(context.Background(), "onibi-abc"); err != nil {
		t.Fatal(err)
	}
	if err := c.CopyModePageDown(context.Background(), "onibi-abc"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "copy-mode", "-u", "-t", "onibi-abc"},
		{"tmux", "send-keys", "-X", "-t", "onibi-abc", "page-down"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestListPanesParsesRows(t *testing.T) {
	r := &fakeRunner{out: []byte("%1\ts\tw\tclaude\ttitle\n")}
	c := NewWithRunner(r)
	panes, err := c.ListPanes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 || panes[0].ID != "%1" || panes[0].Command != "claude" {
		t.Fatalf("panes = %#v", panes)
	}
}

func TestDefaultBinUsesEnvOverride(t *testing.T) {
	t.Setenv("ONIBI_TMUX_BIN", "/tmp/onibi-tmux")
	if got := DefaultBin(); got != "/tmp/onibi-tmux" {
		t.Fatalf("DefaultBin = %q", got)
	}
}

func TestRunHintsWhenTmuxMissing(t *testing.T) {
	r := &fakeRunner{err: exec.ErrNotFound}
	c := NewWithRunner(r)
	_, err := c.Capture(context.Background(), "%1", 50)
	if err == nil || !strings.Contains(err.Error(), "set ONIBI_TMUX_BIN") {
		t.Fatalf("err = %v", err)
	}
}
