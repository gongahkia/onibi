use std::{
    fs, io,
    os::unix::{
        fs::{FileTypeExt, MetadataExt, PermissionsExt},
        net::UnixStream as StdUnixStream,
    },
    path::{Path, PathBuf},
    pin::Pin,
    task::{Context, Poll},
};

use tokio::net::{UnixListener, UnixStream};
use tokio::runtime::Handle;
use tokio_stream::{Stream, wrappers::UnixListenerStream};

use crate::{DaemonEndpoint, DaemonEndpointConfig, DaemonRuntime};

pub use crate::DEFAULT_DAEMON_UNIX_SOCKET_NAME as DAEMON_UNIX_SOCKET_FILE;
const DAEMON_UNIX_SOCKET_MODE: u32 = 0o600;

#[derive(Clone, Copy, Eq, PartialEq)]
struct SocketPathIdentity {
    device: u64,
    inode: u64,
}

pub struct DaemonUnixListener<'runtime> {
    runtime: &'runtime DaemonRuntime,
    listener: Option<UnixListener>,
    socket_path: PathBuf,
    socket_identity: SocketPathIdentity,
    cleanup: bool,
}

pub struct DaemonUnixIncoming<'runtime> {
    _runtime: &'runtime DaemonRuntime,
    incoming: UnixListenerStream,
    socket_path: PathBuf,
    socket_identity: SocketPathIdentity,
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonUnixListenerError {
    #[error("daemon must be running before its Unix listener can bind")]
    NotRunning,
    #[error("daemon Unix listener requires an active Tokio runtime")]
    RuntimeUnavailable,
    #[error("daemon endpoint is not a Unix socket")]
    UnsupportedEndpoint,
    #[error("daemon Unix socket path is already active")]
    AlreadyListening,
    #[error("daemon Unix socket path is not a socket")]
    SocketPathNotSocket,
    #[error("daemon Unix socket path was replaced")]
    SocketPathReplaced,
    #[error("daemon Unix socket operation failed")]
    Socket(#[source] io::Error),
    #[error("daemon Unix listener is closed")]
    Closed,
}

impl<'runtime> DaemonUnixListener<'runtime> {
    pub fn bind(runtime: &'runtime DaemonRuntime) -> Result<Self, DaemonUnixListenerError> {
        let endpoint = DaemonEndpointConfig::unix_socket_default();
        Self::bind_configured(runtime, &endpoint)
    }

    pub fn bind_configured(
        runtime: &'runtime DaemonRuntime,
        endpoint: &DaemonEndpointConfig,
    ) -> Result<Self, DaemonUnixListenerError> {
        if !runtime.is_running() {
            return Err(DaemonUnixListenerError::NotRunning);
        }
        if Handle::try_current().is_err() {
            return Err(DaemonUnixListenerError::RuntimeUnavailable);
        }
        let DaemonEndpoint::UnixSocket(socket_name) = endpoint.endpoint() else {
            return Err(DaemonUnixListenerError::UnsupportedEndpoint);
        };
        let socket_path = runtime.state_directory().root().join(socket_name);
        prepare_socket_path(&socket_path)?;
        let listener = UnixListener::bind(&socket_path).map_err(DaemonUnixListenerError::Socket)?;
        let socket_identity = match socket_path_identity(&socket_path) {
            Ok(socket_identity) => socket_identity,
            Err(error) => {
                drop(listener);
                return Err(error);
            }
        };
        if let Err(error) = fs::set_permissions(
            &socket_path,
            fs::Permissions::from_mode(DAEMON_UNIX_SOCKET_MODE),
        ) {
            drop(listener);
            let _ = remove_socket_path_if_matches(&socket_path, socket_identity);
            return Err(DaemonUnixListenerError::Socket(error));
        }
        Ok(Self {
            runtime,
            listener: Some(listener),
            socket_path,
            socket_identity,
            cleanup: true,
        })
    }

    #[must_use]
    pub fn socket_path(&self) -> &Path {
        &self.socket_path
    }

    pub async fn accept(&self) -> Result<UnixStream, DaemonUnixListenerError> {
        let listener = self
            .listener
            .as_ref()
            .ok_or(DaemonUnixListenerError::Closed)?;
        listener
            .accept()
            .await
            .map(|(stream, _)| stream)
            .map_err(DaemonUnixListenerError::Socket)
    }

    pub(crate) fn into_incoming(
        mut self,
    ) -> Result<DaemonUnixIncoming<'runtime>, DaemonUnixListenerError> {
        let listener = self
            .listener
            .take()
            .ok_or(DaemonUnixListenerError::Closed)?;
        self.cleanup = false;
        Ok(DaemonUnixIncoming {
            _runtime: self.runtime,
            incoming: UnixListenerStream::new(listener),
            socket_path: self.socket_path.clone(),
            socket_identity: self.socket_identity,
        })
    }

    pub fn shutdown(mut self) -> Result<(), DaemonUnixListenerError> {
        self.listener.take();
        self.cleanup = false;
        remove_socket_path_if_matches(&self.socket_path, self.socket_identity)
    }
}

