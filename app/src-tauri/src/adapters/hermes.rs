use super::{acp, AdapterInfo};
use crate::config;
use anyhow::{bail, Result};

pub fn info() -> AdapterInfo {
    let adapter = config::load().unwrap_or_default().adapters.hermes;
    acp::status_info("hermes", &adapter.acp_command, &adapter.acp_args)
}

pub fn install() -> Result<String> {
    let adapter = config::load().unwrap_or_default().adapters.hermes;
    if which::which(&adapter.acp_command).is_err() {
        bail!("Hermes ACP command not found: {}", adapter.acp_command);
    }
    Ok("hermes ACP transport is available; no plugin hook was installed".to_string())
}

pub fn uninstall() -> Result<String> {
    Ok("hermes ACP transport is built into the Hermes CLI; no plugin hook was removed".to_string())
}
