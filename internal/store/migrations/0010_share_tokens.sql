-- Onibi migration 0010: reusable viewer share token metadata.
--
-- Viewer tokens point at a session, remain valid until expires_at, and can be
-- claimed until use_count reaches max_uses. Owner tokens keep max_uses=1.

ALTER TABLE pairing_tokens
  ADD COLUMN session_id TEXT NOT NULL DEFAULT '';

ALTER TABLE pairing_tokens
  ADD COLUMN max_uses INTEGER NOT NULL DEFAULT 1 CHECK (max_uses > 0);

ALTER TABLE pairing_tokens
  ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0 CHECK (use_count >= 0);
