package telegram

import (
	"context"
	"testing"
)

func TestRegisterCommands(t *testing.T) {
	mock := NewMock(nil)
	if err := RegisterCommands(context.Background(), mock); err != nil {
		t.Fatal(err)
	}
	got := mock.RegisteredCommands()
	if len(got) != 1 {
		t.Fatalf("calls = %d", len(got))
	}
	if len(got[0].Commands) == 0 || got[0].Commands[0].Command != "sessions" {
		t.Fatalf("commands = %#v", got[0].Commands)
	}
}
