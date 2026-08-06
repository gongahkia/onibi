package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

type Options struct {
	Command string
	Args    []string
	Dir     string
	Version string
}
type RPCError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func (e RPCError) Error() string { return e.Message }

type ServerRequest struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}
type Notification struct {
	Method string
	Params json.RawMessage
}
type response struct {
	Result json.RawMessage
	Error  *RPCError
}
type wireMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type Client struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	writeMu       sync.Mutex
	nextID        atomic.Int64
	pendingMu     sync.Mutex
	pending       map[int64]chan response
	Requests      chan ServerRequest
	Notifications chan Notification
	Done          chan error
}

func Start(ctx context.Context, opts Options) (*Client, error) {
	command := opts.Command
	if command == "" {
		command = "codex"
	}
	args := opts.Args
	if len(args) == 0 {
		args = []string{"app-server"}
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = opts.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, stdin: stdin, pending: map[int64]chan response{}, Requests: make(chan ServerRequest, 64), Notifications: make(chan Notification, 256), Done: make(chan error, 1)}
	go c.read(stdout)
	go func() {
		err := cmd.Wait()
		c.failPending(err)
		c.Done <- err
		close(c.Done)
		close(c.Requests)
		close(c.Notifications)
	}()
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	if _, err := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "onibi", "title": "Onibi", "version": version}, "capabilities": map[string]any{"experimentalApi": true}}); err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) StartThread(ctx context.Context, cwd string) (string, error) {
	result, err := c.Call(ctx, "thread/start", map[string]any{"cwd": cwd, "approvalPolicy": "on-request", "approvalsReviewer": "user"})
	if err != nil {
		return "", err
	}
	return threadID(result)
}
func (c *Client) ResumeThread(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("thread id required")
	}
	_, err := c.Call(ctx, "thread/resume", map[string]any{"threadId": id, "approvalPolicy": "on-request", "approvalsReviewer": "user"})
	return err
}
func threadID(raw json.RawMessage) (string, error) {
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Thread.ID == "" {
		return "", errors.New("app-server returned no thread id")
	}
	return result.Thread.ID, nil
}
func (c *Client) StartTurn(ctx context.Context, threadID, text string) (string, error) {
	if threadID == "" || text == "" {
		return "", errors.New("thread id and text required")
	}
	result, err := c.Call(ctx, "turn/start", map[string]any{"threadId": threadID, "input": []map[string]string{{"type": "text", "text": text}}})
	if err != nil {
		return "", err
	}
	var decoded struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		return "", err
	}
	if decoded.Turn.ID == "" {
		return "", errors.New("app-server returned no turn id")
	}
	return decoded.Turn.ID, nil
}
func (c *Client) Interrupt(ctx context.Context, threadID, turnID string) error {
	if threadID == "" || turnID == "" {
		return errors.New("thread and turn id required")
	}
	_, err := c.Call(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID})
	return err
}
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("app-server client nil")
	}
	id := c.nextID.Add(1)
	ch := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	if err := c.write(wireMessage{ID: json.RawMessage(fmt.Sprintf("%d", id)), Method: method, Params: mustJSON(params)}); err != nil {
		c.removePending(id)
		return nil, err
	}
	select {
	case reply := <-ch:
		if reply.Error != nil {
			return nil, *reply.Error
		}
		return reply.Result, nil
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	}
}
func (c *Client) Notify(method string, params any) error {
	return c.write(wireMessage{Method: method, Params: mustJSON(params)})
}
func (c *Client) Respond(id json.RawMessage, result any) error {
	if len(id) == 0 {
		return errors.New("request id required")
	}
	return c.write(wireMessage{ID: id, Result: mustJSON(result)})
}
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		err := c.cmd.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}
func (c *Client) read(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var message wireMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if message.Method != "" && len(message.ID) > 0 {
			c.Requests <- ServerRequest{ID: message.ID, Method: message.Method, Params: message.Params}
			continue
		}
		if message.Method != "" {
			c.Notifications <- Notification{Method: message.Method, Params: message.Params}
			continue
		}
		var id int64
		if json.Unmarshal(message.ID, &id) != nil {
			continue
		}
		c.pendingMu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.pendingMu.Unlock()
		if ch != nil {
			ch <- response{Result: message.Result, Error: message.Error}
		}
	}
}
func (c *Client) write(message wireMessage) error {
	if c == nil || c.stdin == nil {
		return errors.New("app-server stdin unavailable")
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(raw, '\n'))
	return err
}
func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}
func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- response{Error: &RPCError{Message: fmt.Sprintf("app-server stopped: %v", err)}}
		delete(c.pending, id)
	}
}
func mustJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
