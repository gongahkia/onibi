// The GUI library uses the client helpers while the daemon binary uses the
// server/runtime side, so compiling this module into either target leaves one
// half intentionally unused.
#![allow(dead_code)]

mod decider;
mod invariants;
mod projector;

use crate::{
    pty::{
        PtyEvent, PtyId, PtyManager, PtySpawnRequest, RemoteSessionMetadata, ShellMode, TrustMode,
    },
    secret,
};
use anyhow::{anyhow, Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use regex::Regex;
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    collections::{HashMap, HashSet},
    fs,
    path::{Path, PathBuf},
    process::Command,
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{
    io::{AsyncBufReadExt, AsyncRead, AsyncWrite, AsyncWriteExt, BufReader},
    net::{TcpListener, TcpStream},
    sync::{broadcast, RwLock},
    time,
};

#[cfg(unix)]
use tokio::net::UnixListener;

use decider::{classify_command, infer_status_from_output, CommandKind};
use invariants::{key_to_bytes, normalize_session_name, resolve_provider_event_session};
use projector::{snapshot_json, tail_lines, unwrap_recent_lines, TerminalScreen};

pub const PROTOCOL_VERSION: &str = "1.0";
pub const DEFAULT_ORCHESTRATION_PORT: u16 = 17894;

#[derive(Debug, Clone, Copy, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum AgentStatus {
    Idle,
    Working,
    Blocked,
    Done,
}

