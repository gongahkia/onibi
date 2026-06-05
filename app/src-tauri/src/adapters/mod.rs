pub mod aider;
pub mod claude_code;
pub mod codex;
pub mod copilot;
pub mod cursor;
pub mod gemini;
pub mod goose;
pub mod hermes;
pub mod omp;
pub mod opencode;
pub mod pi;
pub mod qoder;

use crate::{
    orchestration::{AgentStatus, ProviderEventUpdate, ProviderResumeMetadata},
    protocol::{ProviderEventBody, PROTOCOL_VERSION},
    secret,
};
use anyhow::{bail, Context, Result};
use serde::Serialize;
use serde_json::{json, Value};
use std::{
    fs,
    io::{Read, Write},
    net::TcpStream,
    path::{Path, PathBuf},
};

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct IntegrationInfo {
    pub name: &'static str,
    pub support: &'static str,
    pub installed: bool,
    pub installed_version: Option<String>,
    pub bundled_version: Option<&'static str>,
    pub outdated: bool,
    pub install_path: Option<PathBuf>,
    pub message: Option<String>,
}

pub type AdapterInfo = IntegrationInfo;

pub const INTEGRATION_VERSION: &str = "1.0.0";
pub const INTEGRATION_VERSION_HEADER: &str = "X-Onibi-Integration-Version";
pub const INTEGRATION_VERSION_FIELD: &str = "onibiIntegrationVersion";
pub const EVENT_ROUTE_PREFIX: &str = "/v1/adapters";

pub fn list() -> Vec<IntegrationInfo> {
    integrations()
}

pub fn integrations() -> Vec<IntegrationInfo> {
    vec![
        claude_code::info(),
        codex::info(),
        opencode::info(),
        gemini::info(),
        aider::info(),
        cursor::info(),
        goose::info(),
        qoder::info(),
        copilot::info(),
        hermes::info(),
        pi::info(),
        omp::info(),
    ]
}

pub fn status(outdated_only: bool) -> Vec<IntegrationInfo> {
    filter_status(integrations(), outdated_only)
}

fn filter_status(integrations: Vec<IntegrationInfo>, outdated_only: bool) -> Vec<IntegrationInfo> {
    integrations
        .into_iter()
        .filter(|integration| !outdated_only || integration.outdated)
        .collect()
}

pub fn install(name: &str, token: &str) -> Result<String> {
    match name {
        "claude-code" | "claude" => claude_code::install(token),
        "codex" => codex::install(),
        "opencode" => opencode::install(token),
        "gemini" => gemini::install(),
        "aider" => aider::install(),
        "cursor" => cursor::install(),
        "goose" => goose::install(),
        "qoder" | "qoder-cli" => qoder::install(),
        "copilot" | "github-copilot" => copilot::install(),
        "hermes" => hermes::install(),
        "pi" => pi::install(),
        "omp" => omp::install(),
        other => bail!("unknown adapter: {other}"),
    }
}

pub fn uninstall(name: &str) -> Result<String> {
    match name {
        "claude-code" | "claude" => claude_code::uninstall(),
        "codex" => codex::uninstall(),
        "opencode" => opencode::uninstall(),
        "gemini" => gemini::uninstall(),
        "aider" => aider::uninstall(),
        "cursor" => cursor::uninstall(),
        "goose" => goose::uninstall(),
        "qoder" | "qoder-cli" => qoder::uninstall(),
        "copilot" | "github-copilot" => copilot::uninstall(),
        "hermes" => hermes::uninstall(),
        "pi" => pi::uninstall(),
        "omp" => omp::uninstall(),
        other => bail!("unknown adapter: {other}"),
    }
}

#[cfg(test)]
pub fn stub_info(name: &'static str) -> AdapterInfo {
    AdapterInfo {
        name,
        support: "stub",
        installed: false,
        installed_version: None,
        bundled_version: None,
        outdated: false,
        install_path: None,
        message: Some("stub integration; no hook installer is available yet".to_string()),
    }
}

pub fn static_info(
    name: &'static str,
    support: &'static str,
    installed: bool,
    install_path: Option<PathBuf>,
    message: impl Into<Option<String>>,
) -> IntegrationInfo {
    IntegrationInfo {
        name,
        support,
        installed,
        installed_version: installed.then(|| INTEGRATION_VERSION.to_string()),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: false,
        install_path,
        message: message.into(),
    }
}

