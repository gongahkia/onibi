package daemon

import (
	"context"
	"strconv"
	"time"
)

var pendingTTL = 10 * time.Minute

const (
	pendingKindApprovalEdit = "approval_edit"
	pendingKindPromptEdit   = "prompt_edit"
	pendingKindInject       = "inject"
	pendingKindSend         = "send"
)

func pendingKey(kind string, chatID int64) string {
	return "pending:" + kind + ":" + strconv.FormatInt(chatID, 10)
}

func (d *Daemon) setPending(ctx context.Context, kind string, chatID int64, value string) {
	if d.DB == nil {
		return
	}
	_ = d.DB.KVSet(ctx, pendingKey(kind, chatID), []byte(value), time.Now().Add(pendingTTL).Unix())
}

func (d *Daemon) takePending(ctx context.Context, kind string, chatID int64) (string, bool) {
	if d.DB == nil {
		return "", false
	}
	v, ok, err := d.DB.KVGet(ctx, pendingKey(kind, chatID))
	if err != nil || !ok {
		return "", false
	}
	_ = d.DB.KVDel(ctx, pendingKey(kind, chatID))
	return string(v), true
}

func (d *Daemon) peekPending(ctx context.Context, kind string, chatID int64) (string, bool) {
	if d.DB == nil {
		return "", false
	}
	v, ok, err := d.DB.KVGet(ctx, pendingKey(kind, chatID))
	if err != nil || !ok {
		return "", false
	}
	return string(v), true
}
