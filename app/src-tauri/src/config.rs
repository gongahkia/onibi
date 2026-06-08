use crate::secret;
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::{
    fs,
    path::{Path, PathBuf},
};

pub const DEFAULT_PORT: u16 = 17_893;
pub const DEFAULT_APPROVAL_TIMEOUT_SECS: u64 = 600;
pub const DEFAULT_PTY_RING_LIMIT: usize = 5_000;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct OnibiConfig {
    pub version: u8,
    pub server: ServerConfig,
    pub checkpointing: CheckpointingConfig,
    pub ui: UiConfig,
    pub terminal: TerminalConfig,
    pub keybindings: KeybindingsConfig,
    pub workspaces: Vec<WorkspaceConfig>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct ServerConfig {
    pub port: u16,
    pub approval_timeout_secs: u64,
    pub pty_ring_limit: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct CheckpointingConfig {
    pub enabled: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct UiConfig {
    pub theme: String,
    pub tab_bar_position: String,
    pub default_agent: String,
    pub new_pane_cwd: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct TerminalConfig {
    pub shell_integration: bool,
    pub font_family: String,
    pub font_size: u16,
    pub scrollback_lines: u32,
    pub pane_history_enabled: bool,
    pub remote_keybinding_policy: String,
    pub remote_ssh_command: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct KeybindingsConfig {
    pub prefix: String,
    pub app: Vec<AppKeybindingConfig>,
    pub command: Vec<CommandKeybindingConfig>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppKeybindingConfig {
    pub keys: String,
    pub action: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandKeybindingConfig {
    pub keys: String,
    pub command: String,
    pub description: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct WorkspaceConfig {
    pub id: Option<String>,
    pub path: String,
    pub name: Option<String>,
    pub collapsed: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct RuntimeConfig {
    pub approval_timeout_secs: u64,
    pub pty_ring_limit: usize,
    pub checkpointing_enabled: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigValidation {
    pub ok: bool,
    pub path: PathBuf,
    pub exists: bool,
    pub runtime: RuntimeConfig,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct KeybindingsReset {
    pub ok: bool,
    pub path: PathBuf,
    pub existed: bool,
    pub prefix: String,
    pub app_binding_count: usize,
    pub command_binding_count: usize,
}

impl Default for OnibiConfig {
    fn default() -> Self {
        Self {
            version: 1,
            server: ServerConfig::default(),
            checkpointing: CheckpointingConfig::default(),
            ui: UiConfig::default(),
            terminal: TerminalConfig::default(),
            keybindings: KeybindingsConfig::default(),
            workspaces: Vec::new(),
        }
    }
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            port: DEFAULT_PORT,
            approval_timeout_secs: DEFAULT_APPROVAL_TIMEOUT_SECS,
            pty_ring_limit: DEFAULT_PTY_RING_LIMIT,
        }
    }
}

impl Default for CheckpointingConfig {
    fn default() -> Self {
        Self { enabled: false }
    }
}

impl Default for UiConfig {
    fn default() -> Self {
        Self {
            theme: "vscode-dark-plus".to_string(),
            tab_bar_position: "left".to_string(),
            default_agent: "claude-code".to_string(),
            new_pane_cwd: "active".to_string(),
        }
    }
}

impl Default for TerminalConfig {
    fn default() -> Self {
        Self {
            shell_integration: true,
            font_family: "Menlo, Monaco, monospace".to_string(),
            font_size: 13,
            scrollback_lines: 0,
            pane_history_enabled: false,
            remote_keybinding_policy: "local".to_string(),
            remote_ssh_command: "ssh".to_string(),
        }
    }
}

impl Default for KeybindingsConfig {
    fn default() -> Self {
        let mut app = vec![
            AppKeybindingConfig::new("cmd+p", "commandPalette.open"),
            AppKeybindingConfig::new("ctrl+p", "commandPalette.open"),
            AppKeybindingConfig::new("cmd+n", "session.new"),
            AppKeybindingConfig::new("ctrl+n", "session.new"),
            AppKeybindingConfig::new("cmd+shift+t", "editor.reopenClosed"),
            AppKeybindingConfig::new("ctrl+shift+t", "editor.reopenClosed"),
            AppKeybindingConfig::new("prefix+v", "terminal.splitRight"),
            AppKeybindingConfig::new("prefix+s", "terminal.splitDown"),
            AppKeybindingConfig::new("prefix+z", "terminal.toggleMaximize"),
            AppKeybindingConfig::new("prefix+n", "terminal.focusNextPane"),
            AppKeybindingConfig::new("prefix+p", "terminal.focusPreviousPane"),
            AppKeybindingConfig::new("prefix+[", "terminal.copyMode.enter"),
            AppKeybindingConfig::new("prefix+x", "session.closeActive"),
            AppKeybindingConfig::new("prefix+w", "workspace.navigator.open"),
            AppKeybindingConfig::new("prefix+g", "session.navigator.open"),
            AppKeybindingConfig::new("prefix+?", "keybindings.help.open"),
            AppKeybindingConfig::new("prefix+right", "workspace.next"),
            AppKeybindingConfig::new("prefix+left", "workspace.previous"),
            AppKeybindingConfig::new("prefix+shift+right", "workspace.tab.next"),
            AppKeybindingConfig::new("prefix+shift+left", "workspace.tab.previous"),
        ];
        app.extend((1..=9).map(|index| {
            AppKeybindingConfig::new(
                &format!("prefix+{index}"),
                &format!("workspace.tab.focusIndex{index}"),
            )
        }));
        Self {
            prefix: "ctrl+b".to_string(),
            app,
            command: Vec::new(),
        }
    }
}

impl Default for WorkspaceConfig {
    fn default() -> Self {
        Self {
            id: None,
            path: String::new(),
            name: None,
            collapsed: false,
        }
    }
}

impl AppKeybindingConfig {
    fn new(keys: &str, action: &str) -> Self {
        Self {
            keys: keys.to_string(),
            action: action.to_string(),
        }
    }
}

impl OnibiConfig {
    pub fn server_port(&self) -> u16 {
        if self.server.port == 0 {
            DEFAULT_PORT
        } else {
            self.server.port
        }
    }

    pub fn approval_timeout_secs(&self) -> u64 {
        self.server.approval_timeout_secs.max(1)
    }

    pub fn pty_ring_limit(&self) -> usize {
        self.server.pty_ring_limit.max(1024)
    }

    pub fn runtime_config(&self) -> RuntimeConfig {
        RuntimeConfig {
            approval_timeout_secs: self.approval_timeout_secs(),
            pty_ring_limit: self.pty_ring_limit(),
            checkpointing_enabled: self.checkpointing.enabled,
        }
    }
}

pub fn path() -> Result<PathBuf> {
    Ok(secret::config_dir()?.join("config.toml"))
}

pub fn load() -> Result<OnibiConfig> {
    let path = path()?;
    load_from_path(&path)
}

pub fn load_from_path(path: &Path) -> Result<OnibiConfig> {
    if !path.exists() {
        return Ok(OnibiConfig::default());
    }
    let raw = fs::read_to_string(&path)
        .with_context(|| format!("read Onibi config {}", path.display()))?;
    toml::from_str(&raw).with_context(|| format!("parse Onibi config {}", path.display()))
}

pub fn validate() -> Result<ConfigValidation> {
    let path = path()?;
    validate_path(&path)
}

pub fn validate_path(path: &Path) -> Result<ConfigValidation> {
    let exists = path.exists();
    let config = load_from_path(path)?;
    Ok(ConfigValidation {
        ok: true,
        path: path.to_path_buf(),
        exists,
        runtime: config.runtime_config(),
    })
}

pub fn reset_keybindings() -> Result<KeybindingsReset> {
    let path = path()?;
    reset_keybindings_path(&path)
}

pub fn reset_keybindings_path(path: &Path) -> Result<KeybindingsReset> {
    let existed = path.exists();
    let mut root = if existed {
        let raw = fs::read_to_string(path)
            .with_context(|| format!("read Onibi config {}", path.display()))?;
        raw.parse::<toml::Value>()
            .with_context(|| format!("parse Onibi config {}", path.display()))?
    } else {
        toml::Value::Table(toml::map::Map::new())
    };
    let root_table = root
        .as_table_mut()
        .context("Onibi config root must be a TOML table")?;

    root_table
        .entry("version".to_string())
        .or_insert_with(|| toml::Value::Integer(1));

    if let Some(settings) = root_table
        .get_mut("settings")
        .and_then(toml::Value::as_table_mut)
    {
        settings.remove("keybinding_prefix");
        settings.remove("app_keybindings");
        settings.remove("command_keybindings");
        settings.remove("custom_command_keybindings");
    }

    let defaults = KeybindingsConfig::default();
    let keybindings = toml::Value::try_from(defaults.clone())
        .context("serialize default keybindings for reset")?;
    root_table.insert("keybindings".to_string(), keybindings);

    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create Onibi config directory {}", parent.display()))?;
    }
    fs::write(path, toml::to_string_pretty(&root)?)
        .with_context(|| format!("write Onibi config {}", path.display()))?;
    let _ = load_from_path(path)?;

    Ok(KeybindingsReset {
        ok: true,
        path: path.to_path_buf(),
        existed,
        prefix: defaults.prefix,
        app_binding_count: defaults.app.len(),
        command_binding_count: defaults.command.len(),
    })
}

#[allow(dead_code)]
pub fn read_raw() -> Result<Option<String>> {
    let path = path()?;
    if !path.exists() {
        return Ok(None);
    }
    fs::read_to_string(&path)
        .map(Some)
        .with_context(|| format!("read Onibi config {}", path.display()))
}

#[allow(dead_code)]
pub fn write_raw(raw: &str) -> Result<()> {
    let path = path()?;
    let _: OnibiConfig = toml::from_str(raw).context("parse Onibi config before writing")?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create Onibi config directory {}", parent.display()))?;
    }
    fs::write(&path, raw).with_context(|| format!("write Onibi config {}", path.display()))
}

pub fn default_config_toml() -> String {
    toml::to_string_pretty(&OnibiConfig::default()).unwrap_or_else(|_| "version = 1\n".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_missing_config_with_defaults() {
        let dir = tempfile::tempdir().unwrap();
        let validation = validate_path(&dir.path().join("config.toml")).unwrap();
        assert!(validation.ok);
        assert!(!validation.exists);
        assert_eq!(validation.runtime.approval_timeout_secs, 600);
        assert_eq!(validation.runtime.pty_ring_limit, 5_000);
    }

    #[test]
    fn defaults_include_indexed_workspace_tab_keybindings() {
        let defaults = KeybindingsConfig::default();
        assert!(defaults
            .app
            .iter()
            .any(|binding| binding.keys == "prefix+1"
                && binding.action == "workspace.tab.focusIndex1"));
        assert!(defaults
            .app
            .iter()
            .any(|binding| binding.keys == "prefix+9"
                && binding.action == "workspace.tab.focusIndex9"));
        assert!(defaults.app.iter().any(
            |binding| binding.keys == "prefix+[" && binding.action == "terminal.copyMode.enter"
        ));
        assert!(defaults.command.is_empty());
    }

    #[test]
    fn validates_runtime_values_from_toml() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.toml");
        fs::write(
            &path,
            r#"
version = 1

[server]
approval_timeout_secs = 12
pty_ring_limit = 2048
"#,
        )
        .unwrap();

        let validation = validate_path(&path).unwrap();
        assert!(validation.exists);
        assert_eq!(validation.runtime.approval_timeout_secs, 12);
        assert_eq!(validation.runtime.pty_ring_limit, 2048);
    }

    #[test]
    fn rejects_malformed_toml() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.toml");
        fs::write(&path, "[server").unwrap();

        assert!(validate_path(&path).is_err());
    }

    #[test]
    fn reset_keybindings_preserves_unrelated_config() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.toml");
        fs::write(
            &path,
            r#"
version = 1

[server]
port = 12345
approval_timeout_secs = 42

[settings]
theme = "github-light"
keybinding_prefix = "ctrl+a"

[[settings.app_keybindings]]
keys = "ctrl+x"
action = "workspace.next"

[[settings.command_keybindings]]
keys = "prefix+t"
command = "pnpm test"

[ui]
theme = "custom"

[custom]
value = "kept"
"#,
        )
        .unwrap();

        let reset = reset_keybindings_path(&path).unwrap();
        assert!(reset.ok);
        assert!(reset.existed);
        assert_eq!(reset.prefix, "ctrl+b");
        assert_eq!(reset.command_binding_count, 0);

        let raw = fs::read_to_string(&path).unwrap();
        assert!(raw.contains("port = 12345"));
        assert!(raw.contains("theme = \"github-light\""));
        assert!(raw.contains("[custom]"));
        assert!(raw.contains("value = \"kept\""));
        assert!(!raw.contains("keybinding_prefix"));
        assert!(!raw.contains("settings.app_keybindings"));
        assert!(!raw.contains("settings.command_keybindings"));

        let parsed = load_from_path(&path).unwrap();
        assert_eq!(parsed.keybindings.prefix, "ctrl+b");
        assert!(parsed
            .keybindings
            .app
            .iter()
            .any(|binding| binding.keys == "prefix+9"
                && binding.action == "workspace.tab.focusIndex9"));
        assert!(parsed.keybindings.command.is_empty());
    }
}
