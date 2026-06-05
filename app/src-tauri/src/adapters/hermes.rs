use super::{resume_only_info, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    resume_only_info(
        "hermes",
        "Hermes supports --resume session IDs; native plugin hook installation remains pending.",
    )
}

pub fn install() -> Result<String> {
    Ok("hermes resume metadata is supported; no plugin hook was installed".to_string())
}

pub fn uninstall() -> Result<String> {
    Ok("hermes resume metadata is supported; no plugin hook was removed".to_string())
}
