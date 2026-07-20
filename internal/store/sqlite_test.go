package store

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenEphemeral(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestKVRoundtrip(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := db.KVSetString(ctx, "owner_id", "42"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := db.KVGetString(ctx, "owner_id")
	if err != nil || !ok || v != "42" {
		t.Fatalf("got %q, %v, %v", v, ok, err)
	}
}

func TestEncryptedKVRoundtrip(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := db.KVSetEncryptedString(ctx, "push_vapid_priv_enc", "private-key"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := db.KVGetEncryptedString(ctx, "push_vapid_priv_enc")
	if err != nil || !ok || v != "private-key" {
		t.Fatalf("got %q, %v, %v", v, ok, err)
	}
	var raw []byte
	if err := db.sql.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, "push_vapid_priv_enc").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) == "private-key" {
		t.Fatal("encrypted kv stored plaintext")
	}
}

func TestKVExpire(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).Unix()
	if err := db.KVSet(ctx, "stale", []byte("v"), past); err != nil {
		t.Fatal(err)
	}
	_, ok, err := db.KVGet(ctx, "stale")
	if err != nil || ok {
		t.Fatalf("expected expired-miss, got ok=%v err=%v", ok, err)
	}
}

func TestKVPurgeExpiredRemovesRows(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).Unix()
	if err := db.KVSet(ctx, "pending:inject:1", []byte("old"), past); err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(ctx, "pending:inject:2", "new"); err != nil {
		t.Fatal(err)
	}
	if err := db.KVPurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM kv WHERE key = 'pending:inject:1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expired rows = %d", n)
	}
	if _, ok, err := db.KVGetString(ctx, "pending:inject:2"); err != nil || !ok {
		t.Fatalf("live key ok=%v err=%v", ok, err)
	}
}

