use super::{stub_info, stub_install, stub_uninstall, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    stub_info("opencode")
}

pub fn install() -> Result<String> {
    stub_install("opencode")
}

pub fn uninstall() -> Result<String> {
    stub_uninstall("opencode")
}
