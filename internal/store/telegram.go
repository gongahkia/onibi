package store

import (
	"context"
	"strconv"
)

const (
	TelegramOffsetKey         = "telegram:last_update_offset"
	TelegramPollerConflictKey = "telegram:poller_conflict"
)

func (d *DB) TelegramOffset(ctx context.Context) (int64, bool, error) {
	v, ok, err := d.KVGetString(ctx, TelegramOffsetKey)
	if err != nil || !ok {
		return 0, ok, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

func (d *DB) SetTelegramOffset(ctx context.Context, offset int64) error {
	return d.KVSetString(ctx, TelegramOffsetKey, strconv.FormatInt(offset, 10))
}

func (d *DB) SetTelegramPollerConflict(ctx context.Context, detail string) error {
	return d.KVSetString(ctx, TelegramPollerConflictKey, detail)
}

func (d *DB) ClearTelegramPollerConflict(ctx context.Context) error {
	return d.KVDel(ctx, TelegramPollerConflictKey)
}
