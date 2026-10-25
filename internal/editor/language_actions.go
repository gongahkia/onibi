package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/shack/internal/lsp"
)

type languageCommand struct {
	Client *lsp.Client
	Cancel context.CancelFunc
}
type serverCommand struct {
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (a *App) cancelPreview(reason string) {
	if job := a.resourcePreparation; job != nil {
		a.resourcePreparation = nil
		job.Cancel()
		job.Finish(false, reason)
	}
	p := a.Preview
	if p != nil && p.CommitCancel != nil {
		p.CommitCancel()
		a.Status = "Cancelling file operations; waiting for rollback"
		return
	}
	a.Preview = nil
	if p != nil && p.Finish != nil {
		p.Finish(false, reason)
	}
}

func (a *App) reviewServerEdit(ctx context.Context, key string, state *languageState, params lsp.ApplyEditParams) lsp.ApplyEditResult {
	done := make(chan lsp.ApplyEditResult, 1)
	var job *resourcePreparation
	var pending *preview // accessed only by closures on the event loop
	a.post(func() {
		finish := func(applied bool, reason string) {
			select {
			case done <- lsp.ApplyEditResult{Applied: applied, FailureReason: reason}:
			default:
			}
		}
		if ctx.Err() != nil {
			finish(false, "Request cancelled")
			return
		}
		if a.languages[key] != state || state.Client == nil || !state.Client.Alive() {
			finish(false, "Language session changed")
			return
		}
		if a.Readonly {
			finish(false, "Read-only mode")
			return
		}
		if a.resourcePreparation != nil || a.Preview != nil || (a.Dialog != nil && !a.Dialog.Inline) {
			finish(false, "Another dialog is open; retry the action after closing it")
			return
		}
		title := "Language server edit"
		if params.Label != "" {
			title += ": " + params.Label[:min(len(params.Label), 160)]
		}
		job = a.actionPreview(title, params.Edit, finish, func(p *preview) {
			pending = p
			p.Server = state.Client
			p.Valid = ctx.Err
			if ctx.Err() != nil {
				a.cancelPreview("Language edit request cancelled")
			}
		})
	})
	select {
	case result := <-done:
		return result
	case <-ctx.Done():
		// An applied edit wins a simultaneous transport cancellation.
		select {
		case result := <-done:
			return result
		default:
		}
		a.post(func() {
			if job != nil && a.resourcePreparation == job {
				a.cancelPreview("Language edit request cancelled")
			}
			if pending != nil && a.Preview == pending {
				a.cancelPreview("Language edit request cancelled")
				a.Status = "Language edit request cancelled"
			}
			if pending == nil || pending.CommitCancel == nil {
				select {
				case done <- lsp.ApplyEditResult{FailureReason: ctx.Err().Error()}:
				default:
				}
			}
		})
		select {
		case result := <-done:
			return result
		case <-a.ctx.Done():
			return lsp.ApplyEditResult{FailureReason: "Editor closed"}
		}
	}
}

func (a *App) actionPreview(title string, raw json.RawMessage, finish func(bool, string), ready func(*preview)) *resourcePreparation {
	if a.Preview != nil && a.Preview.CommitCancel != nil {
		finish(false, "A file operation is still running")
		return nil
	}
	if hasResourceChanges(raw) {
		return a.prepareResourcePreview(title, raw, finish, ready)
	}
	targets, err := a.decodeWorkspaceEdit(raw)
	if err != nil {
		a.Status = err.Error()
		finish(false, a.Status)
		return nil
	}
	a.showWorkspacePreview(title, targets)
	if a.Preview == nil {
		finish(true, "")
		return nil
	}
	a.Preview.Strict = true
	a.Preview.Finish = finish
	if ready != nil {
		ready(a.Preview)
	}
	return nil
}

func actionServerCommand(c codeAction) (*serverCommand, error) {
	if !hasJSON(c.Command) {
		return nil, nil
	}
	var cmd serverCommand
	if json.Unmarshal(c.Command, &cmd.Command) == nil {
		cmd.Arguments = c.Arguments // legacy Command rather than CodeAction
	} else if err := json.Unmarshal(c.Command, &cmd); err != nil {
		return nil, err
	}
	if cmd.Command == "" || len(cmd.Command) > 1024 || len(cmd.Arguments) > 1<<20 {
		return nil, errors.New("invalid or oversized language server command")
	}
	if hasJSON(cmd.Arguments) {
		var args []json.RawMessage
		if json.Unmarshal(cmd.Arguments, &args) != nil {
			return nil, errors.New("server command arguments must be an array")
		}
	}
	return &cmd, nil
}

func (a *App) codeActions(client *lsp.Client, raw json.RawMessage) {
	var actions []json.RawMessage
	if err := json.Unmarshal(raw, &actions); err != nil {
		a.Status = err.Error()
		return
	}
	type version struct {
		number uint64
		path   string
	}
	versions := map[*Buffer]version{}
	for _, b := range a.Buffers {
		versions[b] = version{b.Doc.Version, b.File.Path}
	}
	valid := func() bool {
		if client != nil && !client.Alive() {
			return false
		}
		for b, v := range versions {
			found := false
			for _, open := range a.Buffers {
				found = found || open == b
			}
			if !found || b.Doc.Version != v.number || b.File.Path != v.path {
				return false
			}
		}
		return true
	}
	items := []choice{}
	for _, raw := range actions[:min(1000, len(actions))] {
		var action codeAction
		if json.Unmarshal(raw, &action) != nil {
			continue
		}
		label := action.Title
		if action.Disabled != nil {
			label += " (" + action.Disabled.Reason + ")"
		}
		items = append(items, choice{label, func() {
			if !valid() {
				a.Status = "Code action is stale"
				return
			}
			if action.Disabled != nil {
				a.Status = action.Disabled.Reason
				return
			}
			if hasJSON(action.Data) && client != nil && client.ResolveCodeActions {
				if a.languageBusy {
					a.Status = "Language request already running"
					return
				}
				a.languageBusy = true
				go func() {
					ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
					defer cancel()
					resolved, err := client.Request(ctx, "codeAction/resolve", raw)
					a.post(func() {
						a.languageBusy = false
						if !valid() || a.Preview != nil || a.Dialog != nil {
							a.Status = "Code action became stale"
							return
						}
						if err != nil {
							a.Status = err.Error()
							return
						}
						var resolvedAction codeAction
						if err = json.Unmarshal(resolved, &resolvedAction); err != nil {
							a.Status = err.Error()
							return
						}
						a.runCodeAction(client, resolvedAction)
					})
				}()
				return
			}
			a.runCodeAction(client, action)
		}})
	}
	if len(items) == 0 {
		a.Status = "No code actions here"
		return
	}
	a.list("Code actions · edits will be previewed", items)
}

func (a *App) runCodeAction(client *lsp.Client, action codeAction) {
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	if action.Disabled != nil {
		a.Status = action.Disabled.Reason
		return
	}
	cmd, err := actionServerCommand(action)
	if err != nil {
		a.Status = err.Error()
		return
	}
	if cmd != nil && (client == nil || !client.Alive()) {
		a.Status = "Language server exited"
		return
	}
	if cmd != nil && a.languageCommand != nil {
		a.Status = "A server command is still running · Escape cancels"
		return
	}
	finish := func(applied bool, reason string) {
		if applied && cmd != nil {
			a.executeServerCommand(client, action.Title, *cmd)
		}
	}
	if hasJSON(action.Edit) {
		a.actionPreview(action.Title, action.Edit, finish, func(p *preview) {
			if cmd != nil {
				p.Valid = func() error {
					if !client.Alive() {
						return errors.New("Language server exited")
					}
					return nil
				}
			}
		})
		return
	}
	if cmd != nil {
		a.executeServerCommand(client, action.Title, *cmd)
		return
	}
	a.Status = "This action has no edit or executable server command"
}

func (a *App) executeServerCommand(client *lsp.Client, title string, command serverCommand) {
	if a.resourceClosing {
		return
	}
	if a.languageCommand != nil {
		a.Status = "A server command is still running"
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	job := &languageCommand{client, cancel}
	a.languageCommand = job
	// Include buffers opened by an edit-based action before executing its command.
	type syncItem struct {
		uri, language, source string
		version, epoch        uint64
	}
	items := []syncItem{}
	total := 0
	for _, b := range a.Buffers {
		if b.Transient || b.File.Path == "" || b.Doc.Text.Len() > a.Config.ServiceMaxBytes {
			continue
		}
		server, available := lsp.ServerFor(b.File.Path, a.Config.Servers)
		key := lsp.Root(b.File.Path) + "\x00" + strings.Join(server.Command, "\x00")
		state := a.languages[key]
		if !available || state == nil || state.Client != client {
			continue
		}
		total += b.Doc.Text.Len()
		if total > workspaceBytes {
			cancel()
			a.languageCommand = nil
			a.Status = "Server command exceeds 8 MiB of open workspace text"
			return
		}
		if b.LanguageEpoch == 0 {
			a.languageSequence++
			b.LanguageEpoch = a.languageSequence
		}
		b.LanguageKey, b.LanguageID = key, server.Language
		items = append(items, syncItem{lsp.URI(b.File.Path), b.LanguageID, b.Doc.Text.String(), b.Doc.Version, b.LanguageEpoch})
	}
	a.Status = "Running " + title + " · Escape cancels; incoming edits will be previewed"
	go func() {
		defer cancel()
		var err error
		for _, item := range items {
			err = syncWithTimeout(ctx, client, item.uri, item.language, item.source, item.version, item.epoch)
			if err != nil {
				break
			}
		}
		if err == nil {
			_, err = client.Request(ctx, "workspace/executeCommand", command)
		}
		a.post(func() {
			if a.languageCommand != job {
				return
			}
			a.languageCommand = nil
			if err != nil {
				if a.Preview != nil && a.Preview.Server == client {
					a.cancelPreview(err.Error())
				}
				a.Status = err.Error()
			} else if a.Preview == nil {
				a.Status = fmt.Sprintf("Completed %s · Save all writes edited buffers", title)
			}
		})
	}()
}

func (a *App) cancelLanguageCommand() {
	if job := a.languageCommand; job != nil {
		job.Cancel()
		a.languageCommand = nil
		if a.Preview != nil && a.Preview.Server == job.Client {
			a.cancelPreview("Server command cancelled")
		}
		a.Status = "Server command cancelled; already accepted edits remain undoable"
	}
}