#[derive(Debug, Clone, Copy, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum SessionLifecycle {
    Running,
    Stale,
    Stopped,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionRestartMetadata {
    pub command: String,
    pub args: Vec<String>,
    pub cwd: Option<String>,
    pub env: Vec<(String, String)>,
    #[serde(default)]
    pub shell_mode: ShellMode,
    #[serde(default)]
    pub safe_mode: bool,
    #[serde(default)]
    pub trust_mode: TrustMode,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub remote: Option<RemoteSessionMetadata>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ProviderResumeMetadata {
    pub command: String,
    pub args: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ProviderSessionMetadata {
    pub agent: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub provider_session_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub conversation_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub resume: Option<ProviderResumeMetadata>,
    pub updated_at: i64,
}

#[derive(Debug, Clone)]
pub struct ProviderEventUpdate {
    pub agent: String,
    pub session_id: Option<String>,
    pub provider_session_id: Option<String>,
    pub conversation_id: Option<String>,
    pub cwd: Option<String>,
    pub status: Option<AgentStatus>,
    pub resume: Option<ProviderResumeMetadata>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionInfo {
    pub id: String,
    pub pane_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub agent: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub workspace_id: Option<String>,
    #[serde(default)]
    pub safe_mode: bool,
    #[serde(default)]
    pub trust_mode: TrustMode,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cwd: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    pub status: AgentStatus,
    pub lifecycle: SessionLifecycle,
    pub rows: u16,
    pub cols: u16,
    pub created_at: i64,
    pub updated_at: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub process_id: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stopped_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub exit_code: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub exit_signal: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub restart: Option<SessionRestartMetadata>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub provider: Option<ProviderSessionMetadata>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub remote: Option<RemoteSessionMetadata>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct OrchestrationSummary {
    pub total_sessions: usize,
    pub running_sessions: usize,
    pub stale_sessions: usize,
    pub stopped_sessions: usize,
    pub pane_count: usize,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub socket_path: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(tag = "type", rename_all = "kebab-case")]
pub enum OrchestrationEvent {
    SessionStarted {
        session: SessionInfo,
    },
    SessionStatus {
        session_id: String,
        status: AgentStatus,
        session: Option<SessionInfo>,
    },
    PtyOutput {
        session_id: String,
        pane_id: String,
        data: String,
        text: String,
        offset: u64,
    },
    PtyExit {
        session_id: String,
        pane_id: String,
        code: u32,
        signal: Option<String>,
    },
    PtyNotification {
        session_id: String,
        pane_id: String,
        notification: crate::pty::OscNotification,
    },
    AgentFocusRequested {
        session: SessionInfo,
    },
}

#[derive(Debug)]
struct SessionMetadataStore {
    path: PathBuf,
}

impl SessionMetadataStore {
    fn open_default() -> Result<Self> {
        Self::open(secret::db_path()?)
    }

    fn open(path: impl AsRef<Path>) -> Result<Self> {
        let path = path.as_ref().to_path_buf();
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("create db directory {}", parent.display()))?;
        }
        let store = Self { path };
        store.init()?;
        Ok(store)
    }

    fn init(&self) -> Result<()> {
        let conn = self.connect()?;
        conn.execute_batch(
            r#"
            PRAGMA journal_mode = WAL;
            CREATE TABLE IF NOT EXISTS orchestration_sessions (
              id          TEXT PRIMARY KEY,
              name        TEXT,
              lifecycle   TEXT NOT NULL,
              updated_at  INTEGER NOT NULL,
              data        TEXT NOT NULL
            );
            CREATE INDEX IF NOT EXISTS idx_orchestration_sessions_updated
              ON orchestration_sessions(updated_at DESC);
            CREATE INDEX IF NOT EXISTS idx_orchestration_sessions_name
              ON orchestration_sessions(name);
            "#,
        )
        .context("initialize orchestration session metadata")?;
        Ok(())
    }

    fn connect(&self) -> Result<Connection> {
        Connection::open(&self.path)
            .with_context(|| format!("open session metadata db {}", self.path.display()))
    }

    fn upsert(&self, session: &SessionInfo) -> Result<()> {
        let conn = self.connect()?;
        conn.execute(
            r#"
            INSERT INTO orchestration_sessions(id, name, lifecycle, updated_at, data)
            VALUES(?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
              name = excluded.name,
              lifecycle = excluded.lifecycle,
              updated_at = excluded.updated_at,
              data = excluded.data
            "#,
            params![
                session.id,
                session.name,
                serde_json::to_string(&session.lifecycle)?,
                session.updated_at,
                serde_json::to_string(session)?,
            ],
        )
        .with_context(|| format!("persist orchestration session {}", session.id))?;
        Ok(())
    }

    fn list(&self) -> Result<Vec<SessionInfo>> {
        let conn = self.connect()?;
        let mut stmt = conn.prepare(
            r#"
            SELECT data
            FROM orchestration_sessions
            ORDER BY updated_at DESC
            "#,
        )?;
        let mut rows = stmt.query([])?;
        let mut sessions = Vec::new();
        while let Some(row) = rows.next()? {
            let raw: String = row.get(0)?;
            match serde_json::from_str::<SessionInfo>(&raw) {
                Ok(session) => sessions.push(session),
                Err(error) => {
                    tracing::warn!(%error, "skipping invalid orchestration session metadata");
                }
            }
        }
        Ok(sessions)
    }
}

fn load_persisted_sessions(store: &SessionMetadataStore) -> HashMap<String, SessionInfo> {
    let now = now_millis();
    let mut loaded = HashMap::new();
    let sessions = match store.list() {
        Ok(sessions) => sessions,
        Err(error) => {
            tracing::warn!(%error, "failed to load persisted orchestration sessions");
            return loaded;
        }
    };
    for mut session in sessions {
        if let Some(restart) = session.restart.take() {
            session.restart = Some(normalize_restart_metadata(
                session.agent.as_deref(),
                restart,
            ));
        }
        if session.lifecycle == SessionLifecycle::Running {
            session.lifecycle = SessionLifecycle::Stale;
            session.status = AgentStatus::Done;
            session.updated_at = now;
            session.stopped_at.get_or_insert(now);
            session.exit_code.get_or_insert(1);
            session
                .exit_signal
                .get_or_insert_with(|| "daemon restart".to_string());
            if let Err(error) = store.upsert(&session) {
                tracing::warn!(session_id = %session.id, %error, "failed to mark session stale");
            }
        }
        loaded.insert(session.id.clone(), session);
    }
    loaded
}

fn normalize_restart_metadata(
    agent: Option<&str>,
    mut restart: SessionRestartMetadata,
) -> SessionRestartMetadata {
    let command_name = Path::new(&restart.command)
        .file_name()
        .and_then(|name| name.to_str())
        .unwrap_or(restart.command.as_str());
    if agent == Some("claude-code")
        && command_name == "claude"
        && restart.args.first().is_some_and(|arg| arg == "code")
    {
        restart.args.remove(0);
    }
    restart
}

#[derive(Debug, Deserialize)]
struct RequestFrame {
    #[serde(default)]
    id: Option<String>,
    #[serde(default)]
    protocol_version: Option<String>,
    command: String,
    #[serde(default)]
    token: Option<String>,
    #[serde(default)]
    payload: Value,
}

#[derive(Debug, Serialize)]
struct ResponseFrame {
    id: Option<String>,
    protocol_version: &'static str,
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    payload: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<ErrorFrame>,
}

#[derive(Debug, Serialize)]
struct ErrorFrame {
    code: &'static str,
    message: String,
}

#[derive(Clone)]
pub struct OrchestrationState {
    manager: Arc<PtyManager>,
    token: String,
    sessions: Arc<RwLock<HashMap<String, SessionInfo>>>,
    store: Option<Arc<SessionMetadataStore>>,
    screens: Arc<RwLock<HashMap<String, TerminalScreen>>>,
    events: broadcast::Sender<OrchestrationEvent>,
}

impl OrchestrationState {
    pub fn new(token: String) -> Arc<Self> {
        let store = match SessionMetadataStore::open_default() {
            Ok(store) => Some(Arc::new(store)),
            Err(error) => {
                tracing::warn!(%error, "session metadata persistence unavailable");
                None
            }
        };
        Self::with_store(token, store)
    }

    fn with_store(token: String, store: Option<Arc<SessionMetadataStore>>) -> Arc<Self> {
        let (events, _) = broadcast::channel(1024);
        let sessions = store
            .as_ref()
            .map(|store| load_persisted_sessions(store))
            .unwrap_or_default();
        Arc::new(Self {
            manager: PtyManager::new(),
            token,
            sessions: Arc::new(RwLock::new(sessions)),
            store,
            screens: Arc::new(RwLock::new(HashMap::new())),
            events,
        })
    }

    pub async fn start_listeners(self: Arc<Self>) -> Result<()> {
        let tcp = TcpListener::bind(("127.0.0.1", DEFAULT_ORCHESTRATION_PORT))
            .await
            .with_context(|| {
                format!(
                    "bind orchestration TCP on 127.0.0.1:{}",
                    DEFAULT_ORCHESTRATION_PORT
                )
            })?;
        let tcp_state = self.clone();
        tokio::spawn(async move {
            loop {
                match tcp.accept().await {
                    Ok((stream, _)) => {
                        let state = tcp_state.clone();
                        tokio::spawn(async move {
                            let _ = handle_connection(stream, state, false).await;
                        });
                    }
                    Err(error) => {
                        tracing::warn!(%error, "orchestration TCP accept failed");
                        break;
                    }
                }
            }
        });

        #[cfg(unix)]
        {
            let path = orchestration_socket_path()?;
            if path.exists() {
                let _ = std::fs::remove_file(&path);
            }
            if let Some(parent) = path.parent() {
                std::fs::create_dir_all(parent)
                    .with_context(|| format!("create {}", parent.display()))?;
            }
            let unix = UnixListener::bind(&path)
                .with_context(|| format!("bind orchestration socket {}", path.display()))?;
            let unix_state = self.clone();
            tokio::spawn(async move {
                loop {
                    match unix.accept().await {
                        Ok((stream, _)) => {
                            let state = unix_state.clone();
                            tokio::spawn(async move {
                                let _ = handle_connection(stream, state, true).await;
                            });
                        }
                        Err(error) => {
                            tracing::warn!(%error, "orchestration Unix accept failed");
                            break;
                        }
                    }
                }
            });
        }

        Ok(())
    }

    pub async fn set_status(&self, session_id: &str, status: AgentStatus) {
        let mut sessions = self.sessions.write().await;
        if let Some(session) = sessions.get_mut(session_id) {
            if session.status != status {
                session.status = status;
                session.updated_at = now_millis();
                let updated = session.clone();
                drop(sessions);
                self.persist_session(&updated);
                let _ = self.events.send(OrchestrationEvent::SessionStatus {
                    session_id: session_id.to_string(),
                    status,
                    session: Some(updated),
                });
            }
        }
    }

    pub async fn summary(&self) -> OrchestrationSummary {
        let sessions = self.sessions.read().await;
        let total_sessions = sessions.len();
        let running_sessions = sessions
            .values()
            .filter(|session| session.lifecycle == SessionLifecycle::Running)
            .count();
        let stale_sessions = sessions
            .values()
            .filter(|session| session.lifecycle == SessionLifecycle::Stale)
            .count();
        let stopped_sessions = sessions
            .values()
            .filter(|session| session.lifecycle == SessionLifecycle::Stopped)
            .count();
        OrchestrationSummary {
            total_sessions,
            running_sessions,
            stale_sessions,
            stopped_sessions,
            pane_count: running_sessions,
            socket_path: orchestration_socket_path()
                .ok()
                .map(|path| path.display().to_string()),
        }
    }

    pub async fn stop_all_running_sessions(&self) -> usize {
        let ids = self.manager.list().await;
        let mut stopped = 0;
        for id in ids {
            if self.manager.kill(id).await.is_ok() {
                stopped += 1;
            }
            self.mark_session_stopped(&id.to_string(), None).await;
        }
        stopped
    }

    fn persist_session(&self, session: &SessionInfo) {
        if let Some(store) = &self.store {
            if let Err(error) = store.upsert(session) {
                tracing::warn!(session_id = %session.id, %error, "failed to persist session metadata");
            }
        }
    }

    async fn upsert_session(&self, session: SessionInfo) {
        self.sessions
            .write()
            .await
            .insert(session.id.clone(), session.clone());
        self.persist_session(&session);
    }

    #[cfg(test)]
    pub async fn upsert_session_for_test(&self, session: SessionInfo) {
        self.upsert_session(session).await;
    }

    #[cfg(test)]
    pub async fn spawn_for_test(&self, req: PtySpawnRequest) -> Result<SessionInfo> {
        let metadata = SpawnMetadata::from_payload(&json!({}), &req);
        self.spawn_from_request(req, metadata).await
    }

    async fn mark_session_stopped(
        &self,
        session_id: &str,
        exit: Option<crate::pty::PtyExitStatus>,
    ) -> Option<SessionInfo> {
        let now = now_millis();
        let mut sessions = self.sessions.write().await;
        let session = sessions.get_mut(session_id)?;
        session.status = AgentStatus::Done;
        session.lifecycle = SessionLifecycle::Stopped;
        session.updated_at = now;
        session.stopped_at = Some(now);
        if let Some(exit) = exit {
            session.exit_code = Some(exit.code);
            session.exit_signal = exit.signal;
        }
        let updated = session.clone();
        drop(sessions);
        self.persist_session(&updated);
        let _ = self.events.send(OrchestrationEvent::SessionStatus {
            session_id: session_id.to_string(),
            status: AgentStatus::Done,
            session: Some(updated.clone()),
        });
        Some(updated)
    }

    async fn set_heuristic_status(&self, session_id: &str, status: AgentStatus) {
        let current = self
            .sessions
            .read()
            .await
            .get(session_id)
            .map(|session| session.status);
        if matches!(current, Some(AgentStatus::Blocked | AgentStatus::Done)) {
            return;
        }
        self.set_status(session_id, status).await;
    }

    async fn set_heuristic_agent(&self, session_id: &str, agent: &'static str) -> Option<String> {
        let mut sessions = self.sessions.write().await;
        let session = sessions.get_mut(session_id)?;
        if session.agent.is_some() {
            return session.agent.clone();
        }
        session.agent = Some(agent.to_string());
        session.updated_at = now_millis();
        let status = session.status;
        let updated = session.clone();
        drop(sessions);
        self.persist_session(&updated);
        let _ = self.events.send(OrchestrationEvent::SessionStatus {
            session_id: session_id.to_string(),
            status,
            session: Some(updated.clone()),
        });
        updated.agent
    }

    pub async fn apply_provider_event(&self, update: ProviderEventUpdate) -> Option<SessionInfo> {
        let now = now_millis();
        let mut sessions = self.sessions.write().await;
        let target_id = resolve_provider_event_session(&sessions, &update)?;
        let session = sessions.get_mut(&target_id)?;
        if session.agent.is_none() {
            session.agent = Some(update.agent.clone());
        }
        if session.cwd.is_none() {
            session.cwd = update.cwd.clone();
        }
        if let Some(status) = update.status {
            session.status = status;
        }
        let previous_provider = session.provider.clone();
        session.provider = Some(ProviderSessionMetadata {
            agent: update.agent,
            provider_session_id: update.provider_session_id.or_else(|| {
                previous_provider
                    .as_ref()
                    .and_then(|item| item.provider_session_id.clone())
            }),
            conversation_id: update.conversation_id.or_else(|| {
                previous_provider
                    .as_ref()
                    .and_then(|item| item.conversation_id.clone())
            }),
            resume: update.resume.or_else(|| {
                previous_provider
                    .as_ref()
                    .and_then(|item| item.resume.clone())
            }),
            updated_at: now,
        });
        session.updated_at = now;
        let status = session.status;
        let updated = session.clone();
        drop(sessions);
        self.persist_session(&updated);
        let _ = self.events.send(OrchestrationEvent::SessionStatus {
            session_id: target_id,
            status,
            session: Some(updated.clone()),
        });
        Some(updated)
    }

    async fn session_agent(&self, session_id: &str) -> Option<String> {
        self.sessions
            .read()
            .await
            .get(session_id)
            .and_then(|session| session.agent.clone())
    }

    async fn dispatch(&self, command: &str, payload: Value) -> Result<Value> {
        match command {
            "hello" => Ok(json!({
                "protocol_version": PROTOCOL_VERSION,
                "features": [
                    "pty.spawn",
                    "pty.write",
                    "pty.resize",
                    "pty.kill",
                    "pty.list",
                    "pty.replay",
                    "session.list",
                    "session.attach",
                    "session.stop",
                    "session.set_trust",
                    "pane.read",
                    "pane.send_keys",
                    "wait.output",
                    "wait.agent_status",
                    "agent.list",
                    "agent.read",
                    "agent.send",
                    "agent.start",
                    "agent.focus",
                    "events.subscribe"
                ],
                "tcpPort": DEFAULT_ORCHESTRATION_PORT,
                "socketPath": orchestration_socket_path().ok().map(|path| path.display().to_string()),
            })),
            other => match classify_command(other) {
                Some(CommandKind::Spawn) => self.spawn(payload).await,
                Some(CommandKind::Write) => self.write(payload).await,
                Some(CommandKind::Resize) => self.resize(payload).await,
                Some(CommandKind::Kill) => self.kill(payload).await,
                Some(CommandKind::ListLive) => self.list_live().await,
                Some(CommandKind::Replay) => self.replay(payload).await,
                Some(CommandKind::ListSessions) => self.list_sessions().await,
                Some(CommandKind::Attach) => self.attach(payload).await,
                Some(CommandKind::Stop) => self.stop(payload).await,
                Some(CommandKind::SetTrust) => self.set_trust(payload).await,
                Some(CommandKind::Read) => self.read(payload).await,
                Some(CommandKind::SendKeys) => self.send_keys(payload).await,
                Some(CommandKind::WaitOutput) => self.wait_output(payload).await,
                Some(CommandKind::WaitAgentStatus) => self.wait_agent_status(payload).await,
                Some(CommandKind::Focus) => self.focus(payload).await,
                Some(CommandKind::Subscribe) | None => {
                    Err(anyhow!("unknown orchestration command: {other}"))
                }
            },
        }
    }

    async fn spawn(&self, payload: Value) -> Result<Value> {
        let mut req: PtySpawnRequest =
            serde_json::from_value(payload.clone()).context("parse pty spawn request")?;
        if req.rows == 0 {
            req.rows = 30;
        }
        if req.cols == 0 {
            req.cols = 100;
        }
        let metadata = SpawnMetadata::from_payload(&payload, &req);
        let session = self.spawn_from_request(req, metadata).await?;
        Ok(json!({
            "id": session.id,
            "sessionId": session.id,
            "paneId": session.pane_id,
            "session": session,
        }))
    }

    async fn spawn_from_request(
        &self,
        req: PtySpawnRequest,
        metadata: SpawnMetadata,
    ) -> Result<SessionInfo> {
        self.ensure_name_available(metadata.name.as_deref(), None)
            .await?;
        let restart = restart_metadata_from_request(&req);
        let safe_mode = req.safe_mode;
        let trust_mode = req.trust_mode;
        let rows = req.rows.max(1);
        let cols = req.cols.max(1);
        let id = self.manager.spawn(req).await?;
        let process_id = self.manager.process_id(id).ok().flatten();
        let id_string = id.to_string();
        let now = now_millis();
        let remote = metadata.remote.clone().or_else(|| restart.remote.clone());
        let session = SessionInfo {
            id: id_string.clone(),
            pane_id: id_string.clone(),
            name: metadata.name,
            agent: metadata.agent,
            workspace_id: metadata.workspace_id,
            safe_mode,
            trust_mode,
            cwd: metadata.cwd,
            title: metadata.title,
            status: AgentStatus::Working,
            lifecycle: SessionLifecycle::Running,
            rows,
            cols,
            created_at: now,
            updated_at: now,
            process_id,
            stopped_at: None,
            exit_code: None,
            exit_signal: None,
            restart: Some(restart),
            provider: None,
            remote,
        };
        self.upsert_session(session.clone()).await;
        self.screens
            .write()
            .await
            .insert(id_string, TerminalScreen::new(rows as usize, cols as usize));
        let _ = self.events.send(OrchestrationEvent::SessionStarted {
            session: session.clone(),
        });
        self.spawn_event_relay(id).await?;
        self.spawn_process_detection_relay(id);
        Ok(session)
    }

    async fn spawn_event_relay(&self, id: PtyId) -> Result<()> {
        let mut rx = self.manager.subscribe(id)?;
        let state = self.clone();
        tokio::spawn(async move {
            while let Ok(event) = rx.recv().await {
                match event {
                    PtyEvent::Data { bytes, offset } => {
                        let text = String::from_utf8_lossy(&bytes).to_string();
                        let encoded = STANDARD.encode(&bytes);
                        let id = id.to_string();
                        let mut agent = state.session_agent(&id).await;
                        if agent.is_none() {
                            if let Some(detected_agent) = infer_agent_from_output(&text) {
                                agent = state.set_heuristic_agent(&id, detected_agent).await;
                            }
                        }
                        if let Some(status) = infer_status_from_output(agent.as_deref(), &text) {
                            state.set_heuristic_status(&id, status).await;
                        }
                        if let Some(screen) = state.screens.write().await.get_mut(&id) {
                            screen.feed(&text);
                        }
                        let _ = state.events.send(OrchestrationEvent::PtyOutput {
                            session_id: id.clone(),
                            pane_id: id,
                            data: encoded,
                            text,
                            offset,
                        });
                    }
                    PtyEvent::Exit(status) => {
                        let id_string = id.to_string();
                        state
                            .mark_session_stopped(&id_string, Some(status.clone()))
                            .await;
                        let _ = state.events.send(OrchestrationEvent::PtyExit {
                            session_id: id_string.clone(),
                            pane_id: id_string,
                            code: status.code,
                            signal: status.signal,
                        });
                        break;
                    }
                    PtyEvent::Notification(notification) => {
                        let id = id.to_string();
                        let _ = state.events.send(OrchestrationEvent::PtyNotification {
                            session_id: id.clone(),
                            pane_id: id,
                            notification,
                        });
                    }
                }
            }
        });
        Ok(())
    }

    fn spawn_process_detection_relay(&self, id: PtyId) {
        let state = self.clone();
        tokio::spawn(async move {
            let id_string = id.to_string();
            let mut interval = time::interval(Duration::from_millis(750));
            interval.set_missed_tick_behavior(time::MissedTickBehavior::Skip);
            loop {
                interval.tick().await;
                if state.session_agent(&id_string).await.is_some() {
                    break;
                }
                if !state.session_is_live(&id_string).await {
                    break;
                }
                let Some(root_pid) = state.manager.process_id(id).ok().flatten() else {
                    break;
                };
                if let Some(agent) = infer_agent_from_process_tree(root_pid) {
                    state.set_heuristic_agent(&id_string, agent).await;
                    break;
                }
            }
        });
    }

    async fn write(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let bytes = if let Some(data) = payload.get("data").and_then(Value::as_str) {
            STANDARD.decode(data).context("decode base64 data")?
        } else {
            payload
                .get("text")
                .and_then(Value::as_str)
                .unwrap_or_default()
                .as_bytes()
                .to_vec()
        };
        self.manager.write(id, &bytes).await?;
        Ok(json!({"ok": true, "bytes": bytes.len()}))
    }

    async fn send_keys(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let mut bytes = Vec::new();
        if let Some(keys) = payload.get("keys").and_then(Value::as_array) {
            for key in keys {
                let Some(key) = key.as_str() else { continue };
                bytes.extend_from_slice(key_to_bytes(key)?.as_ref());
            }
        } else if let Some(key) = payload.get("key").and_then(Value::as_str) {
            bytes.extend_from_slice(key_to_bytes(key)?.as_ref());
        }
        self.manager.write(id, &bytes).await?;
        Ok(json!({"ok": true, "bytes": bytes.len()}))
    }

    async fn resize(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let rows = payload.get("rows").and_then(Value::as_u64).unwrap_or(30) as u16;
        let cols = payload.get("cols").and_then(Value::as_u64).unwrap_or(100) as u16;
        self.manager.resize(id, rows, cols).await?;
        if let Some(session) = self.sessions.write().await.get_mut(&id.to_string()) {
            session.rows = rows;
            session.cols = cols;
            session.updated_at = now_millis();
            self.persist_session(&session.clone());
        }
        if let Some(screen) = self.screens.write().await.get_mut(&id.to_string()) {
            screen.resize(rows as usize, cols as usize);
        }
        Ok(json!({"ok": true}))
    }

    async fn kill(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        self.manager.kill(id).await?;
        let session = self.mark_session_stopped(&id.to_string(), None).await;
        Ok(json!({"ok": true, "session": session}))
    }

    async fn stop(&self, payload: Value) -> Result<Value> {
        let session = self.resolve_session_record(&payload).await?;
        if session.lifecycle == SessionLifecycle::Running {
            if let Ok(id) = session.id.parse() {
                let _ = self.manager.kill(id).await;
            }
        }
        let stopped = self
            .mark_session_stopped(&session.id, None)
            .await
            .unwrap_or(session);
        Ok(json!({"ok": true, "session": stopped}))
    }

    async fn set_trust(&self, payload: Value) -> Result<Value> {
        let session = self.resolve_session_record(&payload).await?;
        let mode = payload
            .get("trustMode")
            .or_else(|| payload.get("trust_mode"))
            .and_then(Value::as_str)
            .unwrap_or("approval-required");
        let trust_mode = match mode {
            "approval-required" => TrustMode::ApprovalRequired,
            "full-access" => TrustMode::FullAccess,
            other => {
                anyhow::bail!("trustMode must be 'approval-required' or 'full-access', got {other}")
            }
        };
        let mut sessions = self.sessions.write().await;
        let current = sessions
            .get_mut(&session.id)
            .ok_or_else(|| anyhow!("session not found: {}", session.id))?;
        current.trust_mode = trust_mode;
        current.updated_at = now_millis();
        let updated = current.clone();
        drop(sessions);
        self.persist_session(&updated);
        let _ = self.events.send(OrchestrationEvent::SessionStatus {
            session_id: updated.id.clone(),
            status: updated.status,
            session: Some(updated.clone()),
        });
        Ok(json!({"ok": true, "session": updated}))
    }

    async fn attach(&self, payload: Value) -> Result<Value> {
        let session = self.resolve_session_record(&payload).await?;
        if session.lifecycle == SessionLifecycle::Running && self.session_is_live(&session.id).await
        {
            let _ = self.events.send(OrchestrationEvent::AgentFocusRequested {
                session: session.clone(),
            });
            return Ok(json!({
                "ok": true,
                "attached": true,
                "relaunched": false,
                "id": session.id,
                "sessionId": session.id,
                "paneId": session.pane_id,
                "session": session,
            }));
        }

        let restart = provider_restart_metadata(&session)
            .or_else(|| session.restart.clone())
            .ok_or_else(|| anyhow!("session has no restart metadata: {}", session.id))?;
        let restart = normalize_restart_metadata(session.agent.as_deref(), restart);
        let remote = restart.remote.clone().or_else(|| session.remote.clone());
        let previous_id = session.id.clone();
        self.mark_session_stopped(&previous_id, None).await;
        let req = PtySpawnRequest {
            command: restart.command,
            args: restart.args,
            cwd: restart.cwd.map(PathBuf::from),
            env: restart.env,
            shell_mode: restart.shell_mode,
            safe_mode: restart.safe_mode,
            trust_mode: restart.trust_mode,
            rows: session.rows.max(1),
            cols: session.cols.max(1),
            name: session.name.clone(),
            agent: session.agent.clone(),
            workspace_id: session.workspace_id.clone(),
            title: session.title.clone(),
            remote,
        };
        let metadata = SpawnMetadata {
            name: session.name.clone(),
            agent: session.agent.clone(),
            workspace_id: session.workspace_id.clone(),
            cwd: req.cwd.as_ref().map(|path| path.display().to_string()),
            title: session.title.clone(),
            remote: req.remote.clone(),
        };
        let relaunched = self.spawn_from_request(req, metadata).await?;
        Ok(json!({
            "ok": true,
            "attached": true,
            "relaunched": true,
            "previousSessionId": previous_id,
            "id": relaunched.id,
            "sessionId": relaunched.id,
            "paneId": relaunched.pane_id,
            "session": relaunched,
        }))
    }

    pub async fn session_safe_mode(&self, session_id: &str) -> bool {
        self.sessions
            .read()
            .await
            .get(session_id)
            .is_some_and(|session| session.safe_mode)
    }

    pub async fn session_trust_mode(&self, session_id: &str) -> TrustMode {
        self.sessions
            .read()
            .await
            .get(session_id)
            .map(|session| session.trust_mode)
            .unwrap_or_default()
    }

    pub async fn session_for_pane(&self, pane_id: &str) -> Option<SessionInfo> {
        let sessions = self.sessions.read().await;
        sessions.get(pane_id).cloned().or_else(|| {
            sessions
                .values()
                .find(|session| session.pane_id == pane_id)
                .cloned()
        })
    }

    pub async fn pane_targets(&self) -> Vec<SessionInfo> {
        let mut sessions = self
            .sessions
            .read()
            .await
            .values()
            .filter(|session| session.lifecycle != SessionLifecycle::Stopped)
            .cloned()
            .collect::<Vec<_>>();
        sessions.sort_by(|left, right| {
            right
                .updated_at
                .cmp(&left.updated_at)
                .then_with(|| right.created_at.cmp(&left.created_at))
        });
        sessions
    }

    pub async fn send_text_to_pane(
        &self,
        pane_id: &str,
        text: &str,
        send_enter: bool,
    ) -> Result<(String, usize)> {
        self.send_remote_input_to_pane(pane_id, Some(text), &[], send_enter)
            .await
    }

    pub async fn send_keys_to_pane(
        &self,
        pane_id: &str,
        keys: &[String],
    ) -> Result<(String, usize)> {
        self.send_remote_input_to_pane(pane_id, None, keys, false)
            .await
    }

    pub async fn send_remote_input_to_pane(
        &self,
        pane_id: &str,
        text: Option<&str>,
        keys: &[String],
        send_enter: bool,
    ) -> Result<(String, usize)> {
        let id = self
            .resolve_target_id(&json!({ "paneId": pane_id }))
            .await
            .with_context(|| format!("resolve pane {pane_id}"))?;
        let mut bytes = Vec::new();
        if let Some(text) = text {
            bytes.extend_from_slice(text.as_bytes());
        }
        for key in keys {
            bytes.extend_from_slice(key_to_bytes(key)?.as_ref());
        }
        if send_enter {
            bytes.push(b'\r');
        }
        self.manager.write(id, &bytes).await?;
        Ok((id.to_string(), bytes.len()))
    }

    async fn session_is_live(&self, session_id: &str) -> bool {
        let live = self
            .manager
            .list()
            .await
            .into_iter()
            .map(|id| id.to_string())
            .collect::<Vec<_>>();
        live.iter().any(|id| id == session_id)
    }

    async fn list_live(&self) -> Result<Value> {
        let active = self
            .manager
            .list()
            .await
            .into_iter()
            .map(|id| id.to_string())
            .collect::<Vec<_>>();
        let sessions = self.sessions.read().await;
        let items = active
            .iter()
            .filter_map(|id| sessions.get(id).cloned())
            .collect::<Vec<_>>();
        Ok(json!({"sessions": items}))
    }

    async fn list_sessions(&self) -> Result<Value> {
        let mut items = self
            .sessions
            .read()
            .await
            .values()
            .cloned()
            .collect::<Vec<_>>();
        items.sort_by(|left, right| {
            right
                .updated_at
                .cmp(&left.updated_at)
                .then_with(|| right.created_at.cmp(&left.created_at))
        });
        Ok(json!({"sessions": items}))
    }

    async fn ensure_name_available(
        &self,
        name: Option<&str>,
        replacing_id: Option<&str>,
    ) -> Result<()> {
        let Some(name) = name else {
            return Ok(());
        };
        let needle = name.to_lowercase();
        let sessions = self.sessions.read().await;
        if let Some(existing) = sessions.values().find(|session| {
            session.lifecycle != SessionLifecycle::Stopped
                && replacing_id != Some(session.id.as_str())
                && session
                    .name
                    .as_deref()
                    .is_some_and(|candidate| candidate.to_lowercase() == needle)
        }) {
            return Err(anyhow!(
                "session name is already in use by {}: {name}",
                existing.id
            ));
        }
        Ok(())
    }

    async fn replay(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        Ok(snapshot_json(self.manager.output_snapshot(id)?))
    }

    async fn read(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let source = payload
            .get("source")
            .and_then(Value::as_str)
            .unwrap_or("recent");
        let format = payload
            .get("format")
            .and_then(Value::as_str)
            .unwrap_or("text");
        let limit = payload.get("limit").and_then(Value::as_u64).unwrap_or(0) as usize;
        let snapshot = self.manager.output_snapshot(id)?;
        let session = self.sessions.read().await.get(&id.to_string()).cloned();
        let mut bytes = snapshot.data.to_vec();
        if limit > 0 && bytes.len() > limit {
            bytes = bytes[bytes.len() - limit..].to_vec();
        }
        let raw_text = String::from_utf8_lossy(&bytes).to_string();
        let text = match source {
            "visible" => self
                .screens
                .read()
                .await
                .get(&id.to_string())
                .map(TerminalScreen::visible_text)
                .unwrap_or_else(|| {
                    let rows = session.as_ref().map(|item| item.rows).unwrap_or(30) as usize;
                    tail_lines(&strip_ansi(&raw_text), rows)
                }),
            "recent-unwrapped" => {
                let cols = session.as_ref().map(|item| item.cols).unwrap_or(100) as usize;
                unwrap_recent_lines(&strip_ansi(&raw_text), cols)
            }
            _ if format == "ansi" => raw_text,
            _ => strip_ansi(&raw_text),
        };
        Ok(json!({
            "id": id.to_string(),
            "source": source,
            "format": format,
            "text": text,
            "data": STANDARD.encode(&bytes),
            "startOffset": snapshot.start_offset,
            "endOffset": snapshot.end_offset,
        }))
    }

    async fn wait_output(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let timeout_ms = payload
            .get("timeoutMs")
            .or_else(|| payload.get("timeout_ms"))
            .and_then(Value::as_u64)
            .unwrap_or(30_000);
        let matcher = OutputMatcher::from_payload(&payload)?;
        let snapshot = self.manager.output_snapshot(id)?;
        let snapshot_text = String::from_utf8_lossy(&snapshot.data);
        if matcher.is_match(&snapshot_text) {
            return Ok(json!({"matched": true, "source": "snapshot"}));
        }
        let mut rx = self.manager.subscribe(id)?;
        let wait = async {
            loop {
                match rx.recv().await? {
                    PtyEvent::Data { bytes, .. } => {
                        let text = String::from_utf8_lossy(&bytes);
                        if matcher.is_match(&text) {
                            return Ok::<_, anyhow::Error>(
                                json!({"matched": true, "source": "event"}),
                            );
                        }
                    }
                    PtyEvent::Exit(_) => {
                        return Err(anyhow!("session exited before output matched"))
                    }
                    PtyEvent::Notification(_) => {}
                }
            }
        };
        time::timeout(Duration::from_millis(timeout_ms), wait)
            .await
            .map_err(|_| anyhow!("timeout waiting for output"))?
    }

    async fn wait_agent_status(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let target_status: AgentStatus = serde_json::from_value(
            payload
                .get("status")
                .cloned()
                .ok_or_else(|| anyhow!("missing status"))?,
        )
        .context("parse status")?;
        let timeout_ms = payload
            .get("timeoutMs")
            .or_else(|| payload.get("timeout_ms"))
            .and_then(Value::as_u64)
            .unwrap_or(30_000);
        if self
            .sessions
            .read()
            .await
            .get(&id.to_string())
            .is_some_and(|session| session.status == target_status)
        {
            return Ok(json!({"matched": true, "status": target_status}));
        }
        let mut rx = self.events.subscribe();
        let wait = async {
            loop {
                if let OrchestrationEvent::SessionStatus {
                    session_id, status, ..
                } = rx.recv().await?
                {
                    if session_id == id.to_string() && status == target_status {
                        return Ok::<_, anyhow::Error>(json!({"matched": true, "status": status}));
                    }
                }
            }
        };
        time::timeout(Duration::from_millis(timeout_ms), wait)
            .await
            .map_err(|_| anyhow!("timeout waiting for agent status"))?
    }

    async fn focus(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        let id_string = id.to_string();
        if self
            .sessions
            .read()
            .await
            .get(&id_string)
            .is_some_and(|session| session.status == AgentStatus::Done)
        {
            self.set_status(&id_string, AgentStatus::Idle).await;
        }
        let session = self
            .sessions
            .read()
            .await
            .get(&id_string)
            .cloned()
            .ok_or_else(|| anyhow!("session not found: {id}"))?;
        let _ = self.events.send(OrchestrationEvent::AgentFocusRequested {
            session: session.clone(),
        });
        Ok(json!({"ok": true, "session": session}))
    }

    async fn resolve_session_record(&self, payload: &Value) -> Result<SessionInfo> {
        let raw = ["id", "sessionId", "paneId", "name", "target"]
            .iter()
            .find_map(|key| payload.get(key).and_then(Value::as_str))
            .or_else(|| payload.as_str())
            .ok_or_else(|| anyhow!("missing session id or name"))?;
        let sessions = self.sessions.read().await;
        if let Some(session) = sessions.get(raw) {
            return Ok(session.clone());
        }
        let needle = raw.to_lowercase();
        let mut matches = sessions
            .values()
            .filter(|session| {
                session
                    .name
                    .as_deref()
                    .is_some_and(|name| name.to_lowercase() == needle)
                    || session
                        .title
                        .as_deref()
                        .is_some_and(|title| title.to_lowercase() == needle)
            })
            .cloned()
            .collect::<Vec<_>>();
        matches.sort_by_key(|session| match session.lifecycle {
            SessionLifecycle::Running => 0,
            SessionLifecycle::Stale => 1,
            SessionLifecycle::Stopped => 2,
        });
        match matches.as_slice() {
            [] => Err(anyhow!("session not found: {raw}")),
            [session] => Ok(session.clone()),
            [first, second, ..] if first.lifecycle != second.lifecycle => Ok(first.clone()),
            _ => Err(anyhow!("session target is ambiguous: {raw}")),
        }
    }

    async fn resolve_target_id(&self, payload: &Value) -> Result<PtyId> {
        for key in ["id", "paneId", "sessionId", "agentId"] {
            if let Some(raw) = payload.get(key).and_then(Value::as_str) {
                if let Ok(id) = raw.parse() {
                    return Ok(id);
                }
                if let Some(id) = self.resolve_agent_label(raw).await? {
                    return Ok(id);
                }
            }
        }
        if let Some(raw) = payload.get("agent").and_then(Value::as_str) {
            if let Some(id) = self.resolve_agent_label(raw).await? {
                return Ok(id);
            }
        }
        Err(anyhow!(
            "missing or ambiguous session, pane, or agent target"
        ))
    }

    async fn resolve_agent_label(&self, raw: &str) -> Result<Option<PtyId>> {
        let needle = raw.to_lowercase();
        let matches = self
            .sessions
            .read()
            .await
            .values()
            .filter(|session| {
                session.lifecycle == SessionLifecycle::Running
                    && (session
                        .name
                        .as_deref()
                        .is_some_and(|name| name.to_lowercase() == needle)
                        || session
                            .agent
                            .as_deref()
                            .is_some_and(|agent| agent.eq_ignore_ascii_case(raw))
                        || session
                            .title
                            .as_deref()
                            .is_some_and(|title| title.to_lowercase() == needle))
            })
            .map(|session| session.id.clone())
            .collect::<Vec<_>>();
        match matches.as_slice() {
            [] => Ok(None),
            [id] => id
                .parse()
                .map(Some)
                .with_context(|| format!("parse pty id {id}")),
            _ => Err(anyhow!("agent target is ambiguous: {raw}")),
        }
    }
}

#[derive(Debug, Clone)]
struct SpawnMetadata {
    name: Option<String>,
    agent: Option<String>,
    workspace_id: Option<String>,
    cwd: Option<String>,
    title: Option<String>,
    remote: Option<RemoteSessionMetadata>,
}

impl SpawnMetadata {
    fn from_payload(payload: &Value, req: &PtySpawnRequest) -> Self {
        let title = payload
            .get("title")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned)
            .or_else(|| req.title.clone());
        let explicit_agent = payload
            .get("agent")
            .and_then(Value::as_str)
            .or(req.agent.as_deref())
            .and_then(normalize_agent_label);
        let agent = explicit_agent.or_else(|| infer_agent_from_launch(req, title.as_deref()));
        Self {
            name: payload
                .get("name")
                .and_then(Value::as_str)
                .or(req.name.as_deref())
                .and_then(normalize_session_name),
            agent,
            workspace_id: payload
                .get("workspaceId")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned)
                .or_else(|| req.workspace_id.clone()),
            cwd: req.cwd.as_ref().map(|path| path.display().to_string()),
            title,
            remote: payload
                .get("remote")
                .cloned()
                .and_then(|value| serde_json::from_value(value).ok())
                .or_else(|| req.remote.clone()),
        }
    }
}

#[derive(Debug, Clone, Copy)]
struct AgentDetectionSpec {
    canonical: &'static str,
    command_aliases: &'static [&'static str],
    title_markers: &'static [&'static str],
    output_markers: &'static [&'static str],
}

