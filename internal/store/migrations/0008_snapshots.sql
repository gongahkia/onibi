-- Onibi migration 0008: encrypted session snapshots and transcript turns.
--
-- Fresh schemas create both tables from internal/store/sqlite.go. These
-- columns with "_enc" suffix contain app-layer encrypted BLOB frames sealed
-- with internal/store.CryptBox and row-scoped AAD.

CREATE TABLE IF NOT EXISTS snapshots (
  id                TEXT PRIMARY KEY,
  session_id        TEXT NOT NULL,
  name              TEXT NOT NULL,
  created_at        INTEGER NOT NULL,
  ring_buffer_enc   BLOB NOT NULL,
  cwd_enc           BLOB NOT NULL,
  env_enc           BLOB NOT NULL,
  transcript_offset INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_snapshots_session_created ON snapshots(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_name ON snapshots(name);

CREATE TABLE IF NOT EXISTS transcript_turns (
  id              TEXT PRIMARY KEY,
  session_id      TEXT NOT NULL,
  turn_index      INTEGER NOT NULL,
  role            TEXT NOT NULL,
  content_enc     BLOB NOT NULL,
  tool_calls_enc  BLOB NOT NULL,
  ts              INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_transcript_turns_session_turn ON transcript_turns(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_transcript_turns_ts ON transcript_turns(ts);
