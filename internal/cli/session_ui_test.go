package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAttachCommandUsesEachMultiplexersOwnAttach(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		row        sessionRow
		insideTmux bool
		args       []string
	}{
		{sessionRow{Multiplexer: "tmux", target: "onibi-abc"}, false, []string{"attach-session", "-t", "=onibi-abc"}},
		{sessionRow{Multiplexer: "tmux", target: "onibi-abc"}, true, []string{"switch-client", "-t", "=onibi-abc"}},
		{sessionRow{Multiplexer: "zellij", target: "onibi-abc|terminal_0"}, false, []string{"attach", "onibi-abc"}},
		{sessionRow{Multiplexer: "screen", target: "onibi-abc"}, false, []string{"-r", "onibi-abc"}},
	} {
		bin, args, err := attachCommand(tc.row, tc.insideTmux)
		if err != nil || bin != tc.row.Multiplexer || !reflect.DeepEqual(args, tc.args) {
			t.Fatalf("%s: bin=%q args=%v err=%v", tc.row.Multiplexer, bin, args, err)
		}
	}
	if _, _, err := attachCommand(sessionRow{Multiplexer: "codex"}, false); err == nil {
		t.Fatal("attached an App Server session")
	}
}

func TestAgoIsCompact(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{3 * time.Second: "just now", 42 * time.Second: "42s ago", 5 * time.Minute: "5m ago", 3 * time.Hour: "3h ago", 72 * time.Hour: "3d ago"} {
		if got := ago(now, now.Add(-d)); got != want {
			t.Fatalf("%s: got %q want %q", d, got, want)
		}
	}
	if ago(now, time.Time{}) != "-" {
		t.Fatal("zero time")
	}
}

func TestConfirmAcceptsOnlyYes(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "YES\n": true, "\n": false, "n\n": false, "": false, "yep\n": false} {
		var out bytes.Buffer
		if got, _ := confirm(strings.NewReader(input), &out, "End? "); got != want || out.String() != "End? " {
			t.Fatalf("%q: got %t prompt %q", input, got, out.String())
		}
	}
}