const AGENT_DETECTION_SPECS: &[AgentDetectionSpec] = &[
    AgentDetectionSpec {
        canonical: "cursor",
        command_aliases: &["cursor", "cursor-agent"],
        title_markers: &["cursor", "cursor agent"],
        output_markers: &["cursor agent"],
    },
    AgentDetectionSpec {
        canonical: "cline",
        command_aliases: &["cline", "cline-agent"],
        title_markers: &["cline"],
        output_markers: &["cline agent", "cline wants", "welcome to cline"],
    },
    AgentDetectionSpec {
        canonical: "copilot",
        command_aliases: &["copilot", "gh-copilot"],
        title_markers: &["copilot", "github copilot"],
        output_markers: &["github copilot", "copilot cli"],
    },
    AgentDetectionSpec {
        canonical: "gemini",
        command_aliases: &["gemini", "gemini-cli"],
        title_markers: &["gemini"],
        output_markers: &["gemini cli", "welcome to gemini"],
    },
    AgentDetectionSpec {
        canonical: "grok",
        command_aliases: &["grok", "grok-cli"],
        title_markers: &["grok"],
        output_markers: &["grok cli", "welcome to grok"],
    },
    AgentDetectionSpec {
        canonical: "kimi",
        command_aliases: &["kimi", "kimi-cli"],
        title_markers: &["kimi"],
        output_markers: &["kimi cli", "welcome to kimi"],
    },
    AgentDetectionSpec {
        canonical: "kiro",
        command_aliases: &["kiro", "kiro-agent", "kiro-cli"],
        title_markers: &["kiro"],
        output_markers: &["kiro agent", "kiro cli", "welcome to kiro"],
    },
    AgentDetectionSpec {
        canonical: "droid",
        command_aliases: &["droid", "droid-cli", "droid-agent"],
        title_markers: &["droid"],
        output_markers: &["droid agent", "droid cli", "welcome to droid"],
    },
    AgentDetectionSpec {
        canonical: "amp",
        command_aliases: &["amp", "ampcode"],
        title_markers: &["amp"],
        output_markers: &["amp agent", "amp code", "amp cli"],
    },
    AgentDetectionSpec {
        canonical: "antigravity",
        command_aliases: &["antigravity", "antigravity-agent", "antigravity-cli"],
        title_markers: &["antigravity"],
        output_markers: &["antigravity agent", "antigravity cli"],
    },
    AgentDetectionSpec {
        canonical: "kilo",
        command_aliases: &["kilo", "kilo-code", "kilo-cli"],
        title_markers: &["kilo", "kilo code"],
        output_markers: &["kilo code", "kilo agent", "kilo cli"],
    },
];