pub fn pending_info(
    name: &'static str,
    install_path: Option<PathBuf>,
    message: impl Into<String>,
) -> IntegrationInfo {
    IntegrationInfo {
        name,
        support: "pending",
        installed: false,
        installed_version: None,
        bundled_version: None,
        outdated: false,
        install_path,
        message: Some(message.into()),
    }
}

pub fn resume_only_info(name: &'static str, message: impl Into<String>) -> IntegrationInfo {
    IntegrationInfo {
        name,
        support: "resume-only",
        installed: false,
        installed_version: None,
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: false,
        install_path: None,
        message: Some(message.into()),
    }
}

pub fn home_path(env_key: &str, segments: &[&str]) -> Result<PathBuf> {
    if let Ok(path) = std::env::var(env_key) {
        return Ok(PathBuf::from(path));
    }
    let mut path = directories::BaseDirs::new()
        .context("resolve home directory")?
        .home_dir()
        .to_path_buf();
    for segment in segments {
        path.push(segment);
    }
    Ok(path)
}

pub fn read_json(path: &Path, default: Value) -> Result<Value> {
    if !path.exists() {
        return Ok(default);
    }
    let raw = fs::read_to_string(path).with_context(|| format!("read {}", path.display()))?;
    if raw.trim().is_empty() {
        return Ok(default);
    }
    serde_json::from_str(&raw).with_context(|| format!("parse {}", path.display()))
}

pub fn write_json(path: &Path, value: &Value) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(path, format!("{}\n", serde_json::to_string_pretty(value)?))
        .with_context(|| format!("write {}", path.display()))
}

pub fn command_string_hook(name: &str) -> String {
    format!("onibi _hook {name}")
}

pub fn run_stdin_event_hook(name: &str, port: u16) -> Result<()> {
    let mut raw = String::new();
    std::io::stdin()
        .read_to_string(&mut raw)
        .with_context(|| format!("read {name} hook payload from stdin"))?;
    let payload = if raw.trim().is_empty() {
        json!({})
    } else {
        serde_json::from_str(&raw).with_context(|| format!("parse {name} hook payload"))?
    };
    let token = secret::load_or_create_token()?.token;
    let _ = post_json(
        port,
        &format!("{EVENT_ROUTE_PREFIX}/{name}/event"),
        &token,
        &payload,
    )?;
    Ok(())
}

