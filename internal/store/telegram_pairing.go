package store

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// TelegramPairOwner commits both owner identifiers and consumes the pairing
// code together. A failed transaction must never authorize an in-memory owner.
func (d *DB) TelegramPairOwner(ctx context.Context, chat, user int64, code string) error {
	if chat == 0 || user == 0 || code == "" {
		return errors.New("Telegram owner and pairing code required")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM kv WHERE key='telegram.pair_code' AND value=? AND (expire=0 OR expire>?)`, []byte(code), time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Telegram pairing code expired or already used")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kv(key,value,expire) VALUES('telegram.owner_chat_id',?,0),('telegram.owner_user_id',?,0)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,expire=0`, []byte(strconv.FormatInt(chat, 10)), []byte(strconv.FormatInt(user, 10))); err != nil {
		return err
	}
	return tx.Commit()
}
