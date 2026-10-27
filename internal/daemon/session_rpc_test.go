package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
)

func addTmuxSession(t *testing.T, d *Daemon, id, name string) *Session {
	t.Helper()
	s := NewSession(id, name, "shell", 4096)
	s.TmuxTarget = "onibi-" + id
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionListReportsLiveSessionsOnly(t *testing.T) {
	d, _ := testDaemon(t)
	addTmuxSession(t, d, "aaaa1111", "build")
	ended := addTmuxSession(t, d, "bbbb2222", "old")
	ended.MarkEnded()
	resp, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionList})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].ID != "aaaa1111" || resp.Sessions[0].Name != "build" || resp.Sessions[0].Transport != "tmux" {
		t.Fatalf("sessions=%#v", resp.Sessions)
	}
}

func TestSessionCaptureRequiresExactIDAndReturnsPlainAndANSI(t *testing.T) {
	d, runner := testDaemon(t)
	addTmuxSession(t, d, "aaaa1111", "build")
	runner.capture = func(context.Context) ([]byte, error) { return []byte("\x1b[31mred\x1b[0m line\n"), nil }
	for _, id := range []string{"aaaa", "build", ""} {
		_, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionCapture, Session: id})
		if err == nil || !intake.IsRejected(err) {
			t.Fatalf("id=%q err=%v", id, err)
		}
	}
	resp, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionCapture, Session: "aaaa1111", Lines: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "red line" || resp.ANSI != "\x1b[31mred\x1b[0m line" {
		t.Fatalf("resp=%#v", resp)
	}
	calls := runner.snapshot()
	want := []string{"tmux", "capture-pane", "-e", "-p", "-t", "onibi-aaaa1111", "-S", "-2000"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestSessionKeyAndInputRejectBeforeDispatch(t *testing.T) {
	d, runner := testDaemon(t)
	addTmuxSession(t, d, "aaaa1111", "build")
	if _, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionKey, Session: "aaaa1111", Key: "hyper-x"}); !intake.IsRejected(err) {
		t.Fatalf("unsupported key err=%v", err)
	}
	if _, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionInput, Session: "aaaa1111", Exact: true}); !intake.IsRejected(err) {
		t.Fatalf("empty input err=%v", err)
	}
	if _, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionControl, Session: "build", Action: "kill", Exact: true}); !intake.IsRejected(err) {
		t.Fatalf("name-matched kill err=%v", err)
	}
	if len(runner.snapshot()) != 0 {
		t.Fatalf("rejected requests reached tmux: %#v", runner.snapshot())
	}
	if _, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionKey, Session: "aaaa1111", Key: "space"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionInput, Session: "aaaa1111", Text: "-n literal", Exact: true}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "send-keys", "-t", "onibi-aaaa1111", "Space"},
		{"tmux", "send-keys", "-t", "onibi-aaaa1111", "-l", "--", "-n literal"},
	}
	if calls := runner.snapshot(); !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%#v", calls)
	}
}
