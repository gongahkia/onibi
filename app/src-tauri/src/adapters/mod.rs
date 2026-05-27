pub mod aider;
pub mod claude_code;
pub mod codex;
pub mod cursor;
pub mod gemini;
pub mod goose;
pub mod opencode;

use anyhow::{bail, Result};

#[derive(Debug, Clone)]
pub struct AdapterInfo {
    pub name: &'static str,
    pub support: &'static str,
    pub installed: bool,
}

pub fn list() -> Vec<AdapterInfo> {
    vec![
        AdapterInfo {
            name: "claude-code",
            support: "full",
            installed: claude_code::installed().unwrap_or(false),
        },
        AdapterInfo {
            name: "codex",
            support: "bash-only",
            installed: codex::installed().unwrap_or(false),
        },
        opencode::info(),
        gemini::info(),
        aider::info(),
        cursor::info(),
        goose::info(),
    ]
}

pub fn install(name: &str, token: &str) -> Result<String> {
    match name {
        "claude-code" | "claude" => claude_code::install(token),
        "codex" => codex::install(),
        "opencode" => opencode::install(),
        "gemini" => gemini::install(),
        "aider" => aider::install(),
        "cursor" => cursor::install(),
        "goose" => goose::install(),
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
        other => bail!("unknown adapter: {other}"),
    }
}

pub fn stub_info(name: &'static str) -> AdapterInfo {
    AdapterInfo {
        name,
        support: "stub",
        installed: false,
    }
}

pub fn stub_install(name: &str) -> Result<String> {
    Ok(format!(
        "{name} adapter is registered as a Phase-03 stub; no hooks were installed"
    ))
}

pub fn stub_uninstall(name: &str) -> Result<String> {
    Ok(format!(
        "{name} adapter is a Phase-03 stub; no hooks were removed"
    ))
}
