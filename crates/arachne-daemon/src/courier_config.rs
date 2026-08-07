use std::{
    fs,
    net::SocketAddr,
    path::{Path, PathBuf},
    time::Duration,
};

use crate::{ClientStateDirectory, ExternalTorRuntimeConfig, ExternalTorRuntimeConfigError};

pub const COURIER_DAEMON_CONFIG_SCHEMA_VERSION: u8 = 1;
pub const MAX_COURIER_DAEMON_CONFIG_BYTES: usize = 16 * 1024;
pub const DEFAULT_COURIER_POLL_INTERVAL: Duration = Duration::from_secs(5);
pub const MIN_COURIER_POLL_INTERVAL: Duration = Duration::from_secs(1);
pub const MAX_COURIER_POLL_INTERVAL: Duration = Duration::from_secs(60);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CourierDaemonConfig {
    state_directory: PathBuf,
    tor: ExternalTorRuntimeConfig,
    poll_interval: Duration,
}

impl CourierDaemonConfig {
    pub fn load(path: &Path) -> Result<Self, CourierDaemonConfigError> {
        let source = fs::read(path).map_err(CourierDaemonConfigError::Read)?;
        if source.len() > MAX_COURIER_DAEMON_CONFIG_BYTES {
            return Err(CourierDaemonConfigError::TooLarge);
        }
        let source =
            std::str::from_utf8(&source).map_err(|_| CourierDaemonConfigError::InvalidUtf8)?;
        Self::parse(source)
    }

