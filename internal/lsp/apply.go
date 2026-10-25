package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type ApplyEditParams struct {
	Label string          `json:"label"`
	Edit  json.RawMessage `json:"edit"`
}
type ApplyEditResult struct {
	Applied       bool   `json:"applied"`
	FailureReason string `json:"failureReason,omitempty"`
}

// ApplyEditHandler runs off the reader goroutine. Implementations must observe
// ctx while awaiting user review, and must not publish edits after cancellation.
type ApplyEditHandler func(context.Context, ApplyEditParams) ApplyEditResult

// Only small protocol replies/cancellation notices enter this queue. Keeping a
// single writer prevents a server request flood from creating blocked goroutines.
func (c *Client) queueReply(response any) {
	select {
	case c.replies <- response:
	case <-c.ctx.Done():
	default:
		c.fail(errors.New("language server reply queue exceeded"))
	}
}

func (c *Client) handleApplyEdit(m wire) {
	respond := func(result ApplyEditResult) {
		c.queueReply(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
	if c.applyEdit == nil {
		respond(ApplyEditResult{FailureReason: "No workspace edit reviewer is available"})
		return
	}
	var p ApplyEditParams
	if json.Unmarshal(m.Params, &p) != nil || len(p.Edit) == 0 || string(p.Edit) == "null" {
		respond(ApplyEditResult{FailureReason: "Invalid workspace edit request"})
		return
	}
	id := string(m.ID)
	c.mu.Lock()
	if len(c.serverCalls) >= 4 || c.serverCalls[id] != nil || c.serverBytes+len(m.Params) > maxMessage {
		c.mu.Unlock()
		respond(ApplyEditResult{FailureReason: "Another workspace edit is awaiting review"})
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Minute)
	c.serverCalls[id] = cancel
	c.serverBytes += len(m.Params)
	c.mu.Unlock()
	go func() {
		defer cancel()
		result := c.applyEdit(ctx, p)
		c.mu.Lock()
		delete(c.serverCalls, id)
		c.serverBytes -= len(m.Params)
		c.mu.Unlock()
		respond(result)
	}()
}

func (c *Client) cancelServerRequest(params json.RawMessage) {
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	cancel := c.serverCalls[string(p.ID)]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
