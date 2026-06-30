-- Onibi migration 0009: owner/viewer role columns for pairing tokens and web sessions.
--
-- Fresh schemas create these columns from internal/store/sqlite.go. Existing
-- encrypted stores get the same columns through DB.ensureColumn during open.

ALTER TABLE pairing_tokens
  ADD COLUMN role TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer'));

ALTER TABLE web_sessions
  ADD COLUMN role TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer'));
