use std::fmt::{Display, Formatter};
use std::io;
use std::process::Command;

pub const PINNED_NMAP_RUNTIME_VERSION: &str = "7.95";
pub const PINNED_NMAP_DEBIAN_PACKAGE_VERSION: &str = "7.95+dfsg-3";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NmapVersion {
    pub runtime_version: String,
    pub debian_package_version: &'static str,
    pub raw: String,
}

#[derive(Debug)]
pub enum NmapVersionError {
    Io(io::Error),
    Exit(String),
    Parse(String),
    VersionMismatch { expected: String, found: String },
}

pub fn probe_pinned_nmap_version(nmap_bin: &str) -> Result<NmapVersion, NmapVersionError> {
    let output = Command::new(nmap_bin).arg("--version").output()?;
    if !output.status.success() {
        return Err(NmapVersionError::Exit(format!(
            "{nmap_bin} --version exited with {}",
            output.status
        )));
    }
    let raw = String::from_utf8_lossy(&output.stdout).to_string();
    let runtime_version = parse_nmap_runtime_version(&raw).ok_or_else(|| {
        NmapVersionError::Parse("unable to parse nmap runtime version".to_string())
    })?;
    if runtime_version != PINNED_NMAP_RUNTIME_VERSION {
        return Err(NmapVersionError::VersionMismatch {
            expected: PINNED_NMAP_RUNTIME_VERSION.to_string(),
            found: runtime_version,
        });
    }
    Ok(NmapVersion {
        runtime_version,
        debian_package_version: PINNED_NMAP_DEBIAN_PACKAGE_VERSION,
        raw,
    })
}

fn parse_nmap_runtime_version(raw: &str) -> Option<String> {
    raw.lines().find_map(|line| {
        let line = line.trim();
        let rest = line.strip_prefix("Nmap version ")?;
        rest.split_whitespace().next().map(str::to_string)
    })
}

impl Display for NmapVersionError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            NmapVersionError::Io(error) => write!(formatter, "{error}"),
            NmapVersionError::Exit(message) => write!(formatter, "{message}"),
            NmapVersionError::Parse(message) => write!(formatter, "{message}"),
            NmapVersionError::VersionMismatch { expected, found } => write!(
                formatter,
                "nmap version mismatch: expected {expected}, found {found}"
            ),
        }
    }
}

impl std::error::Error for NmapVersionError {}

impl From<io::Error> for NmapVersionError {
    fn from(error: io::Error) -> Self {
        NmapVersionError::Io(error)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_nmap_runtime_version() {
        assert_eq!(
            parse_nmap_runtime_version("Nmap version 7.95 ( https://nmap.org )\n"),
            Some("7.95".to_string())
        );
        assert_eq!(parse_nmap_runtime_version("not nmap"), None);
    }
}
