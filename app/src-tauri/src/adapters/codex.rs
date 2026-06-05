use crate::{
    adapters::{IntegrationInfo, INTEGRATION_VERSION, INTEGRATION_VERSION_FIELD},
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

pub fn info() -> IntegrationInfo {
    match hooks_path() {
        Ok(path) => status_at(&path).unwrap_or_else(|error| IntegrationInfo {
            name: "codex",
            support: "bash-only",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => IntegrationInfo {
            name: "codex",
            support: "bash-only",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: None,
            message: Some(error.to_string()),
        },
    }
}

pub fn install() -> Result<String> {
    install_at(&hooks_path()?)?;
    Ok("codex adapter installed for Bash tool calls".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&hooks_path()?)?;
    Ok("codex adapter uninstalled".to_string())
}

pub async fn handle_http_hook(state: &AppState, payload: Value) -> Result<Value> {
    if let Ok(ingest) = super::normalize_provider_event("codex", payload.clone()) {
        state
            .orchestration
            .apply_provider_event(ingest.update)
            .await;
    }
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
        "command": ["onibi", "_hook", "codex"],
        "onibiIntegrationVersion": INTEGRATION_VERSION
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

fn status_at(path: &Path) -> Result<IntegrationInfo> {
    if !path.exists() {
        return Ok(IntegrationInfo {
            name: "codex",
            support: "bash-only",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path.to_path_buf()),
            message: Some("not installed".to_string()),
        });
    }

    let hooks = read_hooks(path)?;
    let installed_version = onibi_hook_version(&hooks);
    let installed = installed_version.is_some() || contains_legacy_onibi_hook(&hooks);
    let outdated = installed && installed_version.as_deref() != Some(INTEGRATION_VERSION);
    Ok(IntegrationInfo {
        name: "codex",
        support: "bash-only",
        installed,
        installed_version,
        bundled_version: Some(INTEGRATION_VERSION),
        outdated,
        install_path: Some(path.to_path_buf()),
        message: installed.then_some("Codex Bash hook installed".to_string()),
    })
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
        hooks.retain(|hook| !is_onibi_hook(hook));
    }
}

fn onibi_hook_version(config: &Value) -> Option<String> {
    onibi_hooks(config).find_map(|hook| {
        hook.get(INTEGRATION_VERSION_FIELD)
            .and_then(Value::as_str)
            .map(ToString::to_string)
    })
}

fn contains_legacy_onibi_hook(config: &Value) -> bool {
    onibi_hooks(config).next().is_some()
}

fn onibi_hooks(config: &Value) -> impl Iterator<Item = &Value> {
    config
        .get("hooks")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter(|hook| is_onibi_hook(hook))
}

fn is_onibi_hook(hook: &Value) -> bool {
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
        == Some(HOOK_COMMAND)
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

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn install_uninstall_preserves_existing_hooks() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("hooks.json");
        fs::write(
            &path,
            serde_json::to_string_pretty(&json!({
                "hooks": [{
                    "event": "tool",
                    "tool": "Shell",
                    "command": ["/tmp/existing-hook"]
                }]
            }))
            .unwrap(),
        )
        .unwrap();

        install_at(&path).unwrap();
        install_at(&path).unwrap();
        let installed = read_hooks(&path).unwrap();
        assert_eq!(onibi_hooks(&installed).count(), 1);
        let status = status_at(&path).unwrap();
        assert!(status.installed);
        assert_eq!(
            status.installed_version.as_deref(),
            Some(INTEGRATION_VERSION)
        );
        assert!(!status.outdated);

        uninstall_at(&path).unwrap();
        let uninstalled = read_hooks(&path).unwrap();
        assert_eq!(onibi_hooks(&uninstalled).count(), 0);
        assert!(uninstalled
            .get("hooks")
            .and_then(Value::as_array)
            .is_some_and(|hooks| hooks.iter().any(|hook| {
                hook.get("command")
                    .and_then(Value::as_array)
                    .is_some_and(|command| {
                        command.first().and_then(Value::as_str) == Some("/tmp/existing-hook")
                    })
            })));
    }

    #[test]
    fn status_marks_legacy_hook_without_marker_as_outdated() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("hooks.json");
        fs::write(
            &path,
            serde_json::to_string_pretty(&json!({
                "hooks": [{
                    "event": "tool",
                    "tool": "Bash",
                    "command": ["onibi", "_hook", "codex"]
                }]
            }))
            .unwrap(),
        )
        .unwrap();

        let status = status_at(&path).unwrap();
        assert!(status.installed);
        assert_eq!(status.installed_version, None);
        assert!(status.outdated);
    }

    #[test]
    fn status_marks_old_hook_marker_as_outdated() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("hooks.json");
        fs::write(
            &path,
            serde_json::to_string_pretty(&json!({
                "hooks": [{
                    "event": "tool",
                    "tool": "Bash",
                    "command": ["onibi", "_hook", "codex"],
                    "onibiIntegrationVersion": "0.9.0"
                }]
            }))
            .unwrap(),
        )
        .unwrap();

        let status = status_at(&path).unwrap();
        assert!(status.installed);
        assert_eq!(status.installed_version.as_deref(), Some("0.9.0"));
        assert!(status.outdated);
    }

    #[test]
    fn status_reports_missing_hook_file_as_not_installed() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("hooks.json");
        let status = status_at(&path).unwrap();
        assert!(!status.installed);
        assert!(!status.outdated);
        assert_eq!(status.installed_version, None);
    }
}
