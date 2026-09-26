package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestFakeAppServer(t *testing.T) {
	if os.Getenv("ONIBI_FAKE_APP_SERVER") != "1" {
		return
	}
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	enc := json.NewEncoder(os.Stdout)
	for {
		var message map[string]json.RawMessage
		if err := dec.Decode(&message); err != nil {
			return
		}
		var method string
		_ = json.Unmarshal(message["method"], &method)
		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{"id": json.RawMessage(message["id"]), "result": map[string]any{"capabilities": map[string]any{}}})
		case "thread/start":
			_ = enc.Encode(map[string]any{"id": json.RawMessage(message["id"]), "result": map[string]any{"thread": map[string]string{"id": "thread-1"}}})
		case "turn/start":
			_ = enc.Encode(map[string]any{"id": json.RawMessage(message["id"]), "result": map[string]any{"turn": map[string]string{"id": "turn-1"}}})
			_ = enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]string{"delta": "working", "turnId": "turn-1"}})
			_ = enc.Encode(map[string]any{"id": "approval-1", "method": "item/commandExecution/requestApproval", "params": map[string]string{"command": "pwd"}})
		case "turn/steer":
			_ = enc.Encode(map[string]any{"id": json.RawMessage(message["id"]), "result": map[string]string{"turnId": "turn-1"}})
		}
	}
}

func TestStartThreadTurnAndServerRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	t.Setenv("ONIBI_FAKE_APP_SERVER", "1")
	client, err := Start(ctx, Options{Command: os.Args[0], Args: []string{"-test.run=TestFakeAppServer"}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	thread, err := client.StartThread(ctx, "/tmp")
	if err != nil || thread != "thread-1" {
		t.Fatalf("thread=%q err=%v", thread, err)
	}
	turn, err := client.StartTurn(ctx, thread, "hello")
	if err != nil || turn != "turn-1" {
		t.Fatalf("turn=%q err=%v", turn, err)
	}
	if err := client.SteerTurn(ctx, thread, turn, "continue"); err != nil {
		t.Fatal(err)
	}
	select {
	case notification := <-client.Notifications:
		if notification.Method != "item/agentMessage/delta" {
			t.Fatalf("notification=%#v", notification)
		}
	case <-ctx.Done():
		t.Fatal("notification timeout")
	}
	select {
	case request := <-client.Requests:
		if request.Method != "item/commandExecution/requestApproval" || string(request.ID) != `"approval-1"` {
			t.Fatalf("request=%#v", request)
		}
		if err := client.Respond(request.ID, map[string]string{"decision": "decline"}); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("request timeout")
	}
}
