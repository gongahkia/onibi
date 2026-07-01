package cli

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestDemoScriptUsesShellAndDemoApproval(t *testing.T) {
	old := demoRPC
	defer func() { demoRPC = old }()
	var mu sync.Mutex
	var events []intake.Event
	demoRPC = func(_ context.Context, ev intake.Event) (intake.Response, error) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		switch ev.Type {
		case intake.TypeSessionNew:
			return intake.Response{SessionID: "demo1", Text: "started"}, nil
		case intake.TypeDemoApproval:
			return intake.Response{Decision: "approve"}, nil
		case intake.TypeSessionInput:
			return intake.Response{Text: "sent"}, nil
		case intake.TypeSessionShow:
			return intake.Response{SessionID: ev.Session, Text: "shown"}, nil
		case intake.TypeSessionHide:
			return intake.Response{SessionID: ev.Session, Text: "hidden"}, nil
		default:
			return intake.Response{Text: "ok"}, nil
		}
	}
	cmd := demoCmd()
	cmd.SetArgs([]string{"--duration=1ms"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "demo ready") {
		t.Fatalf("output = %q", out.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if !hasDemoEvent(events, intake.TypeSessionNew, "shell") {
		t.Fatalf("events missing shell session_new: %+v", events)
	}
	if !hasDemoEvent(events, intake.TypeDemoApproval, "demo") {
		t.Fatalf("events missing demo approval: %+v", events)
	}
	if !hasDemoApprovalRequest(events) {
		t.Fatalf("events missing nonblocking demo approval request: %+v", events)
	}
	if !hasDemoType(events, intake.TypeSessionShow) || !hasDemoType(events, intake.TypeSessionHide) {
		t.Fatalf("events missing handover: %+v", events)
	}
	for _, ev := range events {
		if ev.Agent == "claude" {
			t.Fatalf("demo must not require claude: %+v", events)
		}
	}
}

func hasDemoEvent(events []intake.Event, typ, agent string) bool {
	for _, ev := range events {
		if ev.Type == typ && ev.Agent == agent {
			return true
		}
	}
	return false
}

func hasDemoType(events []intake.Event, typ string) bool {
	for _, ev := range events {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

func hasDemoApprovalRequest(events []intake.Event) bool {
	for _, ev := range events {
		if ev.Type == intake.TypeDemoApproval && ev.Action == "request" {
			return true
		}
	}
	return false
}
