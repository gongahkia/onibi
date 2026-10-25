package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/dap"
	"github.com/gongahkia/shack/internal/text"
)

func debugUntil(t *testing.T, a *App, predicate func() bool) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !predicate() {
		select {
		case fn := <-a.updates:
			fn()
		case <-tick.C:
		case <-deadline.C:
			var output string
			if a.debug != nil && a.debug.Client != nil {
				output = a.debug.Client.Output()
			}
			t.Fatalf("debugger UI timeout: %s\n%s", a.Status, output)
		}
		a.draw()
	}
}
func TestBreakpointRebaseMouseAndLateVerification(t *testing.T) {
	a := testApp(t)
	a.Root = t.TempDir()
	path := filepath.Join(a.Root, "main.py")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0600)
	a.Open(path)
	b := a.Current()
	path = b.File.Path
	b.Doc.Selections = []text.Selection{{Anchor: 4, Head: 4}}
	key(a, tcell.KeyF9)
	point := a.breakpoints[path][2]
	if point == nil {
		t.Fatal("F9 did not set breakpoint")
	}
	old := point.Spec
	b.Doc.Selections = []text.Selection{{}}
	a.insert("prefix\n")
	if a.breakpoints[path][3] != point {
		t.Fatal("breakpoint did not rebase")
	}
	result, _ := json.Marshal(map[string]any{"breakpoints": []dap.Breakpoint{{Line: 2, Verified: true}}})
	a.acceptBreakpoints(path, []dap.SourceBreakpoint{old}, result)
	if point.Verified {
		t.Fatal("stale verification moved breakpoint")
	}
	key(a, tcell.KeyCtrlZ)
	if a.breakpoints[path][2] != point {
		t.Fatal("undo lost breakpoint")
	}
	a.draw()
	click(a, 2, 2, tcell.Button1)
	if len(a.breakpoints[path]) != 0 {
		t.Fatal("gutter click did not remove breakpoint")
	}
}
func TestDebugProfileExpansion(t *testing.T) {
	value, err := expandDebug(map[string]any{"program": "${file}", "cwd": "${workspaceFolder}", "args": []any{"${fileBasename}", "literal"}}, "/project", "/project/main.py", 0)
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	if m["program"] != "/project/main.py" || m["cwd"] != "/project" || m["args"].([]any)[0] != "main.py" {
		t.Fatal(m)
	}
	if _, err = expandDebug("${file}", "/project", "", 0); err == nil {
		t.Fatal("empty current file accepted")
	}
}
func TestLiveDebugEditorWorkflow(t *testing.T) {
	python := os.Getenv("SHACK_DEBUGPY_PYTHON")
	if python == "" {
		t.Skip("set SHACK_DEBUGPY_PYTHON for live debug UI checks")
	}
	a := testApp(t)
	a.Root = t.TempDir()
	t.Cleanup(a.closeDebugger)
	path := filepath.Join(a.Root, "main.py")
	os.WriteFile(path, []byte("def add(a, b):\n    value = a + b\n    return value\nanswer = add(2, 3)\nprint('UI_RESULT', answer)\n"), 0600)
	a.Open(path)
	b := a.Current()
	path = b.File.Path
	b.Doc.Selections = []text.Selection{{Anchor: b.Doc.Text.LineStart(1), Head: b.Doc.Text.LineStart(1)}}
	key(a, tcell.KeyF9)
	profile := config.DebugProfile{Command: []string{python, "-m", "debugpy.adapter"}, Request: "launch", Arguments: map[string]any{"program": "${file}", "cwd": "${workspaceFolder}", "python": []any{python}, "console": "internalConsole", "redirectOutput": true, "subProcess": false}}
	a.prepareDebug("test", profile)
	debugUntil(t, a, func() bool { return a.debug != nil && a.debug.Ready && a.debug.Stopped })

	a.debugScopes()
	debugUntil(t, a, func() bool { return a.Dialog != nil && strings.HasPrefix(a.Dialog.Title, "Debug scopes") })
	if a.Current() != b || b.Doc.Text.LineAt(b.Doc.Selections[0].Head) != 1 {
		t.Fatal("stopped location did not open source")
	}
	var locals func()
	for _, item := range a.Dialog.Items {
		if strings.Contains(item.Label, "Locals") {
			locals = item.Run
		}
	}
	if locals == nil {
		t.Fatal("locals scope missing")
	}
	a.Dialog = nil
	locals()
	debugUntil(t, a, func() bool { return a.Dialog != nil && strings.HasPrefix(a.Dialog.Title, "Debug variables") })
	found := false
	for _, item := range a.Dialog.Items {
		found = found || strings.Contains(item.Label, "a = 2")
	}
	if !found {
		t.Fatal("local variable not shown", a.Dialog.Items)
	}
	a.Dialog = nil
	key(a, tcell.KeyF10)
	debugUntil(t, a, func() bool { return a.debug.Stopped && a.debug.Frame.Line == 3 })
	a.debugStack()
	debugUntil(t, a, func() bool { return a.Dialog != nil && a.Dialog.Title == "Debug stack frames" })
	if len(a.Dialog.Items) < 2 {
		t.Fatal("caller frame missing")
	}
	a.Dialog = nil
	a.Handle(tcell.NewEventKey(tcell.KeyF11, 0, tcell.ModShift))
	debugUntil(t, a, func() bool { return a.debug.Stopped && a.debug.Frame.Name == "<module>" })
	key(a, tcell.KeyF5)
	debugUntil(t, a, func() bool { return a.debug.Ended })
	a.debugOutput()
	if !strings.Contains(a.Current().Doc.Text.String(), "UI_RESULT 5") {
		t.Fatal("debug output missing", a.Current().Doc.Text.String())
	}
}
