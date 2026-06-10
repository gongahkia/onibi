use super::{home_path, read_json, write_json, AdapterInfo, INTEGRATION_VERSION};
use anyhow::Result;
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

pub fn info() -> AdapterInfo {
    match hooks_path() {
        Ok(path) => status_at(&path).unwrap_or_else(|error| AdapterInfo {
            name: "goose",
            support: "event-bridge",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => AdapterInfo {
            name: "goose",
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
    write_json(&hooks_path()?, &hook_config())?;
    Ok("goose lifecycle hooks installed".to_string())
}

pub fn uninstall() -> Result<String> {
    let path = hooks_path()?;
    if path.exists() {
        std::fs::remove_file(&path)?;
    }
    Ok("goose lifecycle hooks uninstalled".to_string())
}

fn hooks_path() -> Result<PathBuf> {
    home_path(
        "ONIBI_GOOSE_HOOKS",
        &[".agents", "plugins", "onibi", "hooks", "hooks.json"],
    )
}

fn hook_config() -> Value {
    let command = "onibi _hook goose";
    json!({
        "onibiIntegrationVersion": INTEGRATION_VERSION,
        "hooks": {
            "SessionStart": [hook(command)],
            "UserPromptSubmit": [hook(command)],
            "PreToolUse": [hook(command)],
            "PostToolUse": [hook(command)],
            "PostToolUseFailure": [hook(command)],
            "Stop": [hook(command)]
        }
    })
}

fn hook(command: &str) -> Value {
    json!({
        "type": "command",
        "command": command
    })
}

fn status_at(path: &Path) -> Result<AdapterInfo> {
    if !path.exists() {
        return Ok(AdapterInfo {
            name: "goose",
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
        name: "goose",
        support: "event-bridge",
        installed,
        installed_version: installed_version.clone(),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: installed && installed_version.as_deref() != Some(INTEGRATION_VERSION),
        install_path: Some(path.to_path_buf()),
        message: installed.then_some("goose lifecycle hooks installed".to_string()),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hook_config_installs_pre_tool_use_hook() {
        let config = hook_config();
        assert_eq!(
            config["hooks"]["PreToolUse"][0]["command"].as_str(),
            Some("onibi _hook goose")
        );
        assert_eq!(
            config["onibiIntegrationVersion"].as_str(),
            Some(INTEGRATION_VERSION)
        );
    }
}
