package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/gongahkia/onibi/internal/codexapp"
	"github.com/gongahkia/onibi/internal/store"
)

const codexThreadPrefix = "codex.thread."

type CodexEvent struct {
	SessionID string
	Kind      string
	Text      string
	Request   *codexapp.ServerRequest
}
type codexRuntime struct {
	client    *codexapp.Client
	threadID  string
	turnID    string
	mu        sync.Mutex
	responded map[string]bool
	pending   map[string]bool
}

func (d *Daemon) CodexEvents() <-chan CodexEvent { return d.codexEvents }

func (d *Daemon) StartCodexSession(ctx context.Context, name, cwd string) (*Session, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cwd, err := normalizeSessionCWD(cwd)
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, fmt.Errorf("codex not found in PATH: %w", err)
	}
	client, err := codexapp.Start(ctx, codexapp.Options{Command: path, Dir: cwd})
	if err != nil {
		return nil, fmt.Errorf("start Codex App Server: %w", err)
	}
	threadID, err := client.StartThread(ctx, cwd)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	name, err = d.sessionName(name, "codex")
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	s := NewSession(NewID(), name, "codex", d.bufferSize())
	s.Transport = "codex"
	s.Cmd = "codex app-server"
	s.CWD = cwd
	if err := d.Registry.Add(s); err != nil {
		_ = client.Close()
		return nil, err
	}
	d.addCodexRuntime(ctx, s, client, threadID)
	if d.DB != nil {
		_ = d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, s.CWD, s.Cmd, s.Transport, "", s.StartedAt())
		_ = d.DB.KVSetString(ctx, codexThreadPrefix+s.ID, threadID)
	}
	d.audit(ctx, "session.start", s.ID, "", 0, "agent=codex transport=app-server")
	d.Log.Info("session started", "session", s.ID, "name", s.Name, "agent", "codex", "transport", "app-server")
	return s, nil
}

func (d *Daemon) addCodexRuntime(ctx context.Context, s *Session, client *codexapp.Client, threadID string) {
	runtime := &codexRuntime{client: client, threadID: threadID, responded: map[string]bool{}, pending: map[string]bool{}}
	d.codexMu.Lock()
	d.codex[s.ID] = runtime
	d.codexMu.Unlock()
	go d.forwardCodexRequests(ctx, s, runtime)
	go d.forwardCodexNotifications(ctx, s, runtime)
	go d.watchCodexRuntime(ctx, s, runtime)
}

