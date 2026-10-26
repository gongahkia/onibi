package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/testutil/languages"
	"github.com/gongahkia/shack/internal/text"
)

func liveUIUntil(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case update := <-a.updates:
			update()
		case <-tick.C:
			a.syncLanguage()
		case <-deadline.C:
			t.Fatalf("live editor timed out: %s", a.Status)
		}
	}
}
func TestLiveLanguageEditor(t *testing.T) {
	if os.Getenv("SHACK_LIVE_LSP") != "1" {
		t.Skip("set SHACK_LIVE_LSP=1 for installed language-server workflows")
	}
	for _, tc := range languages.Cases() {
		t.Run(tc.Name, func(t *testing.T) {
			program := os.Getenv(tc.Env)
			if program == "" {
				program = tc.Program
			}
			resolved, e := exec.LookPath(program)
			if e != nil {
				t.Fatal(e)
			}
			root := t.TempDir()
			for path, data := range tc.Files {
				if e = os.WriteFile(filepath.Join(root, path), []byte(data), 0600); e != nil {
					t.Fatal(e)
				}
			}
			path := filepath.Join(root, tc.Path)
			if e = os.WriteFile(path, []byte(tc.Source), 0600); e != nil {
				t.Fatal(e)
			}
			a := testApp(t)
			a.Config.AutoComplete = false
			a.Config.Servers = map[string]config.Server{filepath.Ext(path): {Command: append([]string{resolved}, tc.Args...), Language: tc.Language}}
			if e = a.Open(path); e != nil {
				t.Fatal(e)
			}
			b := a.Current()
			a.Root = filepath.Dir(b.File.Path)
			t.Cleanup(func() {
				for _, s := range a.languages {
					if s.Client != nil {
						s.Client.Close()
					}
				}
			})
			a.syncLanguage()
			liveUIUntil(t, a, func() bool { return b.Synced })
			caret := func(offset int) { b.Doc.Selections = []text.Selection{{Anchor: offset, Head: offset}}; a.reveal() }
			request := func(kind string) { a.languageRequest(kind); liveUIUntil(t, a, func() bool { return !a.languageBusy }) }
			definition := strings.Index(tc.Source, "greet(")
			use := strings.LastIndex(tc.Source, "greet(")
			deadline := time.Now().Add(40 * time.Second)
			for time.Now().Before(deadline) {
				caret(use + 2)
				request("definition")
				if b.Doc.Selections[0].Head == definition {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if b.Doc.Selections[0].Head != definition {
				t.Fatal("definition did not navigate", a.Status, b.Doc.Selections)
			}
			// Navigation may be available before semantic analysis has loaded.
			deadline = time.Now().Add(40 * time.Second)
			hoverReady := false
			for time.Now().Before(deadline) {
				caret(use + 2)
				request("hover")
				if a.Current() != b {
					hoverReady = strings.Contains(a.Current().Doc.Text.String(), "greet")
					a.discardBuffer()
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !hoverReady {
				t.Fatal("semantic analysis did not become ready", a.Status)
			}
			// Editor preview, acceptance and grouped undo consume each server's actual
			// workspace edit representation rather than a handcrafted response.
			caret(use + 2)
			a.languageRequestWith("rename", map[string]any{"newName": "salute"})
			liveUIUntil(t, a, func() bool { return a.Preview != nil })
			if b.Doc.Text.String() != tc.Source {
				t.Fatal("rename wrote before review")
			}
			key(a, tcell.KeyEnter)
			liveUIUntil(t, a, func() bool { return a.Preview == nil })
			if strings.Count(b.Doc.Text.String(), "salute") != 2 {
				t.Fatal("rename apply", a.Status, b.Doc.Text.String())
			}
			a.undoWorkspace(false)
			if b.Doc.Text.String() != tc.Source {
				t.Fatal("rename group undo", a.Status)
			}
			if tc.Format {
				request("formatting")
				if a.Preview == nil {
					t.Fatal("missing format preview", a.Status)
				}
				key(a, tcell.KeyEnter)
				if b.Doc.Text.String() == tc.Source || !strings.Contains(b.Doc.Text.String(), "😀") {
					t.Fatal("format apply", a.Status)
				}
				key(a, tcell.KeyCtrlZ)
				if b.Doc.Text.String() != tc.Source {
					t.Fatal("format undo")
				}
			}
			a.apply([]text.Edit{{Start: use, End: use + 5, Text: "gre"}})
			caret(use + 3)
			deadline = time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				request("completion")
				if a.Dialog != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if a.Dialog == nil {
				t.Fatal("no completion dialog", a.Status)
			}
			var choose func()
			for _, item := range a.Dialog.Items {
				if strings.HasPrefix(item.Label, "greet") {
					choose = item.Run
					break
				}
			}
			if choose == nil {
				t.Fatal("greet not in completion dialog")
			}
			a.Dialog = nil
			choose()
			liveUIUntil(t, a, func() bool { return strings.Count(b.Doc.Text.String(), "greet") >= 2 })
			if !strings.Contains(b.Doc.Text.String(), "😀") {
				t.Fatal("completion lost Unicode")
			}
			a.snippet = nil
			key(a, tcell.KeyCtrlZ)
			key(a, tcell.KeyCtrlZ)
			if b.Doc.Text.String() != tc.Source {
				t.Fatal("completion undo", b.Doc.Text.String())
			}
			a.apply([]text.Edit{{Start: b.Doc.Text.Len(), End: b.Doc.Text.Len(), Text: tc.Bad}})
			a.syncLanguage()
			badLine := strings.Count(tc.Source, "\n")
			liveUIUntil(t, a, func() bool {
				if b.DiagnosticVersion != b.Doc.Version {
					return false
				}
				for _, d := range b.Diagnostics {
					if d.Range.Start.Line >= badLine {
						return true
					}
				}
				return false
			})
			a.diagnosticPicker()
			if a.Dialog == nil {
				t.Fatal("diagnostic picker missing")
			}
			a.Dialog = nil
			key(a, tcell.KeyCtrlZ)
			a.syncLanguage()
			liveUIUntil(t, a, func() bool {
				if b.DiagnosticVersion != b.Doc.Version {
					return false
				}
				for _, d := range b.Diagnostics {
					if d.Range.Start.Line >= badLine {
						return false
					}
				}
				return true
			})
			a.draw()
			a.Save()
			data, e := os.ReadFile(path)
			if e != nil || string(data) != tc.Source {
				t.Fatal("save after IDE undo changed source", e)
			}
			t.Log("actual server: definition navigation, reviewed rename/group undo, completion/undo, format/undo where supported, diagnostic picker/clear, Unicode save")
		})
	}
}
