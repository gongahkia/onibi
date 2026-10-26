package editor

import (
	"encoding/json"
	"testing"

	"github.com/gongahkia/shack/internal/lsp"
)

func TestUnversionedDiagnosticsTrackSentDocument(t *testing.T) {
	a := testApp(t)
	a.insert("const x=1")
	b := a.Current()
	b.File.Path = "/tmp/a.ts"
	b.LanguageKey = "server"
	b.LanguageEpoch = 7
	version := b.Doc.Version
	raw, _ := json.Marshal(map[string]any{"uri": lsp.URI(b.File.Path), "diagnostics": []lsp.Diagnostic{{Message: "example"}}})
	event := lsp.Notification{Method: "textDocument/publishDiagnostics", Params: raw, DocumentVersion: &version, DocumentEpoch: 7}
	a.notification("server", event)
	if len(b.Diagnostics) != 1 || b.DiagnosticVersion != version {
		t.Fatal("unversioned diagnostics discarded")
	}
	a.insert(";")
	a.notification("server", event)
	if len(b.Diagnostics) != 0 {
		t.Fatal("stale local version displayed")
	}
	current := b.Doc.Version
	event.DocumentVersion = &current
	event.DocumentEpoch = 6
	a.notification("server", event)
	if len(b.Diagnostics) != 0 {
		t.Fatal("stale document incarnation displayed")
	}
	event.DocumentEpoch = 7
	a.notification("server", event)
	if len(b.Diagnostics) != 1 {
		t.Fatal("current notification discarded")
	}
	event.Params, _ = json.Marshal(map[string]any{"uri": lsp.URI(b.File.Path), "diagnostics": []lsp.Diagnostic{}})
	a.notification("server", event)
	if len(b.Diagnostics) != 0 || b.DiagnosticVersion != current {
		t.Fatal("diagnostic clear discarded")
	}
	// Explicit server versions still take precedence over client-side hints.
	event.Params, _ = json.Marshal(map[string]any{"uri": lsp.URI(b.File.Path), "version": version, "diagnostics": []lsp.Diagnostic{{Message: "old"}}})
	a.notification("server", event)
	if len(b.Diagnostics) != 0 {
		t.Fatal("old server version displayed")
	}
}
