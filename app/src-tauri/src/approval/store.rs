use crate::protocol::{
    Approval, ApprovalDecisionBody, CheckpointRecord, ClientScope, Decision, DesktopCommandBlock,
    RunEvent, PROTOCOL_VERSION,
};
use anyhow::{Context, Result};
use r2d2::Pool;
use r2d2_sqlite::SqliteConnectionManager;
use rusqlite::{params, OptionalExtension};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::{
    fs,
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};

pub type DbPool = Pool<SqliteConnectionManager>;

#[derive(Clone)]
pub struct ApprovalStore {
    pool: DbPool,
    path: PathBuf,
}

#[derive(Debug, Clone, Default)]
pub struct ApprovalHistoryFilter {
    pub agent: Option<String>,
    pub tool: Option<String>,
    pub decision: Option<Decision>,
    pub from: Option<i64>,
    pub to: Option<i64>,
    pub limit: usize,
}

impl ApprovalStore {
    pub fn open(path: impl AsRef<Path>) -> Result<Self> {
        let path = path.as_ref().to_path_buf();
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("create db directory {}", parent.display()))?;
        }
        let manager = SqliteConnectionManager::file(&path);
        let pool = Pool::new(manager).context("create sqlite connection pool")?;
        let store = Self { pool, path };
        store.init()?;
        Ok(store)
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn init(&self) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute_batch(
            r#"
            PRAGMA journal_mode = WAL;
            PRAGMA foreign_keys = ON;

            CREATE TABLE IF NOT EXISTS meta (
              key TEXT PRIMARY KEY,
              value TEXT NOT NULL
            );

            CREATE TABLE IF NOT EXISTS approvals (
              approval_id TEXT PRIMARY KEY,
              machine_id  TEXT NOT NULL,
              session_id  TEXT NOT NULL,
              agent       TEXT NOT NULL,
              tool        TEXT NOT NULL,
              input       TEXT NOT NULL,
              cwd         TEXT NOT NULL,
              metadata    TEXT,
              decision    TEXT,
              updated_input TEXT,
              reason      TEXT,
              decided_by  TEXT,
              created_at  INTEGER NOT NULL,
              decided_at  INTEGER
            );

            CREATE TABLE IF NOT EXISTS run_events (
              id          INTEGER PRIMARY KEY AUTOINCREMENT,
              machine_id  TEXT NOT NULL,
              session_id  TEXT NOT NULL,
              kind        TEXT NOT NULL,
              payload     TEXT NOT NULL,
              ts          INTEGER NOT NULL
            );

            CREATE TABLE IF NOT EXISTS command_blocks (
              block_id      TEXT PRIMARY KEY,
              session_id    TEXT NOT NULL,
              workspace_id  TEXT NOT NULL,
              agent         TEXT NOT NULL,
              command       TEXT NOT NULL,
              cwd           TEXT NOT NULL,
              started_at    INTEGER NOT NULL,
              ended_at      INTEGER,
              exit_code     INTEGER,
              status        TEXT NOT NULL,
              output_preview TEXT NOT NULL,
              preview_url   TEXT,
              changed_files TEXT NOT NULL,
              attention     TEXT,
              source        TEXT,
              updated_at    INTEGER NOT NULL
            );

            CREATE TABLE IF NOT EXISTS checkpoints (
              approval_id TEXT PRIMARY KEY,
              session_id  TEXT NOT NULL,
              cwd         TEXT NOT NULL,
              pre_ref     TEXT NOT NULL,
              post_ref    TEXT,
              created_at  INTEGER NOT NULL,
              updated_at  INTEGER NOT NULL,
              error       TEXT
            );

            CREATE TABLE IF NOT EXISTS devices (
              device_id   TEXT PRIMARY KEY,
              label       TEXT,
              push_subscription TEXT,
              scope       TEXT NOT NULL DEFAULT 'full',
              created_at  INTEGER NOT NULL,
              last_seen   INTEGER
            );

            CREATE TABLE IF NOT EXISTS spectator_tokens (
              token_hash  TEXT PRIMARY KEY,
              created_at  INTEGER NOT NULL,
              consumed_at INTEGER,
              device_id   TEXT
            );

            CREATE INDEX IF NOT EXISTS idx_approvals_undecided
              ON approvals(decided_at)
              WHERE decided_at IS NULL;

            CREATE INDEX IF NOT EXISTS idx_command_blocks_session_started
              ON command_blocks(session_id, started_at DESC);

            CREATE INDEX IF NOT EXISTS idx_command_blocks_started
              ON command_blocks(started_at DESC);

            CREATE INDEX IF NOT EXISTS idx_checkpoints_session_created
              ON checkpoints(session_id, created_at DESC);
            "#,
        )
        .context("initialize sqlite schema")?;
        add_column_if_missing(&conn, "devices", "scope", "TEXT NOT NULL DEFAULT 'full'")?;
        Ok(())
    }

    pub fn get_meta(&self, key: &str) -> Result<Option<String>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.query_row("SELECT value FROM meta WHERE key = ?", [key], |row| {
            row.get(0)
        })
        .optional()
        .with_context(|| format!("read meta {key}"))
    }

    pub fn set_meta(&self, key: &str, value: &str) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            "INSERT INTO meta(key, value) VALUES(?, ?)
             ON CONFLICT(key) DO UPDATE SET value = excluded.value",
            params![key, value],
        )
        .with_context(|| format!("write meta {key}"))?;
        Ok(())
    }

    pub fn insert_approval(&self, approval: &Approval) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            r#"
            INSERT INTO approvals (
              approval_id, machine_id, session_id, agent, tool, input, cwd, metadata,
              decision, updated_input, reason, decided_by, created_at, decided_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            "#,
            params![
                approval.approval_id,
                approval.machine_id,
                approval.session_id,
                approval.agent,
                approval.tool,
                serde_json::to_string(&approval.input)?,
                approval.cwd,
                serialize_option(&approval.metadata)?,
                approval.decision.map(Decision::as_str),
                serialize_option(&approval.updated_input)?,
                approval.reason,
                approval.decided_by,
                approval.created_at,
                approval.decided_at,
            ],
        )
        .with_context(|| format!("insert approval {}", approval.approval_id))?;
        Ok(())
    }

    pub fn list_pending(&self) -> Result<Vec<Approval>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let mut stmt = conn.prepare(
            r#"
            SELECT approval_id, machine_id, session_id, agent, tool, input, cwd, metadata,
                   decision, updated_input, reason, decided_by, created_at, decided_at
            FROM approvals
            WHERE decided_at IS NULL
            ORDER BY created_at ASC
            "#,
        )?;
        let mut rows = stmt.query([])?;
        let mut approvals = Vec::new();
        while let Some(row) = rows.next()? {
            approvals.push(row_to_approval(row)?);
        }
        Ok(approvals)
    }

    pub fn get_approval(&self, approval_id: &str) -> Result<Option<Approval>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let mut stmt = conn.prepare(
            r#"
            SELECT approval_id, machine_id, session_id, agent, tool, input, cwd, metadata,
                   decision, updated_input, reason, decided_by, created_at, decided_at
            FROM approvals
            WHERE approval_id = ?
            "#,
        )?;
        let mut rows = stmt.query([approval_id])?;
        if let Some(row) = rows.next()? {
            Ok(Some(row_to_approval(row)?))
        } else {
            Ok(None)
        }
    }

    pub fn list_approvals(&self, filter: ApprovalHistoryFilter) -> Result<Vec<Approval>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let limit = filter.limit.clamp(1, 1000) as i64;
        let decision = filter.decision.map(Decision::as_str);
        let mut stmt = conn.prepare(
            r#"
            SELECT approval_id, machine_id, session_id, agent, tool, input, cwd, metadata,
                   decision, updated_input, reason, decided_by, created_at, decided_at
            FROM approvals
            WHERE (?1 IS NULL OR agent = ?1)
              AND (?2 IS NULL OR tool = ?2)
              AND (?3 IS NULL OR decision = ?3)
              AND (?4 IS NULL OR created_at >= ?4)
              AND (?5 IS NULL OR created_at <= ?5)
            ORDER BY created_at DESC
            LIMIT ?6
            "#,
        )?;
        let mut rows = stmt.query(params![
            filter.agent.as_deref(),
            filter.tool.as_deref(),
            decision,
            filter.from,
            filter.to,
            limit,
        ])?;
        let mut approvals = Vec::new();
        while let Some(row) = rows.next()? {
            approvals.push(row_to_approval(row)?);
        }
        Ok(approvals)
    }

    pub fn decide(&self, approval_id: &str, decision: &ApprovalDecisionBody) -> Result<bool> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let changed = conn
            .execute(
                r#"
                UPDATE approvals
                SET decision = ?,
                    updated_input = ?,
                    reason = ?,
                    decided_by = ?,
                    decided_at = ?
                WHERE approval_id = ? AND decided_at IS NULL
                "#,
                params![
                    decision.decision.as_str(),
                    serialize_option(&decision.updated_input)?,
                    decision.reason,
                    decision.by,
                    now_millis(),
                    approval_id,
                ],
            )
            .with_context(|| format!("decide approval {approval_id}"))?;
        Ok(changed == 1)
    }

    pub fn insert_run_event(
        &self,
        machine_id: &str,
        session_id: &str,
        kind: &str,
        payload: &Value,
    ) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            "INSERT INTO run_events(machine_id, session_id, kind, payload, ts) VALUES(?, ?, ?, ?, ?)",
            params![machine_id, session_id, kind, serde_json::to_string(payload)?, now_millis()],
        )
        .context("insert run event")?;
        Ok(())
    }

    pub fn list_recent_run_events(&self, limit: usize) -> Result<Vec<RunEvent>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let mut stmt = conn.prepare(
            r#"
            SELECT id, machine_id, session_id, kind, payload, ts
            FROM run_events
            ORDER BY ts DESC
            LIMIT ?
            "#,
        )?;
        let mut rows = stmt.query([limit.min(200) as i64])?;
        let mut events = Vec::new();
        while let Some(row) = rows.next()? {
            let payload: String = row.get(4)?;
            events.push(RunEvent {
                id: row.get(0)?,
                protocol_version: PROTOCOL_VERSION.to_string(),
                machine_id: row.get(1)?,
                session_id: row.get(2)?,
                kind: row.get(3)?,
                payload: serde_json::from_str(&payload).context("parse run event payload")?,
                ts: row.get(5)?,
            });
        }
        Ok(events)
    }

    pub fn upsert_command_block(&self, block: &DesktopCommandBlock) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            r#"
            INSERT INTO command_blocks (
              block_id, session_id, workspace_id, agent, command, cwd, started_at,
              ended_at, exit_code, status, output_preview, preview_url, changed_files,
              attention, source, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(block_id) DO UPDATE SET
              session_id = excluded.session_id,
              workspace_id = excluded.workspace_id,
              agent = excluded.agent,
              command = excluded.command,
              cwd = excluded.cwd,
              started_at = excluded.started_at,
              ended_at = excluded.ended_at,
              exit_code = excluded.exit_code,
              status = excluded.status,
              output_preview = excluded.output_preview,
              preview_url = excluded.preview_url,
              changed_files = excluded.changed_files,
              attention = excluded.attention,
              source = excluded.source,
              updated_at = excluded.updated_at
            "#,
            params![
                &block.id,
                &block.session_id,
                &block.workspace_id,
                &block.agent,
                &block.command,
                &block.cwd,
                block.started_at,
                block.ended_at,
                block.exit_code,
                &block.status,
                &block.output_preview,
                &block.preview_url,
                serde_json::to_string(&block.changed_files)?,
                &block.attention,
                &block.source,
                now_millis(),
            ],
        )
        .with_context(|| format!("upsert command block {}", block.id))?;
        Ok(())
    }

    pub fn list_command_blocks(
        &self,
        session_id: Option<&str>,
        limit: usize,
    ) -> Result<Vec<DesktopCommandBlock>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let limit = limit.clamp(1, 500) as i64;
        let sql = if session_id.is_some() {
            r#"
            SELECT block_id, session_id, workspace_id, agent, command, cwd, started_at,
                   ended_at, exit_code, status, output_preview, preview_url, changed_files,
                   attention, source
            FROM command_blocks
            WHERE session_id = ?
            ORDER BY started_at DESC
            LIMIT ?
            "#
        } else {
            r#"
            SELECT block_id, session_id, workspace_id, agent, command, cwd, started_at,
                   ended_at, exit_code, status, output_preview, preview_url, changed_files,
                   attention, source
            FROM command_blocks
            ORDER BY started_at DESC
            LIMIT ?
            "#
        };
        let mut stmt = conn.prepare(sql)?;
        let mut rows = if let Some(session_id) = session_id {
            stmt.query(params![session_id, limit])?
        } else {
            stmt.query(params![limit])?
        };
        let mut blocks = Vec::new();
        while let Some(row) = rows.next()? {
            blocks.push(row_to_command_block(row)?);
        }
        Ok(blocks)
    }

    pub fn upsert_checkpoint_pre(&self, checkpoint: &CheckpointRecord) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            r#"
            INSERT INTO checkpoints (
              approval_id, session_id, cwd, pre_ref, post_ref, created_at, updated_at, error
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(approval_id) DO UPDATE SET
              session_id = excluded.session_id,
              cwd = excluded.cwd,
              pre_ref = excluded.pre_ref,
              updated_at = excluded.updated_at,
              error = excluded.error
            "#,
            params![
                &checkpoint.approval_id,
                &checkpoint.session_id,
                &checkpoint.cwd,
                &checkpoint.pre_ref,
                &checkpoint.post_ref,
                checkpoint.created_at,
                checkpoint.updated_at,
                &checkpoint.error,
            ],
        )
        .with_context(|| format!("upsert checkpoint {}", checkpoint.approval_id))?;
        Ok(())
    }

    pub fn mark_checkpoint_post(&self, approval_id: &str, post_ref: &str) -> Result<bool> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let changed = conn
            .execute(
                r#"
                UPDATE checkpoints
                SET post_ref = ?, updated_at = ?, error = NULL
                WHERE approval_id = ?
                "#,
                params![post_ref, now_millis(), approval_id],
            )
            .with_context(|| format!("mark checkpoint post {approval_id}"))?;
        Ok(changed == 1)
    }

    pub fn mark_checkpoint_error(&self, approval_id: &str, error: &str) -> Result<bool> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let changed = conn
            .execute(
                r#"
                UPDATE checkpoints
                SET updated_at = ?, error = ?
                WHERE approval_id = ?
                "#,
                params![now_millis(), error, approval_id],
            )
            .with_context(|| format!("mark checkpoint error {approval_id}"))?;
        Ok(changed == 1)
    }

    pub fn get_checkpoint(&self, approval_id: &str) -> Result<Option<CheckpointRecord>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.query_row(
            r#"
            SELECT approval_id, session_id, cwd, pre_ref, post_ref, created_at, updated_at, error
            FROM checkpoints
            WHERE approval_id = ?
            "#,
            [approval_id],
            row_to_checkpoint,
        )
        .optional()
        .with_context(|| format!("get checkpoint {approval_id}"))
    }

    pub fn list_checkpoints(&self, limit: usize) -> Result<Vec<CheckpointRecord>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let mut stmt = conn.prepare(
            r#"
            SELECT approval_id, session_id, cwd, pre_ref, post_ref, created_at, updated_at, error
            FROM checkpoints
            ORDER BY created_at DESC
            LIMIT ?
            "#,
        )?;
        let mut rows = stmt.query([limit.clamp(1, 1000) as i64])?;
        let mut checkpoints = Vec::new();
        while let Some(row) = rows.next()? {
            checkpoints.push(row_to_checkpoint(row)?);
        }
        Ok(checkpoints)
    }

    pub fn latest_checkpoint_without_post(
        &self,
        session_id: &str,
    ) -> Result<Option<CheckpointRecord>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.query_row(
            r#"
            SELECT c.approval_id, c.session_id, c.cwd, c.pre_ref, c.post_ref,
                   c.created_at, c.updated_at, c.error
            FROM checkpoints c
            JOIN approvals a ON a.approval_id = c.approval_id
            WHERE c.session_id = ?
              AND c.post_ref IS NULL
              AND a.decision = 'allow'
            ORDER BY c.created_at DESC
            LIMIT 1
            "#,
            [session_id],
            row_to_checkpoint,
        )
        .optional()
        .with_context(|| format!("get latest checkpoint without post for {session_id}"))
    }

    pub fn insert_device(
        &self,
        device_id: &str,
        label: &str,
        push_subscription: Option<&Value>,
        scope: ClientScope,
    ) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            r#"
            INSERT INTO devices(device_id, label, push_subscription, scope, created_at, last_seen)
            VALUES(?, ?, ?, ?, ?, ?)
            ON CONFLICT(device_id) DO UPDATE SET
              label = excluded.label,
              push_subscription = excluded.push_subscription,
              scope = excluded.scope,
              last_seen = excluded.last_seen
            "#,
            params![
                device_id,
                label,
                match push_subscription {
                    Some(value) => Some(serde_json::to_string(value)?),
                    None => None,
                },
                scope.as_str(),
                now_millis(),
                now_millis(),
            ],
        )
        .context("insert device")?;
        Ok(())
    }

    pub fn insert_spectator_token(&self, token: &str) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            "INSERT INTO spectator_tokens(token_hash, created_at) VALUES(?, ?)",
            params![token_hash(token), now_millis()],
        )
        .context("insert spectator token")?;
        Ok(())
    }

    pub fn consume_spectator_token(&self, token: &str, device_id: &str) -> Result<bool> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let changed = conn
            .execute(
                r#"
                UPDATE spectator_tokens
                SET consumed_at = ?, device_id = ?
                WHERE token_hash = ? AND consumed_at IS NULL
                "#,
                params![now_millis(), device_id, token_hash(token)],
            )
            .context("consume spectator token")?;
        Ok(changed == 1)
    }

    pub fn spectator_token_exists(&self, token: &str) -> Result<bool> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let found: Option<i64> = conn
            .query_row(
                "SELECT 1 FROM spectator_tokens WHERE token_hash = ? LIMIT 1",
                [token_hash(token)],
                |row| row.get(0),
            )
            .optional()
            .context("read spectator token")?;
        Ok(found.is_some())
    }

    pub fn list_push_subscriptions(&self) -> Result<Vec<Value>> {
        let conn = self.pool.get().context("open sqlite connection")?;
        let mut stmt = conn.prepare(
            r#"
            SELECT push_subscription
            FROM devices
            WHERE push_subscription IS NOT NULL AND push_subscription != ''
            "#,
        )?;
        let mut rows = stmt.query([])?;
        let mut subscriptions = Vec::new();
        while let Some(row) = rows.next()? {
            let raw: String = row.get(0)?;
            subscriptions
                .push(serde_json::from_str(&raw).context("parse device push subscription")?);
        }
        Ok(subscriptions)
    }
}

