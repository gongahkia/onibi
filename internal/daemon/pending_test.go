package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

func TestPromptEditSurvivesRestart(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	p, err := d.DB.PromptEnqueue(ctx, "s1", 100, "old prompt")
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindPromptEdit, 100, p.ID)
	restarted := New(Options{DB: d.DB, Secrets: d.Secrets, Owner: d.Owner})
	if !restarted.handlePendingPromptEdit(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "new prompt",
	}) {
		t.Fatal("pending prompt edit not handled")
	}
	got, err := restarted.DB.PromptGet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "new prompt" {
		t.Fatalf("prompt text = %q", got.Text)
	}
}

func TestPendingInjectSurvivesRestart(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	_ = d.Registry.Add(NewSession("aaa111", "one", "claude", nil, 1024))
	_ = d.Registry.Add(NewSession("bbb222", "two", "claude", nil, 1024))
	mock := telegram.NewMock(nil)
	if err := d.onText(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "run tests",
	}); err != nil {
		t.Fatal(err)
	}
	restarted := New(Options{DB: d.DB, Secrets: d.Secrets, Owner: d.Owner})
	r, s2 := pipeSession(t, "bbb222", "two")
	_ = restarted.Registry.Add(NewSession("aaa111", "one", "claude", nil, 1024))
	_ = restarted.Registry.Add(s2)
	if err := restarted.onCallback(ctx, mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "target:" + s2.ID,
	}, "target", s2.ID); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "run tests\n" {
		t.Fatalf("injected = %q", got)
	}
}

func TestPendingTTLExpires(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	old := pendingTTL
	pendingTTL = -time.Second
	t.Cleanup(func() { pendingTTL = old })
	d.setPending(ctx, pendingKindInject, 100, "stale")
	if got, ok := d.takePending(ctx, pendingKindInject, 100); ok {
		t.Fatalf("pending = %q", got)
	}
}

func TestPromptEditCancelClearsPending(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	p, err := d.DB.PromptEnqueue(ctx, "s1", 100, "old prompt")
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindPromptEdit, 100, p.ID)
	if !d.handlePendingPromptEdit(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "cancel",
	}) {
		t.Fatal("pending prompt edit not handled")
	}
	if _, ok := d.peekPending(ctx, pendingKindPromptEdit, 100); ok {
		t.Fatal("pending prompt edit still set")
	}
	got, err := d.DB.PromptGet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.PromptQueued || got.Text != "old prompt" {
		t.Fatalf("prompt = %#v", got)
	}
}
