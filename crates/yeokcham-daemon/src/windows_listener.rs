use std::{
    future::Future,
    io,
    pin::Pin,
    task::{Context, Poll},
};

use tokio::{
    net::windows::named_pipe::{NamedPipeServer, ServerOptions},
    runtime::Handle,
};
use tokio_stream::Stream;

use crate::{DaemonEndpoint, DaemonEndpointConfig, DaemonRuntime};

pub use crate::DEFAULT_DAEMON_WINDOWS_NAMED_PIPE_NAME as DAEMON_WINDOWS_NAMED_PIPE_NAME;

type PendingConnection =
    Pin<Box<dyn Future<Output = io::Result<(NamedPipeServer, NamedPipeServer)>> + Send>>;

pub struct DaemonWindowsListener<'runtime> {
    runtime: &'runtime DaemonRuntime,
    pipe_path: String,
    pipe: Option<NamedPipeServer>,
}

pub struct DaemonWindowsIncoming<'runtime> {
    _runtime: &'runtime DaemonRuntime,
    pipe_path: String,
    pending: Option<PendingConnection>,
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonWindowsListenerError {
    #[error("daemon must be running before its Windows listener can bind")]
    NotRunning,
    #[error("daemon Windows listener requires an active Tokio runtime")]
    RuntimeUnavailable,
    #[error("daemon endpoint is not a Windows named pipe")]
    UnsupportedEndpoint,
    #[error("daemon Windows named-pipe operation failed")]
    Pipe(#[source] io::Error),
    #[error("daemon Windows listener is closed")]
    Closed,
}

impl<'runtime> DaemonWindowsListener<'runtime> {
    pub fn bind(runtime: &'runtime DaemonRuntime) -> Result<Self, DaemonWindowsListenerError> {
        let endpoint = DaemonEndpointConfig::windows_named_pipe_default();
        Self::bind_configured(runtime, &endpoint)
    }

    pub fn bind_configured(
        runtime: &'runtime DaemonRuntime,
        endpoint: &DaemonEndpointConfig,
    ) -> Result<Self, DaemonWindowsListenerError> {
        if !runtime.is_running() {
            return Err(DaemonWindowsListenerError::NotRunning);
        }
        if Handle::try_current().is_err() {
            return Err(DaemonWindowsListenerError::RuntimeUnavailable);
        }
        let DaemonEndpoint::WindowsNamedPipe(pipe_name) = endpoint.endpoint() else {
            return Err(DaemonWindowsListenerError::UnsupportedEndpoint);
        };
        let pipe_path = pipe_path(pipe_name);
        let pipe = create_pipe(&pipe_path, true).map_err(DaemonWindowsListenerError::Pipe)?;
        Ok(Self {
            runtime,
            pipe_path,
            pipe: Some(pipe),
        })
    }

    #[must_use]
    pub fn pipe_path(&self) -> &str {
        &self.pipe_path
    }

    pub async fn accept(&mut self) -> Result<NamedPipeServer, DaemonWindowsListenerError> {
        let pipe = self.pipe.take().ok_or(DaemonWindowsListenerError::Closed)?;
        pipe.connect()
            .await
            .map_err(DaemonWindowsListenerError::Pipe)?;
        let next_pipe =
            create_pipe(&self.pipe_path, false).map_err(DaemonWindowsListenerError::Pipe)?;
        self.pipe = Some(next_pipe);
        Ok(pipe)
    }

    pub fn into_incoming(
        mut self,
    ) -> Result<DaemonWindowsIncoming<'runtime>, DaemonWindowsListenerError> {
        let pipe = self.pipe.take().ok_or(DaemonWindowsListenerError::Closed)?;
        let pipe_path = self.pipe_path.clone();
        Ok(DaemonWindowsIncoming {
            _runtime: self.runtime,
            pending: Some(connecting_pipe(pipe_path.clone(), pipe)),
            pipe_path,
        })
    }

    pub fn shutdown(mut self) -> Result<(), DaemonWindowsListenerError> {
        self.pipe.take();
        Ok(())
    }
}

