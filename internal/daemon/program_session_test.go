package daemon

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/mux"
)

func TestAgentsAndProgramsLaunchAsGiven(t *testing.T) {
	for _, name := range []string{"CODEX", "CLAUDE", "PI", "HTOP"} {
		t.Setenv("ONIBI_"+name+"_BIN", "")
	}
	d, _ := testDaemon(t)
	for _, tc := range []struct {
		program   string
		args      []string
		bin, name string
	}{
		{"codex", []string{"--model", "x"}, "codex", "codex"},
		{"claude", []string{"--dangerously-skip-permissions", "--settings", "mine.json"}, "claude", "claude"},
		{"pi", nil, "pi", "pi"},
		{"htop", []string{"-d", "5"}, "htop", "htop"},
		{"/usr/local/bin/My Tool!", nil, "/usr/local/bin/My Tool!", "mytool"},
		{"/opt/+++", nil, "/opt/+++", "program"},
	} {
		bin, name, args, ok := d.agentCommand(tc.program, tc.args)
		if !ok || bin != tc.bin || name != tc.name || !reflect.DeepEqual(args, tc.args) {
			t.Fatalf("%s: bin=%q name=%q args=%v ok=%t", tc.program, bin, name, args, ok)
		}
	}
	for _, program := range []string{"", "  ", "-x", "bin/tool", "./tool", "a\nb"} {
		if _, _, _, ok := d.agentCommand(program, nil); ok {
			t.Fatalf("accepted %q", program)
		}
	}
	t.Setenv("ONIBI_CLAUDE_BIN", "/opt/claude")
	if bin, name, _, _ := d.agentCommand("claude", nil); bin != "/opt/claude" || name != "claude" {
		t.Fatalf("override bin=%q name=%q", bin, name)
	}
}

func TestClaudeStartsWithoutOnibiSettings(t *testing.T) {
	state, cwd := t.TempDir(), t.TempDir()
	d := New(Options{Paths: config.Paths{StateDir: state}})
	ctrl := &fakeMuxController{}
	oldChoose := chooseMultiplexer
	chooseMultiplexer = func(string, config.Multiplexer, string) (mux.Controller, string, error) {
		return ctrl, "zellij", nil
	}
	defer func() { chooseMultiplexer = oldChoose }()
	args := []string{"--permission-mode", "bypassPermissions"}
	if _, err := d.StartTerminalSession(t.Context(), "", "claude", "/bin/sh", args, cwd, "zellij"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ctrl.started.Args, args) {
		t.Fatalf("args=%v", ctrl.started.Args)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Fatalf("state dir gained %v", entries)
	}
}

func TestRestoreEndsAppServerSessionsInsteadOfResuming(t *testing.T) {
	d, _ := testDaemon(t)
	if err := d.DB.SessionUpsertStart(t.Context(), "aaaa1111", "old", "codex", t.TempDir(), "codex app-server", "codex", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	d.restoreSessions(t.Context())
	if _, err := d.Registry.Get("aaaa1111"); err == nil {
		t.Fatal("App Server session was restored")
	}
	active, err := d.DB.SessionsActive(t.Context())
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%#v err=%v", active, err)
	}
}
