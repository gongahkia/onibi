pub mod aider;
pub mod claude_code;
pub mod codex;
pub mod cursor;
pub mod gemini;
pub mod goose;
pub mod opencode;

use anyhow::{bail, Result};
use serde::Serialize;
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct IntegrationInfo {
    pub name: &'static str,
    pub support: &'static str,
    pub installed: bool,
    pub installed_version: Option<String>,
    pub bundled_version: Option<&'static str>,
    pub outdated: bool,
    pub install_path: Option<PathBuf>,
    pub message: Option<String>,
}

pub type AdapterInfo = IntegrationInfo;

pub const INTEGRATION_VERSION: &str = "1.0.0";
pub const INTEGRATION_VERSION_HEADER: &str = "X-Onibi-Integration-Version";
pub const INTEGRATION_VERSION_FIELD: &str = "onibiIntegrationVersion";

pub fn list() -> Vec<IntegrationInfo> {
    integrations()
}

pub fn integrations() -> Vec<IntegrationInfo> {
    vec![
        claude_code::info(),
        codex::info(),
        opencode::info(),
        gemini::info(),
        aider::info(),
        cursor::info(),
        goose::info(),
    ]
}

pub fn status(outdated_only: bool) -> Vec<IntegrationInfo> {
    filter_status(integrations(), outdated_only)
}

fn filter_status(integrations: Vec<IntegrationInfo>, outdated_only: bool) -> Vec<IntegrationInfo> {
    integrations
        .into_iter()
        .filter(|integration| !outdated_only || integration.outdated)
        .collect()
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
        installed_version: None,
        bundled_version: None,
        outdated: false,
        install_path: None,
        message: Some("stub integration; no hook installer is available yet".to_string()),
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn outdated_only_filters_current_and_missing_integrations() {
        let integrations = vec![
            IntegrationInfo {
                name: "current",
                support: "full",
                installed: true,
                installed_version: Some(INTEGRATION_VERSION.to_string()),
                bundled_version: Some(INTEGRATION_VERSION),
                outdated: false,
                install_path: None,
                message: None,
            },
            IntegrationInfo {
                name: "old",
                support: "full",
                installed: true,
                installed_version: Some("0.9.0".to_string()),
                bundled_version: Some(INTEGRATION_VERSION),
                outdated: true,
                install_path: None,
                message: None,
            },
            stub_info("stub"),
        ];

        let filtered = filter_status(integrations, true);
        assert_eq!(filtered.len(), 1);
        assert_eq!(filtered[0].name, "old");
    }
}
