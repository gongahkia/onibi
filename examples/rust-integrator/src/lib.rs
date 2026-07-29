use std::{ffi::OsString, path::PathBuf};

use arachne_sdk::{SdkClientBuilder, SdkConfig, SdkConfigError};

pub const DEFAULT_EVENT_BUFFER_CAPACITY: usize = 64;
pub const STATE_DIRECTORY_ENV: &str = "ARACHNE_STATE_DIRECTORY";

pub fn config_from_environment() -> Result<SdkConfig, IntegratorExampleError> {
    let state_directory = state_directory_from(std::env::var_os(STATE_DIRECTORY_ENV))?;
    build_embedded_config(state_directory).map_err(IntegratorExampleError::from)
}

pub fn build_embedded_config(state_directory: PathBuf) -> Result<SdkConfig, SdkConfigError> {
    SdkClientBuilder::new(state_directory)
        .embedded()
        .event_buffer_capacity(DEFAULT_EVENT_BUFFER_CAPACITY)
        .build()
}

fn state_directory_from(value: Option<OsString>) -> Result<PathBuf, IntegratorExampleError> {
    value
        .map(PathBuf::from)
        .ok_or(IntegratorExampleError::MissingStateDirectory)
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IntegratorExampleError {
    #[error("ARACHNE_STATE_DIRECTORY must be set to an absolute state directory")]
    MissingStateDirectory,
    #[error("SDK configuration is invalid")]
    Configuration(#[from] SdkConfigError),
}

#[cfg(test)]
mod tests {
    use std::{
        ffi::OsString,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use super::{
        DEFAULT_EVENT_BUFFER_CAPACITY, IntegratorExampleError, build_embedded_config,
        state_directory_from,
    };
    use arachne_sdk::{RuntimeMode, SdkClient, SdkConfigError};

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        std::env::temp_dir().join(format!(
            "arachne-rust-integrator-example-{}-{}",
            std::process::id(),
            NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed)
        ))
    }

    #[test]
    fn builds_a_bounded_embedded_public_sdk_configuration() {
        let state_directory = state_directory();
        let config = build_embedded_config(state_directory.clone()).unwrap();
        assert_eq!(config.state_directory(), state_directory);
        assert_eq!(config.runtime_mode(), &RuntimeMode::Embedded);
        assert_eq!(
            config.event_buffer_capacity(),
            DEFAULT_EVENT_BUFFER_CAPACITY
        );
    }

    #[test]
    fn rejects_missing_or_relative_state_directories() {
        assert_eq!(
            state_directory_from(None),
            Err(IntegratorExampleError::MissingStateDirectory)
        );
        assert_eq!(
            build_embedded_config(PathBuf::from("relative-state")),
            Err(SdkConfigError::InvalidStateDirectory)
        );
    }

    #[test]
    fn starts_and_stops_the_public_sdk_with_example_configuration() {
        let state_directory = state_directory();
        let config = build_embedded_config(state_directory.clone()).unwrap();
        let mut client = SdkClient::start(&config).unwrap();
        assert!(client.is_running());
        client.shutdown().unwrap();
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn accepts_an_explicit_state_directory_value() {
        let state_directory = PathBuf::from("/var/lib/arachne/example");
        assert_eq!(
            state_directory_from(Some(OsString::from(&state_directory))),
            Ok(state_directory)
        );
    }
}
