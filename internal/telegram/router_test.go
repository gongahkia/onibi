package telegram

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/store"
)

func newOwner(t *testing.T, id int64) *auth.Owner {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "r.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	o := &auth.Owner{}
	if err := auth.SetOwner(context.Background(), db, o, id); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestRouterDropsNonOwnerMessages(t *testing.T) {
	o := newOwner(t, 100)
	var called atomic.Int32
	r := &Router{
		Owner:  o,
		OnText: func(_ context.Context, _ API, _ *models.Message) error { called.Add(1); return nil },
	}
	upd := &models.Update{Message: &models.Message{From: &models.User{ID: 9999}, Text: "hi"}}
	r.Dispatch(context.Background(), nil, upd)
	if called.Load() != 0 {
		t.Fatalf("handler should not have been called for non-owner")
	}
	if r.Dropped() != 1 {
		t.Fatalf("dropped counter = %d", r.Dropped())
	}
}

func TestRouterDropsNonOwnerCallbacks(t *testing.T) {
	o := newOwner(t, 100)
	var called atomic.Int32
	r := &Router{
		Owner: o,
		OnCB: func(_ context.Context, _ API, _ *models.CallbackQuery, _, _ string) error {
			called.Add(1)
			return nil
		},
	}
	_ = r
	upd := &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "cb1",
		From: models.User{ID: 9999},
		Data: "approve:abc",
	}}
	r.Dispatch(context.Background(), nil, upd)
	if called.Load() != 0 {
		t.Fatal("callback handler must not run for non-owner")
	}
	if r.Dropped() != 1 {
		t.Fatalf("dropped = %d", r.Dropped())
	}
}

func TestRouterAcceptsOwnerCallback(t *testing.T) {
	o := newOwner(t, 100)
	var gotVerb, gotID string
	r := &Router{
		Owner: o,
		OnCB: func(_ context.Context, _ API, _ *models.CallbackQuery, verb, id string) error {
			gotVerb, gotID = verb, id
			return nil
		},
	}
	_ = r
	upd := &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "cb1",
		From: models.User{ID: 100},
		Data: "approve:abc123",
	}}
	r.Dispatch(context.Background(), nil, upd)
	if gotVerb != "approve" || gotID != "abc123" {
		t.Fatalf("got verb=%q id=%q", gotVerb, gotID)
	}
}

func TestRouterReplyPath(t *testing.T) {
	o := newOwner(t, 100)
	var replyCalled, textCalled atomic.Int32
	r := &Router{
		Owner:   o,
		OnText:  func(_ context.Context, _ API, _ *models.Message) error { textCalled.Add(1); return nil },
		OnReply: func(_ context.Context, _ API, _ *models.Message) error { replyCalled.Add(1); return nil },
	}
	_ = r
	upd := &models.Update{Message: &models.Message{
		From:           &models.User{ID: 100},
		Text:           "edited json",
		ReplyToMessage: &models.Message{ID: 42},
	}}
	r.Dispatch(context.Background(), nil, upd)
	if replyCalled.Load() != 1 {
		t.Fatalf("reply handler not called")
	}
	if textCalled.Load() != 0 {
		t.Fatalf("text handler should NOT have been called when reply path matched")
	}
}
