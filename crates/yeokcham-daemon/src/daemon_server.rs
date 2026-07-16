use std::{future::Future, path::Path};

use tonic::transport::Server;
use yeokcham_daemon_api::v1::daemon_service_server::DaemonServiceServer;
use yeokcham_protocol::ProtocolVersion;

use crate::{
    DaemonEndpointConfig, DaemonGrpcService, DaemonLocalAuth, DaemonRuntime, DaemonUnixListener,
    DaemonUnixListenerError,
};

pub struct DaemonServer<'runtime> {
    listener: DaemonUnixListener<'runtime>,
    auth: DaemonLocalAuth,
    protocol_version: ProtocolVersion,
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonServerError {
    #[error("daemon server listener could not start")]
    Listener(#[from] DaemonUnixListenerError),
    #[error("daemon server stopped with a transport error")]
    Transport(#[source] tonic::transport::Error),
}

impl<'runtime> DaemonServer<'runtime> {
    pub fn bind(
        runtime: &'runtime DaemonRuntime,
        auth: DaemonLocalAuth,
    ) -> Result<Self, DaemonServerError> {
        let listener = DaemonUnixListener::bind(runtime)?;
        Ok(Self {
            listener,
            auth,
            protocol_version: runtime.daemon().protocol_version(),
        })
    }

    pub fn bind_configured(
        runtime: &'runtime DaemonRuntime,
        endpoint: &DaemonEndpointConfig,
        auth: DaemonLocalAuth,
    ) -> Result<Self, DaemonServerError> {
        let listener = DaemonUnixListener::bind_configured(runtime, endpoint)?;
        Ok(Self {
            listener,
            auth,
            protocol_version: runtime.daemon().protocol_version(),
        })
    }

    #[must_use]
    pub fn socket_path(&self) -> &Path {
        self.listener.socket_path()
    }

    pub async fn serve_until<F>(self, shutdown: F) -> Result<(), DaemonServerError>
    where
        F: Future<Output = ()>,
    {
        let incoming = self.listener.into_incoming()?;
        let service = DaemonGrpcService::new(self.protocol_version);
        let service = DaemonServiceServer::with_interceptor(service, self.auth.interceptor());
        Server::builder()
            .add_service(service)
            .serve_with_incoming_shutdown(incoming, shutdown)
            .await
            .map_err(DaemonServerError::Transport)
    }
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use hyper_util::rt::TokioIo;
    use tokio::{net::UnixStream, sync::oneshot};
    use tonic::{Code, Request, transport::Endpoint};
    use tower::service_fn;
    use yeokcham_daemon_api::v1::{StartClientRequest, daemon_service_client::DaemonServiceClient};
    use yeokcham_protocol::ProtocolVersion;

    use super::DaemonServer;
    use crate::{DaemonLocalAuth, DaemonRuntime, LOCAL_AUTH_TOKEN_METADATA_KEY};

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        PathBuf::from("/tmp").join(format!("ycsrv-{}-{number}", std::process::id()))
    }

    #[tokio::test]
    async fn serves_authenticated_status_and_shuts_down_cleanly() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let server = DaemonServer::bind(&runtime, auth).unwrap();
        let socket_path = server.socket_path().to_path_buf();
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();

        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let connector_path = socket_path.clone();
            let channel = Endpoint::from_static("http://[::]:50051")
                .connect_with_connector(service_fn(move |_| {
                    let connector_path = connector_path.clone();
                    async move { UnixStream::connect(connector_path).await.map(TokioIo::new) }
                }))
                .await
                .unwrap();
            let mut client = DaemonServiceClient::new(channel);
            assert_eq!(
                client
                    .start_client(Request::new(StartClientRequest {}))
                    .await
                    .unwrap_err()
                    .code(),
                Code::Unauthenticated
            );
            let mut request = Request::new(StartClientRequest {});
            request
                .metadata_mut()
                .insert_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, token.metadata_value());
            let status = client.start_client(request).await.unwrap().into_inner();
            shutdown_sender.send(()).unwrap();
            status
        };
        let (server, status) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert_eq!(status.api_major, u32::from(ProtocolVersion::INITIAL.get()));
        assert_eq!(status.api_minor, 0);
        assert!(status.running);
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }
}