fn normalize_agent_label(raw: &str) -> Option<String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return None;
    }
    Some(
        canonical_agent(trimmed)
            .map(ToOwned::to_owned)
            .unwrap_or_else(|| trimmed.to_string()),
    )
}

fn canonical_agent(raw: &str) -> Option<&'static str> {
    let token = normalized_agent_token(raw);
    AGENT_DETECTION_SPECS
        .iter()
        .find(|spec| spec.canonical == token || spec.command_aliases.contains(&token.as_str()))
        .map(|spec| spec.canonical)
}

fn infer_agent_from_launch(req: &PtySpawnRequest, title: Option<&str>) -> Option<String> {
    let command = normalized_agent_token(&req.command);
    if command == "gh"
        && req
            .args
            .first()
            .is_some_and(|arg| normalized_agent_token(arg) == "copilot")
    {
        return Some("copilot".to_string());
    }

    for spec in AGENT_DETECTION_SPECS {
        if spec.command_aliases.contains(&command.as_str()) {
            return Some(spec.canonical.to_string());
        }
        if req.args.iter().take(2).any(|arg| {
            let token = normalized_agent_token(arg);
            spec.command_aliases.contains(&token.as_str())
        }) {
            return Some(spec.canonical.to_string());
        }
    }

    let title = title.map(normalize_detection_text);
    title
        .as_deref()
        .and_then(|title| {
            AGENT_DETECTION_SPECS
                .iter()
                .find(|spec| contains_any_phrase(title, spec.title_markers))
        })
        .map(|spec| spec.canonical.to_string())
}

