package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/shack/internal/testutil/languages"
	"github.com/gongahkia/shack/internal/text"
)

// Opt-in integration tests exercise installed servers through the same client
// used by the editor. No server/runtime becomes a release dependency.
func TestLiveLanguageServers(t *testing.T) {
	if os.Getenv("SHACK_LIVE_LSP") != "1" {
		t.Skip("set SHACK_LIVE_LSP=1 and install the documented development servers")
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
			root, e = filepath.EvalSymlinks(root)
			if e != nil {
				t.Fatal(e)
			}
			for path, data := range tc.Files {
				if e = os.WriteFile(filepath.Join(root, path), []byte(data), 0600); e != nil {
					t.Fatal(e)
				}
			}
			path := filepath.Join(root, tc.Path)
			if e = os.WriteFile(path, []byte(tc.Source), 0600); e != nil {
				t.Fatal(e)
			}
			uri := URI(path)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			var mu sync.Mutex
			var diagnostics []Diagnostic
			var diagVersion *uint64
			c, e := Start(ctx, append([]string{resolved}, tc.Args...), root, func(n Notification) {
				if n.Method == "textDocument/publishDiagnostics" {
					var p struct {
						URI         string       `json:"uri"`
						Version     *uint64      `json:"version"`
						Diagnostics []Diagnostic `json:"diagnostics"`
					}
					if json.Unmarshal(n.Params, &p) == nil && p.URI == uri {
						mu.Lock()
						diagnostics = p.Diagnostics
						diagVersion = p.Version
						mu.Unlock()
					}
				}
			})
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			if e = c.Sync(uri, tc.Language, tc.Source, 1); e != nil {
				t.Fatal(e)
			}
			request := func(method string, params any) json.RawMessage {
				t.Helper()
				call, stop := context.WithTimeout(ctx, 15*time.Second)
				defer stop()
				data, e := c.Request(call, method, params)
				if e != nil {
					t.Fatalf("%s: %v", method, e)
				}
				return data
			}
			at := PositionAt(tc.Source, strings.LastIndex(tc.Source, "greet(")+2)
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": at}
			// Workspace loading is asynchronous, especially in rust-analyzer. Poll an
			// actual definition result instead of sleeping for an assumed startup time.
			var definition json.RawMessage
			deadline := time.Now().Add(40 * time.Second)
			for time.Now().Before(deadline) {
				call, stop := context.WithTimeout(ctx, 5*time.Second)
				definition, e = c.Request(call, "textDocument/definition", params)
				stop()
				if e == nil && len(liveLocations(definition)) > 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(100 * time.Millisecond):
				}
			}
			locs := liveLocations(definition)
			if len(locs) == 0 {
				t.Fatalf("definition unavailable: %s (%v)", definition, e)
			}
			found := false
			for _, loc := range locs {
				target, r := loc.URI, loc.Range
				if target == "" {
					target, r = loc.TargetURI, loc.TargetSelectionRange
				}
				if target == uri {
					offset, e := Offset(tc.Source, r.Start)
					if e == nil && strings.HasPrefix(tc.Source[offset:], "greet") {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("wrong definition: %s", definition)
			}
			var hover json.RawMessage
			deadline = time.Now().Add(40 * time.Second)
			for time.Now().Before(deadline) {
				call, stop := context.WithTimeout(ctx, 5*time.Second)
				hover, e = c.Request(call, "textDocument/hover", params)
				stop()
				if e == nil && strings.Contains(string(hover), "greet") {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(100 * time.Millisecond):
				}
			}
			if !strings.Contains(string(hover), "greet") {
				t.Fatalf("hover: %s (%v)", hover, e)
			}
			refs := request("textDocument/references", map[string]any{"textDocument": params["textDocument"], "position": at, "context": map[string]bool{"includeDeclaration": true}})
			if len(liveLocations(refs)) < 2 {
				t.Fatalf("references: %s", refs)
			}
			rename := request("textDocument/rename", map[string]any{"textDocument": params["textDocument"], "position": at, "newName": "salute"})
			renamed := liveApplyWorkspace(t, tc.Source, uri, rename)
			if strings.Count(renamed, "salute") != 2 || strings.Contains(renamed, "greet(") {
				t.Fatalf("rename: %s -> %s", rename, renamed)
			}
			symbols := request("textDocument/documentSymbol", map[string]any{"textDocument": params["textDocument"]})
			if !strings.Contains(string(symbols), "greet") {
				t.Fatalf("symbols: %s", symbols)
			}
			if tc.Format {
				formatted := request("textDocument/formatting", map[string]any{"textDocument": params["textDocument"], "options": map[string]any{"tabSize": 4, "insertSpaces": true}})
				var edits []TextEdit
				if e = json.Unmarshal(formatted, &edits); e != nil || len(edits) == 0 {
					t.Fatalf("formatting: %s %v", formatted, e)
				}
				result := liveApply(t, tc.Source, edits)
				if result == tc.Source || !strings.Contains(result, "😀") {
					t.Fatal("formatting did not preserve Unicode and change spacing")
				}
			}
			// Complete a local function after an unsaved incremental change.
			offset := strings.LastIndex(tc.Source, "greet(")
			partial := tc.Source[:offset] + "gre" + tc.Source[offset+5:]
			if e = c.Sync(uri, tc.Language, partial, 2); e != nil {
				t.Fatal(e)
			}
			complete := request("textDocument/completion", map[string]any{"textDocument": params["textDocument"], "position": PositionAt(partial, offset+3), "context": map[string]int{"triggerKind": 1}})
			var items []Completion
			if json.Unmarshal(complete, &items) != nil {
				var result struct {
					Items []Completion `json:"items"`
				}
				if e = json.Unmarshal(complete, &result); e != nil {
					t.Fatal(e)
				}
				items = result.Items
			}
			matched := false
			for _, item := range items {
				if strings.HasPrefix(item.Label, "greet") {
					matched = true
					if item.TextEdit != nil {
						edit, e := item.TextEdit.Edit()
						if e != nil {
							t.Fatal(e)
						}
						liveApply(t, partial, append([]TextEdit{edit}, item.AdditionalTextEdits...))
					}
					break
				}
			}
			if !matched {
				t.Fatalf("completion: %s", complete)
			}
			bad := tc.Source + tc.Bad
			if e = c.Sync(uri, tc.Language, bad, 3); e != nil {
				t.Fatal(e)
			}
			badLine := strings.Count(tc.Source, "\n")
			var badDiagnostics []Diagnostic
			deadline = time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				mu.Lock()
				for _, d := range diagnostics {
					if d.Range.Start.Line >= badLine && (diagVersion == nil || *diagVersion == 3) {
						badDiagnostics = append(badDiagnostics, d)
					}
				}
				mu.Unlock()
				if len(badDiagnostics) > 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(100 * time.Millisecond):
				}
			}
			if len(badDiagnostics) == 0 {
				t.Fatal("no diagnostics for unsaved erroneous source")
			}
			for _, d := range badDiagnostics {
				if _, e := Offset(bad, d.Range.Start); e != nil {
					t.Fatalf("diagnostic range: %#v %v", d, e)
				}
			}
			actions := request("textDocument/codeAction", map[string]any{"textDocument": params["textDocument"], "range": badDiagnostics[0].Range, "context": map[string]any{"diagnostics": badDiagnostics}})
			var list []json.RawMessage
			if e = json.Unmarshal(actions, &list); e != nil {
				t.Fatal(e)
			}
			if c.ResolveCodeActions && len(list) > 0 {
				var action map[string]json.RawMessage
				json.Unmarshal(list[0], &action)
				if len(action["data"]) > 0 {
					request("codeAction/resolve", list[0])
				}
			}
			if e = c.Sync(uri, tc.Language, tc.Source, 4); e != nil {
				t.Fatal(e)
			}
			deadline = time.Now().Add(30 * time.Second)
			cleared := false
			for time.Now().Before(deadline) {
				mu.Lock()
				cleared = true
				for _, d := range diagnostics {
					if d.Range.Start.Line >= badLine {
						cleared = false
					}
				}
				if diagVersion != nil && *diagVersion < 4 {
					cleared = false
				}
				mu.Unlock()
				if cleared {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(100 * time.Millisecond):
				}
			}
			if !cleared {
				t.Fatal("diagnostics did not clear after fixing the unsaved document")
			}
			c.Forget(uri)
			c.Close()
			select {
			case <-c.done:
			case <-time.After(5 * time.Second):
				t.Fatal("language server did not exit")
			}
			t.Logf("%s: completion, hover, definition, references, symbols, rename, diagnostics update/clear, %d code actions, formatting=%t, process cleanup", tc.Name, len(list), tc.Format)
		})
	}
}
func liveLocations(data json.RawMessage) []Location {
	var out []Location
	if json.Unmarshal(data, &out) == nil {
		return out
	}
	var one Location
	if json.Unmarshal(data, &one) == nil && (one.URI != "" || one.TargetURI != "") {
		return []Location{one}
	}
	return nil
}
func liveApply(t *testing.T, source string, edits []TextEdit) string {
	t.Helper()
	converted, e := Edits(source, edits)
	if e != nil {
		t.Fatal(e)
	}
	d, e := text.NewDocument(source)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Apply(d.Version, converted); e != nil {
		t.Fatal(e)
	}
	return d.Text.String()
}
func liveApplyWorkspace(t *testing.T, source, uri string, raw json.RawMessage) string {
	t.Helper()
	var edit struct {
		Changes         map[string][]TextEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []TextEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if e := json.Unmarshal(raw, &edit); e != nil {
		t.Fatal(e)
	}
	edits := edit.Changes[uri]
	if len(edit.DocumentChanges) > 0 {
		edits = nil
		for _, change := range edit.DocumentChanges {
			if change.TextDocument.URI != uri {
				t.Fatal(fmt.Sprintf("unexpected rename resource %s", change.TextDocument.URI))
			}
			edits = append(edits, change.Edits...)
		}
	}
	return liveApply(t, source, edits)
}
