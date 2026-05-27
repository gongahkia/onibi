use crate::{
    protocol::{ApprovalRequestBody, Decision, PROTOCOL_VERSION},
    secret,
    server::{routes, AppState},
};
use anyhow::{bail, Context, Result};
use directories::BaseDirs;
use serde_json::{json, Value};
use std::{
    fs,
    io::{Read, Write},
    net::TcpStream,
    path::{Path, PathBuf},
};

const HOOK_COMMAND: &str = "onibi _hook codex";

pub fn install() -> Result<String> {
    install_at(&hooks_path()?)?;
    Ok("codex adapter installed for Bash tool calls".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&hooks_path()?)?;
    Ok("codex adapter uninstalled".to_string())
}

pub fn installed() -> Result<bool> {
    let path = hooks_path()?;
    if !path.exists() {
        return Ok(false);
    }
    Ok(fs::read_to_string(path)?.contains(HOOK_COMMAND))
}

pub async fn handle_http_hook(state: &AppState, payload: Value) -> Result<Value> {
    let decision = routes::wait_for_approval_decision(state, body_from_payload(payload)?).await?;
    Ok(json!({
        "permissionDecision": decision.decision.as_str(),
        "reason": decision.reason,
    }))
}

pub fn run_stdin_hook(port: u16) -> Result<()> {
    let mut raw = String::new();
    std::io::stdin()
        .read_to_string(&mut raw)
        .context("read Codex hook payload from stdin")?;
    let payload: Value = serde_json::from_str(&raw).context("parse Codex hook payload")?;
    let token = secret::load_or_create_token()?.token;
    let response = post_json(
        port,
        "/v1/approval/request",
        &token,
        &body_from_payload(payload)?,
    )?;
    let decision = response
        .get("decision")
        .and_then(Value::as_str)
        .unwrap_or("deny");
    println!(
        "{}",
        json!({
            "permissionDecision": if decision == "allow" { "allow" } else { "deny" }
        })
    );
    Ok(())
}

fn body_from_payload(payload: Value) -> Result<ApprovalRequestBody> {
    let tool = payload
        .get("tool_name")
        .or_else(|| payload.get("tool"))
        .and_then(Value::as_str)
        .unwrap_or("Bash")
        .to_string();
    if tool != "Bash" {
        bail!("Codex Phase-03 adapter only intercepts Bash tool calls");
    }
    let input = payload
        .get("tool_input")
        .or_else(|| payload.get("input"))
        .cloned()
        .unwrap_or_else(|| json!({}));
    let cwd = payload
        .get("cwd")
        .and_then(Value::as_str)
        .unwrap_or("")
        .to_string();
    Ok(ApprovalRequestBody {
        protocol_version: Some(PROTOCOL_VERSION.to_string()),
        machine_id: None,
        session_id: payload
            .get("session_id")
            .and_then(Value::as_str)
            .map(ToString::to_string),
        agent: "codex".to_string(),
        tool,
        input,
        cwd,
        metadata: Some(json!({
            "source": "codex",
            "limitation": "bash-only",
            "raw": payload,
        })),
    })
}

fn hooks_path() -> Result<PathBuf> {
    if let Ok(path) = std::env::var("ONIBI_CODEX_HOOKS") {
        return Ok(PathBuf::from(path));
    }
    Ok(BaseDirs::new()
        .context("resolve home directory")?
        .home_dir()
        .join(".codex")
        .join("hooks.json"))
}

fn install_at(path: &Path) -> Result<()> {
    let mut config = read_hooks(path)?;
    remove_onibi_hook(&mut config);
    let hooks = config
        .as_object_mut()
        .context("Codex hooks file must be an object")?
        .entry("hooks")
        .or_insert_with(|| json!([]));
    let hooks = hooks
        .as_array_mut()
        .context("Codex hooks field must be an array")?;
    hooks.push(json!({
        "event": "tool",
        "tool": "Bash",
        "command": ["onibi", "_hook", "codex"]
    }));
    write_hooks(path, &config)
}

fn uninstall_at(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    let mut config = read_hooks(path)?;
    remove_onibi_hook(&mut config);
    write_hooks(path, &config)
}

fn read_hooks(path: &Path) -> Result<Value> {
    if !path.exists() {
        return Ok(json!({"hooks": []}));
    }
    let raw = fs::read_to_string(path).with_context(|| format!("read {}", path.display()))?;
    if raw.trim().is_empty() {
        return Ok(json!({"hooks": []}));
    }
    serde_json::from_str(&raw).with_context(|| format!("parse {}", path.display()))
}

fn write_hooks(path: &Path, config: &Value) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(path, format!("{}\n", serde_json::to_string_pretty(config)?))
        .with_context(|| format!("write {}", path.display()))
}

fn remove_onibi_hook(config: &mut Value) {
    if let Some(hooks) = config.get_mut("hooks").and_then(Value::as_array_mut) {
        hooks.retain(|hook| {
            hook.get("command")
                .and_then(Value::as_array)
                .map(|parts| {
                    parts
                        .iter()
                        .filter_map(Value::as_str)
                        .collect::<Vec<_>>()
                        .join(" ")
                })
                .as_deref()
                != Some(HOOK_COMMAND)
        });
    }
}

fn post_json<T: serde::Serialize>(port: u16, path: &str, token: &str, body: &T) -> Result<Value> {
    let body = serde_json::to_string(body)?;
    let mut stream = TcpStream::connect(("127.0.0.1", port))
        .with_context(|| format!("connect Onibi daemon on 127.0.0.1:{port}"))?;
    write!(
        stream,
        "POST {path} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nAuthorization: Bearer {token}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    )
    .context("write approval request")?;
    let mut raw = String::new();
    stream
        .read_to_string(&mut raw)
        .context("read approval response")?;
    let (head, response_body) = raw
        .split_once("\r\n\r\n")
        .context("invalid HTTP response from Onibi daemon")?;
    if !head.starts_with("HTTP/1.1 200") && !head.starts_with("HTTP/1.0 200") {
        bail!("Onibi daemon returned non-200 response: {head}");
    }
    serde_json::from_str(response_body).context("parse approval response")
}

#[allow(dead_code)]
fn _decision_from_str(raw: &str) -> Decision {
    if raw == "allow" {
        Decision::Allow
    } else {
        Decision::Deny
    }
}
