package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPositionConversionAndIncrementalChanges(t *testing.T) {
	source := "a😀é\r\n你好\n"
	for _, offset := range []int{0, 1, 5, 7, 9, 12, 15, 16} {
		pos := PositionAt(source, offset)
		got, err := Offset(source, pos)
		if err != nil || got != offset {
			t.Fatalf("offset %d -> %+v -> %d: %v", offset, pos, got, err)
		}
	}
	if _, err := Offset(source, Position{0, 2}); err == nil {
		t.Fatal("surrogate split accepted")
	}
	for _, pair := range [][2]string{{"a😀z", "a😁z"}, {"a\r\nb", "a\nb"}, {"a\nb", "a\r\nb"}, {"abc", ""}, {"", "你好"}, {"αβγ", "α🐱γ"}} {
		change := Change(pair[0], pair[1])
		r := change["range"].(Range)
		start, e := Offset(pair[0], r.Start)
		if e != nil {
			t.Fatal(e)
		}
		end, e := Offset(pair[0], r.End)
		if e != nil {
			t.Fatal(e)
		}
		got := pair[0][:start] + change["text"].(string) + pair[0][end:]
		if got != pair[1] {
			t.Fatalf("%q -> %q: %q", pair[0], pair[1], got)
		}
	}
}
func TestFramingRejectsOversizedMessages(t *testing.T) {
	for _, wire := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 999999999\r\n\r\n", "Content-Length: 5\r\n\r\nx", strings.Repeat("x", 40000) + "\n"} {
		if _, err := ReadMessage(bufio.NewReaderSize(strings.NewReader(wire), 32<<10)); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	data, err := ReadMessage(bufio.NewReader(strings.NewReader("Content-Length: 2\r\n\r\n{}")))
	if err != nil || string(data) != "{}" {
		t.Fatal(err)
	}
}
func TestClientLifecycleAndRequests(t *testing.T) {
	t.Setenv("SHACK_TEST_SERVER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Notification, 4)
	c, err := Start(ctx, []string{os.Args[0], "-test.run=TestServerProcess"}, t.TempDir(), func(n Notification) { events <- n })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	uri := URI("/tmp/shack test.go")
	if err = c.Sync(uri, "go", "abc", 0); err != nil {
		t.Fatal(err)
	}
	if err = c.Sync(uri, "go", "a😀bc", 1); err != nil {
		t.Fatal(err)
	}
	request, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	result, err := c.Request(request, "textDocument/completion", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var items []Completion
	if json.Unmarshal(result, &items) != nil || len(items) != 1 || items[0].Label != "hello" {
		t.Fatal(string(result))
	}
	short, done := context.WithTimeout(ctx, 20*time.Millisecond)
	defer done()
	if _, err = c.Request(short, "hang", nil); err == nil {
		t.Fatal("request did not time out")
	}
	c.Close()
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("server did not exit")
	}
}
func TestServerProcess(t *testing.T) {
	if os.Getenv("SHACK_TEST_SERVER") != "1" {
		return
	}
	counts := map[string]int{}
	r := bufio.NewReader(os.Stdin)
	for {
		data, err := ReadMessage(r)
		if err != nil {
			os.Exit(0)
		}
		var m wire
		json.Unmarshal(data, &m)
		if len(m.ID) == 0 {
			continue
		}
		var retry struct {
			Retry bool `json:"fixtureRetry"`
		}
		json.Unmarshal(m.Params, &retry)
		if retry.Retry {
			counts[m.Method]++
			var response map[string]any
			if counts[m.Method] == 1 {
				notice, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "fixture.retry", "params": m.Method})
				fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(notice), notice)
				response = map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32801, "message": "content modified"}}
			} else {
				response = map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": counts[m.Method]}
			}
			encoded, _ := json.Marshal(response)
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(encoded), encoded)
			continue
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16", "textDocumentSync": 2}}
		case "textDocument/completion":
			result = []any{map[string]any{"label": "hello"}}
		case "fixture.diagnostics":
			var p struct {
				URI string `json:"uri"`
			}
			json.Unmarshal(m.Params, &p)
			event, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": p.URI, "diagnostics": []any{}}})
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(event), event)
		case "hang":
			continue
		default:
			result = nil
		}
		encoded, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(encoded), encoded)
	}
}