pub fn post_json<T: serde::Serialize>(
    port: u16,
    path: &str,
    token: &str,
    body: &T,
) -> Result<Value> {
    let body = serde_json::to_string(body)?;
    let mut stream = TcpStream::connect(("127.0.0.1", port))
        .with_context(|| format!("connect Onibi daemon on 127.0.0.1:{port}"))?;
    write!(
        stream,
        "POST {path} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nAuthorization: Bearer {token}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    )
    .context("write provider event request")?;
    let mut raw = String::new();
    stream
        .read_to_string(&mut raw)
        .context("read provider event response")?;
    let (head, response_body) = raw
        .split_once("\r\n\r\n")
        .context("invalid HTTP response from Onibi daemon")?;
    if !head.starts_with("HTTP/1.1 200") && !head.starts_with("HTTP/1.0 200") {
        bail!("Onibi daemon returned non-200 response: {head}");
    }
    serde_json::from_str(response_body).context("parse provider event response")
}

#[derive(Debug, Clone)]
pub struct ProviderEventIngest {
    pub body: ProviderEventBody,
    pub update: ProviderEventUpdate,
    pub event_kind: String,
    pub run_session_id: String,
    pub payload: Value,
}

pub fn normalize_provider_event(agent: &str, payload: Value) -> Result<ProviderEventIngest> {
    let agent = normalize_agent_name(agent)
        .ok_or_else(|| anyhow::anyhow!("unsupported provider event agent: {agent}"))?;
    let body = body_from_provider_payload(agent, payload.clone());
    let provider_session_id = body
        .provider_session_id
        .clone()
        .or_else(|| body.session_id.clone());
    let resume_id = body
        .conversation_id
        .as_ref()
        .or(provider_session_id.as_ref())
        .cloned();
    let status = provider_status(body.event.as_deref(), body.status.as_deref());
    let resume = resume_id
        .as_deref()
        .and_then(|id| resume_metadata_for_agent(agent, id));
    let update = ProviderEventUpdate {
        agent: agent.to_string(),
        session_id: body.session_id.clone(),
        provider_session_id: provider_session_id.clone(),
        conversation_id: body.conversation_id.clone(),
        cwd: body.cwd.clone(),
        status,
        resume,
    };
    let run_session_id = body
        .session_id
        .clone()
        .or(provider_session_id)
        .or_else(|| body.conversation_id.clone())
        .unwrap_or_else(|| format!("{agent}:unknown"));
    let event = body.event.as_deref().unwrap_or("event");
    let event_kind = format!("provider.{agent}.{}", normalize_event_token(event));
    let payload = serde_json::to_value(&body)?;
    Ok(ProviderEventIngest {
        body,
        update,
        event_kind,
        run_session_id,
        payload,
    })
}

fn body_from_provider_payload(agent: &str, payload: Value) -> ProviderEventBody {
    let event = string_field(
        &payload,
        &[
            "event",
            "type",
            "event.type",
            "hookEventName",
            "hook_event_name",
        ],
    )
    .unwrap_or_else(|| "event".to_string());
    let session_id = string_field(
        &payload,
        &[
            "sessionId",
            "session_id",
            "sessionID",
            "id",
            "session.id",
            "event.sessionID",
            "event.sessionId",
            "event.session_id",
        ],
    );
    let provider_session_id = string_field(
        &payload,
        &[
            "providerSessionId",
            "provider_session_id",
            "sessionId",
            "session_id",
            "session.id",
            "event.sessionID",
            "event.sessionId",
            "event.session_id",
        ],
    );
    let conversation_id = string_field(
        &payload,
        &[
            "conversationId",
            "conversation_id",
            "conversation.id",
            "transcriptPath",
            "transcript_path",
        ],
    );
    let cwd = string_field(
        &payload,
        &["cwd", "directory", "worktree", "project.root", "event.cwd"],
    );
    let tool = string_field(
        &payload,
        &[
            "toolName",
            "tool_name",
            "tool",
            "tool.name",
            "input.tool",
            "event.tool",
        ],
    );
    let status = string_field(
        &payload,
        &[
            "status",
            "session.status",
            "event.status",
            "event.properties.status",
        ],
    );
    let input = value_field(
        &payload,
        &[
            "toolArgs",
            "tool_args",
            "tool_input",
            "input",
            "args",
            "output.args",
            "event",
        ],
    )
    .unwrap_or_else(|| json!({}));
    ProviderEventBody {
        protocol_version: Some(PROTOCOL_VERSION.to_string()),
        machine_id: None,
        session_id,
        provider_session_id,
        conversation_id,
        event: Some(event),
        status,
        cwd,
        tool,
        input,
        raw: Some(json!({
            "agent": agent,
            "payload": payload,
        })),
    }
}

fn normalize_agent_name(raw: &str) -> Option<&'static str> {
    match raw {
        "claude" | "claude-code" => Some("claude-code"),
        "codex" => Some("codex"),
        "opencode" => Some("opencode"),
        "gemini" => Some("gemini"),
        "aider" => Some("aider"),
        "cursor" | "cursor-agent" => Some("cursor"),
        "goose" => Some("goose"),
        "qoder" | "qoder-cli" => Some("qoder"),
        "copilot" | "github-copilot" => Some("copilot"),
        "hermes" => Some("hermes"),
        "pi" => Some("pi"),
        "omp" => Some("omp"),
        _ => None,
    }
}

fn provider_status(event: Option<&str>, status: Option<&str>) -> Option<AgentStatus> {
    let status = status.map(normalize_event_token);
    match status.as_deref() {
        Some("idle" | "ready" | "waiting") => return Some(AgentStatus::Idle),
        Some("blocked" | "awaitingapproval" | "permissionrequired") => {
            return Some(AgentStatus::Blocked)
        }
        Some("done" | "complete" | "completed" | "error" | "failed") => {
            return Some(AgentStatus::Done)
        }
        Some("working" | "running" | "busy" | "thinking") => return Some(AgentStatus::Working),
        _ => {}
    }

    let event = event.map(normalize_event_token)?;
    if event.contains("permission") || event == "pretooluse" || event == "tool.executebefore" {
        return Some(AgentStatus::Blocked);
    }
    if event.contains("error")
        || event.contains("failure")
        || event.contains("sessionend")
        || event == "stop"
        || event == "sessionidle"
        || event == "session.idle"
    {
        return Some(AgentStatus::Done);
    }
    if event.contains("start")
        || event.contains("prompt")
        || event.contains("posttooluse")
        || event == "tool.executeafter"
        || event == "sessionstatus"
        || event == "session.status"
        || event == "session.created"
    {
        return Some(AgentStatus::Working);
    }
    None
}

