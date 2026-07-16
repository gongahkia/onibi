use std::path::{Component, Path, PathBuf};

pub const MAX_LOCAL_DAEMON_ENDPOINT_BYTES: usize = 512;
pub const MAX_SDK_EVENT_BUFFER_CAPACITY: usize = 4096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RuntimeMode {
    Embedded,
    Daemon(LocalDaemonEndpoint),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LocalDaemonEndpoint(String);

impl LocalDaemonEndpoint {
    pub fn new(value: String) -> Result<Self, LocalDaemonEndpointError> {
        if value.is_empty() || value.len() > MAX_LOCAL_DAEMON_ENDPOINT_BYTES {
            return Err(LocalDaemonEndpointError::InvalidLength);
        }
        if value.contains('\0') || value.contains('\n') || value.contains('\r') {
            return Err(LocalDaemonEndpointError::InvalidValue);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SdkConfig {
    state_directory: PathBuf,
    runtime_mode: RuntimeMode,
    event_buffer_capacity: usize,
}

impl SdkConfig {
    pub fn new(
        state_directory: PathBuf,
        runtime_mode: RuntimeMode,
        event_buffer_capacity: usize,
    ) -> Result<Self, SdkConfigError> {
        if state_directory.as_os_str().is_empty()
            || !state_directory.is_absolute()
            || state_directory
                .components()
                .any(|component| matches!(component, Component::ParentDir))
        {
            return Err(SdkConfigError::InvalidStateDirectory);
        }
        if event_buffer_capacity == 0 || event_buffer_capacity > MAX_SDK_EVENT_BUFFER_CAPACITY {
            return Err(SdkConfigError::InvalidEventBufferCapacity);
        }
        Ok(Self {
            state_directory,
            runtime_mode,
            event_buffer_capacity,
        })
    }

    #[must_use]
    pub fn state_directory(&self) -> &Path {
        &self.state_directory
    }

    #[must_use]
    pub const fn runtime_mode(&self) -> &RuntimeMode {
        &self.runtime_mode
    }

    #[must_use]
    pub const fn event_buffer_capacity(&self) -> usize {
        self.event_buffer_capacity
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum LocalDaemonEndpointError {
    #[error("local daemon endpoint has an invalid length")]
    InvalidLength,
    #[error("local daemon endpoint is invalid")]
    InvalidValue,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkConfigError {
    #[error("SDK state directory is invalid")]
    InvalidStateDirectory,
    #[error("SDK event buffer capacity is invalid")]
    InvalidEventBufferCapacity,
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use super::{
        LocalDaemonEndpoint, LocalDaemonEndpointError, MAX_LOCAL_DAEMON_ENDPOINT_BYTES,
        MAX_SDK_EVENT_BUFFER_CAPACITY, RuntimeMode, SdkConfig, SdkConfigError,
    };

    #[test]
    fn accepts_bounded_embedded_and_daemon_configuration() {
        let embedded = SdkConfig::new(
            PathBuf::from("/var/lib/yeokcham/alice"),
            RuntimeMode::Embedded,
            64,
        )
        .unwrap();
        assert_eq!(
            embedded.state_directory(),
            PathBuf::from("/var/lib/yeokcham/alice")
        );
        assert_eq!(embedded.event_buffer_capacity(), 64);
        let endpoint = LocalDaemonEndpoint::new("/tmp/yeokcham.sock".to_owned()).unwrap();
        let daemon = SdkConfig::new(
            PathBuf::from("/var/lib/yeokcham/alice"),
            RuntimeMode::Daemon(endpoint.clone()),
            1,
        )
        .unwrap();
        assert_eq!(daemon.runtime_mode(), &RuntimeMode::Daemon(endpoint));
    }

    #[test]
    fn rejects_unsafe_or_unbounded_public_configuration() {
        for path in ["", "state", "../state", "/state/../other"] {
            assert_eq!(
                SdkConfig::new(PathBuf::from(path), RuntimeMode::Embedded, 1),
                Err(SdkConfigError::InvalidStateDirectory)
            );
        }
        assert_eq!(
            SdkConfig::new(PathBuf::from("/state"), RuntimeMode::Embedded, 0),
            Err(SdkConfigError::InvalidEventBufferCapacity)
        );
        assert_eq!(
            SdkConfig::new(
                PathBuf::from("/state"),
                RuntimeMode::Embedded,
                MAX_SDK_EVENT_BUFFER_CAPACITY + 1
            ),
            Err(SdkConfigError::InvalidEventBufferCapacity)
        );
        assert_eq!(
            LocalDaemonEndpoint::new(String::new()),
            Err(LocalDaemonEndpointError::InvalidLength)
        );
        assert_eq!(
            LocalDaemonEndpoint::new("x".repeat(MAX_LOCAL_DAEMON_ENDPOINT_BYTES + 1)),
            Err(LocalDaemonEndpointError::InvalidLength)
        );
        assert_eq!(
            LocalDaemonEndpoint::new("endpoint\n".to_owned()),
            Err(LocalDaemonEndpointError::InvalidValue)
        );
    }
}