pub fn token_hash(token: &str) -> String {
    hex::encode(Sha256::digest(token.as_bytes()))
}

fn add_column_if_missing(
    conn: &rusqlite::Connection,
    table: &str,
    column: &str,
    definition: &str,
) -> Result<()> {
    let mut stmt = conn.prepare(&format!("PRAGMA table_info({table})"))?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let name: String = row.get(1)?;
        if name == column {
            return Ok(());
        }
    }
    conn.execute(
        &format!("ALTER TABLE {table} ADD COLUMN {column} {definition}"),
        [],
    )?;
    Ok(())
}

pub fn now_millis() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
        .min(i64::MAX as u128) as i64
}

fn serialize_option(value: &Option<Value>) -> Result<Option<String>> {
    value
        .as_ref()
        .map(serde_json::to_string)
        .transpose()
        .context("serialize json value")
}

fn parse_option(value: Option<String>) -> Result<Option<Value>> {
    value
        .map(|raw| serde_json::from_str(&raw))
        .transpose()
        .context("parse json value")
}

fn parse_decision(value: Option<String>) -> Result<Option<Decision>> {
    match value.as_deref() {
        Some("allow") => Ok(Some(Decision::Allow)),
        Some("deny") => Ok(Some(Decision::Deny)),
        Some(other) => anyhow::bail!("invalid decision in database: {other}"),
        None => Ok(None),
    }
}