func TestInstalledClangd(t *testing.T) {
	clangd, err := exec.LookPath("clangd")
	if err != nil {
		t.Skip("clangd is optional")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.c")
	source := "int twice(int value){return value*2;}\nint main(){return twice(21);}\n"
	if err = os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Start(ctx, []string{clangd, "--background-index=false"}, dir, func(Notification) {})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Sync(URI(path), "c", source, 0); err != nil {
		t.Fatal(err)
	}
	pos := PositionAt(source, strings.LastIndex(source, "twice")+2)
	result, err := c.Request(ctx, "textDocument/definition", map[string]any{"textDocument": map[string]string{"uri": URI(path)}, "position": pos})
	if err != nil {
		t.Fatal(err)
	}
	var locations []Location
	if err = json.Unmarshal(result, &locations); err != nil || len(locations) == 0 {
		t.Fatalf("definition %s: %v", result, err)
	}
	for _, method := range []string{"hover", "references", "documentSymbol", "signatureHelp", "rename"} {
		params := map[string]any{"textDocument": map[string]string{"uri": URI(path)}, "position": pos}
		if method == "references" {
			params["context"] = map[string]bool{"includeDeclaration": true}
		}
		if method == "rename" {
			params["newName"] = "double_value"
		}
		if method == "signatureHelp" {
			params["position"] = PositionAt(source, strings.LastIndex(source, "21"))
		}
		response, e := c.Request(ctx, "textDocument/"+method, params)
		if e != nil || len(response) == 0 || string(response) == "null" {
			t.Fatalf("%s: %s %v", method, response, e)
		}
		if method == "rename" && !strings.Contains(string(response), "double_value") {
			t.Fatal(string(response))
		}
	}
	result, err = c.Request(ctx, "textDocument/formatting", map[string]any{"textDocument": map[string]string{"uri": URI(path)}, "options": map[string]any{"tabSize": 4, "insertSpaces": true}})
	if err != nil {
		t.Fatal(err)
	}
	var edits []TextEdit
	if err = json.Unmarshal(result, &edits); err != nil || len(edits) == 0 {
		t.Fatalf("formatting %s: %v", result, err)
	}
	if _, err = Edits(source, edits); err != nil {
		t.Fatal(err)
	}
	result, err = c.Request(ctx, "textDocument/completion", map[string]any{"textDocument": map[string]string{"uri": URI(path)}, "position": pos})
	if err != nil || !strings.Contains(string(result), "twice") {
		t.Fatalf("completion: %s %v", result, err)
	}
}

func TestDocumentEpochPreventsLateCloseAndSyncAfterReopen(t *testing.T) {
	t.Setenv("SHACK_TEST_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Start(ctx, []string{os.Args[0], "-test.run=TestServerProcess"}, t.TempDir(), func(Notification) {})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	uri := "file:///tmp/lifecycle.go"
	if err = c.Sync(uri, "go", "old", 9, 1); err != nil {
		t.Fatal(err)
	}
	c.Forget(uri, 1)
	if err = c.Sync(uri, "go", "new", 0, 2); err != nil {
		t.Fatal(err)
	}
	c.Forget(uri, 1)
	c.Sync(uri, "go", "late old", 10, 1)
	c.syncMu.Lock()
	doc := c.docs[uri]
	c.syncMu.Unlock()
	if doc.source != "new" || doc.epoch != 2 {
		t.Fatal(doc)
	}
}

func TestUnversionedDiagnosticMetadata(t *testing.T) {
	t.Setenv("SHACK_TEST_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := make(chan Notification, 8)
	c, e := Start(ctx, []string{os.Args[0], "-test.run=^TestServerProcess$"}, t.TempDir(), func(n Notification) { events <- n })
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	uri := URI(filepath.Join(t.TempDir(), "a.ts"))
	check := func(version *uint64, epoch uint64) {
		t.Helper()
		if _, e := c.Request(ctx, "fixture.diagnostics", map[string]string{"uri": uri}); e != nil {
			t.Fatal(e)
		}
		select {
		case n := <-events:
			if (version == nil) != (n.DocumentVersion == nil) || version != nil && (*version != *n.DocumentVersion || epoch != n.DocumentEpoch) {
				t.Fatalf("metadata: %#v", n)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	check(nil, 0)
	for _, v := range []uint64{1, 2} {
		if e = c.Sync(uri, "typescript", "const x=1", v, 7); e != nil {
			t.Fatal(e)
		}
		check(&v, 7)
	}
	c.Forget(uri, 7)
	check(nil, 0)
	v := uint64(0)
	if e = c.Sync(uri, "typescript", "const y=2", v, 8); e != nil {
		t.Fatal(e)
	}
	check(&v, 8)
}

func TestContentModifiedRetryDoesNotReplayCommands(t *testing.T) {
	t.Setenv("SHACK_TEST_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := Start(ctx, []string{os.Args[0], "-test.run=^TestServerProcess$"}, t.TempDir(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	params := map[string]bool{"fixtureRetry": true}
	result, e := c.Request(ctx, "textDocument/hover", params)
	if e != nil || string(result) != "2" {
		t.Fatal(string(result), e)
	}
	for _, method := range []string{"workspace/executeCommand", "codeAction/resolve"} {
		if _, e = c.Request(ctx, method, params); e == nil {
			t.Fatal("replayed", method)
		}
		result, e = c.Request(ctx, method, params)
		if e != nil || string(result) != "2" {
			t.Fatal("unexpected count", string(result), e)
		}
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if _, e = c.Request(short, "textDocument/references", params); e != context.DeadlineExceeded {
		t.Fatal(e)
	}
}

func TestContentModifiedRetryStopsAfterDocumentSync(t *testing.T) {
	t.Setenv("SHACK_TEST_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	notified := make(chan struct{}, 1)
	resume := make(chan struct{})
	c, e := Start(ctx, []string{os.Args[0], "-test.run=^TestServerProcess$"}, t.TempDir(), func(n Notification) {
		if n.Method == "fixture.retry" {
			notified <- struct{}{}
			select {
			case <-resume:
			case <-ctx.Done():
			}
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	done := make(chan error, 1)
	params := map[string]bool{"fixtureRetry": true}
	go func() { _, e := c.Request(ctx, "textDocument/definition", params); done <- e }()
	select {
	case <-notified:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e = c.Sync(URI(filepath.Join(t.TempDir(), "a.go")), "go", "changed", 1); e != nil {
		t.Fatal(e)
	}
	close(resume)
	select {
	case e = <-done:
		var rpc *rpcError
		if !errors.As(e, &rpc) || rpc.Code != -32801 {
			t.Fatal("retried stale position", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result, e := c.Request(ctx, "textDocument/definition", params)
	if e != nil || string(result) != "2" {
		t.Fatal(string(result), e)
	}
}
