use crate::{
    protocol::{ApprovalRequestBody, Decision},
    server::{routes, AppState},
};
use anyhow::{bail, Context, Result};
use directories::BaseDirs;
use serde_json::{json, Value};
use std::{
    fs,
    path::{Path, PathBuf},
    process::Command,
};

const HOOK_PATH: &str = "/v1/adapters/claude-code/hook";
const MIN_VERSION: (u64, u64, u64) = (2, 0, 10);

pub fn install(token: &str) -> Result<String> {
    let version = Command::new("claude")
        .arg("--version")
        .output()
        .context("run claude --version; install Claude Code v2.0.10+")?;
    let version_text = String::from_utf8_lossy(&version.stdout);
    ensure_supported_version(&version_text)?;
    install_at(&settings_path()?, token)?;
    Ok("claude-code adapter installed".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&settings_path()?)?;
    Ok("claude-code adapter uninstalled".to_string())
}

pub fn installed() -> Result<bool> {
    let path = settings_path()?;
    if !path.exists() {
        return Ok(false);
    }
    let raw = fs::read_to_string(path)?;
    Ok(raw.contains(HOOK_PATH))
}

pub async fn handle_http_hook(state: &AppState, payload: Value) -> Result<Value> {
    let tool = payload
        .get("tool_name")
        .or_else(|| payload.get("tool"))
        .and_then(Value::as_str)
        .unwrap_or("unknown")
        .to_string();
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
    let session_id = payload
        .get("session_id")
        .and_then(Value::as_str)
        .map(ToString::to_string);

    let decision = routes::wait_for_approval_decision(
        state,
        ApprovalRequestBody {
            protocol_version: Some(crate::protocol::PROTOCOL_VERSION.to_string()),
            machine_id: Some(state.machine_id.clone()),
            session_id,
            agent: "claude-code".to_string(),
            tool,
            input,
            cwd,
            metadata: Some(json!({
                "hook": "PreToolUse",
                "source": "claude-code",
                "raw": payload,
            })),
        },
    )
    .await?;

    let permission_decision = decision.decision.as_str();
    let mut hook_specific = json!({
        "hookEventName": "PreToolUse",
        "permissionDecision": permission_decision,
    });
    if let Some(reason) = decision.reason {
        hook_specific["permissionDecisionReason"] = Value::String(reason);
    }
    if decision.decision == Decision::Allow {
        if let Some(updated_input) = decision.updated_input {
            hook_specific["updatedInput"] = updated_input;
        }
    }

    Ok(json!({
        "permissionDecision": permission_decision,
        "hookSpecificOutput": hook_specific,
    }))
}

pub fn ensure_supported_version(raw: &str) -> Result<()> {
    let version =
        parse_version(raw).with_context(|| format!("parse Claude Code version: {raw}"))?;
    if version < MIN_VERSION {
        bail!(
            "Claude Code v{}.{}.{} is unsupported; Onibi requires v2.0.10+",
            version.0,
            version.1,
            version.2
        );
    }
    Ok(())
}

pub fn parse_version(raw: &str) -> Option<(u64, u64, u64)> {
    let parts: Vec<u64> = raw
        .split(|ch: char| !ch.is_ascii_digit())
        .filter(|part| !part.is_empty())
        .filter_map(|part| part.parse::<u64>().ok())
        .collect();
    (parts.len() >= 3).then_some((parts[0], parts[1], parts[2]))
}

fn settings_path() -> Result<PathBuf> {
    if let Ok(path) = std::env::var("ONIBI_CLAUDE_SETTINGS") {
        return Ok(PathBuf::from(path));
    }
    Ok(BaseDirs::new()
        .context("resolve home directory")?
        .home_dir()
        .join(".claude")
        .join("settings.json"))
}