impl Drop for DaemonUnixListener<'_> {
    fn drop(&mut self) {
        self.listener.take();
        if self.cleanup {
            let _ = remove_socket_path_if_matches(&self.socket_path, self.socket_identity);
        }
    }
}

impl Stream for DaemonUnixIncoming<'_> {
    type Item = Result<UnixStream, io::Error>;

    fn poll_next(self: Pin<&mut Self>, context: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        Pin::new(&mut self.get_mut().incoming).poll_next(context)
    }
}

impl Drop for DaemonUnixIncoming<'_> {
    fn drop(&mut self) {
        let _ = remove_socket_path_if_matches(&self.socket_path, self.socket_identity);
    }
}

fn prepare_socket_path(socket_path: &Path) -> Result<(), DaemonUnixListenerError> {
    let metadata = match fs::symlink_metadata(socket_path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(error) => return Err(DaemonUnixListenerError::Socket(error)),
    };
    if !metadata.file_type().is_socket() {
        return Err(DaemonUnixListenerError::SocketPathNotSocket);
    }
    match StdUnixStream::connect(socket_path) {
        Ok(_) => Err(DaemonUnixListenerError::AlreadyListening),
        Err(error) if error.kind() == io::ErrorKind::ConnectionRefused => {
            remove_socket_path(socket_path)
        }
        Err(error) => Err(DaemonUnixListenerError::Socket(error)),
    }
}

fn remove_socket_path(socket_path: &Path) -> Result<(), DaemonUnixListenerError> {
    let metadata = match fs::symlink_metadata(socket_path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(error) => return Err(DaemonUnixListenerError::Socket(error)),
    };
    if !metadata.file_type().is_socket() {
        return Err(DaemonUnixListenerError::SocketPathNotSocket);
    }
    match fs::remove_file(socket_path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(DaemonUnixListenerError::Socket(error)),
    }
}

fn socket_path_identity(socket_path: &Path) -> Result<SocketPathIdentity, DaemonUnixListenerError> {
    let metadata = fs::symlink_metadata(socket_path).map_err(DaemonUnixListenerError::Socket)?;
    if !metadata.file_type().is_socket() {
        return Err(DaemonUnixListenerError::SocketPathNotSocket);
    }
    Ok(SocketPathIdentity {
        device: metadata.dev(),
        inode: metadata.ino(),
    })
}

