package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestTelegramUpdateClaimIsAtMostOnce(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	claimed, err := db.TelegramClaimUpdate(ctx, 41)
	if err != nil || !claimed {
		t.Fatalf("claimed=%t err=%v", claimed, err)
	}
	offset, err := db.TelegramNextOffset(ctx)
	if err != nil || offset != 42 {
		t.Fatalf("offset=%d err=%v", offset, err)
	}
	claimed, err = db.TelegramClaimUpdate(ctx, 41)
	if err != nil || claimed {
		t.Fatalf("replayed claimed=%t err=%v", claimed, err)
	}
	if err := db.TelegramCompleteUpdate(ctx, 41); err != nil {
		t.Fatal(err)
	}
	if n, err := db.TelegramMarkUncertainUpdates(ctx); err != nil || n != 0 {
		t.Fatalf("uncertain=%d err=%v", n, err)
	}
	if _, err := db.TelegramClaimUpdate(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if n, err := db.TelegramMarkUncertainUpdates(ctx); err != nil || n != 1 {
		t.Fatalf("uncertain=%d err=%v", n, err)
	}
	claimed, err = db.TelegramClaimUpdate(ctx, 42)
	if err != nil || claimed {
		t.Fatalf("uncertain update replayed: claimed=%t err=%v", claimed, err)
	}
}

func TestTelegramOutboxRetriesAndDoesNotStoreTerminalPayload(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	item := TelegramOutboxIntent{ID: "out-1", DedupeKey: "screen:42:s1", Kind: "screen", ChatID: 42, SessionID: "s1", Title: "Updated · work", Lines: 80, ForceScreen: true}
	if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.TelegramOutboxClaim(ctx)
	if err != nil || claimed == nil || claimed.State != OutboxRunning || claimed.Title != item.Title || !claimed.ForceScreen {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if err := db.TelegramOutboxRetry(ctx, claimed.ID, "temporary failure", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	claimed, err = db.TelegramOutboxClaim(ctx)
	if err != nil || claimed == nil || claimed.Attempts != 1 {
		t.Fatalf("retried=%#v err=%v", claimed, err)
	}
	if err := db.TelegramOutboxDelivered(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	ended := TelegramOutboxIntent{ID: "out-2", DedupeKey: "ended:s1", Kind: "ended", ChatID: 42, SessionID: "s1", Title: "work ended."}
	if err := db.TelegramOutboxUpsert(ctx, ended); err != nil {
		t.Fatal(err)
	}
	claimed, err = db.TelegramOutboxClaim(ctx)
	if err != nil || claimed == nil || claimed.ID != ended.ID {
		t.Fatalf("ended=%#v err=%v", claimed, err)
	}
	if err := db.TelegramOutboxDelivered(ctx, ended.ID); err != nil {
		t.Fatal(err)
	}
	ended.ID, ended.Title = "out-3", "should not be sent"
	if err := db.TelegramOutboxUpsert(ctx, ended); err != nil {
		t.Fatal(err)
	}
	claimed, err = db.TelegramOutboxClaim(ctx)
	if err != nil || claimed != nil {
		t.Fatalf("duplicate ended=%#v err=%v", claimed, err)
	}
	rows, err := db.SQL().QueryContext(ctx, `PRAGMA table_info(telegram_outbox)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"body", "output", "png", "terminal_payload"} {
		if columns[name] {
			t.Fatalf("outbox persisted %q", name)
		}
	}
}

func TestTelegramPurgeRetainsCursor(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.TelegramClaimUpdate(ctx, 99); err != nil {
		t.Fatal(err)
	}
	if err := db.TelegramCompleteUpdate(ctx, 99); err != nil {
		t.Fatal(err)
	}
	if err := db.TelegramOutboxUpsert(ctx, TelegramOutboxIntent{ID: "out-99", DedupeKey: "notice:99", Kind: "notice", ChatID: 42, Title: "notice"}); err != nil {
		t.Fatal(err)
	}
	item, err := db.TelegramOutboxClaim(ctx)
	if err != nil || item == nil {
		t.Fatalf("item=%#v err=%v", item, err)
	}
	if err := db.TelegramOutboxDelivered(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.TelegramPurge(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	offset, err := db.TelegramNextOffset(ctx)
	if err != nil || offset != 100 {
		t.Fatalf("offset=%d err=%v", offset, err)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_updates`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("updates=%d err=%v", count, err)
	}
}
