use std::{
    fs::{self, File, OpenOptions},
    io,
    path::{Path, PathBuf},
};

use fs2::FileExt;
use yeokcham_protocol::ProtocolVersion;

use crate::{Daemon, DaemonConfig, DaemonConfigError};

pub const DAEMON_LOCK_FILE: &str = "yeokcham-daemon.lock";

pub struct DaemonRuntime {
    daemon: Daemon,
    lock_path: PathBuf,
    lock: Option<File>,
}

impl DaemonRuntime {
    pub fn start_from_config(
        version: ProtocolVersion,
        config: &DaemonConfig,
    ) -> Result<Self, DaemonLifecycleError> {
        config
            .validate_for_startup()
            .map_err(DaemonLifecycleError::InvalidConfiguration)?;
        Self::start(version, config.state_directory())
    }

    pub fn start(
        version: ProtocolVersion,
        state_directory: impl AsRef<Path>,
    ) -> Result<Self, DaemonLifecycleError> {
        let daemon = Daemon::new(version)?;
        let state_directory = state_directory.as_ref();
        if state_directory.as_os_str().is_empty() {
            return Err(DaemonLifecycleError::EmptyStateDirectory);
        }
        fs::create_dir_all(state_directory).map_err(DaemonLifecycleError::StateDirectory)?;
        let lock_path = state_directory.join(DAEMON_LOCK_FILE);
        let lock = OpenOptions::new()
            .create(true)
            .read(true)
            .write(true)
            .open(&lock_path)
            .map_err(DaemonLifecycleError::LockOpen)?;
        match FileExt::try_lock_exclusive(&lock) {
            Ok(()) => Ok(Self {
                daemon,
                lock_path,
                lock: Some(lock),
            }),
            Err(error) if error.kind() == io::ErrorKind::WouldBlock => {
                Err(DaemonLifecycleError::AlreadyRunning)
            }
            Err(error) => Err(DaemonLifecycleError::Lock(error)),
        }
    }

    #[must_use]
    pub const fn daemon(&self) -> &Daemon {
        &self.daemon
    }

    #[must_use]
    pub fn lock_path(&self) -> &Path {
        &self.lock_path
    }

    #[must_use]
    pub const fn is_running(&self) -> bool {
        self.lock.is_some()
    }

    pub fn shutdown(&mut self) -> Result<(), DaemonLifecycleError> {
        let lock = self.lock.take().ok_or(DaemonLifecycleError::NotRunning)?;
        FileExt::unlock(&lock).map_err(DaemonLifecycleError::Lock)
    }
}

impl Drop for DaemonRuntime {
    fn drop(&mut self) {
        if let Some(lock) = &self.lock {
            let _ = FileExt::unlock(lock);
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonLifecycleError {
    #[error("daemon configuration is invalid for startup")]
    InvalidConfiguration(#[source] DaemonConfigError),
    #[error("daemon state directory must not be empty")]
    EmptyStateDirectory,
    #[error("daemon protocol version is unsupported")]
    Daemon(#[from] yeokcham_core::Error),
    #[error("daemon state directory operation failed")]
    StateDirectory(#[source] io::Error),
    #[error("daemon lock file could not be opened")]
    LockOpen(#[source] io::Error),
    #[error("another daemon already holds the state lock")]
    AlreadyRunning,
    #[error("daemon lock operation failed")]
    Lock(#[source] io::Error),
    #[error("daemon is not running")]
    NotRunning,
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        path::{Path, PathBuf},
        sync::atomic::{AtomicU64, Ordering},
    };

    use super::{DAEMON_LOCK_FILE, DaemonLifecycleError, DaemonRuntime};
    use crate::{DaemonConfig, DaemonConfigError};
    use yeokcham_protocol::ProtocolVersion;

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir().join(format!(
            "yeokcham-daemon-lifecycle-{}-{number}",
            std::process::id()
        ))
    }

    fn config_for(state_directory: &Path) -> DaemonConfig {
        let state_directory = state_directory
            .to_str()
            .expect("temporary state directory must be valid UTF-8")
            .replace('\\', "\\\\")
            .replace('"', "\\\"");
        DaemonConfig::parse(&format!(
            "config_version = 1\nstate_directory = \"{state_directory}\"\n"
        ))
        .unwrap()
    }

    #[test]
    fn startup_shutdown_and_drop_manage_the_exclusive_lock() {
        let state_directory = state_directory();
        let config = config_for(&state_directory);
        let mut first =
            DaemonRuntime::start_from_config(ProtocolVersion::INITIAL, &config).unwrap();
        assert_eq!(first.daemon().protocol_version(), ProtocolVersion::INITIAL);
        assert_eq!(first.lock_path(), state_directory.join(DAEMON_LOCK_FILE));
        assert!(first.is_running());
        assert!(matches!(
            DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory),
            Err(DaemonLifecycleError::AlreadyRunning)
        ));
        first.shutdown().unwrap();
        assert!(!first.is_running());
        assert!(matches!(
            first.shutdown(),
            Err(DaemonLifecycleError::NotRunning)
        ));
        {
            let restarted =
                DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
            assert!(restarted.is_running());
        }
        let after_drop = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        drop(after_drop);
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn rejects_an_empty_state_directory() {
        assert!(matches!(
            DaemonRuntime::start(ProtocolVersion::INITIAL, ""),
            Err(DaemonLifecycleError::EmptyStateDirectory)
        ));
    }

    #[test]
    fn startup_from_config_validates_before_creating_state() {
        let state_directory = format!(
            "yeokcham-daemon-relative-state-{}",
            NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed)
        );
        let config = DaemonConfig::parse(&format!(
            "config_version = 1\nstate_directory = \"{state_directory}\"\n"
        ))
        .unwrap();
        assert!(matches!(
            DaemonRuntime::start_from_config(ProtocolVersion::INITIAL, &config),
            Err(DaemonLifecycleError::InvalidConfiguration(
                DaemonConfigError::InvalidStateDirectory
            ))
        ));
        assert!(!PathBuf::from(state_directory).exists());
    }
}