fn infer_agent_from_output(text: &str) -> Option<&'static str> {
    let normalized = normalize_detection_text(&strip_ansi(text));
    AGENT_DETECTION_SPECS
        .iter()
        .find(|spec| contains_any_phrase(&normalized, spec.output_markers))
        .map(|spec| spec.canonical)
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ProcessInfo {
    pid: u32,
    ppid: u32,
    command: String,
}

fn infer_agent_from_processes(root_pid: u32, processes: &[ProcessInfo]) -> Option<&'static str> {
    let mut descendants = HashSet::from([root_pid]);
    let mut changed = true;
    while changed {
        changed = false;
        for process in processes {
            if !descendants.contains(&process.pid) && descendants.contains(&process.ppid) {
                descendants.insert(process.pid);
                changed = true;
            }
        }
    }
    processes
        .iter()
        .filter(|process| descendants.contains(&process.pid))
        .filter_map(|process| canonical_agent(&process.command))
        .next()
}

#[cfg(unix)]
fn infer_agent_from_process_tree(root_pid: u32) -> Option<&'static str> {
    process_snapshot()
        .ok()
        .and_then(|processes| infer_agent_from_processes(root_pid, &processes))
}

#[cfg(not(unix))]
fn infer_agent_from_process_tree(_root_pid: u32) -> Option<&'static str> {
    None
}

