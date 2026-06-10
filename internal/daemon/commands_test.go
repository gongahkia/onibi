package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/telegram"
)

func TestRenderOverrideExplicitTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	msg := &models.Message{From: &models.User{ID: 100}, Chat: models.Chat{ID: 100}, Text: "/screenshot abc"}
	if !d.handleTextCommand(context.Background(), mock, msg) {
		t.Fatal("command not handled")
	}
	if got := d.renderOverride("abc123"); got != render.ModePNG {
		t.Fatalf("override = %s", got)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "png") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestRenderOverrideAmbiguousWithoutTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	_ = d.Registry.Add(NewSession("a", "one", "claude", nil, 1024))
	_ = d.Registry.Add(NewSession("b", "two", "claude", nil, 1024))
	mock := telegram.NewMock(nil)
	d.handleTextCommand(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/text",
	})
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Multiple active sessions") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestNotifyTurnCompleteUsesScreenshotOverride(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	_, _ = s.Buf.Write([]byte("hello"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.setRenderOverride("abc123", render.ModePNG)
	if err := d.notifyTurnComplete(context.Background(), "abc123", "agent_done", ""); err != nil {
		t.Fatal(err)
	}
	if len(mock.Photos()) != 1 {
		t.Fatalf("photos = %d messages = %d", len(mock.Photos()), len(mock.Sent()))
	}
}

func TestNotifyTurnCompleteDefaultsToText(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	_, _ = s.Buf.Write([]byte("hello"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.notifyTurnComplete(context.Background(), "abc123", "agent_done", ""); err != nil {
		t.Fatal(err)
	}
	if len(mock.Sent()) != 1 || len(mock.Photos()) != 0 {
		t.Fatalf("messages = %d photos = %d", len(mock.Sent()), len(mock.Photos()))
	}
}
