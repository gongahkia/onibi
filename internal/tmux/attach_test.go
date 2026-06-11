package tmux

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeRunner struct {
	calls [][]string
	out   []byte
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
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

func TestCaptureWrapsTmuxError(t *testing.T) {
	r := &fakeRunner{out: []byte("can't find pane"), err: errors.New("exit status 1")}
	c := NewWithRunner(r)
	_, err := c.Capture(context.Background(), "%missing", 50)
	if err == nil {
		t.Fatal("expected error")
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
