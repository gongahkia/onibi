package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/text"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type languageState struct {
	Client   *lsp.Client
	Starting bool
	Error    string
}

func (a *App) languageCommands() {
	a.Commands = append(a.Commands,
		command{"Complete symbol", "Ctrl+Space", func() { a.languageRequest("completion") }},
		command{"Go to definition", "F12", func() { a.languageRequest("definition") }},
		command{"Preview format document", "", func() { a.languageRequest("formatting") }},
		command{"Diagnostics", "", a.diagnosticPicker},
		command{"Language server status", "", func() {
			a.Status = a.Current().LanguageStatus
			if a.Status == "" {
				a.Status = "No language server for this buffer"
			}
		}},
		command{"Restart language servers", "", func() {
			for _, state := range a.languages {
				if state.Client != nil {
					state.Client.Close()
				}
			}
			a.languages = map[string]*languageState{}
			for _, b := range a.Buffers {
				b.LanguageKey = ""
				b.Synced = false
				b.Diagnostics = nil
			}
			a.schedule()
			a.Status = "Language servers will restart for the active buffer"
		}},
	)
}
func (a *App) languageClient(b *Buffer) *lsp.Client {
	if b.Transient {
		return nil
	}
	if !a.Config.LSP {
		b.LanguageStatus = "Language services disabled"
		return nil
	}
	if b.Doc.Text.Len() > a.Config.ServiceMaxBytes {
		b.LanguageStatus = "Language services paused for large file"
		return nil
	}
	if b.File.Path == "" {
		b.LanguageStatus = "Save the buffer to enable language services"
		return nil
	}
	server, available := lsp.ServerFor(b.File.Path, a.Config.Servers)
	if !available {
		if len(server.Command) > 0 {
			b.LanguageStatus = "Optional server missing: " + server.Command[0]
		} else {
			b.LanguageStatus = "No language server configured for this file type"
		}
		return nil
	}
	root := lsp.Root(b.File.Path)
	key := root + "\x00" + strings.Join(server.Command, "\x00")
	if b.LanguageEpoch == 0 {
		a.languageSequence++
		b.LanguageEpoch = a.languageSequence
	}
	b.LanguageKey = key
	b.LanguageID = server.Language
	if a.languages == nil {
		a.languages = map[string]*languageState{}
	}
	state := a.languages[key]
	if state == nil {
		if len(a.languages) >= 4 {
			b.LanguageStatus = "Four language workspaces active; restart servers to release them"
			return nil
		}
		state = &languageState{Starting: true}
		a.languages[key] = state
		b.LanguageStatus = "Starting " + server.Command[0]
		go func() {
			client, err := lsp.Start(a.ctx, server.Command, root, func(n lsp.Notification) {
				a.post(func() {
					if a.languages[key] == state {
						a.notification(key, n)
					}
				})
			},
				func(ctx context.Context, p lsp.ApplyEditParams) lsp.ApplyEditResult {
					return a.reviewServerEdit(ctx, key, state, p)
				})
			a.post(func() {
				if a.languages[key] != state {
					if client != nil {
						client.Close()
					}
					return
				}
				state.Starting = false
				if err != nil {
					state.Error = err.Error()
					b.LanguageStatus = state.Error
				} else {
					state.Client = client
					b.LanguageStatus = server.Command[0] + " ready"
					a.schedule()
				}
			})
		}()
		return nil
	}
	if state.Starting {
		b.LanguageStatus = "Language server starting"
		return nil
	}
	if state.Error != "" {
		b.LanguageStatus = state.Error
		return nil
	}
	if state.Client == nil || !state.Client.Alive() {
		b.LanguageStatus = "Language server exited; use Restart language servers"
		return nil
	}
	b.LanguageStatus = server.Command[0] + " ready"
	return state.Client
}
func (a *App) syncLanguage() {
	if a.resourceCommitCancel != nil {
		return
	}
	b := a.Current()
	client := a.languageClient(b)
	if client == nil || b.Syncing || (b.Synced && b.SyncedVersion == b.Doc.Version) {
		return
	}
	b.Syncing = true
	snapshot := b.Doc.Text
	version := b.Doc.Version
	uri := lsp.URI(b.File.Path)
	language := b.LanguageID
	epoch := b.LanguageEpoch
	go func() {
		err := syncWithTimeout(a.ctx, client, uri, language, snapshot.String(), version, epoch)
		a.post(func() {
			b.Syncing = false
			if lsp.URI(b.File.Path) != uri {
				return
			}
			if err != nil {
				b.LanguageStatus = err.Error()
				return
			}
			b.Synced = true
			b.SyncedVersion = version
			if b.Doc.Version != version {
				a.schedule()
			}
		})
	}()
}
func syncWithTimeout(parent context.Context, c *lsp.Client, uri, language, source string, version uint64, epoch ...uint64) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Sync(uri, language, source, version, epoch...) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		c.Close()
		return ctx.Err()
	}
}
func (a *App) notification(key string, n lsp.Notification) {
	if n.Method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string           `json:"uri"`
		Version     *uint64          `json:"version"`
		Diagnostics []lsp.Diagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(n.Params, &p) != nil {
		return
	}
	for _, b := range a.Buffers {
		if b.LanguageKey != key || lsp.URI(b.File.Path) != p.URI {
			continue
		}
		if p.Version != nil && *p.Version != b.Doc.Version {
			continue
		}
		if p.Version == nil && (n.DocumentVersion == nil || *n.DocumentVersion != b.Doc.Version || n.DocumentEpoch != b.LanguageEpoch) {
			continue
		}
		b.Diagnostics = p.Diagnostics[:min(len(p.Diagnostics), 1000)]
		b.DiagnosticVersion = b.Doc.Version
	}
}
func (a *App) diagnosticPicker() {
	b := a.Current()
	if len(b.Diagnostics) == 0 {
		a.Status = b.LanguageStatus + " · no current diagnostics"
		return
	}
	items := []choice{}
	source := b.Doc.Text.String()
	version := b.Doc.Version
	for _, d := range b.Diagnostics {
		diag := d
		items = append(items, choice{fmt.Sprintf("%d:%d  %s", d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message), func() {
			if version != b.Doc.Version {
				a.Status = "Diagnostics changed"
				return
			}
			p, err := lsp.Offset(source, diag.Range.Start)
			if err == nil {
				b.Doc.Selections = []text.Selection{{Anchor: p, Head: p}}
				a.reveal()
				a.Status = diag.Message
			}
		}})
	}
	a.list("Diagnostics", items)
}
func (a *App) languageRequest(kind string) { a.languageRequestWith(kind, nil) }
func (a *App) languageRequestWith(kind string, extra map[string]any) {
	automatic := a.completionAutomatic
	b := a.Current()
	client := a.languageClient(b)
	if client == nil {
		if !automatic {
			a.Status = b.LanguageStatus
		}
		return
	}
	if a.languageBusy {
		a.Status = "Language request already running"
		return
	}
	a.languageBusy = true
	if kind == "completion" {
		a.wantCompletion = false
	}
	versions := map[*Buffer]uint64{}
	type syncItem struct {
		buffer                *Buffer
		uri, language, source string
		version, epoch        uint64
	}
	pending := []syncItem{}
	syncBytes := b.Doc.Text.Len()
	for _, other := range a.Buffers {
		versions[other] = other.Doc.Version
		if other != b && !other.Transient && other.File.Path != "" && other.Doc.Text.Len() <= a.Config.ServiceMaxBytes {
			if other.LanguageKey == "" {
				server, available := lsp.ServerFor(other.File.Path, a.Config.Servers)
				key := lsp.Root(other.File.Path) + "\x00" + strings.Join(server.Command, "\x00")
				if available && key == b.LanguageKey {
					other.LanguageKey = key
					other.LanguageID = server.Language
					if other.LanguageEpoch == 0 {
						a.languageSequence++
						other.LanguageEpoch = a.languageSequence
					}
				}
			}
			if other.LanguageKey != b.LanguageKey || (other.Synced && other.SyncedVersion == other.Doc.Version) {
				continue
			}
			syncBytes += other.Doc.Text.Len()
			if syncBytes > workspaceBytes {
				a.languageBusy = false
				a.Status = "Language request exceeds 8 MiB of open workspace text"
				return
			}
			pending = append(pending, syncItem{other, lsp.URI(other.File.Path), other.LanguageID, other.Doc.Text.String(), other.Doc.Version, other.LanguageEpoch})
		}
	}
	snapshot := b.Doc.Text
	version := b.Doc.Version
	caret := b.Doc.Selections[0].Head
	uri := lsp.URI(b.File.Path)
	language := b.LanguageID
	epoch := b.LanguageEpoch
	tabSize, spaces := a.Config.TabSize, a.Config.InsertSpaces
	if !automatic {
		a.Status = "Requesting " + kind + "…"
	}
	go func() {
		source := snapshot.String()
		err := syncWithTimeout(a.ctx, client, uri, language, source, version, epoch)
		var result json.RawMessage
		for _, item := range pending {
			if err != nil {
				break
			}
			err = syncWithTimeout(a.ctx, client, item.uri, item.language, item.source, item.version, item.epoch)
		}
		if err == nil {
			params := map[string]any{"textDocument": map[string]string{"uri": uri}}
			if kind == "formatting" {
				params["options"] = map[string]any{"tabSize": tabSize, "insertSpaces": spaces}
			} else {
				params["position"] = lsp.PositionAt(source, caret)
			}
			ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
			for k, v := range extra {
				params[k] = v
			}
			method := "textDocument/" + kind
			if kind == "workspaceSymbol" {
				method = "workspace/symbol"
				params = extra
			}
			result, err = client.Request(ctx, method, params)
			cancel()
		}
		a.post(func() {
			a.languageBusy = false
			if a.wantCompletion {
				a.schedule()
			}
			if b != a.Current() || b.Doc.Version != version || b.Doc.Selections[0].Head != caret || lsp.URI(b.File.Path) != uri {
				if !automatic {
					a.Status = "Discarded outdated language result"
				}
				return
			}
			if err != nil {
				if !automatic {
					a.Status = err.Error()
				}
				return
			}
			b.Synced = true
			b.SyncedVersion = version
			for _, item := range pending {
				if item.buffer.LanguageEpoch == item.epoch {
					item.buffer.Synced = true
					item.buffer.SyncedVersion = item.version
				}
			}
			switch kind {
			case "completion":
				if a.Dialog != nil || a.Preview != nil {
					return
				}
				a.completions(b, version, caret, source, result)
			case "definition", "references", "implementation", "typeDefinition":
				a.definitions(result)
			case "formatting":
				var serverEdits []lsp.TextEdit
				if err := json.Unmarshal(result, &serverEdits); err != nil {
					a.Status = err.Error()
					return
				}
				edits, err := lsp.Edits(source, serverEdits)
				if err != nil {
					a.Status = err.Error()
					return
				}
				a.showPreview("Format document", b, version, edits)
			default:
				for other, v := range versions {
					if other.Doc.Version != v {
						a.Status = "Workspace changed during the language request; try again"
						return
					}
				}
				a.intelligenceResult(kind, client, result)
			}
		})
	}()
}
func (a *App) completions(b *Buffer, version uint64, caret int, source string, result json.RawMessage) {
	var items []lsp.Completion
	if json.Unmarshal(result, &items) != nil {
		var list struct {
			Items []lsp.Completion `json:"items"`
		}
		if json.Unmarshal(result, &list) != nil {
			a.Status = "Invalid completion response"
			return
		}
		items = list.Items
	}
	choices := []choice{}
	start := caret
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(source[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		start -= n
	}
	for _, item := range items[:min(len(items), 1000)] {
		item := item
		label := item.Label
		if item.Detail != "" {
			label += "  " + item.Detail
		}
		choices = append(choices, choice{label, func() {
			if b.Doc.Version != version {
				a.Status = "Completion is stale"
				return
			}
			a.snippetRequest++
			insert := item.InsertText
			if insert == "" {
				insert = item.Label
			}
			edits := []text.Edit{{Start: start, End: caret, Text: insert}}
			if item.TextEdit != nil {
				var err error
				edit, conversionErr := item.TextEdit.Edit()
				if conversionErr != nil {
					a.Status = conversionErr.Error()
					return
				}
				edits, err = lsp.Edits(source, []lsp.TextEdit{edit})
				if err != nil {
					a.Status = err.Error()
					return
				}
			}
			additional, err := lsp.Edits(source, item.AdditionalTextEdits)
			if err != nil {
				a.Status = err.Error()
				return
			}
			if item.InsertTextFormat == 2 {
				a.snippetCompletion(b, version, edits[0], additional)
				return
			}
			edits = append(edits, additional...)
			a.apply(edits)
		}})
	}
	if len(choices) == 0 {
		a.Status = "No completions at this position"
		return
	}
	a.list("Completions", choices)
	a.Dialog.Inline = true
}
func (a *App) definitions(result json.RawMessage) {
	var locations []lsp.Location
	if json.Unmarshal(result, &locations) != nil {
		var location lsp.Location
		if json.Unmarshal(result, &location) != nil {
			a.Status = "Invalid definition response"
			return
		}
		locations = []lsp.Location{location}
	}
	items := []choice{}
	for _, location := range locations {
		loc := location
		if loc.URI == "" {
			loc.URI = loc.TargetURI
			loc.Range = loc.TargetSelectionRange
		}
		if loc.URI == "" {
			continue
		}
		path, err := lsp.Path(loc.URI)
		if err != nil {
			a.Status = err.Error()
			continue
		}
		items = append(items, choice{fmt.Sprintf("%s:%d", path, loc.Range.Start.Line+1), func() {
			a.rememberJump()
			if err := a.Open(path); err != nil {
				a.Status = err.Error()
				return
			}
			b := a.Current()
			p, err := lsp.Offset(b.Doc.Text.String(), loc.Range.Start)
			if err != nil {
				a.Status = err.Error()
				return
			}
			a.finishJump(b, p)
		}})
	}
	if len(items) == 1 {
		items[0].Run()
	} else if len(items) > 1 {
		a.list("Locations", items)
	} else {
		a.Status = "No locations found"
	}
}

// Key aliases remain available through the palette when terminals intercept them.
func (a *App) languageKey(key tcell.Key) bool {
	switch key {
	case tcell.KeyCtrlSpace:
		a.languageRequest("completion")
		return true
	case tcell.KeyF12:
		a.languageRequest("definition")
		return true
	}
	return false
}

func (a *App) forgetLanguage(b *Buffer) {
	state := a.languages[b.LanguageKey]
	if state == nil || state.Client == nil || b.File.Path == "" {
		return
	}
	client, uri, epoch := state.Client, lsp.URI(b.File.Path), b.LanguageEpoch
	go func() {
		done := make(chan struct{})
		go func() { client.Forget(uri, epoch); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			client.Close()
		case <-a.ctx.Done():
			client.Close()
		}
	}()
}
