use super::{resume_only_info, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    resume_only_info(
        "aider",
        "Aider can restore chat history, but Onibi has no provider session ID hook for it.",
    )
}

pub fn install() -> Result<String> {
    Ok("aider is history-restore only; no hook files were installed".to_string())
}

pub fn uninstall() -> Result<String> {
    Ok("aider is history-restore only; no hook files were removed".to_string())
}