    pub fn parse(source: &str) -> Result<Self, CourierDaemonConfigError> {
        if source.len() > MAX_COURIER_DAEMON_CONFIG_BYTES {
            return Err(CourierDaemonConfigError::TooLarge);
        }
        let mut version = None;
        let mut state_directory = None;
        let mut socks_proxy = None;
        let mut poll_interval_seconds = None;
        for line in source.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let (key, value) = line
                .split_once('=')
                .ok_or(CourierDaemonConfigError::InvalidLine)?;
            match key.trim() {
                "config_version" => set_once(&mut version, parse_u8(value.trim())?)?,
                "state_directory" => set_once(&mut state_directory, parse_string(value.trim())?)?,
                "socks_proxy" => set_once(&mut socks_proxy, parse_string(value.trim())?)?,
                "poll_interval_seconds" => {
                    set_once(&mut poll_interval_seconds, parse_u64(value.trim())?)?
                }
                _ => return Err(CourierDaemonConfigError::UnknownSetting),
            }
        }
        if version.ok_or(CourierDaemonConfigError::MissingSchemaVersion)?
            != COURIER_DAEMON_CONFIG_SCHEMA_VERSION
        {
            return Err(CourierDaemonConfigError::UnsupportedSchemaVersion);
        }
        let state_directory =
            PathBuf::from(state_directory.ok_or(CourierDaemonConfigError::MissingStateDirectory)?);
        ClientStateDirectory::new(&state_directory)
            .map_err(|_| CourierDaemonConfigError::InvalidStateDirectory)?;
        let socks_proxy = socks_proxy
            .ok_or(CourierDaemonConfigError::MissingSocksProxy)?
            .parse::<SocketAddr>()
            .map_err(|_| CourierDaemonConfigError::InvalidSocksProxy)?;
        let poll_interval = Duration::from_secs(
            poll_interval_seconds.ok_or(CourierDaemonConfigError::MissingPollInterval)?,
        );
        if !(MIN_COURIER_POLL_INTERVAL..=MAX_COURIER_POLL_INTERVAL).contains(&poll_interval) {
            return Err(CourierDaemonConfigError::InvalidPollInterval);
        }
        let tor = ExternalTorRuntimeConfig::new(socks_proxy, poll_interval)
            .map_err(CourierDaemonConfigError::Tor)?;
        Ok(Self {
            state_directory,
            tor,
            poll_interval,
        })
    }

    #[must_use]
    pub fn state_directory(&self) -> &Path {
        &self.state_directory
    }

    #[must_use]
    pub const fn tor(&self) -> ExternalTorRuntimeConfig {
        self.tor
    }

    #[must_use]
    pub const fn poll_interval(&self) -> Duration {
        self.poll_interval
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierDaemonConfigError {
    #[error("courier daemon configuration could not be read")]
    Read(#[source] std::io::Error),
    #[error("courier daemon configuration exceeds the configured limit")]
    TooLarge,
    #[error("courier daemon configuration is not valid UTF-8")]
    InvalidUtf8,
    #[error("courier daemon configuration has an invalid line")]
    InvalidLine,
    #[error("courier daemon configuration has a duplicate setting")]
    DuplicateSetting,
    #[error("courier daemon configuration has an unknown setting")]
    UnknownSetting,
    #[error("courier daemon configuration has an invalid string")]
    InvalidString,
    #[error("courier daemon configuration has an invalid numeric value")]
    InvalidNumber,
    #[error("courier daemon configuration omits its schema version")]
    MissingSchemaVersion,
    #[error("courier daemon configuration schema version is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier daemon configuration omits its state directory")]
    MissingStateDirectory,
    #[error("courier daemon state directory is invalid")]
    InvalidStateDirectory,
    #[error("courier daemon configuration omits its Tor SOCKS proxy")]
    MissingSocksProxy,
    #[error("courier daemon Tor SOCKS proxy is invalid")]
    InvalidSocksProxy,
    #[error("courier daemon configuration omits its poll interval")]
    MissingPollInterval,
    #[error("courier daemon poll interval is outside the supported bounds")]
    InvalidPollInterval,
    #[error("courier daemon Tor configuration is invalid")]
    Tor(#[source] ExternalTorRuntimeConfigError),
}

fn set_once<T>(slot: &mut Option<T>, value: T) -> Result<(), CourierDaemonConfigError> {
    if slot.replace(value).is_some() {
        return Err(CourierDaemonConfigError::DuplicateSetting);
    }
    Ok(())
}

fn parse_u8(value: &str) -> Result<u8, CourierDaemonConfigError> {
    if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(CourierDaemonConfigError::InvalidNumber);
    }
    value
        .parse()
        .map_err(|_| CourierDaemonConfigError::InvalidNumber)
}

fn parse_u64(value: &str) -> Result<u64, CourierDaemonConfigError> {
    if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(CourierDaemonConfigError::InvalidNumber);
    }
    value
        .parse()
        .map_err(|_| CourierDaemonConfigError::InvalidNumber)
}

fn parse_string(value: &str) -> Result<String, CourierDaemonConfigError> {
    let value = value
        .strip_prefix('"')
        .and_then(|value| value.strip_suffix('"'))
        .ok_or(CourierDaemonConfigError::InvalidString)?;
    if value.contains(['\0', '"']) {
        return Err(CourierDaemonConfigError::InvalidString);
    }
    Ok(value.to_owned())
}

#[cfg(test)]
mod tests {
    use super::{CourierDaemonConfig, CourierDaemonConfigError};

    #[test]
    fn parses_a_loopback_only_courier_daemon_configuration() {
        let config = CourierDaemonConfig::parse(
            "config_version = 1\nstate_directory = \"/var/lib/arachne/alice\"\nsocks_proxy = \"127.0.0.1:9050\"\npoll_interval_seconds = 5\n",
        )
        .unwrap();
        assert_eq!(
            config.tor().socks_proxy(),
            "127.0.0.1:9050".parse().unwrap()
        );
        assert_eq!(config.poll_interval().as_secs(), 5);
    }

    #[test]
    fn rejects_unbounded_or_unsafe_courier_daemon_configuration() {
        assert!(matches!(
            CourierDaemonConfig::parse(
                "config_version = 1\nstate_directory = \"relative\"\nsocks_proxy = \"127.0.0.1:9050\"\npoll_interval_seconds = 5\n"
            ),
            Err(CourierDaemonConfigError::InvalidStateDirectory)
        ));
        assert!(matches!(
            CourierDaemonConfig::parse(
                "config_version = 1\nstate_directory = \"/var/lib/arachne/alice\"\nsocks_proxy = \"192.0.2.1:9050\"\npoll_interval_seconds = 5\n"
            ),
            Err(CourierDaemonConfigError::Tor(_))
        ));
        assert!(matches!(
            CourierDaemonConfig::parse(
                "config_version = 1\nstate_directory = \"/var/lib/arachne/alice\"\nsocks_proxy = \"127.0.0.1:9050\"\npoll_interval_seconds = 61\n"
            ),
            Err(CourierDaemonConfigError::InvalidPollInterval)
        ));
    }
}
