package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Notification struct {
	Method string
	Params json.RawMessage
}
type wire struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type reply struct {
	data json.RawMessage
	err  error
}
type document struct {
	version uint64
	source  string
}
type Client struct {
	cmd         *exec.Cmd
	input       io.WriteCloser
	writer      sync.Mutex
	mu          sync.Mutex
	next        int
	pending     map[int]chan reply
	docs        map[string]document
	syncMu      sync.Mutex
	incremental bool
	notify      func(Notification)
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	closeOnce   sync.Once
}

const maxMessage = 8 << 20

func ReadMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	headers := 0
	for {
		line, err := r.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		headers += len(line)
		if headers > 32<<10 {
			return nil, errors.New("LSP header too large")
		}
		s := strings.TrimSpace(string(line))
		if s == "" {
			break
		}
		key, value, ok := strings.Cut(s, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 || length > maxMessage {
		return nil, errors.New("invalid LSP content length")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(r, data)
	return data, err
}
func Start(parent context.Context, argv []string, root string, notify func(Notification)) (*Client, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty server command")
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	c := &Client{cmd: cmd, input: stdin, pending: map[int]chan reply{}, docs: map[string]document{}, notify: notify, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if err = cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	go c.read(stdout)
	go func() { _ = cmd.Wait(); c.fail(errors.New("language server exited")); close(c.done) }()
	initCtx, stop := context.WithTimeout(ctx, 8*time.Second)
	defer stop()
	params := map[string]any{"processId": os.Getpid(), "rootUri": URI(root), "clientInfo": map[string]string{"name": "shack"}, "workspaceFolders": []any{map[string]string{"uri": URI(root), "name": root}}, "capabilities": map[string]any{
		"general": map[string]any{"positionEncodings": []string{"utf-16"}}, "workspace": map[string]any{"configuration": true, "workspaceEdit": map[string]any{"documentChanges": true, "failureHandling": "transactional"}},
		"textDocument": map[string]any{"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": true}}, "hover": map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
			"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
			"codeAction":     map[string]any{"codeActionLiteralSupport": map[string]any{"codeActionKind": map[string]any{"valueSet": []string{"quickfix", "refactor", "source.organizeImports"}}}, "resolveSupport": map[string]any{"properties": []string{"edit"}}},
			"definition":     map[string]any{"linkSupport": true}, "publishDiagnostics": map[string]any{"versionSupport": true}},
	}}
	result, err := c.Request(initCtx, "initialize", params)
	if err != nil {
		c.Close()
		return nil, err
	}
	var response struct {
		Capabilities struct {
			PositionEncoding string          `json:"positionEncoding"`
			TextDocumentSync json.RawMessage `json:"textDocumentSync"`
		} `json:"capabilities"`
	}
	if err = json.Unmarshal(result, &response); err != nil {
		c.Close()
		return nil, err
	}
	enc := response.Capabilities.PositionEncoding
	if enc != "" && enc != "utf-16" {
		c.Close()
		return nil, fmt.Errorf("unsupported server position encoding %s", enc)
	}
	var kind int
	if json.Unmarshal(response.Capabilities.TextDocumentSync, &kind) != nil {
		var options struct {
			Change int `json:"change"`
		}
		_ = json.Unmarshal(response.Capabilities.TextDocumentSync, &options)
		kind = options.Change
	}
	c.incremental = kind == 2
	if err = c.Notify("initialized", map[string]any{}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Client) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > maxMessage {
		return errors.New("LSP outgoing message exceeds 8 MiB")
	}
	c.writer.Lock()
	defer c.writer.Unlock()
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	_, err = fmt.Fprintf(c.input, "Content-Length: %d\r\n\r\n", len(data))
	if err != nil {
		return err
	}
	_, err = c.input.Write(data)
	return err
}
func (c *Client) Notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (c *Client) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	// The writer can block on an unresponsive process. Cancellation terminates
	// that process rather than leaving a permanently blocked writer goroutine.
	sent := make(chan error, 1)
	go func() { sent <- c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}) }()
	select {
	case err := <-sent:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		c.cancel()
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
	select {
	case response := <-ch:
		return response.data, response.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}
func (c *Client) read(r io.Reader) {
	reader := bufio.NewReaderSize(r, 32<<10)
	for {
		data, err := ReadMessage(reader)
		if err != nil {
			c.fail(err)
			return
		}
		var m wire
		if err = json.Unmarshal(data, &m); err != nil {
			c.fail(err)
			return
		}
		if m.Method != "" {
			if len(m.ID) > 0 {
				result := any(nil)
				var responseErr *rpcError
				switch m.Method {
				case "workspace/configuration":
					var p struct {
						Items []any `json:"items"`
					}
					_ = json.Unmarshal(m.Params, &p)
					result = make([]any, len(p.Items))
				case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
				case "workspace/applyEdit":
					result = map[string]any{"applied": false, "failureReason": "Use a previewable Shack editing command"}
				default:
					responseErr = &rpcError{-32601, "Unsupported client request"}
				}
				response := map[string]any{"jsonrpc": "2.0", "id": m.ID}
				if responseErr != nil {
					response["error"] = responseErr
				} else {
					response["result"] = result
				}
				go func() { _ = c.send(response) }()
			} else if c.notify != nil {
				c.notify(Notification{m.Method, m.Params})
			}
			continue
		}
		var id int
		if json.Unmarshal(m.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			r := reply{data: m.Result}
			if m.Error != nil {
				r.err = fmt.Errorf("LSP: %s", m.Error.Message)
			}
			select {
			case ch <- r:
			default:
			}
		}
	}
}
func (c *Client) fail(err error) {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.pending {
		select {
		case ch <- reply{err: err}:
		default:
		}
	}
}
func (c *Client) Sync(uri, language, source string, version uint64) error {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	old, exists := c.docs[uri]
	if exists && old.version >= version {
		return nil
	}
	var err error
	if !exists {
		err = c.Notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": language, "version": version, "text": source}})
	} else {
		change := map[string]any{"text": source}
		if c.incremental {
			change = Change(old.source, source)
		}
		err = c.Notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{change}})
	}
	if err == nil {
		c.docs[uri] = document{version, source}
	}
	return err
}
func (c *Client) Forget(uri string) {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if _, ok := c.docs[uri]; ok {
		_ = c.Notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}})
		delete(c.docs, uri)
	}
}
func (c *Client) Alive() bool { return c.ctx.Err() == nil }
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		if c.cmd.Process != nil {
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		}
		c.input.Close()
	})
}
