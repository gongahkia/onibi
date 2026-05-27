use super::{stub_info, stub_install, stub_uninstall, AdapterInfo};
use anyhow::Result;

pub fn info() -> AdapterInfo {
    stub_info("cursor")
}

pub fn install() -> Result<String> {
    stub_install("cursor")
}

pub fn uninstall() -> Result<String> {
    stub_uninstall("cursor")
}
