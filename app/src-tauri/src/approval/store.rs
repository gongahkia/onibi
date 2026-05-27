use crate::protocol::{Approval, ApprovalDecisionBody, Decision, PROTOCOL_VERSION};
use anyhow::{Context, Result};
use r2d2::Pool;
use r2d2_sqlite::SqliteConnectionManager;
use rusqlite::{params, OptionalExtension};
use serde_json::Value;
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

            CREATE TABLE IF NOT EXISTS devices (
              device_id   TEXT PRIMARY KEY,
              label       TEXT,
              push_subscription TEXT,
              created_at  INTEGER NOT NULL,
              last_seen   INTEGER
            );

            CREATE INDEX IF NOT EXISTS idx_approvals_undecided
              ON approvals(decided_at)
              WHERE decided_at IS NULL;
            "#,
        )
        .context("initialize sqlite schema")?;
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

    pub fn insert_device(
        &self,
        device_id: &str,
        label: &str,
        push_subscription: Option<&Value>,
    ) -> Result<()> {
        let conn = self.pool.get().context("open sqlite connection")?;
        conn.execute(
            r#"
            INSERT INTO devices(device_id, label, push_subscription, created_at, last_seen)
            VALUES(?, ?, ?, ?, ?)
            ON CONFLICT(device_id) DO UPDATE SET
              label = excluded.label,
              push_subscription = excluded.push_subscription,
              last_seen = excluded.last_seen
            "#,
            params![
                device_id,
                label,
                match push_subscription {
                    Some(value) => Some(serde_json::to_string(value)?),
                    None => None,
                },
                now_millis(),
                now_millis(),
            ],
        )
        .context("insert device")?;
        Ok(())
    }
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::{ApprovalDecisionBody, Decision};
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
}
