use super::{stub_info, stub_install, stub_uninstall, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    stub_info("aider")
}

pub fn install() -> Result<String> {
    stub_install("aider")
}

pub fn uninstall() -> Result<String> {
    stub_uninstall("aider")
}