fn install_at(path: &Path, token: &str) -> Result<()> {
    let mut settings = read_settings(path)?;
    remove_onibi_hook(&mut settings);
    let hook = json!({
        "matcher": "*",
        "hooks": [{
            "type": "http",
            "url": hook_url(),
            "timeout": 600000,
            "headers": {
                "Authorization": format!("Bearer {token}")
            }
        }]
    });
    let root = settings
        .as_object_mut()
        .context("Claude settings must be an object")?;
    let hooks = root.entry("hooks").or_insert_with(|| json!({}));
    let hooks = hooks
        .as_object_mut()
        .context("Claude settings hooks field must be an object")?;
    let pre_tool = hooks.entry("PreToolUse").or_insert_with(|| json!([]));
    let pre_tool = pre_tool
        .as_array_mut()
        .context("Claude PreToolUse hooks field must be an array")?;
    pre_tool.push(hook);
    write_settings(path, &settings)
}

fn uninstall_at(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    let mut settings = read_settings(path)?;
    remove_onibi_hook(&mut settings);
    write_settings(path, &settings)
}

fn read_settings(path: &Path) -> Result<Value> {
    if !path.exists() {
        return Ok(json!({}));
    }
    let raw = fs::read_to_string(path).with_context(|| format!("read {}", path.display()))?;
    if raw.trim().is_empty() {
        return Ok(json!({}));
    }
    serde_json::from_str(&raw).with_context(|| format!("parse {}", path.display()))
}

fn write_settings(path: &Path, settings: &Value) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(
        path,
        format!("{}\n", serde_json::to_string_pretty(settings)?),
    )
    .with_context(|| format!("write {}", path.display()))
}

fn remove_onibi_hook(settings: &mut Value) {
    let Some(pre_tool) = settings
        .get_mut("hooks")
        .and_then(|hooks| hooks.get_mut("PreToolUse"))
        .and_then(Value::as_array_mut)
    else {
        return;
    };

    for group in pre_tool.iter_mut() {
        if let Some(handlers) = group.get_mut("hooks").and_then(Value::as_array_mut) {
            handlers.retain(|handler| !is_onibi_handler(handler));
        }
    }
    pre_tool.retain(|group| {
        group
            .get("hooks")
            .and_then(Value::as_array)
            .is_none_or(|handlers| !handlers.is_empty())
    });
}

fn hook_url() -> String {
    let port = std::env::var("ONIBI_PORT").unwrap_or_else(|_| "17893".to_string());
    format!("http://127.0.0.1:{port}{HOOK_PATH}")
}

fn is_onibi_handler(handler: &Value) -> bool {
    handler
        .get("url")
        .and_then(Value::as_str)
        .is_some_and(|url| url.starts_with("http://127.0.0.1:") && url.ends_with(HOOK_PATH))
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn minimum_version_rejects_2_0_9() {
        assert!(ensure_supported_version("Claude Code 2.0.9").is_err());
        assert!(ensure_supported_version("Claude Code 2.0.10").is_ok());
    }

    #[test]
    fn install_uninstall_preserves_existing_hooks() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("settings.json");
        fs::write(
            &path,
            serde_json::to_string_pretty(&json!({
                "hooks": {
                    "PreToolUse": [{
                        "matcher": "Bash",
                        "hooks": [{
                            "type": "command",
                            "command": "/tmp/existing.sh"
                        }]
                    }],
                    "Stop": [{
                        "hooks": [{
                            "type": "command",
                            "command": "/tmp/stop.sh"
                        }]
                    }]
                }
            }))
            .unwrap(),
        )
        .unwrap();

        install_at(&path, "token-1").unwrap();
        install_at(&path, "token-1").unwrap();
        let installed = fs::read_to_string(&path).unwrap();
        assert_eq!(installed.matches(HOOK_PATH).count(), 1);
        assert!(installed.contains("/tmp/existing.sh"));
        assert!(installed.contains("/tmp/stop.sh"));

        uninstall_at(&path).unwrap();
        let uninstalled = fs::read_to_string(&path).unwrap();
        assert!(!uninstalled.contains(HOOK_PATH));
        assert!(uninstalled.contains("/tmp/existing.sh"));
        assert!(uninstalled.contains("/tmp/stop.sh"));
    }
}
