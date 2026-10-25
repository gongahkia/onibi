package dap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func debugPython(t *testing.T) string {
	t.Helper()
	python := os.Getenv("SHACK_DEBUGPY_PYTHON")
	if python == "" {
		t.Skip("set SHACK_DEBUGPY_PYTHON to a Python with debugpy for live DAP checks")
	}
	return python
}
func request(t *testing.T, c *Client, command string, args any, out any) {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	body, err := c.Request(ctx, command, args)
	if err != nil {
		t.Fatalf("%s: %v\n%s", command, err, c.Output())
	}
	if out != nil {
		if err = json.Unmarshal(body, out); err != nil {
			t.Fatal(command, err, string(body))
		}
	}
}
func event(t *testing.T, c *Client, name string) Message {
	t.Helper()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-c.Events:
			if e.Event == name {
				return e
			}
		case <-c.Done():
			t.Fatalf("waiting for %s: %v\n%s", name, c.Err(), c.Output())
		case <-timer.C:
			t.Fatalf("no %s\n%s", name, c.Output())
		}
	}
}
func configure(t *testing.T, c *Client, kind string, args any, path string, line int) {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	caps, err := c.Initialize(ctx)
	if err != nil {
		t.Fatal(err, c.Output())
	}
	result := make(chan error, 1)
	go func() { _, e := c.Request(ctx, kind, args); result <- e }()
	event(t, c, "initialized")
	var response struct {
		Breakpoints []Breakpoint `json:"breakpoints"`
	}
	request(t, c, "setBreakpoints", map[string]any{"source": Source{Path: path}, "breakpoints": []SourceBreakpoint{{Line: line}}}, &response)
	if len(response.Breakpoints) != 1 {
		t.Fatal("breakpoint response", response)
	}
	if caps.ConfigurationDone {
		request(t, c, "configurationDone", map[string]any{}, nil)
	}
	if err := <-result; err != nil {
		t.Fatal(kind, err, c.Output())
	}
}
func stoppedFrame(t *testing.T, c *Client) (int, StackFrame) {
	t.Helper()
	e := event(t, c, "stopped")
	var stopped Stopped
	json.Unmarshal(e.Body, &stopped)
	var threads struct {
		Threads []Thread `json:"threads"`
	}
	request(t, c, "threads", nil, &threads)
	if len(threads.Threads) == 0 {
		t.Fatal("no debug threads")
	}
	id := stopped.ThreadID
	if id == 0 {
		id = threads.Threads[0].ID
	}
	var stack struct {
		Frames []StackFrame `json:"stackFrames"`
	}
	request(t, c, "stackTrace", map[string]int{"threadId": id, "startFrame": 0, "levels": 100}, &stack)
	if len(stack.Frames) == 0 {
		t.Fatal("no stack")
	}
	return id, stack.Frames[0]
}
func variables(t *testing.T, c *Client, frame StackFrame) map[string]string {
	var scopes struct {
		Scopes []Scope `json:"scopes"`
	}
	request(t, c, "scopes", map[string]int{"frameId": frame.ID}, &scopes)
	values := map[string]string{}
	for _, scope := range scopes.Scopes {
		if scope.Expensive {
			continue
		}
		var vars struct {
			Variables []Variable `json:"variables"`
		}
		request(t, c, "variables", map[string]int{"variablesReference": scope.Reference, "start": 0, "count": 100}, &vars)
		for _, v := range vars.Variables {
			values[v.Name] = v.Value
		}
	}
	return values
}
func TestLiveDebugpyLaunchStepVariablesAndExit(t *testing.T) {
	python := debugPython(t)
	root := t.TempDir()
	path := filepath.Join(root, "example.py")
	os.WriteFile(path, []byte("def add(a, b):\n    value = a + b\n    return value\nanswer = add(2, 3)\nprint('DAP_RESULT', answer)\n"), 0600)
	c, err := Start(context.Background(), []string{python, "-m", "debugpy.adapter"}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	configure(t, c, "launch", map[string]any{"program": path, "python": []string{python}, "console": "internalConsole", "redirectOutput": true, "justMyCode": true, "subProcess": false}, path, 2)
	thread, frame := stoppedFrame(t, c)
	if frame.Line != 2 || frame.Source.Path != path {
		t.Fatal(frame)
	}
	if got := variables(t, c, frame); got["a"] != "2" || got["b"] != "3" {
		t.Fatal(got)
	}
	request(t, c, "next", map[string]int{"threadId": thread}, nil)
	thread, frame = stoppedFrame(t, c)
	if got := variables(t, c, frame); got["value"] != "5" {
		t.Fatal(got)
	}
	request(t, c, "stepOut", map[string]int{"threadId": thread}, nil)
	thread, _ = stoppedFrame(t, c)
	request(t, c, "continue", map[string]int{"threadId": thread}, nil)
	event(t, c, "terminated")
	if !strings.Contains(c.Output(), "DAP_RESULT 5") {
		t.Fatal(c.Output())
	}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	_ = c.Disconnect(ctx, false)
}
func TestLiveDebugpyAttachDetachPreservesTarget(t *testing.T) {
	python := debugPython(t)
	root := t.TempDir()
	path := filepath.Join(root, "attach.py")
	portfile := filepath.Join(root, "port")
	code := fmt.Sprintf("import debugpy, pathlib, time\naddress = debugpy.listen(('127.0.0.1', 0))\npathlib.Path(%q).write_text(str(address[1]))\ndebugpy.wait_for_client()\nvalue = 42\nwhile True:\n    time.sleep(0.1)\n", portfile)
	os.WriteFile(path, []byte(code), 0600)
	target := exec.Command(python, path)
	target.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { syscall.Kill(-target.Process.Pid, syscall.SIGKILL); target.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	port := ""
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(portfile)
		port = string(data)
		if port != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if port == "" {
		t.Fatal("debug target did not listen")
	}
	c, err := Dial(context.Background(), "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	configure(t, c, "attach", map[string]any{"justMyCode": true, "subProcess": false}, path, 6)
	_, frame := stoppedFrame(t, c)
	if got := variables(t, c, frame); got["value"] != "42" {
		t.Fatal(got)
	}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err = c.Disconnect(ctx, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := target.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("detach killed attached target", err)
	}
}
