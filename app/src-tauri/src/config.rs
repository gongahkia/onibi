use crate::secret;
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::{fs, path::PathBuf};

pub const DEFAULT_PORT: u16 = 17_893;
pub const DEFAULT_APPROVAL_TIMEOUT_SECS: u64 = 600;
pub const DEFAULT_PTY_RING_LIMIT: usize = 5_000;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct OnibiConfig {
    pub version: u8,
    pub server: ServerConfig,
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
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct KeybindingsConfig {
    pub prefix: String,
    pub app: Vec<AppKeybindingConfig>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppKeybindingConfig {
    pub keys: String,
    pub action: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct WorkspaceConfig {
    pub id: Option<String>,
    pub path: String,
    pub name: Option<String>,
    pub collapsed: bool,
}

impl Default for OnibiConfig {
    fn default() -> Self {
        Self {
            version: 1,
            server: ServerConfig::default(),
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
        }
    }
}

impl Default for KeybindingsConfig {
    fn default() -> Self {
        Self {
            prefix: "ctrl+b".to_string(),
            app: vec![
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
                AppKeybindingConfig::new("prefix+w", "session.closeActive"),
                AppKeybindingConfig::new("prefix+right", "workspace.next"),
                AppKeybindingConfig::new("prefix+left", "workspace.previous"),
                AppKeybindingConfig::new("prefix+shift+right", "workspace.tab.next"),
                AppKeybindingConfig::new("prefix+shift+left", "workspace.tab.previous"),
            ],
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
}

pub fn path() -> Result<PathBuf> {
    Ok(secret::config_dir()?.join("config.toml"))
}

pub fn load() -> Result<OnibiConfig> {
    let path = path()?;
    if !path.exists() {
        return Ok(OnibiConfig::default());
    }
    let raw = fs::read_to_string(&path)
        .with_context(|| format!("read Onibi config {}", path.display()))?;
    toml::from_str(&raw).with_context(|| format!("parse Onibi config {}", path.display()))
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