fn row_to_approval(row: &rusqlite::Row<'_>) -> Result<Approval> {
    let input: String = row.get(5)?;
    let metadata: Option<String> = row.get(7)?;
    let decision: Option<String> = row.get(8)?;
    let updated_input: Option<String> = row.get(9)?;
    Ok(Approval {
        protocol_version: PROTOCOL_VERSION.to_string(),
        approval_id: row.get(0)?,
        machine_id: row.get(1)?,
        session_id: row.get(2)?,
        agent: row.get(3)?,
        tool: row.get(4)?,
        input: serde_json::from_str(&input).context("parse approval input")?,
        cwd: row.get(6)?,
        metadata: parse_option(metadata)?,
        decision: parse_decision(decision)?,
        updated_input: parse_option(updated_input)?,
        reason: row.get(10)?,
        decided_by: row.get(11)?,
        created_at: row.get(12)?,
        decided_at: row.get(13)?,
    })
}

fn row_to_command_block(row: &rusqlite::Row<'_>) -> Result<DesktopCommandBlock> {
    let changed_files: String = row.get(12)?;
    Ok(DesktopCommandBlock {
        id: row.get(0)?,
        protocol_version: Some(PROTOCOL_VERSION.to_string()),
        session_id: row.get(1)?,
        workspace_id: row.get(2)?,
        agent: row.get(3)?,
        command: row.get(4)?,
        cwd: row.get(5)?,
        started_at: row.get(6)?,
        ended_at: row.get(7)?,
        exit_code: row.get(8)?,
        status: row.get(9)?,
        output_preview: row.get(10)?,
        preview_url: row.get(11)?,
        changed_files: serde_json::from_str(&changed_files)
            .context("parse command block changed files")?,
        attention: row.get(13)?,
        source: row.get(14)?,
    })
}