func TestKVKeysWithPrefix(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	for k, v := range map[string]string{
		"target:1": "a",
		"target:2": "b",
		"other:1":  "c",
	} {
		if err := db.KVSetString(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := db.KVKeysWithPrefix(ctx, "target:")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"target:1", "target:2"}) {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestPairingSingleUse(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	tok := "abc123tokenvalue"
	if err := db.PutPairingToken(ctx, tok, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	ok, err := db.ConsumePairingToken(ctx, tok)
	if err != nil || !ok {
		t.Fatalf("first consume: ok=%v err=%v", ok, err)
	}
	ok, err = db.ConsumePairingToken(ctx, tok)
	if err != nil || ok {
		t.Fatalf("second consume must fail: ok=%v err=%v", ok, err)
	}
}

func TestPairingExpired(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	tok := "expiredtoken123"
	if err := db.PutPairingToken(ctx, tok, -time.Minute); err != nil {
		t.Fatal(err)
	}
	ok, err := db.ConsumePairingToken(ctx, tok)
	if err != nil || ok {
		t.Fatalf("expired consume must fail: ok=%v err=%v", ok, err)
	}
}

func TestPairingRaceOnlyOneWins(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	tok := "racetoken1234567"
	if err := db.PutPairingToken(ctx, tok, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var wins int
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := db.ConsumePairingToken(ctx, tok)
			if err != nil {
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", wins)
	}
}

func TestEncryptedSchemaHasNoPlainPairingOrWebSessionColumns(t *testing.T) {
	db := openTemp(t)
	pairingCols := tableColumns(t, db, "pairing_tokens")
	for _, forbidden := range []string{"token"} {
		if slices.Contains(pairingCols, forbidden) {
			t.Fatalf("pairing_tokens contains plaintext column %q: %#v", forbidden, pairingCols)
		}
	}
	for _, want := range []string{"token_hash", "token_enc"} {
		if !slices.Contains(pairingCols, want) {
			t.Fatalf("pairing_tokens missing %q: %#v", want, pairingCols)
		}
	}
	webCols := tableColumns(t, db, "web_sessions")
	for _, forbidden := range []string{"session_id", "device_label", "cookie", "user_agent"} {
		if slices.Contains(webCols, forbidden) {
			t.Fatalf("web_sessions contains plaintext column %q: %#v", forbidden, webCols)
		}
	}
	for _, want := range []string{"cookie_hash", "cookie_enc", "user_agent_enc", "key_verifier_enc", "revoked_reason"} {
		if !slices.Contains(webCols, want) {
			t.Fatalf("web_sessions missing %q: %#v", want, webCols)
		}
	}
	var version int
	if err := db.sql.QueryRowContext(context.Background(), `SELECT version FROM schema_version WHERE version = 14`).Scan(&version); err != nil {
		t.Fatalf("schema version 14 missing: %v", err)
	}
}

func TestFleetStateIsNotCreatedAndLegacyTablesAreIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fleet-legacy.sqlite")
	key := bytes.Repeat([]byte{1}, 32)
	db, err := Open(path, WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, table := range []string{"fleet_hosts", "fleet_enrollment_challenges", "fleet_key_rotation_challenges", "control_commands"} {
		var count int
		if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("fresh database created removed state table %q", table)
		}
	}
	if err := db.SessionUpsertStart(ctx, "local", "codex", "codex", "/tmp", "codex", "pty", "", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE fleet_hosts (id TEXT PRIMARY KEY); INSERT INTO fleet_hosts(id) VALUES ('legacy-host')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, ok, err := db.SessionByID(ctx, "local"); err != nil || !ok {
		t.Fatalf("local session after legacy fleet state: ok=%v err=%v", ok, err)
	}
	var hostID string
	if err := db.sql.QueryRowContext(ctx, `SELECT id FROM fleet_hosts`).Scan(&hostID); err != nil || hostID != "legacy-host" {
		t.Fatalf("legacy fleet table: id=%q err=%v", hostID, err)
	}
}

func TestViewerRoleDefaultsToOwner(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := db.PutPairingToken(ctx, "role-token", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(ctx, "role-session", "iPhone", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		table string
		col   string
		hash  string
	}{
		{"pairing_tokens", "token_hash", lookupHash("role-token")},
		{"web_sessions", "cookie_hash", lookupHash("role-session")},
	} {
		var role string
		if err := db.sql.QueryRowContext(ctx, `SELECT role FROM `+tc.table+` WHERE `+tc.col+` = ?`, tc.hash).Scan(&role); err != nil {
			t.Fatal(err)
		}
		if role != "owner" {
			t.Fatalf("%s role = %q, want owner", tc.table, role)
		}
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO web_sessions(cookie_hash, cookie_enc, user_agent_enc, role, created_at, last_seen_at, revoked) VALUES ('bad', x'01', x'02', 'admin', 1, 1, 0)`); err == nil {
		t.Fatal("invalid web session role accepted")
	}
}

func TestViewerPairingTokenClaimsUntilMaxUses(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := db.PutViewerPairingToken(ctx, "viewer-token", "s1", time.Hour, 2); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		claim, ok, err := db.ClaimPairingToken(ctx, "viewer-token")
		if err != nil || !ok {
			t.Fatalf("claim %d ok=%v err=%v", i+1, ok, err)
		}
		if claim.Role != PairRoleViewer || claim.SessionID != "s1" {
			t.Fatalf("claim = %#v", claim)
		}
	}
	if _, ok, err := db.ClaimPairingToken(ctx, "viewer-token"); err != nil || ok {
		t.Fatalf("third claim ok=%v err=%v", ok, err)
	}
}

func TestViewerRoleMigrationBackfillsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "role-upgrade.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
CREATE TABLE schema_version (version INTEGER PRIMARY KEY);
CREATE TABLE pairing_tokens (
  token_hash TEXT PRIMARY KEY,
  token_enc BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  consumed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE web_sessions (
  cookie_hash TEXT PRIMARY KEY,
  cookie_enc BLOB NOT NULL,
  user_agent_enc BLOB NOT NULL,
  key_verifier_enc BLOB,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  revoked INTEGER NOT NULL DEFAULT 0
);
INSERT INTO pairing_tokens(token_hash, token_enc, created_at, expires_at, consumed) VALUES ('pair-hash', x'01', 1, 2, 0);
INSERT INTO web_sessions(cookie_hash, cookie_enc, user_agent_enc, created_at, last_seen_at, revoked) VALUES ('session-hash', x'02', x'03', 1, 2, 0);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	db, err := Open(path, WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, tc := range []struct {
		table string
		col   string
		hash  string
	}{
		{"pairing_tokens", "token_hash", "pair-hash"},
		{"web_sessions", "cookie_hash", "session-hash"},
	} {
		if cols := tableColumns(t, db, tc.table); !slices.Contains(cols, "role") {
			t.Fatalf("%s missing role: %#v", tc.table, cols)
		}
		var role string
		if err := db.sql.QueryRowContext(context.Background(), `SELECT role FROM `+tc.table+` WHERE `+tc.col+` = ?`, tc.hash).Scan(&role); err != nil {
			t.Fatal(err)
		}
		if role != "owner" {
			t.Fatalf("%s migrated role = %q, want owner", tc.table, role)
		}
	}
}

func TestSnapshotSchemaLoadsOnFreshDB(t *testing.T) {
	db := openTemp(t)
	snapshotCols := tableColumns(t, db, "snapshots")
	for _, want := range []string{"id", "session_id", "name", "created_at", "ring_buffer_enc", "cwd_enc", "env_enc", "transcript_offset"} {
		if !slices.Contains(snapshotCols, want) {
			t.Fatalf("snapshots missing %q: %#v", want, snapshotCols)
		}
	}
	turnCols := tableColumns(t, db, "transcript_turns")
	for _, want := range []string{"id", "session_id", "turn_index", "role", "content_enc", "tool_calls_enc", "ts"} {
		if !slices.Contains(turnCols, want) {
			t.Fatalf("transcript_turns missing %q: %#v", want, turnCols)
		}
	}
	for _, tc := range []struct {
		table string
		col   string
	}{
		{"snapshots", "ring_buffer_enc"},
		{"snapshots", "cwd_enc"},
		{"snapshots", "env_enc"},
		{"transcript_turns", "content_enc"},
		{"transcript_turns", "tool_calls_enc"},
	} {
		if typ := tableColumnType(t, db, tc.table, tc.col); typ != "BLOB" {
			t.Fatalf("%s.%s type = %q, want BLOB", tc.table, tc.col, typ)
		}
	}
	var version int
	if err := db.sql.QueryRowContext(context.Background(), `SELECT version FROM schema_version WHERE version = 8`).Scan(&version); err != nil {
		t.Fatalf("schema version 8 missing: %v", err)
	}
}

func TestWorkspaceSchemaLoadsOnFreshDB(t *testing.T) {
	db := openTemp(t)
	workspaceCols := tableColumns(t, db, "workspaces")
	for _, want := range []string{"name", "path_enc", "ssh_key_ref", "last_seen"} {
		if !slices.Contains(workspaceCols, want) {
			t.Fatalf("workspaces missing %q: %#v", want, workspaceCols)
		}
	}
	if typ := tableColumnType(t, db, "workspaces", "path_enc"); typ != "BLOB" {
		t.Fatalf("workspaces.path_enc type = %q, want BLOB", typ)
	}
	var version int
	if err := db.sql.QueryRowContext(context.Background(), `SELECT version FROM schema_version WHERE version = 9`).Scan(&version); err != nil {
		t.Fatalf("schema version 9 missing: %v", err)
	}
}

func TestPushSubscriptionSchemaLoadsOnFreshDB(t *testing.T) {
	db := openTemp(t)
	cols := tableColumns(t, db, "push_subscriptions")
	for _, want := range []string{"endpoint_hash", "endpoint_enc", "p256dh_enc", "auth_enc", "created_at", "last_seen_at"} {
		if !slices.Contains(cols, want) {
			t.Fatalf("push_subscriptions missing %q: %#v", want, cols)
		}
	}
	for _, col := range []string{"endpoint_enc", "p256dh_enc", "auth_enc"} {
		if typ := tableColumnType(t, db, "push_subscriptions", col); typ != "BLOB" {
			t.Fatalf("push_subscriptions.%s type = %q, want BLOB", col, typ)
		}
	}
}

func TestPushSubscriptionRoundtripEncrypted(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	endpoint := "https://push.example.invalid/sub/1"
	if err := db.PutPushSubscription(ctx, endpoint, "p256dh-key", "auth-secret", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	subs, err := db.PushSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Endpoint != endpoint || subs[0].P256dh != "p256dh-key" || subs[0].Auth != "auth-secret" {
		t.Fatalf("subscriptions = %#v", subs)
	}
	var endpointEnc, p256dhEnc, authEnc []byte
	if err := db.sql.QueryRowContext(ctx, `SELECT endpoint_enc, p256dh_enc, auth_enc FROM push_subscriptions WHERE endpoint_hash = ?`, lookupHash(endpoint)).Scan(&endpointEnc, &p256dhEnc, &authEnc); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
		want string
	}{
		{"endpoint", endpointEnc, endpoint},
		{"p256dh", p256dhEnc, "p256dh-key"},
		{"auth", authEnc, "auth-secret"},
	} {
		if bytes.Contains(tc.raw, []byte(tc.want)) {
			t.Fatalf("%s stored plaintext", tc.name)
		}
	}
	if err := db.DeletePushSubscription(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	subs, err = db.PushSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Fatalf("subscriptions after delete = %#v", subs)
	}
}

func TestEncryptedUpgradeFromPlaintextSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
CREATE TABLE schema_version (version INTEGER PRIMARY KEY);
CREATE TABLE pairing_tokens (
  token TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  consumed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_pairing_expires ON pairing_tokens(expires_at);
CREATE TABLE web_sessions (
  session_id TEXT PRIMARY KEY,
  device_label TEXT,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  revoked INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_web_sessions_revoked ON web_sessions(revoked, last_seen_at);
INSERT INTO pairing_tokens(token, created_at, expires_at, consumed) VALUES ('old-token', 1, 4102444800, 0);
INSERT INTO web_sessions(session_id, device_label, created_at, last_seen_at, revoked) VALUES ('old-session', 'Old iPhone', 1, 2, 0);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	db, err := Open(path, WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if cols := tableColumns(t, db, "pairing_tokens"); slices.Contains(cols, "token") {
		t.Fatalf("plaintext token survived upgrade: %#v", cols)
	}
	if ok, err := db.ConsumePairingToken(context.Background(), "old-token"); err != nil || !ok {
		t.Fatalf("consume upgraded token ok=%v err=%v", ok, err)
	}
	if cols := tableColumns(t, db, "web_sessions"); slices.Contains(cols, "session_id") || slices.Contains(cols, "device_label") {
		t.Fatalf("plaintext web columns survived upgrade: %#v", cols)
	}
	session, ok, err := db.WebSession(context.Background(), "old-session")
	if err != nil || !ok {
		t.Fatalf("web session ok=%v err=%v", ok, err)
	}
	if session.SessionID != "old-session" || session.DeviceLabel != "Old iPhone" {
		t.Fatalf("session = %#v", session)
	}
}

func tableColumns(t *testing.T, db *DB, table string) []string {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tableColumnType(t *testing.T, db *DB, table, column string) string {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return typ
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("%s.%s not found", table, column)
	return ""
}
