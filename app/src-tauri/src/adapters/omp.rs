use super::{pending_info, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    pending_info(
        "omp",
        None,
        "OMP native extension API is not verified; hook installation remains pending.",
    )
}

pub fn install() -> Result<String> {
    Ok("omp native extension is pending; no hook files were installed".to_string())
}

pub fn uninstall() -> Result<String> {
    Ok("omp native extension is pending; no hook files were removed".to_string())
}
