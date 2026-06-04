// The GUI library uses the client helpers while the daemon binary uses the
// server/runtime side, so compiling this module into either target leaves one
// half intentionally unused.
#![allow(dead_code)]

use crate::{
    pty::{PtyEvent, PtyId, PtyManager, PtyOutputSnapshot, PtySpawnRequest},
    secret,
};
use anyhow::{anyhow, Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use bytes::Bytes;
use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    collections::HashMap,
    path::PathBuf,
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

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionInfo {
    pub id: String,
    pub pane_id: String,
    pub agent: Option<String>,
    pub workspace_id: Option<String>,
    pub cwd: Option<String>,
    pub title: Option<String>,
    pub status: AgentStatus,
    pub rows: u16,
    pub cols: u16,
    pub created_at: i64,
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
    screens: Arc<RwLock<HashMap<String, TerminalScreen>>>,
    events: broadcast::Sender<OrchestrationEvent>,
}

impl OrchestrationState {
    pub fn new(token: String) -> Arc<Self> {
        let (events, _) = broadcast::channel(1024);
        Arc::new(Self {
            manager: PtyManager::new(),
            token,
            sessions: Arc::new(RwLock::new(HashMap::new())),
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
                let _ = self.events.send(OrchestrationEvent::SessionStatus {
                    session_id: session_id.to_string(),
                    status,
                    session: Some(session.clone()),
                });
            }
        }
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
            "pty.spawn" => self.spawn(payload).await,
            "pty.write" => self.write(payload).await,
            "pty.resize" => self.resize(payload).await,
            "pty.kill" => self.kill(payload).await,
            "pty.list" | "agent.list" => self.list().await,
            "pty.replay" => self.replay(payload).await,
            "pane.read" | "agent.read" => self.read(payload).await,
            "pane.send_keys" => self.send_keys(payload).await,
            "wait.output" => self.wait_output(payload).await,
            "wait.agent_status" => self.wait_agent_status(payload).await,
            "agent.send" => self.write(payload).await,
            "agent.start" => self.spawn(payload).await,
            "agent.focus" => self.focus(payload).await,
            other => Err(anyhow!("unknown orchestration command: {other}")),
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
        let agent = payload
            .get("agent")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned);
        let workspace_id = payload
            .get("workspaceId")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned);
        let title = payload
            .get("title")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned);
        let cwd = req.cwd.as_ref().map(|path| path.display().to_string());
        let id = self.manager.spawn(req.clone()).await?;
        let id_string = id.to_string();
        let session = SessionInfo {
            id: id_string.clone(),
            pane_id: id_string.clone(),
            agent,
            workspace_id,
            cwd,
            title,
            status: AgentStatus::Working,
            rows: req.rows,
            cols: req.cols,
            created_at: now_millis(),
        };
        self.sessions
            .write()
            .await
            .insert(id_string.clone(), session.clone());
        self.screens.write().await.insert(
            id_string.clone(),
            TerminalScreen::new(req.rows as usize, req.cols as usize),
        );
        let _ = self.events.send(OrchestrationEvent::SessionStarted {
            session: session.clone(),
        });
        self.spawn_event_relay(id).await?;
        Ok(json!({
            "id": id_string,
            "sessionId": session.id,
            "paneId": session.pane_id,
            "session": session,
        }))
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
                        if let Some(status) = infer_status_from_output(&text) {
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
                        state.set_status(&id_string, AgentStatus::Done).await;
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
        }
        if let Some(screen) = self.screens.write().await.get_mut(&id.to_string()) {
            screen.resize(rows as usize, cols as usize);
        }
        Ok(json!({"ok": true}))
    }

    async fn kill(&self, payload: Value) -> Result<Value> {
        let id = self.resolve_target_id(&payload).await?;
        self.manager.kill(id).await?;
        Ok(json!({"ok": true}))
    }

    async fn list(&self) -> Result<Value> {
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
                session
                    .agent
                    .as_deref()
                    .is_some_and(|agent| agent.eq_ignore_ascii_case(raw))
                    || session
                        .title
                        .as_deref()
                        .is_some_and(|title| title.to_lowercase() == needle)
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

#[derive(Debug, Clone)]
struct TerminalScreen {
    rows: usize,
    cols: usize,
    cursor_row: usize,
    cursor_col: usize,
    cells: Vec<Vec<char>>,
    parser: ScreenParser,
}

#[derive(Debug, Clone)]
enum ScreenParser {
    Normal,
    Esc,
    Csi(String),
    Osc,
    OscEsc,
}

impl TerminalScreen {
    fn new(rows: usize, cols: usize) -> Self {
        let rows = rows.max(1);
        let cols = cols.max(1);
        Self {
            rows,
            cols,
            cursor_row: 0,
            cursor_col: 0,
            cells: vec![vec![' '; cols]; rows],
            parser: ScreenParser::Normal,
        }
    }

    fn resize(&mut self, rows: usize, cols: usize) {
        let rows = rows.max(1);
        let cols = cols.max(1);
        self.cells.resize_with(rows, || vec![' '; cols]);
        for row in &mut self.cells {
            row.resize(cols, ' ');
        }
        self.rows = rows;
        self.cols = cols;
        self.cursor_row = self.cursor_row.min(rows - 1);
        self.cursor_col = self.cursor_col.min(cols - 1);
    }

    fn feed(&mut self, text: &str) {
        for ch in text.chars() {
            match std::mem::replace(&mut self.parser, ScreenParser::Normal) {
                ScreenParser::Normal => self.feed_normal(ch),
                ScreenParser::Esc => match ch {
                    '[' => self.parser = ScreenParser::Csi(String::new()),
                    ']' => self.parser = ScreenParser::Osc,
                    _ => self.parser = ScreenParser::Normal,
                },
                ScreenParser::Csi(mut raw) => {
                    if ch.is_ascii_alphabetic() || matches!(ch, '@' | '`' | '~') {
                        self.apply_csi(&raw, ch);
                        self.parser = ScreenParser::Normal;
                    } else {
                        raw.push(ch);
                        self.parser = ScreenParser::Csi(raw);
                    }
                }
                ScreenParser::Osc => {
                    self.parser = match ch {
                        '\u{7}' => ScreenParser::Normal,
                        '\u{1b}' => ScreenParser::OscEsc,
                        _ => ScreenParser::Osc,
                    };
                }
                ScreenParser::OscEsc => {
                    self.parser = if ch == '\\' {
                        ScreenParser::Normal
                    } else {
                        ScreenParser::Osc
                    };
                }
            }
        }
    }

    fn feed_normal(&mut self, ch: char) {
        match ch {
            '\u{1b}' => self.parser = ScreenParser::Esc,
            '\r' => self.cursor_col = 0,
            '\n' => {
                self.linefeed();
                self.cursor_col = 0;
            }
            '\u{8}' => self.cursor_col = self.cursor_col.saturating_sub(1),
            '\t' => {
                let next = ((self.cursor_col / 8) + 1) * 8;
                self.cursor_col = next.min(self.cols - 1);
            }
            ch if ch.is_control() => {}
            ch => self.put_char(ch),
        }
    }

    fn apply_csi(&mut self, raw: &str, command: char) {
        let params = csi_params(raw);
        match command {
            'A' => self.cursor_row = self.cursor_row.saturating_sub(param_or(&params, 0, 1)),
            'B' => self.cursor_row = (self.cursor_row + param_or(&params, 0, 1)).min(self.rows - 1),
            'C' => self.cursor_col = (self.cursor_col + param_or(&params, 0, 1)).min(self.cols - 1),
            'D' => self.cursor_col = self.cursor_col.saturating_sub(param_or(&params, 0, 1)),
            'G' | '`' => {
                self.cursor_col = param_or(&params, 0, 1).saturating_sub(1).min(self.cols - 1)
            }
            'H' | 'f' => {
                self.cursor_row = param_or(&params, 0, 1).saturating_sub(1).min(self.rows - 1);
                self.cursor_col = param_or(&params, 1, 1).saturating_sub(1).min(self.cols - 1);
            }
            'J' => {
                if param_or(&params, 0, 0) == 2 {
                    self.clear_all();
                }
            }
            'K' => self.clear_line(param_or(&params, 0, 0)),
            'm' | 'h' | 'l' | '?' => {}
            _ => {}
        }
    }

    fn put_char(&mut self, ch: char) {
        if self.cursor_row >= self.rows {
            self.cursor_row = self.rows - 1;
        }
        if self.cursor_col >= self.cols {
            self.linefeed();
            self.cursor_col = 0;
        }
        self.cells[self.cursor_row][self.cursor_col] = ch;
        self.cursor_col += 1;
        if self.cursor_col >= self.cols {
            self.linefeed();
            self.cursor_col = 0;
        }
    }

    fn linefeed(&mut self) {
        if self.cursor_row + 1 >= self.rows {
            self.cells.remove(0);
            self.cells.push(vec![' '; self.cols]);
        } else {
            self.cursor_row += 1;
        }
    }

    fn clear_all(&mut self) {
        for row in &mut self.cells {
            row.fill(' ');
        }
        self.cursor_row = 0;
        self.cursor_col = 0;
    }

    fn clear_line(&mut self, mode: usize) {
        let row = &mut self.cells[self.cursor_row];
        match mode {
            1 => {
                for cell in row.iter_mut().take(self.cursor_col + 1) {
                    *cell = ' ';
                }
            }
            2 => row.fill(' '),
            _ => {
                for cell in row.iter_mut().skip(self.cursor_col) {
                    *cell = ' ';
                }
            }
        }
    }

    fn visible_text(&self) -> String {
        let mut lines = self
            .cells
            .iter()
            .map(|row| row.iter().collect::<String>().trim_end().to_string())
            .collect::<Vec<_>>();
        while lines.last().is_some_and(|line| line.is_empty()) {
            lines.pop();
        }
        lines.join("\n")
    }
}

fn csi_params(raw: &str) -> Vec<usize> {
    raw.trim_start_matches('?')
        .split(';')
        .map(|part| part.parse::<usize>().unwrap_or(0))
        .collect()
}

fn param_or(params: &[usize], index: usize, default: usize) -> usize {
    params
        .get(index)
        .copied()
        .filter(|value| *value > 0)
        .unwrap_or(default)
}

fn infer_status_from_output(text: &str) -> Option<AgentStatus> {
    let command_start = text.rfind("\u{1b}]133;C");
    let command_done = text
        .rfind("\u{1b}]133;D")
        .into_iter()
        .chain(text.rfind("\u{1b}]133;A"))
        .max();
    match (command_start, command_done) {
        (Some(start), Some(done)) if done > start => Some(AgentStatus::Idle),
        (Some(_), _) => Some(AgentStatus::Working),
        (_, Some(_)) => Some(AgentStatus::Idle),
        _ => None,
    }
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

fn target_id(payload: &Value) -> Result<PtyId> {
    let raw = payload
        .get("id")
        .or_else(|| payload.get("paneId"))
        .or_else(|| payload.get("sessionId"))
        .or_else(|| payload.get("agentId"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("missing session or pane id"))?;
    raw.parse().with_context(|| format!("parse pty id {raw}"))
}

fn snapshot_json(snapshot: PtyOutputSnapshot) -> Value {
    json!({
        "data": STANDARD.encode(snapshot.data.as_ref()),
        "startOffset": snapshot.start_offset,
        "endOffset": snapshot.end_offset,
    })
}

fn key_to_bytes(key: &str) -> Result<Bytes> {
    let bytes = match key {
        "Enter" | "Return" => b"\r".to_vec(),
        "Tab" => b"\t".to_vec(),
        "Escape" | "Esc" => b"\x1b".to_vec(),
        "Backspace" => b"\x7f".to_vec(),
        "Ctrl+C" | "C-c" => b"\x03".to_vec(),
        "Ctrl+D" | "C-d" => b"\x04".to_vec(),
        "Ctrl+Z" | "C-z" => b"\x1a".to_vec(),
        other if other.starts_with("Text:") => other["Text:".len()..].as_bytes().to_vec(),
        other if other.len() == 1 => other.as_bytes().to_vec(),
        other => return Err(anyhow!("unsupported key: {other}")),
    };
    Ok(Bytes::from(bytes))
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

fn unwrap_recent_lines(text: &str, cols: usize) -> String {
    let cols = cols.max(1);
    let mut out = String::new();
    let mut previous_soft_wrapped = false;
    for line in text.lines() {
        if previous_soft_wrapped {
            out.push_str(line);
        } else {
            if !out.is_empty() {
                out.push('\n');
            }
            out.push_str(line);
        }
        previous_soft_wrapped = line.chars().count() >= cols && !line.trim().is_empty();
    }
    out
}

fn tail_lines(text: &str, rows: usize) -> String {
    if rows == 0 {
        return String::new();
    }
    let mut lines = text.lines().rev().take(rows).collect::<Vec<_>>();
    lines.reverse();
    lines.join("\n")
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
            infer_status_from_output("\x1b]133;C;make test\x07"),
            Some(AgentStatus::Working)
        );
        assert_eq!(
            infer_status_from_output("\x1b]133;D;0\x07\x1b]133;A\x07"),
            Some(AgentStatus::Idle)
        );
    }
}