fn row_to_checkpoint(row: &rusqlite::Row<'_>) -> rusqlite::Result<CheckpointRecord> {
    Ok(CheckpointRecord {
        approval_id: row.get(0)?,
        session_id: row.get(1)?,
        cwd: row.get(2)?,
        pre_ref: row.get(3)?,
        post_ref: row.get(4)?,
        created_at: row.get(5)?,
        updated_at: row.get(6)?,
        error: row.get(7)?,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::{ApprovalDecisionBody, Decision, DesktopCommandBlock};
    use serde_json::json;
    use tempfile::tempdir;

    fn sample(id: &str) -> Approval {
        Approval {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: id.to_string(),
            machine_id: "01H00000000000000000000000".to_string(),
            session_id: "01H00000000000000000000001".to_string(),
            agent: "claude-code".to_string(),
            tool: "Bash".to_string(),
            input: json!({"command": "rm -rf node_modules"}),
            cwd: "/tmp/project".to_string(),
            metadata: Some(json!({"prompt": "clean"})),
            decision: None,
            updated_input: None,
            reason: None,
            decided_by: None,
            created_at: now_millis(),
            decided_at: None,
        }
    }

    #[test]
    fn roundtrip_insert_decide_and_load_pending_on_boot() {
        let dir = tempdir().unwrap();
        let db = dir.path().join("onibi.db");
        let store = ApprovalStore::open(&db).unwrap();
        store.insert_approval(&sample("approval-1")).unwrap();
        store.insert_approval(&sample("approval-2")).unwrap();

        let pending = store.list_pending().unwrap();
        assert_eq!(pending.len(), 2);
        assert_eq!(pending[0].agent, "claude-code");

        let decided = store
            .decide(
                "approval-1",
                &ApprovalDecisionBody {
                    decision: Decision::Allow,
                    updated_input: Some(json!({"command": "echo skipped"})),
                    reason: None,
                    by: Some("desktop".to_string()),
                },
            )
            .unwrap();
        assert!(decided);

        let restarted = ApprovalStore::open(&db).unwrap();
        let pending = restarted.list_pending().unwrap();
        assert_eq!(pending.len(), 1);
        assert_eq!(pending[0].approval_id, "approval-2");

        let stored = restarted.get_approval("approval-1").unwrap().unwrap();
        assert_eq!(stored.decision, Some(Decision::Allow));
        assert_eq!(stored.decided_by.as_deref(), Some("desktop"));
    }

    #[test]
    fn stores_recent_runs_and_push_subscriptions() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        store
            .insert_run_event(
                "machine",
                "session",
                "started",
                &json!({"cwd": "/tmp/project"}),
            )
            .unwrap();
        store
            .insert_device(
                "device",
                "phone",
                Some(&json!({
                    "endpoint": "https://push.example/device",
                    "keys": {"p256dh": "p256dh", "auth": "auth"}
                })),
                ClientScope::Full,
            )
            .unwrap();

        let events = store.list_recent_run_events(10).unwrap();
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].kind, "started");

        let subscriptions = store.list_push_subscriptions().unwrap();
        assert_eq!(subscriptions.len(), 1);
        assert_eq!(subscriptions[0]["endpoint"], "https://push.example/device");
    }

    #[test]
    fn lists_approval_history_with_filters() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        store.insert_approval(&sample("approval-1")).unwrap();
        store
            .insert_approval(&Approval {
                approval_id: "approval-2".to_string(),
                agent: "codex".to_string(),
                tool: "Shell".to_string(),
                created_at: now_millis() + 10,
                ..sample("approval-2")
            })
            .unwrap();
        store
            .decide(
                "approval-2",
                &ApprovalDecisionBody {
                    decision: Decision::Deny,
                    updated_input: None,
                    reason: Some("too broad".to_string()),
                    by: Some("mobile".to_string()),
                },
            )
            .unwrap();

        let all = store
            .list_approvals(ApprovalHistoryFilter {
                limit: 10,
                ..Default::default()
            })
            .unwrap();
        assert_eq!(all.len(), 2);
        assert_eq!(all[0].approval_id, "approval-2");

        let denied = store
            .list_approvals(ApprovalHistoryFilter {
                decision: Some(Decision::Deny),
                limit: 10,
                ..Default::default()
            })
            .unwrap();
        assert_eq!(denied.len(), 1);
        assert_eq!(denied[0].reason.as_deref(), Some("too broad"));
    }

    #[test]
    fn stores_command_blocks() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let block = DesktopCommandBlock {
            id: "cmd-1".to_string(),
            protocol_version: Some(PROTOCOL_VERSION.to_string()),
            session_id: "pty-1".to_string(),
            workspace_id: "workspace:/repo".to_string(),
            agent: "codex".to_string(),
            command: "pnpm test".to_string(),
            cwd: "/repo".to_string(),
            started_at: 10,
            ended_at: Some(20),
            exit_code: Some(1),
            status: "failed".to_string(),
            output_preview: "failed".to_string(),
            preview_url: Some("http://localhost:1420/".to_string()),
            changed_files: vec!["src/main.ts".to_string()],
            attention: Some("failed".to_string()),
            source: Some("shell-integration".to_string()),
        };
        store.upsert_command_block(&block).unwrap();

        let blocks = store.list_command_blocks(Some("pty-1"), 10).unwrap();
        assert_eq!(blocks.len(), 1);
        assert_eq!(blocks[0].command, "pnpm test");
        assert_eq!(blocks[0].changed_files, vec!["src/main.ts"]);
    }

    #[test]
    fn stores_checkpoint_refs() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let mut approval = sample("approval-1");
        approval.session_id = "pty-1".to_string();
        approval.decision = Some(Decision::Allow);
        approval.decided_at = Some(now_millis());
        store.insert_approval(&approval).unwrap();
        store
            .upsert_checkpoint_pre(&CheckpointRecord {
                approval_id: "approval-1".to_string(),
                session_id: "pty-1".to_string(),
                cwd: "/repo".to_string(),
                pre_ref: "refs/onibi/turns/approval-1/pre".to_string(),
                post_ref: None,
                created_at: 10,
                updated_at: 10,
                error: None,
            })
            .unwrap();

        let pending = store
            .latest_checkpoint_without_post("pty-1")
            .unwrap()
            .unwrap();
        assert_eq!(pending.approval_id, "approval-1");
        assert!(store
            .mark_checkpoint_post("approval-1", "refs/onibi/turns/approval-1/post")
            .unwrap());

        let checkpoint = store.get_checkpoint("approval-1").unwrap().unwrap();
        assert_eq!(
            checkpoint.post_ref.as_deref(),
            Some("refs/onibi/turns/approval-1/post")
        );
        assert!(store
            .latest_checkpoint_without_post("pty-1")
            .unwrap()
            .is_none());
    }
}
