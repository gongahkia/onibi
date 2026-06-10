package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/gongahkia/onibi/internal/store"
)

// KVKeyOwnerID and KVKeyBotID are the canonical KV keys.
const (
	KVKeyOwnerID = "owner_id"
	KVKeyBotID   = "bot_id"
)

// ErrOwnerNotSet is returned by LoadOwner when no owner has been paired.
var ErrOwnerNotSet = errors.New("owner not paired — run `onibi setup`")

// Owner holds the current authorized chat ID, atomically swappable so
// router-side checks are lock-free.
type Owner struct {
	id atomic.Int64
}

// LoadOwner reads owner_id from the store. Returns ErrOwnerNotSet if absent.
func LoadOwner(ctx context.Context, db *store.DB) (*Owner, error) {
	v, ok, err := db.KVGetString(ctx, KVKeyOwnerID)
	if err != nil {
		return nil, err
	}
	if !ok || v == "" {
		return nil, ErrOwnerNotSet
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, err
	}
	o := &Owner{}
	o.id.Store(id)
	return o, nil
}

// MustBeOwner returns true iff fromChatID equals the configured owner.
// Constant-time integer compare (subtle.ConstantTimeEq on the byte
// representation). Idempotent + lock-free for every Telegram update.
func (o *Owner) MustBeOwner(fromChatID int64) bool {
	want := o.id.Load()
	return subtle.ConstantTimeEq(int32(want^fromChatID), 0) == 1 ||
		// fall back to slow equality if want^fromChatID overflows int32 — both 64-bit ints
		want == fromChatID
}

// ID returns the configured owner chat id (informational; do not use for
// auth — call MustBeOwner instead).
func (o *Owner) ID() int64 { return o.id.Load() }

// SetOwner atomically writes a new owner_id to storage and to the live
// pointer. Used by the pair flow and `onibi setup --rotate-owner`.
func SetOwner(ctx context.Context, db *store.DB, o *Owner, chatID int64) error {
	if err := db.KVSetString(ctx, KVKeyOwnerID, strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	o.id.Store(chatID)
	return nil
}

// IsOwnerSet reports whether an owner has been recorded.
func IsOwnerSet(ctx context.Context, db *store.DB) (bool, error) {
	_, ok, err := db.KVGetString(ctx, KVKeyOwnerID)
	return ok, err
}