#[cfg(unix)]
fn process_snapshot() -> std::io::Result<Vec<ProcessInfo>> {
    let output = Command::new("ps")
        .args(["-eo", "pid=,ppid=,comm="])
        .output()?;
    if !output.status.success() {
        return Ok(Vec::new());
    }
    Ok(String::from_utf8_lossy(&output.stdout)
        .lines()
        .filter_map(parse_process_line)
        .collect())
}

#[cfg(unix)]
fn parse_process_line(line: &str) -> Option<ProcessInfo> {
    let mut parts = line.split_whitespace();
    let pid = parts.next()?.trim().parse().ok()?;
    let ppid = parts.next()?.trim().parse().ok()?;
    let command = parts.collect::<Vec<_>>().join(" ");
    (!command.is_empty()).then(|| ProcessInfo {
        pid,
        ppid,
        command: command.to_string(),
    })
}

fn normalized_agent_token(raw: &str) -> String {
    let trimmed = raw.trim().trim_matches('"').trim_matches('\'');
    let basename = Path::new(trimmed)
        .file_name()
        .and_then(|name| name.to_str())
        .unwrap_or(trimmed);
    basename.to_ascii_lowercase()
}

fn normalize_detection_text(raw: &str) -> String {
    raw.to_ascii_lowercase()
        .replace(['_', '-'], " ")
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
}

fn restart_metadata_from_request(req: &PtySpawnRequest) -> SessionRestartMetadata {
    SessionRestartMetadata {
        command: req.command.clone(),
        args: req.args.clone(),
        cwd: req.cwd.as_ref().map(|path| path.display().to_string()),
        env: req.env.clone(),
        shell_mode: req.shell_mode,
        safe_mode: req.safe_mode,
        trust_mode: req.trust_mode,
        remote: req.remote.clone(),
    }
}

fn provider_restart_metadata(session: &SessionInfo) -> Option<SessionRestartMetadata> {
    let resume = session.provider.as_ref()?.resume.as_ref()?;
    Some(SessionRestartMetadata {
        command: resume.command.clone(),
        args: resume.args.clone(),
        cwd: session
            .cwd
            .clone()
            .or_else(|| session.restart.as_ref()?.cwd.clone()),
        env: session
            .restart
            .as_ref()
            .map(|restart| restart.env.clone())
            .unwrap_or_default(),
        shell_mode: session
            .restart
            .as_ref()
            .map(|restart| restart.shell_mode)
            .unwrap_or_default(),
        safe_mode: session.safe_mode,
        trust_mode: session.trust_mode,
        remote: session
            .remote
            .clone()
            .or_else(|| session.restart.as_ref()?.remote.clone()),
    })
}

#[derive(Debug)]
enum OutputMatcher {
    Literal(String),
    Regex(Regex),
}

impl OutputMatcher {
    fn from_payload(payload: &Value) -> Result<Self> {
        if let Some(pattern) = payload.get("regex").and_then(Value::as_str) {
            return Ok(Self::Regex(Regex::new(pattern)?));
        }
        let needle = payload
            .get("match")
            .or_else(|| payload.get("text"))
            .and_then(Value::as_str)
            .ok_or_else(|| anyhow!("missing match text or regex"))?;
        Ok(Self::Literal(needle.to_string()))
    }

    fn is_match(&self, text: &str) -> bool {
        match self {
            Self::Literal(needle) => text.contains(needle),
            Self::Regex(regex) => regex.is_match(text),
        }
    }
}

fn contains_any_phrase(haystack: &str, needles: &[&str]) -> bool {
    needles
        .iter()
        .any(|needle| contains_phrase(haystack, needle))
}

fn contains_phrase(haystack: &str, needle: &str) -> bool {
    if needle.contains(' ') {
        return haystack.contains(needle);
    }
    haystack.split_whitespace().any(|token| token == needle)
}

async fn handle_connection<S>(
    stream: S,
    state: Arc<OrchestrationState>,
    trusted: bool,
) -> Result<()>
where
    S: AsyncRead + AsyncWrite + Unpin,
{
    let (reader, mut writer) = tokio::io::split(stream);
    let mut reader = BufReader::new(reader);
    let mut line = String::new();
    let mut authed = trusted;
    loop {
        line.clear();
        if reader.read_line(&mut line).await? == 0 {
            return Ok(());
        }
        let frame: RequestFrame = match serde_json::from_str(line.trim_end()) {
            Ok(frame) => frame,
            Err(error) => {
                write_response(&mut writer, ResponseFrame::error(None, "bad_json", error)).await?;
                continue;
            }
        };
        if frame
            .protocol_version
            .as_deref()
            .is_some_and(|version| version != PROTOCOL_VERSION)
        {
            write_response(
                &mut writer,
                ResponseFrame::error(frame.id, "protocol_mismatch", "protocol_version mismatch"),
            )
            .await?;
            continue;
        }
        if frame.command == "hello" {
            if !trusted && !frame_token(&frame).is_some_and(|token| token == state.token) {
                write_response(
                    &mut writer,
                    ResponseFrame::error(frame.id, "unauthorized", "invalid bearer token"),
                )
                .await?;
                continue;
            }
            authed = true;
            let payload = state.dispatch("hello", frame.payload).await?;
            write_response(&mut writer, ResponseFrame::ok(frame.id, payload)).await?;
            continue;
        }
        if !authed {
            write_response(
                &mut writer,
                ResponseFrame::error(frame.id, "unauthorized", "send hello first"),
            )
            .await?;
            continue;
        }
        if frame.command == "events.subscribe" {
            write_response(
                &mut writer,
                ResponseFrame::ok(frame.id, json!({"subscribed": true})),
            )
            .await?;
            stream_events(&mut writer, state.events.subscribe()).await?;
            return Ok(());
        }
        let id = frame.id;
        match state.dispatch(&frame.command, frame.payload).await {
            Ok(payload) => write_response(&mut writer, ResponseFrame::ok(id, payload)).await?,
            Err(error) => {
                write_response(
                    &mut writer,
                    ResponseFrame::error(id, "command_error", error),
                )
                .await?
            }
        }
    }
}

async fn stream_events<W>(
    writer: &mut W,
    mut rx: broadcast::Receiver<OrchestrationEvent>,
) -> Result<()>
where
    W: AsyncWrite + Unpin,
{
    while let Ok(event) = rx.recv().await {
        let frame = json!({
            "protocol_version": PROTOCOL_VERSION,
            "event": event,
        });
        write_json_line(writer, &frame).await?;
    }
    Ok(())
}

