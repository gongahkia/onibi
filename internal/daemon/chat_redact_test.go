package daemon

import (
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestChatProviderOutputRedactionDefault(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte("$ env\nPASSWORD=\"super-secret-value\"\n"),
		[]byte("$ env\nPASSWORD=\"super-secret-value\"\n"),
		[]byte("$ env\nPASSWORD=\"super-secret-value\"\n"),
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
	out, err := d.handleProviderText(t.Context(), "", "env", 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "super-secret-value") || !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("out = %q", out)
	}
}

func TestChatApprovalPayloadRedactionDefault(t *testing.T) {
	a := &approval.Approval{
		ID:        "a1",
		SessionID: "s1",
		Agent:     "claude",
		Tool:      "Bash",
		InputJSON: `{"command":"curl -H 'Authorization: Bearer abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN' https://example.invalid"}`,
	}
	got := formatApproval(a)
	if strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("approval = %q", got)
	}
}

func TestChatUnredactedEscapeHatch(t *testing.T) {
	t.Setenv(ChatUnredactedEnv, "1")
	got := redactChatText(`PASSWORD="super-secret-value"`)
	if !strings.Contains(got, "super-secret-value") {
		t.Fatalf("got = %q", got)
	}
}
