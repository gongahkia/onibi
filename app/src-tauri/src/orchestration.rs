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
    events: broadcast::Sender<OrchestrationEvent>,
}

impl OrchestrationState {
    pub fn new(token: String) -> Arc<Self> {
        let (events, _) = broadcast::channel(1024);
        Arc::new(Self {
            manager: PtyManager::new(),
            token,
            sessions: Arc::new(RwLock::new(HashMap::new())),
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
                });
            }
        }
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
            "agent.focus" => Ok(json!({"ok": true})),
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
        let id = target_id(&payload)?;
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
        let id = target_id(&payload)?;
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
        let id = target_id(&payload)?;
        let rows = payload.get("rows").and_then(Value::as_u64).unwrap_or(30) as u16;
        let cols = payload.get("cols").and_then(Value::as_u64).unwrap_or(100) as u16;
        self.manager.resize(id, rows, cols).await?;
        if let Some(session) = self.sessions.write().await.get_mut(&id.to_string()) {
            session.rows = rows;
            session.cols = cols;
        }
        Ok(json!({"ok": true}))
    }

    async fn kill(&self, payload: Value) -> Result<Value> {
        let id = target_id(&payload)?;
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
        let id = target_id(&payload)?;
        Ok(snapshot_json(self.manager.output_snapshot(id)?))
    }

    async fn read(&self, payload: Value) -> Result<Value> {
        let id = target_id(&payload)?;
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
        let mut text = if format == "ansi" {
            raw_text
        } else {
            strip_ansi(&raw_text)
        };
        if source == "visible" {
            let rows = session.map(|item| item.rows).unwrap_or(30) as usize;
            text = tail_lines(&text, rows);
        }
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
        let id = target_id(&payload)?;
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
        let id = target_id(&payload)?;
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
                if let OrchestrationEvent::SessionStatus { session_id, status } = rx.recv().await? {
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
}