func (d *Daemon) watchCodexRuntime(ctx context.Context, s *Session, runtime *codexRuntime) {
	select {
	case <-ctx.Done():
		return
	case err, ok := <-runtime.client.Done:
		if !ok || ctx.Err() != nil {
			return
		}
		d.codexMu.Lock()
		if d.codex[s.ID] != runtime {
			d.codexMu.Unlock()
			return
		}
		delete(d.codex, s.ID)
		d.codexMu.Unlock()
		d.markSessionEndedReason(context.Background(), s, "Codex App Server stopped")
		message := "Codex App Server stopped."
		if err != nil {
			message = "Codex App Server stopped: " + err.Error()
		}
		if detail := runtime.client.StderrTail(); detail != "" {
			message += "\n" + detail
		}
		d.Log.Warn("Codex App Server stopped", "session", s.ID, "err", err)
		select {
		case d.codexEvents <- CodexEvent{SessionID: s.ID, Kind: "failed", Text: message}:
		default:
			d.Log.Warn("dropping Codex failure event", "session", s.ID)
		}
	}
}
func (d *Daemon) forwardCodexRequests(ctx context.Context, s *Session, r *codexRuntime) {
	for req := range r.client.Requests {
		req := req
		r.mu.Lock()
		r.pending[string(req.ID)] = true
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case d.codexEvents <- CodexEvent{SessionID: s.ID, Kind: "request", Request: &req}:
		}
	}
}
func (d *Daemon) forwardCodexNotifications(ctx context.Context, s *Session, r *codexRuntime) {
	for notification := range r.client.Notifications {
		kind, text, turnID := classifyCodexNotification(notification)
		if turnID != "" {
			r.mu.Lock()
			r.turnID = turnID
			r.mu.Unlock()
		}
		if kind == "completed" || kind == "failed" {
			r.mu.Lock()
			if turnID == "" || r.turnID == turnID {
				r.turnID = ""
			}
			r.mu.Unlock()
		}
		if text == "" {
			continue
		}
		_, _ = s.Buf.Write([]byte(text + "\n"))
		d.touchSession(context.Background(), s)
		select {
		case <-ctx.Done():
			return
		case d.codexEvents <- CodexEvent{SessionID: s.ID, Kind: kind, Text: text}:
		default:
			d.Log.Warn("dropping Codex event", "session", s.ID)
		}
	}
}
func classifyCodexNotification(n codexapp.Notification) (string, string, string) {
	var payload any
	_ = json.Unmarshal(n.Params, &payload)
	turnID := findJSONText(payload, "turnId")
	switch n.Method {
	case "item/agentMessage/delta":
		return "progress", findJSONText(payload, "delta"), turnID
	case "turn/started":
		return "progress", "Codex is working…", firstNonEmpty(turnID, findJSONText(payload, "id"))
	case "turn/completed":
		status := findJSONText(payload, "status")
		if status == "" {
			status = "completed"
		}
		return "completed", "Codex turn " + status + ".", turnID
	case "item/started":
		return "progress", codexItemSummary(payload, "started"), turnID
	case "item/completed":
		return "progress", codexItemSummary(payload, "completed"), turnID
	case "error":
		return "failed", firstNonEmpty(findJSONText(payload, "message"), "Codex reported an error."), turnID
	default:
		return "", "", turnID
	}
}
func codexItemSummary(value any, state string) string {
	typ := findJSONText(value, "type")
	if typ == "" || typ == "agentMessage" {
		return ""
	}
	return "Codex " + typ + " " + state + "."
}
func findJSONText(value any, key string) string {
	switch x := value.(type) {
	case map[string]any:
		if v, ok := x[key].(string); ok && v != "" {
			return v
		}
		for _, v := range x {
			if out := findJSONText(v, key); out != "" {
				return out
			}
		}
	case []any:
		for _, v := range x {
			if out := findJSONText(v, key); out != "" {
				return out
			}
		}
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (d *Daemon) RespondCodexRequest(ctx context.Context, sessionID string, id json.RawMessage, result any) error {
	r, err := d.codexRuntime(sessionID)
	if err != nil {
		return err
	}
	key := string(id)
	r.mu.Lock()
	if r.responded[key] {
		r.mu.Unlock()
		return nil
	}
	if !r.pending[key] {
		r.mu.Unlock()
		return errors.New("Codex request is no longer pending")
	}
	r.responded[key] = true
	delete(r.pending, key)
	r.mu.Unlock()
	if err := r.client.Respond(id, result); err != nil {
		r.mu.Lock()
		delete(r.responded, key)
		r.pending[key] = true
		r.mu.Unlock()
		return err
	}
	d.audit(ctx, "codex.decision", sessionID, key, 0, "response sent")
	return nil
}
func (d *Daemon) sendCodexTurn(ctx context.Context, sessionID, text string) (string, error) {
	s, err := d.sessionByID(sessionID)
	if err != nil {
		return "", err
	}
	r, err := d.codexRuntime(sessionID)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	threadID, activeTurnID := r.threadID, r.turnID
	r.mu.Unlock()
	if activeTurnID != "" {
		if err := r.client.SteerTurn(ctx, threadID, activeTurnID, text); err != nil {
			return "", err
		}
		d.audit(ctx, "codex.turn.steer", s.ID, text, 0, "turn="+activeTurnID)
		d.Log.Info("Codex turn steered", "session", s.ID, "turn", activeTurnID)
		return "Codex turn steered.", nil
	}
	turnID, err := r.client.StartTurn(ctx, threadID, text)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.turnID = turnID
	r.mu.Unlock()
	d.audit(ctx, "codex.turn.start", s.ID, text, 0, "turn="+turnID)
	d.Log.Info("Codex turn started", "session", s.ID, "turn", turnID)
	return "Codex turn started.", nil
}
func (d *Daemon) interruptCodexTurn(ctx context.Context, sessionID string) error {
	r, err := d.codexRuntime(sessionID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	threadID, turnID := r.threadID, r.turnID
	r.mu.Unlock()
	if turnID == "" {
		return errors.New("no active Codex turn")
	}
	return r.client.Interrupt(ctx, threadID, turnID)
}
func (d *Daemon) killCodexSession(ctx context.Context, sessionID string) error {
	r, err := d.codexRuntime(sessionID)
	if err != nil {
		return err
	}
	_ = r.client.Close()
	d.codexMu.Lock()
	delete(d.codex, sessionID)
	d.codexMu.Unlock()
	if s, err := d.sessionByID(sessionID); err == nil {
		d.markSessionEndedReason(ctx, s, "ended by /kill")
	}
	return nil
}
func (d *Daemon) codexRuntime(id string) (*codexRuntime, error) {
	d.codexMu.Lock()
	r := d.codex[id]
	d.codexMu.Unlock()
	if r == nil {
		return nil, errors.New("Codex runtime unavailable")
	}
	return r, nil
}
func (d *Daemon) restoreCodexSession(ctx context.Context, row store.SessionEntry) error {
	if d.DB == nil {
		return errors.New("state database required")
	}
	threadID, ok, err := d.DB.KVGetString(ctx, codexThreadPrefix+row.ID)
	if err != nil || !ok || threadID == "" {
		return errors.New("stored Codex thread unavailable")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		return err
	}
	client, err := codexapp.Start(ctx, codexapp.Options{Command: path, Dir: row.CWD})
	if err != nil {
		return err
	}
	if err := client.ResumeThread(ctx, threadID); err != nil {
		_ = client.Close()
		return err
	}
	s := newSessionAt(row.ID, row.Name, "codex", d.bufferSize(), row.StartedAt, row.LastActivity)
	s.Transport = "codex"
	s.Cmd = "codex app-server"
	s.CWD = row.CWD
	if err := d.Registry.Add(s); err != nil {
		_ = client.Close()
		return err
	}
	d.addCodexRuntime(ctx, s, client, threadID)
	d.audit(ctx, "session.restore", s.ID, "", 0, "agent=codex")
	return nil
}