fn frame_token(frame: &RequestFrame) -> Option<&str> {
    frame
        .token
        .as_deref()
        .or_else(|| frame.payload.get("token").and_then(Value::as_str))
}

impl ResponseFrame {
    fn ok(id: Option<String>, payload: Value) -> Self {
        Self {
            id,
            protocol_version: PROTOCOL_VERSION,
            ok: true,
            payload: Some(payload),
            error: None,
        }
    }

    fn error(id: Option<String>, code: &'static str, message: impl std::fmt::Display) -> Self {
        Self {
            id,
            protocol_version: PROTOCOL_VERSION,
            ok: false,
            payload: None,
            error: Some(ErrorFrame {
                code,
                message: message.to_string(),
            }),
        }
    }
}

async fn write_response<W>(writer: &mut W, response: ResponseFrame) -> Result<()>
where
    W: AsyncWrite + Unpin,
{
    write_json_line(writer, &response).await
}

async fn write_json_line<W, T>(writer: &mut W, value: &T) -> Result<()>
where
    W: AsyncWrite + Unpin,
    T: Serialize,
{
    let raw = serde_json::to_vec(value)?;
    writer.write_all(&raw).await?;
    writer.write_all(b"\n").await?;
    writer.flush().await?;
    Ok(())
}

fn strip_ansi(text: &str) -> String {
    let no_osc = Regex::new(r"\x1b\].*?(\x07|\x1b\\)")
        .unwrap()
        .replace_all(text, "");
    Regex::new(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-Z\\-_]")
        .unwrap()
        .replace_all(&no_osc, "")
        .to_string()
}

fn now_millis() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
        .min(i64::MAX as u128) as i64
}

pub fn orchestration_socket_path() -> Result<PathBuf> {
    Ok(secret::config_dir()?.join("onibi-orchestration.sock"))
}

pub mod client {
    use super::*;
    use tokio::sync::mpsc;

    pub async fn request(command: &str, payload: Value) -> Result<Value> {
        let token = secret::load_or_create_token()?.token;
        let stream = connect_tcp().await?;
        let (reader, mut writer) = tokio::io::split(stream);
        let mut reader = BufReader::new(reader);
        send_frame(&mut writer, "hello", Some(&token), json!({"token": token})).await?;
        read_response(&mut reader).await?;
        send_frame(&mut writer, command, None, payload).await?;
        read_response(&mut reader).await
    }

    pub async fn event_receiver(payload: Value) -> Result<mpsc::UnboundedReceiver<Value>> {
        let token = secret::load_or_create_token()?.token;
        let stream = connect_tcp().await?;
        let (reader, mut writer) = tokio::io::split(stream);
        let mut reader = BufReader::new(reader);
        send_frame(&mut writer, "hello", Some(&token), json!({"token": token})).await?;
        read_response(&mut reader).await?;
        send_frame(&mut writer, "events.subscribe", None, payload).await?;
        read_response(&mut reader).await?;
        let (tx, rx) = mpsc::unbounded_channel();
        tokio::spawn(async move {
            let mut line = String::new();
            loop {
                line.clear();
                match reader.read_line(&mut line).await {
                    Ok(0) | Err(_) => break,
                    Ok(_) => {
                        if let Ok(value) = serde_json::from_str::<Value>(line.trim_end()) {
                            let _ = tx.send(value);
                        }
                    }
                }
            }
        });
        Ok(rx)
    }

    async fn connect_tcp() -> Result<TcpStream> {
        let mut last_error = None;
        for _ in 0..50 {
            match TcpStream::connect(("127.0.0.1", DEFAULT_ORCHESTRATION_PORT)).await {
                Ok(stream) => return Ok(stream),
                Err(error) => {
                    last_error = Some(error);
                    time::sleep(Duration::from_millis(20)).await;
                }
            }
        }
        Err(anyhow!(
            "connect orchestration TCP: {}",
            last_error
                .map(|error| error.to_string())
                .unwrap_or_else(|| "unavailable".to_string())
        ))
    }

    async fn send_frame<W>(
        writer: &mut W,
        command: &str,
        token: Option<&str>,
        payload: Value,
    ) -> Result<()>
    where
        W: AsyncWrite + Unpin,
    {
        let frame = json!({
            "id": uuid::Uuid::new_v4().to_string(),
            "protocol_version": PROTOCOL_VERSION,
            "command": command,
            "token": token,
            "payload": payload,
        });
        write_json_line(writer, &frame).await
    }

