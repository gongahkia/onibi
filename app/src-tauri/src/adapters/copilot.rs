use super::{home_path, read_json, write_json, AdapterInfo, INTEGRATION_VERSION};
use anyhow::Result;
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

pub fn info() -> AdapterInfo {
    match hook_path() {
        Ok(path) => status_at(&path).unwrap_or_else(|error| AdapterInfo {
            name: "copilot",
            support: "event-bridge",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => AdapterInfo {
            name: "copilot",
            support: "event-bridge",
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
    write_json(&hook_path()?, &hook_config())?;
    Ok("GitHub Copilot provider-event hook installed".to_string())
}

pub fn uninstall() -> Result<String> {
    let path = hook_path()?;
    if path.exists() {
        std::fs::remove_file(&path)?;
    }
    Ok("GitHub Copilot provider-event hook uninstalled".to_string())
}

fn hook_path() -> Result<PathBuf> {
    home_path(
        "ONIBI_COPILOT_HOOK",
        &[".copilot", "hooks", "onibi-provider-events.json"],
    )
}

fn hook_config() -> Value {
    let command = "onibi _hook copilot";
    json!({
        "version": 1,
        "onibiIntegrationVersion": INTEGRATION_VERSION,
        "hooks": {
            "sessionStart": [hook(command)],
            "sessionEnd": [hook(command)],
            "userPromptSubmitted": [hook(command)],
            "preToolUse": [hook(command)],
            "postToolUse": [hook(command)],
            "postToolUseFailure": [hook(command)],
            "agentStop": [hook(command)],
            "errorOccurred": [hook(command)]
        }
    })
}

fn hook(command: &str) -> Value {
    json!({
        "type": "command",
        "bash": command,
        "timeoutSec": 30
    })
}

fn status_at(path: &Path) -> Result<AdapterInfo> {
    if !path.exists() {
        return Ok(AdapterInfo {
            name: "copilot",
            support: "event-bridge",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path.to_path_buf()),
            message: Some("not installed".to_string()),
        });
    }
    let config = read_json(path, json!({}))?;
    let installed_version = config
        .get("onibiIntegrationVersion")
        .and_then(Value::as_str)
        .map(ToString::to_string);
    let installed = installed_version.is_some();
    Ok(AdapterInfo {
        name: "copilot",
        support: "event-bridge",
        installed,
        installed_version: installed_version.clone(),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: installed && installed_version.as_deref() != Some(INTEGRATION_VERSION),
        install_path: Some(path.to_path_buf()),
        message: installed.then_some("GitHub Copilot provider-event hook installed".to_string()),
    })
}
