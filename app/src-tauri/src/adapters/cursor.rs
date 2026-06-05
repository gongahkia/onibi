use super::{pending_info, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    pending_info(
        "cursor",
        None,
        "Cursor agent is launch-detected, but no stable native hook installer is wired.",
    )
}

pub fn install() -> Result<String> {
    Ok("cursor native hooks are pending; no hook files were installed".to_string())
}

pub fn uninstall() -> Result<String> {
    Ok("cursor native hooks are pending; no hook files were removed".to_string())
}
