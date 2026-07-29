use std::{
    fs,
    path::{Path, PathBuf},
};

use crate::ClientStateDirectory;

pub const DAEMON_CONFIG_SCHEMA_VERSION: u8 = 1;
pub const MAX_DAEMON_CONFIG_BYTES: usize = 16 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DaemonConfig {
    state_directory: PathBuf,
}

impl DaemonConfig {
    pub fn load(path: &Path) -> Result<Self, DaemonConfigError> {
        let source = fs::read(path).map_err(DaemonConfigError::Read)?;
        if source.len() > MAX_DAEMON_CONFIG_BYTES {
            return Err(DaemonConfigError::TooLarge);
        }
        let source = std::str::from_utf8(&source).map_err(|_| DaemonConfigError::InvalidUtf8)?;
        Self::parse(source)
    }

    pub fn parse(source: &str) -> Result<Self, DaemonConfigError> {
        if source.len() > MAX_DAEMON_CONFIG_BYTES {
            return Err(DaemonConfigError::TooLarge);
        }
        let mut schema_version = None;
        let mut state_directory = None;
        for line in source.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let (key, value) = line.split_once('=').ok_or(DaemonConfigError::InvalidLine)?;
            let key = key.trim();
            let value = value.trim();
            match key {
                "config_version" => {
                    if schema_version.replace(parse_version(value)?).is_some() {
                        return Err(DaemonConfigError::DuplicateSetting);
                    }
                }
                "state_directory" => {
                    if state_directory.replace(parse_string(value)?).is_some() {
                        return Err(DaemonConfigError::DuplicateSetting);
                    }
                }
                _ => return Err(DaemonConfigError::UnknownSetting),
            }
        }
        let schema_version = schema_version.ok_or(DaemonConfigError::MissingSchemaVersion)?;
        if schema_version != DAEMON_CONFIG_SCHEMA_VERSION {
            return Err(DaemonConfigError::UnsupportedSchemaVersion(schema_version));
        }
        let state_directory = state_directory.ok_or(DaemonConfigError::MissingStateDirectory)?;
        if state_directory.contains('\0') {
            return Err(DaemonConfigError::InvalidStateDirectory);
        }
        Ok(Self {
            state_directory: state_directory.into(),
        })
    }

    #[must_use]
    pub fn state_directory(&self) -> &Path {
        &self.state_directory
    }

    pub fn validate_for_startup(&self) -> Result<(), DaemonConfigError> {
        ClientStateDirectory::new(&self.state_directory)
            .map(|_| ())
            .map_err(|_| DaemonConfigError::InvalidStateDirectory)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonConfigError {
    #[error("daemon configuration could not be read")]
    Read(#[source] std::io::Error),
    #[error("daemon configuration exceeds the configured limit")]
    TooLarge,
    #[error("daemon configuration is not valid UTF-8")]
    InvalidUtf8,
    #[error("daemon configuration has an invalid line")]
    InvalidLine,
    #[error("daemon configuration has an invalid string")]
    InvalidString,
    #[error("daemon configuration has an invalid schema version")]
    InvalidSchemaVersion,
    #[error("daemon configuration repeats a setting")]
    DuplicateSetting,
    #[error("daemon configuration has an unknown setting")]
    UnknownSetting,
    #[error("daemon configuration omits its schema version")]
    MissingSchemaVersion,
    #[error("daemon configuration schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("daemon configuration omits its state directory")]
    MissingStateDirectory,
    #[error("daemon state directory is invalid")]
    InvalidStateDirectory,
}

fn parse_version(value: &str) -> Result<u8, DaemonConfigError> {
    if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(DaemonConfigError::InvalidSchemaVersion);
    }
    value
        .parse()
        .map_err(|_| DaemonConfigError::InvalidSchemaVersion)
}

fn parse_string(value: &str) -> Result<String, DaemonConfigError> {
    let value = value
        .strip_prefix('"')
        .and_then(|value| value.strip_suffix('"'))
        .ok_or(DaemonConfigError::InvalidString)?;
    let mut output = String::with_capacity(value.len());
    let mut characters = value.chars();
    while let Some(character) = characters.next() {
        if character == '"' {
            return Err(DaemonConfigError::InvalidString);
        }
        if character != '\\' {
            output.push(character);
            continue;
        }
        match characters.next() {
            Some('"') => output.push('"'),
            Some('\\') => output.push('\\'),
            Some('n') => output.push('\n'),
            Some('r') => output.push('\r'),
            Some('t') => output.push('\t'),
            _ => return Err(DaemonConfigError::InvalidString),
        }
    }
    Ok(output)
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use super::{
        DAEMON_CONFIG_SCHEMA_VERSION, DaemonConfig, DaemonConfigError, MAX_DAEMON_CONFIG_BYTES,
    };

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn config_path() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir().join(format!(
            "arachne-daemon-config-{}-{number}.conf",
            std::process::id()
        ))
    }

    #[test]
    fn loads_a_typed_versioned_configuration() {
        let path = config_path();
        fs::write(
            &path,
            r#"# daemon configuration
config_version = 1
state_directory = "C:\\state"
"#,
        )
        .unwrap();
        let config = DaemonConfig::load(&path).unwrap();
        assert_eq!(config.state_directory(), PathBuf::from("C:\\state"));
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_malformed_unknown_duplicate_and_oversized_configuration() {
        assert!(matches!(
            DaemonConfig::parse("config_version = 1\nstate_directory = \"/state\"\nunknown = 1\n"),
            Err(DaemonConfigError::UnknownSetting)
        ));
        assert!(matches!(
            DaemonConfig::parse(
                "config_version = 1\nconfig_version = 1\nstate_directory = \"/state\"\n"
            ),
            Err(DaemonConfigError::DuplicateSetting)
        ));
        assert!(matches!(
            DaemonConfig::parse("config_version = 2\nstate_directory = \"/state\"\n"),
            Err(DaemonConfigError::UnsupportedSchemaVersion(2))
        ));
        assert!(matches!(
            DaemonConfig::parse("config_version = 1\nstate_directory = /state\n"),
            Err(DaemonConfigError::InvalidString)
        ));
        assert!(matches!(
            DaemonConfig::parse("config_version = +1\nstate_directory = \"/state\"\n"),
            Err(DaemonConfigError::InvalidSchemaVersion)
        ));
        assert!(matches!(
            DaemonConfig::parse(&"x".repeat(MAX_DAEMON_CONFIG_BYTES + 1)),
            Err(DaemonConfigError::TooLarge)
        ));
        assert_eq!(DAEMON_CONFIG_SCHEMA_VERSION, 1);
    }

    #[test]
    fn startup_validation_rejects_unsafe_state_directories() {
        for state_directory in ["", "state", "../state", "/state/../other"] {
            let config = DaemonConfig::parse(&format!(
                "config_version = 1\nstate_directory = \"{state_directory}\"\n"
            ))
            .unwrap();
            assert!(matches!(
                config.validate_for_startup(),
                Err(DaemonConfigError::InvalidStateDirectory)
            ));
        }
    }
}
