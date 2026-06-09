use super::{
    command_string_hook, home_path, read_json, write_json, AdapterInfo, INTEGRATION_VERSION,
    INTEGRATION_VERSION_FIELD,
};
use anyhow::{Context, Result};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

const AGENT: &str = "cursor";
const EVENTS: &[&str] = &[
    "beforeShellExecution",
    "afterShellExecution",
    "beforeMCPExecution",
    "afterMCPExecution",
    "afterFileEdit",
];

pub fn info() -> AdapterInfo {
    match hooks_path() {
        Ok(path) => status_at(&path).unwrap_or_else(|error| AdapterInfo {
            name: AGENT,
            support: "native-observe",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => AdapterInfo {
            name: AGENT,
            support: "native-observe",
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
    Ok("Cursor native-observe CLI hooks installed".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&hooks_path()?)?;
    Ok("Cursor native-observe CLI hooks uninstalled".to_string())
}

fn hooks_path() -> Result<PathBuf> {
    home_path("ONIBI_CURSOR_HOOKS", &[".cursor", "hooks.json"])
}

fn install_at(path: &Path) -> Result<()> {
    let mut settings = read_json(path, json!({}))?;
    remove_onibi_hooks(&mut settings);
    let root = settings
        .as_object_mut()
        .context("Cursor hooks config must be an object")?;
    root.insert(
        INTEGRATION_VERSION_FIELD.to_string(),
        Value::String(INTEGRATION_VERSION.to_string()),
    );
    let hooks = root.entry("hooks").or_insert_with(|| json!({}));
    let hooks = hooks
        .as_object_mut()
        .context("Cursor hooks field must be an object")?;
    for event in EVENTS {
        let handlers = hooks.entry(*event).or_insert_with(|| json!([]));
        let handlers = handlers
            .as_array_mut()
            .with_context(|| format!("Cursor {event} hooks field must be an array"))?;
        handlers.push(hook_config());
    }
    write_json(path, &settings)
}

fn uninstall_at(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    let mut settings = read_json(path, json!({}))?;
    remove_onibi_hooks(&mut settings);
    if let Some(root) = settings.as_object_mut() {
        root.remove(INTEGRATION_VERSION_FIELD);
    }
    write_json(path, &settings)
}

fn status_at(path: &Path) -> Result<AdapterInfo> {
    if !path.exists() {
        return Ok(AdapterInfo {
            name: AGENT,
            support: "native-observe",
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
        name: AGENT,
        support: "native-observe",
        installed,
        installed_version: installed_version.clone(),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: installed && installed_version.as_deref() != Some(INTEGRATION_VERSION),
        install_path: Some(path.to_path_buf()),
        message: installed.then_some(
            "Cursor native-observe CLI hooks installed; lifecycle hooks are not installed"
                .to_string(),
        ),
    })
}

fn hook_config() -> Value {
    json!({
        "type": "command",
        "command": command_string_hook(AGENT),
        "onibiIntegrationVersion": INTEGRATION_VERSION,
        "blocking": false
    })
}

fn remove_onibi_hooks(settings: &mut Value) {
    let Some(hooks) = settings.get_mut("hooks").and_then(Value::as_object_mut) else {
        return;
    };
    for handlers in hooks.values_mut().filter_map(Value::as_array_mut) {
        handlers.retain(|handler| !is_onibi_handler(handler));
    }
    hooks.retain(|_, handlers| {
        handlers
            .as_array()
            .is_none_or(|handlers| !handlers.is_empty())
    });
}

fn onibi_hook_version(settings: &Value) -> Option<String> {
    settings
        .get(INTEGRATION_VERSION_FIELD)
        .and_then(Value::as_str)
        .map(ToString::to_string)
        .or_else(|| {
            onibi_handlers(settings).find_map(|handler| {
                handler
                    .get(INTEGRATION_VERSION_FIELD)
                    .and_then(Value::as_str)
                    .map(ToString::to_string)
            })
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
        .filter(|handler| is_onibi_handler(handler))
}

fn is_onibi_handler(handler: &Value) -> bool {
    handler.get("command").and_then(Value::as_str) == Some("onibi _hook cursor")
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn install_merges_cursor_observe_hooks() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("hooks.json");
        write_json(
            &path,
            &json!({
                "hooks": {
                    "beforeShellExecution": [{"command": "custom"}]
                }
            }),
        )
        .unwrap();

        install_at(&path).unwrap();
        install_at(&path).unwrap();
        let settings = read_json(&path, json!({})).unwrap();

        assert!(
            settings["hooks"]["BeforeShellExecution"]
                .as_array()
                .is_none()
        );
        assert_eq!(
            settings["hooks"]["beforeShellExecution"]
                .as_array()
                .unwrap()
                .len(),
            2
        );
        assert_eq!(onibi_handlers(&settings).count(), EVENTS.len());
        let status = status_at(&path).unwrap();
        assert!(status.installed);
        assert_eq!(status.support, "native-observe");
        assert_eq!(
            status.installed_version.as_deref(),
            Some(INTEGRATION_VERSION)
        );

        uninstall_at(&path).unwrap();
        let settings = read_json(&path, json!({})).unwrap();
        assert!(
            settings["hooks"]["BeforeShellExecution"]
                .as_array()
                .is_none()
        );
        assert_eq!(
            settings["hooks"]["beforeShellExecution"]
                .as_array()
                .unwrap()
                .len(),
            1
        );
        assert_eq!(onibi_handlers(&settings).count(), 0);
    }
}
