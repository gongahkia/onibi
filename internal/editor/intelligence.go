package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/text"
	"sort"
	"strings"
)

func (a *App) intelligenceCommands() {
	a.Commands = append(a.Commands,
		command{"Cancel language server command", "", a.cancelLanguageCommand},
		command{"Hover documentation", "", func() { a.languageRequest("hover") }}, command{"Signature help", "", func() { a.languageRequest("signatureHelp") }},
		command{"Find references", "", func() {
			a.languageRequestWith("references", map[string]any{"context": map[string]bool{"includeDeclaration": true}})
		}},
		command{"Go to implementation", "", func() { a.languageRequest("implementation") }}, command{"Go to type definition", "", func() { a.languageRequest("typeDefinition") }},
		command{"Document symbols", "", func() { a.languageRequest("documentSymbol") }},
		command{"Workspace symbols", "", func() {
			a.prompt("Workspace symbol", "", func(q string) { a.languageRequestWith("workspaceSymbol", map[string]any{"query": q}) })
		}},
		command{"Preview rename symbol", "", func() {
			a.prompt("New symbol name", "", func(name string) {
				if strings.TrimSpace(name) != "" {
					a.languageRequestWith("rename", map[string]any{"newName": name})
				}
			})
		}},
		command{"Code actions", "", func() {
			b := a.Current()
			s := b.Doc.Selections[0]
			source := b.Doc.Text.String()
			a.languageRequestWith("codeAction", map[string]any{"range": lsp.Range{Start: lsp.PositionAt(source, s.Start()), End: lsp.PositionAt(source, s.End())}, "context": map[string]any{"diagnostics": b.Diagnostics}})
		}},
		command{"Next diagnostic", "", func() { a.nextDiagnostic(1) }}, command{"Previous diagnostic", "", func() { a.nextDiagnostic(-1) }})
}
func (a *App) report(title, body string) {
	a.rememberJump()
	var b *Buffer
	for i, open := range a.Buffers {
		if open.Transient && open.Title == title {
			b = open
			a.Active = i
			break
		}
	}
	if b == nil {
		a.NewBuffer()
		b = a.Current()
	}
	d, err := text.NewDocument(strings.ToValidUTF8(body, "�"))
	if err != nil {
		a.Status = err.Error()
		return
	}
	b.Doc = d
	b.Merge = nil
	a.closeReading(b)
	b.Transient = true
	b.Title = title
	b.Top = 0
	b.Left = 0
	b.WrapTop = 0
	b.SyntaxReady = false
	a.reveal()
	a.Status = "Read-only report · Ctrl+W closes · Save as exports"
}
func (a *App) intelligenceResult(kind string, client *lsp.Client, result json.RawMessage) {
	if string(result) == "null" || len(result) == 0 {
		a.Status = "No " + kind + " result"
		return
	}
	switch kind {
	case "hover":
		var h struct {
			Contents json.RawMessage `json:"contents"`
		}
		if json.Unmarshal(result, &h) != nil {
			a.Status = "Invalid hover response"
			return
		}
		a.report("Hover", markupText(h.Contents))
	case "signatureHelp":
		var h struct {
			Signatures []struct {
				Label         string          `json:"label"`
				Documentation json.RawMessage `json:"documentation"`
			} `json:"signatures"`
			Active int `json:"activeSignature"`
		}
		if json.Unmarshal(result, &h) != nil || len(h.Signatures) == 0 {
			a.Status = "No signatures"
			return
		}
		var out strings.Builder
		for i, s := range h.Signatures {
			if i == h.Active {
				out.WriteString("→ ")
			}
			out.WriteString(s.Label + "\n" + markupText(s.Documentation) + "\n\n")
		}
		a.report("Signatures", out.String())
	case "documentSymbol", "workspaceSymbol":
		a.symbols(result)
	case "rename":
		a.workspaceEdit("Rename symbol", result)
	case "codeAction":
		a.codeActions(client, result)
	}
}
func markupText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var v struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &v) == nil && v.Value != "" {
		return v.Value
	}
	var many []json.RawMessage
	if json.Unmarshal(raw, &many) == nil {
		out := []string{}
		for _, item := range many {
			out = append(out, markupText(item))
		}
		return strings.Join(out, "\n\n")
	}
	return ""
}

type symbol struct {
	Name           string        `json:"name"`
	Detail         string        `json:"detail"`
	SelectionRange lsp.Range     `json:"selectionRange"`
	Location       *lsp.Location `json:"location"`
	Children       []symbol      `json:"children"`
}