fn remove_socket_path_if_matches(
    socket_path: &Path,
    expected: SocketPathIdentity,
) -> Result<(), DaemonUnixListenerError> {
    if socket_path_identity(socket_path)? != expected {
        return Err(DaemonUnixListenerError::SocketPathReplaced);
    }
    match fs::remove_file(socket_path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(DaemonUnixListenerError::Socket(error)),
    }
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        os::unix::{fs::PermissionsExt, net::UnixListener as StdUnixListener},
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use tokio::net::UnixStream;

    use super::{DAEMON_UNIX_SOCKET_FILE, DaemonUnixListener, DaemonUnixListenerError};
    use crate::DaemonEndpointConfig;
    use crate::DaemonRuntime;
    use yeokcham_protocol::ProtocolVersion;

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        PathBuf::from("/tmp").join(format!("ycuds-{}-{number}", std::process::id()))
    }

    #[tokio::test]
    async fn binds_accepts_and_removes_a_mode_restricted_unix_socket() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let endpoint = DaemonEndpointConfig::parse(
            "endpoint_version = 1\nendpoint_kind = \"unix_socket\"\nendpoint_name = \"daemon.sock\"\n",
        )
        .unwrap();
        let listener = DaemonUnixListener::bind_configured(&runtime, &endpoint).unwrap();
        assert_eq!(listener.socket_path(), state_directory.join("daemon.sock"));
        assert_eq!(
            fs::metadata(listener.socket_path())
                .unwrap()
                .permissions()
                .mode()
                & 0o777,
            0o600
        );
        let pipe = DaemonEndpointConfig::parse(
            "endpoint_version = 1\nendpoint_kind = \"windows_named_pipe\"\nendpoint_name = \"yeokcham-daemon\"\n",
        )
        .unwrap();
        assert!(matches!(
            DaemonUnixListener::bind_configured(&runtime, &pipe),
            Err(DaemonUnixListenerError::UnsupportedEndpoint)
        ));

        let connect = UnixStream::connect(listener.socket_path());
        let accept = listener.accept();
        let (connected, accepted) = tokio::join!(connect, accept);
        drop(connected.unwrap());
        drop(accepted.unwrap());

        let socket_path = listener.socket_path().to_path_buf();
        listener.shutdown().unwrap();
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[tokio::test]
    async fn rejects_active_and_non_socket_paths_and_reclaims_only_stale_sockets() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let listener = DaemonUnixListener::bind(&runtime).unwrap();
        assert!(matches!(
            DaemonUnixListener::bind(&runtime),
            Err(DaemonUnixListenerError::AlreadyListening)
        ));
        listener.shutdown().unwrap();

        let socket_path = state_directory.join(DAEMON_UNIX_SOCKET_FILE);
        StdUnixListener::bind(&socket_path).unwrap();
        let listener = DaemonUnixListener::bind(&runtime).unwrap();
        listener.shutdown().unwrap();

        let listener = DaemonUnixListener::bind(&runtime).unwrap();
        fs::remove_file(listener.socket_path()).unwrap();
        fs::write(listener.socket_path(), b"replacement").unwrap();
        assert!(matches!(
            listener.shutdown(),
            Err(DaemonUnixListenerError::SocketPathNotSocket)
        ));
        assert_eq!(fs::read(&socket_path).unwrap(), b"replacement");
        fs::remove_file(&socket_path).unwrap();

        let listener = DaemonUnixListener::bind(&runtime).unwrap();
        fs::remove_file(listener.socket_path()).unwrap();
        let replacement_listener = StdUnixListener::bind(listener.socket_path()).unwrap();
        assert!(matches!(
            listener.shutdown(),
            Err(DaemonUnixListenerError::SocketPathReplaced)
        ));
        assert!(socket_path.exists());
        drop(replacement_listener);
        fs::remove_file(&socket_path).unwrap();

        fs::write(&socket_path, b"not a socket").unwrap();
        assert!(matches!(
            DaemonUnixListener::bind(&runtime),
            Err(DaemonUnixListenerError::SocketPathNotSocket)
        ));
        fs::remove_file(socket_path).unwrap();
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn refuses_to_bind_after_daemon_shutdown() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        runtime.shutdown().unwrap();
        assert!(matches!(
            DaemonUnixListener::bind(&runtime),
            Err(DaemonUnixListenerError::NotRunning)
        ));
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn fails_closed_without_a_tokio_runtime() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        assert!(matches!(
            DaemonUnixListener::bind(&runtime),
            Err(DaemonUnixListenerError::RuntimeUnavailable)
        ));
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }
}