fn resume_metadata_for_agent(agent: &str, id: &str) -> Option<ProviderResumeMetadata> {
    let id = id.trim();
    if id.is_empty() {
        return None;
    }
    let (command, args, source) = match agent {
        "claude-code" => ("claude", vec!["--resume", id], "claude-code --resume"),
        "opencode" => ("opencode", vec!["--session", id], "opencode --session"),
        "gemini" => ("gemini", vec!["--resume", id], "gemini --resume"),
        "qoder" => ("qoder", vec!["-r", id], "qoder -r"),
        "hermes" => ("hermes", vec!["--resume", id], "hermes --resume"),
        "goose" => (
            "goose",
            vec!["session", "resume", id],
            "goose session resume",
        ),
        _ => return None,
    };
    Some(ProviderResumeMetadata {
        command: command.to_string(),
        args: args.into_iter().map(ToString::to_string).collect(),
        source: Some(source.to_string()),
    })
}

fn normalize_event_token(raw: &str) -> String {
    raw.chars()
        .filter(|ch| ch.is_ascii_alphanumeric() || *ch == '.')
        .flat_map(char::to_lowercase)
        .collect()
}

fn string_field(value: &Value, paths: &[&str]) -> Option<String> {
    paths
        .iter()
        .filter_map(|path| value_at(value, path))
        .find_map(|value| match value {
            Value::String(raw) if !raw.trim().is_empty() => Some(raw.trim().to_string()),
            Value::Number(number) => Some(number.to_string()),
            _ => None,
        })
}

fn value_field(value: &Value, paths: &[&str]) -> Option<Value> {
    paths
        .iter()
        .filter_map(|path| value_at(value, path))
        .find(|value| !value.is_null())
        .cloned()
}

fn value_at<'a>(value: &'a Value, path: &str) -> Option<&'a Value> {
    let mut current = value;
    for segment in path.split('.') {
        current = current.get(segment)?;
    }
    Some(current)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn outdated_only_filters_current_and_missing_integrations() {
        let integrations = vec![
            IntegrationInfo {
                name: "current",
                support: "full",
                installed: true,
                installed_version: Some(INTEGRATION_VERSION.to_string()),
                bundled_version: Some(INTEGRATION_VERSION),
                outdated: false,
                install_path: None,
                message: None,
            },
            IntegrationInfo {
                name: "old",
                support: "full",
                installed: true,
                installed_version: Some("0.9.0".to_string()),
                bundled_version: Some(INTEGRATION_VERSION),
                outdated: true,
                install_path: None,
                message: None,
            },
            stub_info("stub"),
        ];

        let filtered = filter_status(integrations, true);
        assert_eq!(filtered.len(), 1);
        assert_eq!(filtered[0].name, "old");
    }

    #[test]
    fn normalizes_copilot_pre_tool_event() {
        let ingest = normalize_provider_event(
            "copilot",
            json!({
                "hook_event_name": "PreToolUse",
                "session_id": "copilot-session-1",
                "cwd": "/repo",
                "tool_name": "bash",
                "tool_input": { "command": "make test" }
            }),
        )
        .unwrap();

        assert_eq!(ingest.body.session_id.as_deref(), Some("copilot-session-1"));
        assert_eq!(
            ingest.update.provider_session_id.as_deref(),
            Some("copilot-session-1")
        );
        assert_eq!(ingest.update.status, Some(AgentStatus::Blocked));
        assert_eq!(ingest.event_kind, "provider.copilot.pretooluse");
    }

    #[test]
    fn normalizes_opencode_idle_event_with_resume_metadata() {
        let ingest = normalize_provider_event(
            "opencode",
            json!({
                "event": {
                    "type": "session.idle",
                    "sessionID": "ses_123",
                    "cwd": "/repo"
                }
            }),
        )
        .unwrap();

        assert_eq!(
            ingest.update.provider_session_id.as_deref(),
            Some("ses_123")
        );
        assert_eq!(ingest.update.status, Some(AgentStatus::Done));
        let resume = ingest.update.resume.unwrap();
        assert_eq!(resume.command, "opencode");
        assert_eq!(
            resume.args,
            vec!["--session".to_string(), "ses_123".to_string()]
        );
    }
}
