// Package testutil holds subprocess fixtures imported only by tests.
package testutil

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/gongahkia/shack/internal/lsp"
	"os"
)

// ActionServer is a framed, bidirectional LSP fixture. It keeps accepting
// requests while an executeCommand is waiting for the client's edit review.
func ActionServer() {
	reader := bufio.NewReader(os.Stdin)
	send := func(m any) {
		b, _ := json.Marshal(m)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	respond := func(id json.RawMessage, result any) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	var pending json.RawMessage
	for {
		body, err := lsp.ReadMessage(reader)
		if err != nil {
			return
		}
		var m struct {
			ID             json.RawMessage `json:"id"`
			Method         string          `json:"method"`
			Params, Result json.RawMessage
		}
		if json.Unmarshal(body, &m) != nil {
			return
		}
		switch m.Method {
		case "initialize":
			respond(m.ID, map[string]any{"capabilities": map[string]any{"textDocumentSync": 2, "codeActionProvider": map[string]bool{"resolveProvider": true}}})
		case "codeAction/resolve":
			var action map[string]any
			json.Unmarshal(m.Params, &action)
			if action["kind"] != "quickfix" || action["customField"] != "preserved" {
				respond(m.ID, nil)
				continue
			}
			action["command"] = map[string]any{"command": "fixture.apply", "arguments": []any{action["data"]}}
			respond(m.ID, action)
		case "workspace/executeCommand":
			var p struct {
				Command   string            `json:"command"`
				Arguments []json.RawMessage `json:"arguments"`
			}
			json.Unmarshal(m.Params, &p)
			if p.Command == "fixture.hang" {
				pending = m.ID
				continue
			}
			pending = m.ID
			if len(p.Arguments) == 0 {
				respond(m.ID, nil)
				continue
			}
			var params any
			json.Unmarshal(p.Arguments[0], &params)
			send(map[string]any{"jsonrpc": "2.0", "id": "review", "method": "workspace/applyEdit", "params": params})
			if p.Command == "fixture.cancel" {
				send(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": "review"}})
			}
		case "fixture.ping":
			respond(m.ID, "pong")
		case "$/cancelRequest":
			send(map[string]any{"jsonrpc": "2.0", "method": "fixture.cancelled", "params": m.Params})
		case "":
			if string(m.ID) == `"review"` && len(pending) > 0 {
				respond(pending, m.Result)
				pending = nil
			}
		}
	}
}
