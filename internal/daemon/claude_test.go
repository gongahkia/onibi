package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
)

func TestClaudeHooksContainApprovalAndCompletionHandlers(t *testing.T) {
	settings := claudeHookSettings("/tmp/onibi-notify", 3*time.Minute)
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PermissionRequest", "PreToolUse", "AskUserQuestion", "Stop", "StopFailure", "--agent claude", "approval_request", "question_request", "agent_lifecycle", "claude-json", "claude-question-json"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestClaudeHooksWriteOwnedSettings(t *testing.T) {
	d := New(Options{Paths: config.Paths{StateDir: t.TempDir()}})
	path, err := d.writeClaudeHooks("/tmp/onibi-notify")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(d.Paths.StateDir, "claude-hooks.json") {
		t.Fatalf("path=%q", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}

func TestClaudeArgsRejectsHookBypass(t *testing.T) {
	d := New(Options{})
	for _, args := range [][]string{{"--bare"}, {"--settings", "/tmp/settings.json"}, {"--settings=/tmp/settings.json"}, {"--dangerously-skip-permissions"}, {"--dangerously-skip-permissions=true"}, {"--allow-dangerously-skip-permissions"}, {"--permission-mode=bypassPermissions"}, {"--permission-mode", "dontAsk"}, {"--permission-mode", "--dangerously-skip-permissions"}} {
		if _, err := d.claudeArgs(args); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
}

func TestClaudeArgsAllowsPromptTextAfterTerminator(t *testing.T) {
	if err := validateClaudeArgs([]string{"--", "--dangerously-skip-permissions"}); err != nil {
		t.Fatal(err)
	}
}