impl Stream for DaemonWindowsIncoming<'_> {
    type Item = Result<NamedPipeServer, io::Error>;

    fn poll_next(self: Pin<&mut Self>, context: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        let this = self.get_mut();
        if this.pending.is_none() {
            let pipe = match create_pipe(&this.pipe_path, false) {
                Ok(pipe) => pipe,
                Err(error) => return Poll::Ready(Some(Err(error))),
            };
            this.pending = Some(connecting_pipe(this.pipe_path.clone(), pipe));
        }
        let result = this
            .pending
            .as_mut()
            .expect("pending Windows pipe connection must be initialized")
            .as_mut()
            .poll(context);
        match result {
            Poll::Pending => Poll::Pending,
            Poll::Ready(Ok((connected, next_pipe))) => {
                this.pending = Some(connecting_pipe(this.pipe_path.clone(), next_pipe));
                Poll::Ready(Some(Ok(connected)))
            }
            Poll::Ready(Err(error)) => {
                this.pending = None;
                Poll::Ready(Some(Err(error)))
            }
        }
    }
}

fn pipe_path(name: &str) -> String {
    format!(r"\\.\pipe\{name}")
}

fn create_pipe(path: &str, first_instance: bool) -> io::Result<NamedPipeServer> {
    let mut options = ServerOptions::new();
    options
        .first_pipe_instance(first_instance)
        .reject_remote_clients(true);
    options.create(path)
}

fn connecting_pipe(path: String, pipe: NamedPipeServer) -> PendingConnection {
    Box::pin(async move {
        pipe.connect().await?;
        let next_pipe = create_pipe(&path, false)?;
        Ok((pipe, next_pipe))
    })
}

#[cfg(test)]
mod tests {
    use std::{
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use tokio::net::windows::named_pipe::ClientOptions;

    use super::{DaemonWindowsListener, DaemonWindowsListenerError};
    use crate::{DaemonEndpointConfig, DaemonRuntime};
    use yeokcham_protocol::ProtocolVersion;

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir().join(format!("ycwpl-{}-{number}", std::process::id()))
    }

    fn endpoint(number: u64) -> DaemonEndpointConfig {
        DaemonEndpointConfig::parse(&format!(
            "endpoint_version = 1\nendpoint_kind = \"windows_named_pipe\"\nendpoint_name = \"yeokcham-{number}\"\n"
        ))
        .unwrap()
    }

    #[tokio::test]
    async fn binds_accepts_and_releases_a_local_pipe() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let mut listener = DaemonWindowsListener::bind_configured(&runtime, &endpoint(1)).unwrap();
        let client = ClientOptions::new().open(listener.pipe_path()).unwrap();
        drop(listener.accept().await.unwrap());
        drop(client);
        listener.shutdown().unwrap();
        assert!(DaemonWindowsListener::bind_configured(&runtime, &endpoint(1)).is_ok());
        runtime.shutdown().unwrap();
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[tokio::test]
    async fn rejects_wrong_endpoints_and_active_pipe_instances() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let listener = DaemonWindowsListener::bind_configured(&runtime, &endpoint(2)).unwrap();
        assert!(DaemonWindowsListener::bind_configured(&runtime, &endpoint(2)).is_err());
        let unix = DaemonEndpointConfig::unix_socket_default();
        assert!(matches!(
            DaemonWindowsListener::bind_configured(&runtime, &unix),
            Err(DaemonWindowsListenerError::UnsupportedEndpoint)
        ));
        listener.shutdown().unwrap();
        runtime.shutdown().unwrap();
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn fails_closed_without_runtime_or_after_shutdown() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        assert!(matches!(
            DaemonWindowsListener::bind(&runtime),
            Err(DaemonWindowsListenerError::RuntimeUnavailable)
        ));
        runtime.shutdown().unwrap();
        assert!(matches!(
            DaemonWindowsListener::bind(&runtime),
            Err(DaemonWindowsListenerError::NotRunning)
        ));
        std::fs::remove_dir_all(state_directory).unwrap();
    }
}