    async fn read_response<R>(reader: &mut R) -> Result<Value>
    where
        R: AsyncBufReadExt + Unpin,
    {
        let mut line = String::new();
        reader.read_line(&mut line).await?;
        let value: Value = serde_json::from_str(line.trim_end()).context("parse response frame")?;
        if value.get("ok").and_then(Value::as_bool).unwrap_or(false) {
            return Ok(value.get("payload").cloned().unwrap_or(Value::Null));
        }
        let message = value
            .get("error")
            .and_then(|error| error.get("message"))
            .and_then(Value::as_str)
            .unwrap_or("orchestration command failed");
        Err(anyhow!(message.to_string()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn strips_ansi_sequences() {
        assert_eq!(strip_ansi("\x1b[31mred\x1b[0m"), "red");
    }

    #[test]
    fn maps_common_keys() {
        assert_eq!(key_to_bytes("Enter").unwrap().as_ref(), b"\r");
        assert_eq!(key_to_bytes("Ctrl+C").unwrap().as_ref(), b"\x03");
    }

    #[test]
    fn pty_spawn_request_defaults_shell_mode_to_auto() {
        let req: PtySpawnRequest = serde_json::from_value(json!({
            "command": "/bin/sh",
            "args": [],
        }))
        .unwrap();

        assert_eq!(req.shell_mode, ShellMode::Auto);
    }

    #[test]
    fn restart_metadata_preserves_shell_mode() {
        let mut req = test_spawn_request("", &[], None, Some("shell"));
        req.shell_mode = ShellMode::NonLogin;

        let restart = restart_metadata_from_request(&req);

        assert_eq!(restart.shell_mode, ShellMode::NonLogin);
    }

    #[test]
    fn classifies_orchestration_command_aliases() {
        assert_eq!(classify_command("pty.spawn"), Some(CommandKind::Spawn));
        assert_eq!(classify_command("agent.start"), Some(CommandKind::Spawn));
        assert_eq!(classify_command("agent.list"), Some(CommandKind::ListLive));
        assert_eq!(classify_command("agent.send"), Some(CommandKind::Write));
        assert_eq!(classify_command("missing.command"), None);
    }

    #[test]
    fn tail_lines_limits_rows() {
        assert_eq!(tail_lines("a\nb\nc", 2), "b\nc");
    }

    #[test]
    fn unwraps_soft_wrapped_recent_lines() {
        assert_eq!(unwrap_recent_lines("abcd\nef\ng", 4), "abcdef\ng");
    }

    #[test]
    fn terminal_screen_tracks_visible_output() {
        let mut screen = TerminalScreen::new(2, 6);
        screen.feed("hello\nworld");
        assert_eq!(screen.visible_text(), "hello\nworld");
        screen.feed("\r\x1b[2Kdone");
        assert_eq!(screen.visible_text(), "hello\ndone");
    }

    #[test]
    fn infers_shell_status_markers() {
        assert_eq!(
            infer_status_from_output(None, "\x1b]133;C;make test\x07"),
            Some(AgentStatus::Working)
        );
        assert_eq!(
            infer_status_from_output(None, "\x1b]133;D;0\x07\x1b]133;A\x07"),
            Some(AgentStatus::Idle)
        );
    }

    #[test]
    fn detects_heuristic_agents_from_launch_commands() {
        let cases = vec![
            ("cursor-agent", vec![], "cursor"),
            ("cline", vec![], "cline"),
            ("gh", vec!["copilot"], "copilot"),
            ("gemini", vec![], "gemini"),
            ("grok-cli", vec![], "grok"),
            ("kimi", vec![], "kimi"),
            ("kiro-agent", vec![], "kiro"),
            ("droid", vec![], "droid"),
            ("amp", vec![], "amp"),
            ("antigravity-agent", vec![], "antigravity"),
            ("kilo-code", vec![], "kilo"),
        ];
        for (command, args, expected) in cases {
            let req = test_spawn_request(command, &args, None, None);
            let metadata = SpawnMetadata::from_payload(&json!({}), &req);
            assert_eq!(
                metadata.agent.as_deref(),
                Some(expected),
                "detect {command:?} {args:?}"
            );
        }
    }

    #[test]
    fn explicit_agent_metadata_wins_over_launch_detection() {
        let req = test_spawn_request("gemini", &[], None, Some("shell"));
        let metadata = SpawnMetadata::from_payload(&json!({}), &req);
        assert_eq!(metadata.agent.as_deref(), Some("shell"));

        let req = test_spawn_request("bash", &[], None, Some("cursor-agent"));
        let metadata = SpawnMetadata::from_payload(&json!({}), &req);
        assert_eq!(metadata.agent.as_deref(), Some("cursor"));
    }

    #[test]
    fn detects_heuristic_agents_from_output_markers() {
        assert_eq!(
            infer_agent_from_output("Welcome to Gemini CLI"),
            Some("gemini")
        );
        assert_eq!(
            infer_agent_from_output("Cursor Agent is ready"),
            Some("cursor")
        );
        assert_eq!(infer_agent_from_output("an example line"), None);
    }

    #[test]
    fn infers_agent_status_only_for_known_agents() {
        assert_eq!(
            infer_status_from_output(Some("gemini"), "Thinking about the patch"),
            Some(AgentStatus::Working)
        );
        assert_eq!(
            infer_status_from_output(Some("gemini"), "Task complete"),
            Some(AgentStatus::Done)
        );
        assert_eq!(
            infer_status_from_output(Some("gemini"), "Waiting for approval"),
            Some(AgentStatus::Blocked)
        );
        assert_eq!(infer_status_from_output(None, "done"), None);
        assert_eq!(infer_status_from_output(Some("shell"), "done"), None);
    }

    #[cfg(unix)]
    #[test]
    fn parses_process_snapshot_lines() {
        assert_eq!(
            parse_process_line("  123   45 /opt/bin/gemini"),
            Some(ProcessInfo {
                pid: 123,
                ppid: 45,
                command: "/opt/bin/gemini".to_string(),
            })
        );
        assert_eq!(parse_process_line("not-a-pid 45 gemini"), None);
    }

    #[test]
    fn infers_agent_from_process_descendants() {
        let processes = vec![
            ProcessInfo {
                pid: 10,
                ppid: 1,
                command: "zsh".to_string(),
            },
            ProcessInfo {
                pid: 20,
                ppid: 10,
                command: "node".to_string(),
            },
            ProcessInfo {
                pid: 30,
                ppid: 20,
                command: "/usr/local/bin/gemini".to_string(),
            },
        ];
        assert_eq!(infer_agent_from_processes(10, &processes), Some("gemini"));
        assert_eq!(infer_agent_from_processes(99, &processes), None);
    }

    #[tokio::test]
    async fn heuristic_status_does_not_override_blocked_or_done() {
        let state = OrchestrationState::with_store("token".to_string(), None);
        state
            .upsert_session(test_session(
                "session-1",
                AgentStatus::Working,
                Some("gemini"),
            ))
            .await;

        state.set_status("session-1", AgentStatus::Blocked).await;
        state
            .set_heuristic_status("session-1", AgentStatus::Working)
            .await;
        assert_eq!(
            state
                .sessions
                .read()
                .await
                .get("session-1")
                .map(|session| session.status),
            Some(AgentStatus::Blocked)
        );

        state.set_status("session-1", AgentStatus::Done).await;
        state
            .set_heuristic_status("session-1", AgentStatus::Working)
            .await;
        assert_eq!(
            state
                .sessions
                .read()
                .await
                .get("session-1")
                .map(|session| session.status),
            Some(AgentStatus::Done)
        );
    }

    #[tokio::test]
    async fn provider_event_correlates_by_agent_and_cwd() {
        let state = OrchestrationState::with_store("token".to_string(), None);
        let mut session = test_session("session-1", AgentStatus::Working, Some("opencode"));
        session.cwd = Some("/repo".to_string());
        state.upsert_session(session).await;

        let updated = state
            .apply_provider_event(ProviderEventUpdate {
                agent: "opencode".to_string(),
                session_id: Some("provider-session-1".to_string()),
                provider_session_id: Some("provider-session-1".to_string()),
                conversation_id: None,
                cwd: Some("/repo".to_string()),
                status: Some(AgentStatus::Done),
                resume: Some(ProviderResumeMetadata {
                    command: "opencode".to_string(),
                    args: vec!["--session".to_string(), "provider-session-1".to_string()],
                    source: Some("test".to_string()),
                }),
            })
            .await
            .unwrap();

        assert_eq!(updated.id, "session-1");
        assert_eq!(updated.status, AgentStatus::Done);
        let provider = updated.provider.unwrap();
        assert_eq!(
            provider.provider_session_id.as_deref(),
            Some("provider-session-1")
        );
        assert_eq!(
            provider.resume.unwrap().args,
            vec!["--session".to_string(), "provider-session-1".to_string()]
        );
    }

    #[test]
    fn provider_resume_metadata_overrides_relaunch_command() {
        let mut session = test_session("session-1", AgentStatus::Done, Some("opencode"));
        session.cwd = Some("/repo".to_string());
        session.restart = Some(SessionRestartMetadata {
            command: "opencode".to_string(),
            args: vec![],
            cwd: Some("/repo".to_string()),
            env: vec![("A".to_string(), "B".to_string())],
            shell_mode: ShellMode::Auto,
            safe_mode: false,
            trust_mode: TrustMode::ApprovalRequired,
            remote: None,
        });
        session.provider = Some(ProviderSessionMetadata {
            agent: "opencode".to_string(),
            provider_session_id: Some("provider-session-1".to_string()),
            conversation_id: None,
            resume: Some(ProviderResumeMetadata {
                command: "opencode".to_string(),
                args: vec!["--session".to_string(), "provider-session-1".to_string()],
                source: Some("test".to_string()),
            }),
            updated_at: 1,
        });

        let restart = provider_restart_metadata(&session).unwrap();
        assert_eq!(restart.command, "opencode");
        assert_eq!(
            restart.args,
            vec!["--session".to_string(), "provider-session-1".to_string()]
        );
        assert_eq!(restart.cwd.as_deref(), Some("/repo"));
        assert_eq!(restart.env, vec![("A".to_string(), "B".to_string())]);
    }

    #[test]
    fn normalizes_session_names() {
        assert_eq!(
            normalize_session_name("  build  "),
            Some("build".to_string())
        );
        assert_eq!(normalize_session_name("  "), None);
    }

    #[test]
    fn persisted_running_sessions_become_stale_on_load() {
        let dir = tempfile::tempdir().unwrap();
        let store = SessionMetadataStore::open(dir.path().join("onibi.db")).unwrap();
        let session = SessionInfo {
            id: "session-1".to_string(),
            pane_id: "session-1".to_string(),
            name: Some("dev".to_string()),
            agent: Some("claude-code".to_string()),
            workspace_id: Some("workspace:/repo".to_string()),
            safe_mode: false,
            trust_mode: TrustMode::ApprovalRequired,
            cwd: Some("/repo".to_string()),
            title: Some("Claude Code".to_string()),
            status: AgentStatus::Working,
            lifecycle: SessionLifecycle::Running,
            rows: 30,
            cols: 100,
            created_at: 1,
            updated_at: 1,
            process_id: None,
            stopped_at: None,
            exit_code: None,
            exit_signal: None,
            restart: Some(SessionRestartMetadata {
                command: "claude".to_string(),
                args: vec![
                    "code".to_string(),
                    "--model".to_string(),
                    "sonnet".to_string(),
                ],
                cwd: Some("/repo".to_string()),
                env: vec![],
                shell_mode: ShellMode::Auto,
                safe_mode: false,
                trust_mode: TrustMode::ApprovalRequired,
                remote: None,
            }),
            provider: None,
            remote: None,
        };
        store.upsert(&session).unwrap();

        let loaded = load_persisted_sessions(&store);
        let stale = loaded.get("session-1").unwrap();
        assert_eq!(stale.lifecycle, SessionLifecycle::Stale);
        assert_eq!(stale.status, AgentStatus::Done);
        assert_eq!(stale.exit_signal.as_deref(), Some("daemon restart"));
        assert_eq!(
            stale.restart.as_ref().unwrap().args,
            vec!["--model".to_string(), "sonnet".to_string()]
        );

        let persisted = store.list().unwrap();
        assert_eq!(persisted[0].lifecycle, SessionLifecycle::Stale);
        assert_eq!(
            persisted[0].restart.as_ref().unwrap().args,
            vec!["--model".to_string(), "sonnet".to_string()]
        );
    }

    fn test_spawn_request(
        command: &str,
        args: &[&str],
        title: Option<&str>,
        agent: Option<&str>,
    ) -> PtySpawnRequest {
        PtySpawnRequest {
            command: command.to_string(),
            args: args.iter().map(|arg| arg.to_string()).collect(),
            cwd: None,
            env: vec![],
            shell_mode: ShellMode::Auto,
            rows: 30,
            cols: 100,
            name: None,
            agent: agent.map(ToOwned::to_owned),
            workspace_id: None,
            safe_mode: false,
            trust_mode: TrustMode::ApprovalRequired,
            title: title.map(ToOwned::to_owned),
            remote: None,
        }
    }

    fn test_session(id: &str, status: AgentStatus, agent: Option<&str>) -> SessionInfo {
        SessionInfo {
            id: id.to_string(),
            pane_id: id.to_string(),
            name: None,
            agent: agent.map(ToOwned::to_owned),
            workspace_id: None,
            safe_mode: false,
            trust_mode: TrustMode::ApprovalRequired,
            cwd: None,
            title: None,
            status,
            lifecycle: SessionLifecycle::Running,
            rows: 30,
            cols: 100,
            created_at: 1,
            updated_at: 1,
            process_id: None,
            stopped_at: None,
            exit_code: None,
            exit_signal: None,
            restart: None,
            provider: None,
            remote: None,
        }
    }
}
