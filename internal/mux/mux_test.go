package mux

import (
	"context"
	"reflect"
	"testing"
)

type runnerCall struct {
	name string
	args []string
}

type scriptedRunner struct {
	calls   []runnerCall
	outputs [][]byte
	errors  []error
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	index := len(r.calls) - 1
	var out []byte
	var err error
	if index < len(r.outputs) {
		out = r.outputs[index]
	}
	if index < len(r.errors) {
		err = r.errors[index]
	}
	return out, err
}

func TestZellijStartsHeadlessSessionAndTargetsPane(t *testing.T) {
	runner := &scriptedRunner{outputs: [][]byte{nil, []byte(`[{"id":0,"is_plugin":true,"exited":false},{"id":7,"is_plugin":false,"exited":false}]`)}}
	ctrl := newZellijWithRunner("zellij", "/tmp/zellij.kdl", runner)
	target, err := ctrl.Start(context.Background(), "onibi-1", StartOptions{CWD: "/workspace", Env: []string{"ONIBI_SESSION_ID=1"}, Command: "sh", Args: []string{"-i"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := target.String(), "onibi-1|terminal_7"; got != want {
		t.Fatalf("target=%q want=%q", got, want)
	}
	if got, want := runner.calls[0].args[:5], []string{"--config", "/tmp/zellij.kdl", "attach", "--create-background", "onibi-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start args=%#v want prefix=%#v", runner.calls[0].args, want)
	}
	if got, want := runner.calls[1].args, []string{"--config", "/tmp/zellij.kdl", "--session", "onibi-1", "action", "list-panes", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pane list args=%#v want=%#v", got, want)
	}
}

func TestZellijSendsTextAndCanonicalKey(t *testing.T) {
	runner := &scriptedRunner{}
	ctrl := newZellijWithRunner("zellij", "", runner)
	target := Target{Session: "onibi-1", Pane: "terminal_2"}
	if err := ctrl.SendText(context.Background(), target, "echo hi", true); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SendKey(context.Background(), target, "ctrl-c"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"--session", "onibi-1", "action", "paste", "--pane-id", "terminal_2", "echo hi"},
		{"--session", "onibi-1", "action", "send-keys", "--pane-id", "terminal_2", "Enter"},
		{"--session", "onibi-1", "action", "send-keys", "--pane-id", "terminal_2", "Ctrl c"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("calls=%#v", runner.calls)
	}
	for i := range want {
		if !reflect.DeepEqual(runner.calls[i].args, want[i]) {
			t.Fatalf("call %d args=%#v want=%#v", i, runner.calls[i].args, want[i])
		}
	}
}

func TestScreenCommandsUseConfiguredSession(t *testing.T) {
	runner := &scriptedRunner{}
	ctrl := newScreenWithRunner("screen", "/tmp/screenrc", runner)
	if _, err := ctrl.Start(context.Background(), "onibi-1", StartOptions{Command: "bash"}); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Resize(context.Background(), Target{Session: "onibi-1"}, 120, 40); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-c", "/tmp/screenrc", "-dmS", "onibi-1", "sh", "-lc", "exec 'bash'"},
		{"-c", "/tmp/screenrc", "-S", "onibi-1", "-p", "0", "-X", "width", "120", "40"},
	}
	for i := range want {
		if !reflect.DeepEqual(runner.calls[i].args, want[i]) {
			t.Fatalf("call %d args=%#v want=%#v", i, runner.calls[i].args, want[i])
		}
	}
}

func TestTargetRoundTrip(t *testing.T) {
	if got := ParseTarget("onibi-1|terminal_4"); got != (Target{Session: "onibi-1", Pane: "terminal_4"}) {
		t.Fatalf("target=%#v", got)
	}
	if got := ParseTarget("onibi-1"); got != (Target{Session: "onibi-1"}) {
		t.Fatalf("target=%#v", got)
	}
}
