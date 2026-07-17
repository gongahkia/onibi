use std::{fs, path::Path};

pub const DAEMON_ENDPOINT_CONFIG_VERSION: u8 = 1;
pub const DEFAULT_DAEMON_UNIX_SOCKET_NAME: &str = "yeokcham-daemon.sock";
pub const DEFAULT_DAEMON_WINDOWS_NAMED_PIPE_NAME: &str = "yeokcham-daemon";
pub const MAX_DAEMON_ENDPOINT_CONFIG_BYTES: usize = 1024;
pub const MAX_DAEMON_ENDPOINT_NAME_BYTES: usize = 64;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DaemonEndpointConfig {
    endpoint: DaemonEndpoint,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum DaemonEndpoint {
    UnixSocket(String),
    WindowsNamedPipe(String),
}

impl DaemonEndpointConfig {
    pub fn load(path: &Path) -> Result<Self, DaemonEndpointConfigError> {
        let source = fs::read(path).map_err(DaemonEndpointConfigError::Read)?;
        if source.len() > MAX_DAEMON_ENDPOINT_CONFIG_BYTES {
            return Err(DaemonEndpointConfigError::TooLarge);
        }
        let source =
            std::str::from_utf8(&source).map_err(|_| DaemonEndpointConfigError::InvalidUtf8)?;
        Self::parse(source)
    }

    pub fn parse(source: &str) -> Result<Self, DaemonEndpointConfigError> {
        if source.len() > MAX_DAEMON_ENDPOINT_CONFIG_BYTES {
            return Err(DaemonEndpointConfigError::TooLarge);
        }
        let mut version = None;
        let mut kind = None;
        let mut name = None;
        for line in source.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let (key, value) = line
                .split_once('=')
                .ok_or(DaemonEndpointConfigError::InvalidLine)?;
            let key = key.trim();
            let value = value.trim();
            match key {
                "endpoint_version" => {
                    if version.replace(parse_version(value)?).is_some() {
                        return Err(DaemonEndpointConfigError::DuplicateSetting);
                    }
                }
                "endpoint_kind" => {
                    if kind.replace(parse_string(value)?).is_some() {
                        return Err(DaemonEndpointConfigError::DuplicateSetting);
                    }
                }
                "endpoint_name" => {
                    if name.replace(parse_string(value)?).is_some() {
                        return Err(DaemonEndpointConfigError::DuplicateSetting);
                    }
                }
                _ => return Err(DaemonEndpointConfigError::UnknownSetting),
            }
        }
        let version = version.ok_or(DaemonEndpointConfigError::MissingVersion)?;
        if version != DAEMON_ENDPOINT_CONFIG_VERSION {
            return Err(DaemonEndpointConfigError::UnsupportedVersion(version));
        }
        let kind = kind.ok_or(DaemonEndpointConfigError::MissingKind)?;
        let name = name.ok_or(DaemonEndpointConfigError::MissingName)?;
        let endpoint = match kind.as_str() {
            "unix_socket" => DaemonEndpoint::UnixSocket(validate_name(name)?),
            "windows_named_pipe" => DaemonEndpoint::WindowsNamedPipe(validate_name(name)?),
            _ => return Err(DaemonEndpointConfigError::UnsupportedKind),
        };
        Ok(Self { endpoint })
    }

    #[must_use]
    pub fn unix_socket_default() -> Self {
        Self {
            endpoint: DaemonEndpoint::UnixSocket(DEFAULT_DAEMON_UNIX_SOCKET_NAME.to_owned()),
        }
    }

    #[must_use]
    pub fn windows_named_pipe_default() -> Self {
        Self {
            endpoint: DaemonEndpoint::WindowsNamedPipe(
                DEFAULT_DAEMON_WINDOWS_NAMED_PIPE_NAME.to_owned(),
            ),
        }
    }

    #[must_use]
    pub const fn endpoint(&self) -> &DaemonEndpoint {
        &self.endpoint
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonEndpointConfigError {
    #[error("daemon endpoint configuration could not be read")]
    Read(#[source] std::io::Error),
    #[error("daemon endpoint configuration exceeds the configured limit")]
    TooLarge,
    #[error("daemon endpoint configuration is not valid UTF-8")]
    InvalidUtf8,
    #[error("daemon endpoint configuration has an invalid line")]
    InvalidLine,
    #[error("daemon endpoint configuration has an invalid string")]
    InvalidString,
    #[error("daemon endpoint configuration has an invalid version")]
    InvalidVersion,
    #[error("daemon endpoint configuration repeats a setting")]
    DuplicateSetting,
    #[error("daemon endpoint configuration has an unknown setting")]
    UnknownSetting,
    #[error("daemon endpoint configuration omits its version")]
    MissingVersion,
    #[error("daemon endpoint configuration version is unsupported")]
    UnsupportedVersion(u8),
    #[error("daemon endpoint configuration omits its kind")]
    MissingKind,
    #[error("daemon endpoint configuration omits its name")]
    MissingName,
    #[error("daemon endpoint configuration kind is unsupported")]
    UnsupportedKind,
    #[error("daemon endpoint configuration name is invalid")]
    InvalidName,
}

fn parse_version(value: &str) -> Result<u8, DaemonEndpointConfigError> {
    if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(DaemonEndpointConfigError::InvalidVersion);
    }
    value
        .parse()
        .map_err(|_| DaemonEndpointConfigError::InvalidVersion)
}

fn parse_string(value: &str) -> Result<String, DaemonEndpointConfigError> {
    let value = value
        .strip_prefix('"')
        .and_then(|value| value.strip_suffix('"'))
        .ok_or(DaemonEndpointConfigError::InvalidString)?;
    if value.contains('"') || value.contains('\\') {
        return Err(DaemonEndpointConfigError::InvalidString);
    }
    Ok(value.to_owned())
}

fn validate_name(name: String) -> Result<String, DaemonEndpointConfigError> {
    if name.is_empty()
        || name.len() > MAX_DAEMON_ENDPOINT_NAME_BYTES
        || matches!(name.as_str(), "." | "..")
        || !name
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.'))
    {
        return Err(DaemonEndpointConfigError::InvalidName);
    }
    Ok(name)
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use super::{
        DAEMON_ENDPOINT_CONFIG_VERSION, DEFAULT_DAEMON_UNIX_SOCKET_NAME,
        DEFAULT_DAEMON_WINDOWS_NAMED_PIPE_NAME, DaemonEndpoint, DaemonEndpointConfig,
        DaemonEndpointConfigError, MAX_DAEMON_ENDPOINT_CONFIG_BYTES,
        MAX_DAEMON_ENDPOINT_NAME_BYTES,
    };

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn config_path() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir().join(format!(
            "yeokcham-daemon-endpoint-{}-{number}.conf",
            std::process::id()
        ))
    }

    #[test]
    fn parses_versioned_unix_and_windows_local_endpoints() {
        let unix = DaemonEndpointConfig::parse(
            "endpoint_version = 1\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"daemon.sock\"\n",
        )
        .unwrap();
        assert_eq!(
            unix.endpoint(),
            &DaemonEndpoint::UnixSocket("daemon.sock".to_owned())
        );
        let pipe = DaemonEndpointConfig::parse(
            "endpoint_version = 1\nendpoint_kind = \"windows_named_pipe\"\nendpoint_name = \"yeokcham-daemon\"\n",
        )
        .unwrap();
        assert_eq!(
            pipe.endpoint(),
            &DaemonEndpoint::WindowsNamedPipe("yeokcham-daemon".to_owned())
        );
        assert_eq!(DAEMON_ENDPOINT_CONFIG_VERSION, 1);
        assert_eq!(
            DaemonEndpointConfig::unix_socket_default().endpoint(),
            &DaemonEndpoint::UnixSocket(DEFAULT_DAEMON_UNIX_SOCKET_NAME.to_owned())
        );
        assert_eq!(
            DaemonEndpointConfig::windows_named_pipe_default().endpoint(),
            &DaemonEndpoint::WindowsNamedPipe(DEFAULT_DAEMON_WINDOWS_NAMED_PIPE_NAME.to_owned())
        );
    }

    #[test]
    fn rejects_unbounded_or_ambiguous_endpoint_configuration() {
        for source in [
            "endpoint_version = 2\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"daemon.sock\"\n",
            "endpoint_version = 1\nendpoint_kind = \"unix_socket\"\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"daemon.sock\"\n",
            "endpoint_version = 1\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"../daemon.sock\"\n",
            "endpoint_version = 1\nendpoint_kind = \"invalid\"\nendpoint_name = \"daemon.sock\"\n",
        ] {
            assert!(DaemonEndpointConfig::parse(source).is_err());
        }
        let oversized_name = "a".repeat(MAX_DAEMON_ENDPOINT_NAME_BYTES + 1);
        assert!(DaemonEndpointConfig::parse(&format!(
            "endpoint_version = 1\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"{oversized_name}\"\n"
        ))
        .is_err());
        assert!(matches!(
            DaemonEndpointConfig::parse(&"x".repeat(MAX_DAEMON_ENDPOINT_CONFIG_BYTES + 1)),
            Err(DaemonEndpointConfigError::TooLarge)
        ));
    }

    #[test]
    fn loads_a_bounded_versioned_endpoint_file() {
        let path = config_path();
        fs::write(
            &path,
            "endpoint_version = 1\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"daemon.sock\"\n",
        )
        .unwrap();
        assert_eq!(
            DaemonEndpointConfig::load(&path).unwrap().endpoint(),
            &DaemonEndpoint::UnixSocket("daemon.sock".to_owned())
        );
        fs::remove_file(path).unwrap();
    }
}