func (a *App) symbols(raw json.RawMessage) {
	var symbols []symbol
	if err := json.Unmarshal(raw, &symbols); err != nil {
		a.Status = err.Error()
		return
	}
	uri := lsp.URI(a.Current().File.Path)
	items := []choice{}
	var add func([]symbol, string, int)
	add = func(nodes []symbol, prefix string, depth int) {
		if depth > 64 {
			return
		}
		for _, s := range nodes {
			if len(items) >= 2000 {
				return
			}
			loc := lsp.Location{URI: uri, Range: s.SelectionRange}
			if s.Location != nil {
				loc = *s.Location
			}
			label := prefix + s.Name
			if s.Detail != "" {
				label += "  " + s.Detail
			}
			items = append(items, choice{label, func() { raw, _ := json.Marshal(loc); a.definitions(raw) }})
			add(s.Children, prefix+s.Name+" › ", depth+1)
		}
	}
	add(symbols, "", 0)
	if len(items) == 0 {
		a.Status = "No symbols"
		return
	}
	a.list("Symbols · type to filter", items)
}

type wireWorkspaceEdit struct {
	ChangeAnnotations map[string]struct {
		Label, Description string
		NeedsConfirmation  bool
	} `json:"changeAnnotations"`
	Changes         map[string][]lsp.TextEdit `json:"changes"`
	DocumentChanges []json.RawMessage         `json:"documentChanges"`
}

func (a *App) workspaceEdit(title string, raw json.RawMessage) {
	a.actionPreview(title, raw, func(bool, string) {}, nil)
}
func (a *App) decodeWorkspaceEdit(raw json.RawMessage) ([]*workspaceTarget, error) {
	var wire wireWorkspaceEdit
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	versions := map[string]*uint64{}
	if wire.DocumentChanges != nil {
		wire.Changes = nil
	}
	if wire.Changes == nil {
		wire.Changes = map[string][]lsp.TextEdit{}
	}
	for _, raw := range wire.DocumentChanges {
		var d struct {
			Kind         string `json:"kind"`
			TextDocument struct {
				URI     string  `json:"uri"`
				Version *uint64 `json:"version"`
			} `json:"textDocument"`
			Edits []lsp.TextEdit `json:"edits"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		if d.Kind != "" || d.TextDocument.URI == "" {
			return nil, errors.New("file creation, rename and deletion operations are not supported; no changes applied")
		}
		if _, exists := wire.Changes[d.TextDocument.URI]; exists {
			return nil, errors.New("duplicate document edits; no changes applied")
		}
		wire.Changes[d.TextDocument.URI] = d.Edits
		versions[d.TextDocument.URI] = d.TextDocument.Version
	}
	uris := []string{}
	for uri := range wire.Changes {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	targets := []*workspaceTarget{}
	bytes, count := 0, 0
	seen := map[string]bool{}
	for _, uri := range uris {
		path, err := lsp.Path(uri)
		if err != nil {
			return nil, err
		}
		target, err := a.targetForPath(path)
		if err != nil {
			return nil, err
		}
		if seen[target.Buffer.File.Path] {
			return nil, errors.New("duplicate canonical file in workspace edit")
		}
		seen[target.Buffer.File.Path] = true
		if v := versions[uri]; v != nil && (target.New || *v != target.Version) {
			return nil, errors.New("language server returned a stale document version")
		}
		source := target.Document.Text.String()
		target.Edits, err = lsp.Edits(source, wire.Changes[uri])
		if err != nil {
			return nil, err
		}
		bytes += len(source)
		count += len(target.Edits)
		for _, e := range target.Edits {
			bytes += len(e.Text)
		}
		if bytes > workspaceBytes || count > workspaceMatches {
			return nil, errors.New("workspace edit exceeds 8 MiB or 2,000 edits")
		}
		if err := target.Document.Clone().Apply(target.Version, target.Edits); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

type codeAction struct {
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Title     string          `json:"title"`
	Edit      json.RawMessage `json:"edit"`
	Command   json.RawMessage `json:"command"`
	Data      json.RawMessage `json:"data"`
	Disabled  *struct {
		Reason string `json:"reason"`
	} `json:"disabled"`
}

func hasJSON(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }
func (a *App) nextDiagnostic(delta int) {
	b := a.Current()
	source := b.Doc.Text.String()
	caret := b.Doc.Selections[0].Head
	type item struct {
		offset  int
		message string
	}
	items := []item{}
	for _, d := range b.Diagnostics {
		p, err := lsp.Offset(source, d.Range.Start)
		if err == nil {
			items = append(items, item{p, d.Message})
		}
	}
	if len(items) == 0 {
		a.Status = "No current diagnostics"
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].offset < items[j].offset })
	idx := 0
	if delta > 0 {
		idx = sort.Search(len(items), func(i int) bool { return items[i].offset > caret })
		idx %= len(items)
	} else {
		idx = sort.Search(len(items), func(i int) bool { return items[i].offset >= caret }) - 1
		if idx < 0 {
			idx = len(items) - 1
		}
	}
	a.jumpTo(b, items[idx].offset)
	a.Status = fmt.Sprintf("%d/%d: %s", idx+1, len(items), items[idx].message)
}
