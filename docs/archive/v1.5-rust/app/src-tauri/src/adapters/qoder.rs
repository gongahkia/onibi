use super::{
    command_string_hook, home_path, read_json, write_json, AdapterInfo, INTEGRATION_VERSION,
    INTEGRATION_VERSION_FIELD,
};
use anyhow::{Context, Result};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

const HOOK_COMMAND: &str = "onibi _hook qoder";
const EVENTS: &[&str] = &[
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "Stop",
];

pub fn info() -> AdapterInfo {
    match settings_path() {
        Ok(path) => status_at(&path).unwrap_or_else(|error| AdapterInfo {
            name: "qoder",
            support: "event-bridge",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => AdapterInfo {
            name: "qoder",
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
    install_at(&settings_path()?)?;
    Ok("qoder provider-event hooks installed".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&settings_path()?)?;
    Ok("qoder provider-event hooks uninstalled".to_string())
}

fn settings_path() -> Result<PathBuf> {
    home_path("ONIBI_QODER_SETTINGS", &[".qoder", "settings.json"])
}

fn install_at(path: &Path) -> Result<()> {
    let mut settings = read_json(path, json!({}))?;
    remove_onibi_hooks(&mut settings);
    let root = settings
        .as_object_mut()
        .context("Qoder settings must be an object")?;
    let hooks = root.entry("hooks").or_insert_with(|| json!({}));
    let hooks = hooks
        .as_object_mut()
        .context("Qoder settings hooks field must be an object")?;
    for event in EVENTS {
        let groups = hooks.entry(*event).or_insert_with(|| json!([]));
        let groups = groups
            .as_array_mut()
            .with_context(|| format!("Qoder {event} hooks field must be an array"))?;
        groups.push(json!({
            "hooks": [{
                "type": "command",
                "command": command_string_hook("qoder"),
                "onibiIntegrationVersion": INTEGRATION_VERSION
            }]
        }));
    }
    write_json(path, &settings)
}

fn uninstall_at(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    let mut settings = read_json(path, json!({}))?;
    remove_onibi_hooks(&mut settings);
    write_json(path, &settings)
}

fn status_at(path: &Path) -> Result<AdapterInfo> {
    if !path.exists() {
        return Ok(AdapterInfo {
            name: "qoder",
            support: "event-bridge",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path.to_path_buf()),
            message: Some("not installed".to_string()),
        });
    }
    let settings = read_json(path, json!({}))?;
    let installed_version = onibi_hook_version(&settings);
    let installed = installed_version.is_some() || onibi_handlers(&settings).next().is_some();
    Ok(AdapterInfo {
        name: "qoder",
        support: "event-bridge",
        installed,
        installed_version: installed_version.clone(),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: installed && installed_version.as_deref() != Some(INTEGRATION_VERSION),
        install_path: Some(path.to_path_buf()),
        message: installed.then_some("Qoder provider-event hooks installed".to_string()),
    })
}

fn remove_onibi_hooks(settings: &mut Value) {
    let Some(hooks) = settings.get_mut("hooks").and_then(Value::as_object_mut) else {
        return;
    };
    for groups in hooks.values_mut().filter_map(Value::as_array_mut) {
        for group in groups.iter_mut() {
            if let Some(handlers) = group.get_mut("hooks").and_then(Value::as_array_mut) {
                handlers.retain(|handler| !is_onibi_handler(handler));
            }
        }
        groups.retain(|group| {
            group
                .get("hooks")
                .and_then(Value::as_array)
                .is_none_or(|handlers| !handlers.is_empty())
        });
    }
}

fn onibi_hook_version(settings: &Value) -> Option<String> {
    onibi_handlers(settings).find_map(|handler| {
        handler
            .get(INTEGRATION_VERSION_FIELD)
            .and_then(Value::as_str)
            .map(ToString::to_string)
    })
}

fn onibi_handlers(settings: &Value) -> impl Iterator<Item = &Value> {
    settings
        .get("hooks")
        .and_then(Value::as_object)
        .into_iter()
        .flat_map(|hooks| hooks.values())
        .filter_map(Value::as_array)
        .flatten()
        .filter_map(|group| group.get("hooks").and_then(Value::as_array))
        .flatten()
        .filter(|handler| is_onibi_handler(handler))
}

fn is_onibi_handler(handler: &Value) -> bool {
    handler.get("command").and_then(Value::as_str) == Some(HOOK_COMMAND)
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn install_uses_match_all_groups_without_invalid_star_matcher() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("settings.json");

        install_at(&path).unwrap();
        install_at(&path).unwrap();
        let settings = read_json(&path, json!({})).unwrap();
        let pre_tool_groups = settings["hooks"]["PreToolUse"].as_array().unwrap();

        assert_eq!(onibi_handlers(&settings).count(), EVENTS.len());
        assert_eq!(pre_tool_groups.len(), 1);
        assert!(pre_tool_groups[0].get("matcher").is_none());
    }
}
