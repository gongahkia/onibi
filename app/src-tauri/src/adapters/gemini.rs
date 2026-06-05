use super::{resume_only_info, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    resume_only_info(
        "gemini",
        "Gemini CLI supports checkpoint/resume, but no stable blocking hook installer is wired.",
    )
}

pub fn install() -> Result<String> {
    Ok("gemini is resume-only; no hook files were installed".to_string())
}

pub fn uninstall() -> Result<String> {
    Ok("gemini is resume-only; no hook files were removed".to_string())
}
