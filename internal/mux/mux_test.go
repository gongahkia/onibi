package mux

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
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
		{"--session", "onibi-1", "action", "write-chars", "--pane-id", "terminal_2", "--", "echo hi"},
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

func TestNumericZellijTargetUsesCanonicalPaneID(t *testing.T) {
	if got := ParseTarget("work|0"); got.String() != "work|terminal_0" {
		t.Fatalf("target=%#v", got)
	}
}

func TestZellijLivenessIgnoresPluginWithSameNumericID(t *testing.T) {
	for _, exited := range []string{
		`[{"id":0,"is_plugin":true,"exited":false},{"id":0,"is_plugin":false,"exited":true}]`,
		`[{"id":0,"is_plugin":true,"exited":false}]`,
	} {
		ctrl := newZellijWithRunner("zellij", "", &scriptedRunner{outputs: [][]byte{[]byte(exited)}})
		live, err := ctrl.Has(t.Context(), Target{Session: "work", Pane: "terminal_0"})
		if err != nil || live {
			t.Fatalf("live=%t err=%v for %s", live, err, exited)
		}
	}
}

func TestZellijLiteralInputAndModifierLetters(t *testing.T) {
	runner := &scriptedRunner{}
	ctrl := newZellijWithRunner("zellij", "", runner)
	target := Target{Session: "work", Pane: "terminal_0"}
	if err := ctrl.SendText(t.Context(), target, "--help", false); err != nil {
		t.Fatal(err)
	}
	args := runner.calls[0].args
	if !reflect.DeepEqual(args[len(args)-2:], []string{"--", "--help"}) {
		t.Fatalf("literal text parsed as an option: %#v", args)
	}
	for input, want := range map[string]string{"ctrl-a": "Ctrl a", "meta-q": "Alt q"} {
		if got := zellijKey(input); got != want {
			t.Fatalf("%s = %q, want %q", input, got, want)
		}
	}
}

func TestMissingExecutableOrConfigDoesNotEndSession(t *testing.T) {
	for _, factory := range []func(string, string, Runner) Controller{newScreenWithRunner, newZellijWithRunner} {
		for _, failure := range []error{exec.ErrNotFound, errors.New("configuration file not found")} {
			ctrl := factory("missing", "", &scriptedRunner{errors: []error{failure}})
			_, err := ctrl.Has(t.Context(), Target{Session: "work", Pane: "terminal_0"})
			if err == nil || IsSessionGone(err) {
				t.Fatalf("%s misclassified infrastructure failure: %v", ctrl.Kind(), err)
			}
		}
	}
}

func TestFailedInputDoesNotExposeArguments(t *testing.T) {
	for _, factory := range []func(string, string, Runner) Controller{newTmuxWithRunner, newScreenWithRunner, newZellijWithRunner} {
		runner := &scriptedRunner{errors: []error{errors.New("exit status 1")}, outputs: [][]byte{[]byte("permission denied")}}
		ctrl := factory("mux", "", runner)
		err := ctrl.SendText(t.Context(), Target{Session: "work", Pane: "terminal_0"}, "SECRET_INPUT_123", true)
		if err == nil || strings.Contains(err.Error(), "SECRET_INPUT_123") || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("%s returned unsafe or unhelpful error: %v", ctrl.Kind(), err)
		}
	}
}
