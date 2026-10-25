package config

import (
	"strings"
	"testing"
)

func TestDebugProfileValidation(t *testing.T) {
	good := []DebugProfile{{Command: []string{"adapter"}, Request: "launch"}, {Address: "127.0.0.1:1234", Request: "attach"}}
	for _, p := range good {
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	bad := []DebugProfile{{Request: "launch"}, {Command: []string{""}, Request: "launch"}, {Command: []string{"adapter"}, Address: "localhost:123", Request: "launch"}, {Address: "localhost", Request: "attach"}, {Command: []string{"adapter"}, Request: "run"}, {Command: []string{"adapter"}, Request: "launch", Arguments: map[string]any{"huge": strings.Repeat("x", 65536)}}}
	for _, p := range bad {
		if p.Validate() == nil {
			t.Fatal("invalid profile accepted", p.Request)
		}
	}
}
