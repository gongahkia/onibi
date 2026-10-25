// Package dap implements a bounded Debug Adapter Protocol transport. Debug
// adapters are optional child processes or explicitly configured TCP endpoints.
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

const maxMessage = 8 << 20
const maxOutput = 1 << 20

type Message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success"`
	Command    string          `json:"command,omitempty"`
	Event      string          `json:"event,omitempty"`
	Message    string          `json:"message,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}
type reply struct {
	body json.RawMessage
	err  error
}
type outgoing struct {
	ctx  context.Context
	data []byte
}
type Client struct {
	ctx                          context.Context
	cancel                       context.CancelFunc
	done                         chan struct{}
	once                         sync.Once
	mu                           sync.Mutex
	next                         int
	pending                      map[int]chan reply
	err                          error
	writes                       chan outgoing
	queued                       atomic.Int64
	Events                       chan Message
	OutputReady                  chan struct{}
	output                       []byte
	outputPosition, outputLength int
	SupportsCancel               atomic.Bool
}
type Capabilities struct {
	ConfigurationDone         bool `json:"supportsConfigurationDoneRequest"`
	ConditionalBreakpoints    bool `json:"supportsConditionalBreakpoints"`
	HitConditionalBreakpoints bool `json:"supportsHitConditionalBreakpoints"`
	LogPoints                 bool `json:"supportsLogPoints"`
	Evaluate                  bool `json:"supportsEvaluateForHovers"`
	Terminate                 bool `json:"supportsTerminateRequest"`
	Cancel                    bool `json:"supportsCancelRequest"`
}

func Start(parent context.Context, argv []string, root string) (*Client, error) {
	if len(argv) == 0 {
		return nil, errors.New("no debug adapter command")
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stdout.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		stdin.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		stdout.Close()
		stdin.Close()
		stderr.Close()
		return nil, err
	}
	exited := atomic.Bool{}
	c := connect(parent, stdout, stdin, func() {
		if !exited.Load() {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		stderr.Close()
	})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, e := stderr.Read(buf)
			if n > 0 {
				c.appendOutput("[adapter] " + string(buf[:n]))
			}
			if e != nil {
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		exited.Store(true)
		if err == nil {
			err = errors.New("debug adapter exited")
		}
		c.fail(err)
	}()
	return c, nil
}
func Dial(parent context.Context, address string) (*Client, error) {
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(parent, "tcp", address)
	if err != nil {
		return nil, err
	}
	return connect(parent, conn, conn, func() {}), nil
}
func connect(parent context.Context, r io.ReadCloser, w io.WriteCloser, stop func()) *Client {
	ctx, cancel := context.WithCancel(parent)
	c := &Client{ctx: ctx, cancel: cancel, done: make(chan struct{}), pending: map[int]chan reply{}, writes: make(chan outgoing, 16), Events: make(chan Message, 64), OutputReady: make(chan struct{}, 1)}
	go func() { <-ctx.Done(); r.Close(); w.Close(); stop(); c.fail(ctx.Err()) }()
	go c.read(r)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case item := <-c.writes:
				c.queued.Add(-int64(len(item.data)))
				if item.ctx.Err() != nil {
					continue
				}
				if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(item.data)); err != nil {
					c.fail(err)
					return
				}
				if _, err := w.Write(item.data); err != nil {
					c.fail(err)
					return
				}
			}
		}
	}()
	return c
}
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Close()                { c.fail(errors.New("debug adapter closed")) }
func (c *Client) Err() error            { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *Client) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		for id, ch := range c.pending {
			ch <- reply{err: err}
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.done)
		c.cancel()
	})
}
func (c *Client) send(ctx context.Context, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("DAP request exceeds 1 MiB")
	}
	n := int64(len(data))
	if c.queued.Add(n) > 2<<20 {
		c.queued.Add(-n)
		return errors.New("DAP write queue exceeds 2 MiB")
	}
	select {
	case c.writes <- outgoing{ctx, data}:
		return nil
	case <-ctx.Done():
		c.queued.Add(-n)
		return ctx.Err()
	case <-c.done:
		c.queued.Add(-n)
		return c.Err()
	default:
		c.queued.Add(-n)
		return errors.New("DAP write queue full")
	}
}
func (c *Client) sequence() int { c.mu.Lock(); defer c.mu.Unlock(); c.next++; return c.next }
func (c *Client) Request(ctx context.Context, command string, args any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ch := make(chan reply, 1)
	c.mu.Lock()
	if c.err != nil {
		err = c.err
		c.mu.Unlock()
		return nil, err
	}
	if len(c.pending) >= 32 {
		c.mu.Unlock()
		return nil, errors.New("too many pending DAP requests")
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err = c.send(ctx, Message{Seq: id, Type: "request", Command: command, Arguments: raw}); err != nil {
		return nil, err
	}
	select {
	case response := <-ch:
		return response.body, response.err
	case <-c.done:
		return nil, c.Err()
	case <-ctx.Done():
		if c.SupportsCancel.Load() {
			raw, _ := json.Marshal(map[string]int{"requestId": id})
			// Cancellation cannot wait behind a stuck adapter. The session remains
			// usable and late replies are discarded whether cancellation is supported.
			_ = c.send(c.ctx, Message{Seq: c.sequence(), Type: "request", Command: "cancel", Arguments: raw})
		}
		return nil, ctx.Err()
	}
}
func (c *Client) Initialize(ctx context.Context) (Capabilities, error) {
	body, err := c.Request(ctx, "initialize", map[string]any{"clientID": "shack", "clientName": "Shack", "adapterID": "shack", "locale": "en-US", "linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path", "supportsVariableType": true, "supportsVariablePaging": true, "supportsRunInTerminalRequest": false, "supportsProgressReporting": false, "supportsInvalidatedEvent": false})
	var caps Capabilities
	if err == nil {
		err = json.Unmarshal(body, &caps)
	}
	if err == nil {
		c.SupportsCancel.Store(caps.Cancel)
	}
	return caps, err
}
func (c *Client) Disconnect(ctx context.Context, terminate bool) error {
	defer c.Close()
	_, err := c.Request(ctx, "disconnect", map[string]any{"restart": false, "terminateDebuggee": terminate})
	return err
}
func (c *Client) Output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outputLength == 0 {
		return ""
	}
	start := (c.outputPosition - c.outputLength + maxOutput) % maxOutput
	result := make([]byte, c.outputLength)
	n := copy(result, c.output[start:min(maxOutput, start+c.outputLength)])
	copy(result[n:], c.output[:c.outputLength-n])
	for len(result) > 0 && !utf8.RuneStart(result[0]) {
		result = result[1:]
	}
	return strings.ToValidUTF8(string(result), "�")
}
func (c *Client) appendOutput(value string) {
	c.mu.Lock()
	if c.output == nil {
		c.output = make([]byte, maxOutput)
	}
	if len(value) > maxOutput {
		value = value[len(value)-maxOutput:]
	}
	n := copy(c.output[c.outputPosition:], value)
	copy(c.output, value[n:])
	c.outputPosition = (c.outputPosition + len(value)) % maxOutput
	c.outputLength = min(maxOutput, c.outputLength+len(value))
	c.mu.Unlock()
	select {
	case c.OutputReady <- struct{}{}:
	default:
	}
}
func (c *Client) read(r io.Reader) {
	reader := bufio.NewReader(r)
	for {
		data, err := readMessage(reader)
		if err != nil {
			c.fail(err)
			return
		}
		var m Message
		if err = json.Unmarshal(data, &m); err != nil {
			c.fail(err)
			return
		}
		switch m.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[m.RequestSeq]
			delete(c.pending, m.RequestSeq)
			c.mu.Unlock()
			if ch != nil {
				var e error
				if !m.Success {
					e = fmt.Errorf("%s: %s", m.Command, m.Message)
				}
				ch <- reply{m.Body, e}
			}
		case "event":
			if m.Event == "output" {
				var out struct {
					Output string `json:"output"`
				}
				if json.Unmarshal(m.Body, &out) == nil {
					c.appendOutput(out.Output)
				}
				continue
			}
			if len(m.Body) > 64<<10 {
				c.fail(errors.New("DAP event exceeds 64 KiB"))
				return
			}
			select {
			case c.Events <- m:
			default:
				c.fail(errors.New("DAP event queue overflow"))
				return
			}
		case "request":
			// Never silently execute an adapter-supplied shell command or start a
			// second debug session. Clients advertise these reverse requests as unsupported.
			response := Message{Seq: c.sequence(), Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: false, Message: "Shack does not support this reverse request; use an internal-console launch configuration"}
			err = c.send(c.ctx, response)
			if err != nil {
				c.fail(err)
				return
			}
		default:
			c.fail(errors.New("invalid DAP message type"))
			return
		}
	}
}
func readMessage(r *bufio.Reader) ([]byte, error) {
	length, headers := -1, 0
	for {
		line, err := r.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		headers += len(line)
		if headers > 32<<10 {
			return nil, errors.New("DAP header too large")
		}
		s := strings.TrimSpace(string(line))
		if s == "" {
			break
		}
		key, value, ok := strings.Cut(s, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return nil, errors.New("duplicate DAP content length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 || length > maxMessage {
		return nil, errors.New("invalid DAP content length")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(r, data)
	return data, err
}
