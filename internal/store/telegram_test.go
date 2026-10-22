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
	item := TelegramOutboxIntent{ID: "out-1", DedupeKey: "screen:42:s1", Kind: "screen", ChatID: 42, SessionID: "s1", Title: "Updated · work", Lines: 80, ForceScreen: true, FallbackText: "last visible output"}
	if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.TelegramOutboxClaim(ctx)
	if err != nil || claimed == nil || claimed.State != OutboxRunning || claimed.Title != item.Title || !claimed.ForceScreen || claimed.FallbackText != item.FallbackText {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if err := db.TelegramOutboxRetry(ctx, claimed.ID, "temporary failure", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.TelegramOutboxSetFallback(ctx, item.DedupeKey, "new final output"); err != nil {
		t.Fatal(err)
	}
	claimed, err = db.TelegramOutboxClaim(ctx)
	if err != nil || claimed == nil || claimed.Attempts != 1 || claimed.FallbackText != "new final output" {
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

func TestTelegramOutboxReplacementSurvivesOlderDelivery(t *testing.T) {
	for _, completion := range []string{"delivered", "retry"} {
		t.Run(completion, func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx := t.Context()
			item := TelegramOutboxIntent{ID: "old", DedupeKey: "screen:42:s1", Kind: "screen", ChatID: 42, SessionID: "s1", Title: "old frame"}
			if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
				t.Fatal(err)
			}
			old, err := db.TelegramOutboxClaim(ctx)
			if err != nil || old == nil {
				t.Fatalf("old=%#v err=%v", old, err)
			}
			item.ID, item.Title = "new", "new frame"
			if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
				t.Fatal(err)
			}
			if completion == "delivered" {
				err = db.TelegramOutboxDelivered(ctx, old.ID)
			} else {
				err = db.TelegramOutboxRetry(ctx, old.ID, "old failure", time.Now().Add(time.Hour))
			}
			if err != nil {
				t.Fatal(err)
			}
			latest, err := db.TelegramOutboxClaim(ctx)
			if err != nil || latest == nil || latest.ID != "new" || latest.Title != "new frame" || latest.Attempts != 0 {
				t.Fatalf("new intent lost to older %s: latest=%#v err=%v", completion, latest, err)
			}
		})
	}
}

func TestTelegramEndedNoticeCannotBeRequeuedWhileSending(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	item := TelegramOutboxIntent{ID: "ended-first", DedupeKey: "ended:s1", Kind: "ended", ChatID: 42, SessionID: "s1"}
	if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.TelegramOutboxClaim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	item.ID = "ended-second"
	if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := db.TelegramOutboxClaim(ctx); err != nil || duplicate != nil {
		t.Fatalf("duplicate ended delivery=%#v err=%v", duplicate, err)
	}
	if err := db.TelegramOutboxDelivered(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	stats, err := db.TelegramOutboxStats(ctx)
	if err != nil || stats.Delivered != 1 || stats.Pending != 0 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
}

func TestTelegramOutboxRetainsMillisecondScheduleAndMigratesSeconds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onibi.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	at := time.Now().Add(time.Hour).Truncate(time.Second).Add(123 * time.Millisecond)
	item := TelegramOutboxIntent{ID: "scheduled", DedupeKey: "scheduled", Kind: "screen", ChatID: 42, Title: "frame", NextAttempt: at}
	if err := db.TelegramOutboxUpsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT next_attempt FROM telegram_outbox WHERE id=?`, item.ID).Scan(&stored); err != nil || stored != at.UnixMilli() {
		t.Fatalf("lost subsecond schedule: stored=%d want=%d err=%v", stored, at.UnixMilli(), err)
	}
	if early, err := db.TelegramOutboxClaim(ctx); err != nil || early != nil {
		t.Fatalf("claimed before debounce: %#v err=%v", early, err)
	}
	// Simulate a persisted intent written by the prior seconds-based format.
	if _, err := db.SQL().ExecContext(ctx, `UPDATE telegram_outbox SET next_attempt=?`, at.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SQL().QueryRowContext(ctx, `SELECT next_attempt FROM telegram_outbox WHERE id=?`, item.ID).Scan(&stored); err != nil || stored != at.Unix()*1000 {
		t.Fatalf("legacy retry schedule was not preserved: stored=%d err=%v", stored, err)
	}
	if err := db.migrate(); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT next_attempt FROM telegram_outbox WHERE id=?`, item.ID).Scan(&stored); err != nil || stored != at.Unix()*1000 {
		t.Fatalf("migration changed an existing millisecond schedule: stored=%d err=%v", stored, err)
	}
}
