use std::{future::Future, path::Path};

use tonic::transport::Server;
use yeokcham_core::OsKeystore;
use yeokcham_daemon_api::v1::daemon_service_server::DaemonServiceServer;

use crate::{
    DaemonEndpointConfig, DaemonGrpcService, DaemonGrpcServiceConfigurationError, DaemonLocalAuth,
    DaemonRuntime, DaemonUnixListener, DaemonUnixListenerError,
};

pub struct DaemonServer<'runtime> {
    listener: DaemonUnixListener<'runtime>,
    auth: DaemonLocalAuth,
    service: DaemonGrpcService,
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonServerError {
    #[error("daemon server listener could not start")]
    Listener(#[from] DaemonUnixListenerError),
    #[error("daemon server identity configuration failed")]
    IdentityConfiguration(#[from] DaemonGrpcServiceConfigurationError),
    #[error("daemon server stopped with a transport error")]
    Transport(#[source] tonic::transport::Error),
}

impl<'runtime> DaemonServer<'runtime> {
    pub fn bind(
        runtime: &'runtime DaemonRuntime,
        auth: DaemonLocalAuth,
    ) -> Result<Self, DaemonServerError> {
        let listener = DaemonUnixListener::bind(runtime)?;
        let service = DaemonGrpcService::with_system_keystore(runtime.daemon().protocol_version())?;
        Ok(Self::new(listener, auth, service))
    }

    pub fn bind_configured(
        runtime: &'runtime DaemonRuntime,
        endpoint: &DaemonEndpointConfig,
        auth: DaemonLocalAuth,
    ) -> Result<Self, DaemonServerError> {
        let listener = DaemonUnixListener::bind_configured(runtime, endpoint)?;
        let service = DaemonGrpcService::with_system_keystore(runtime.daemon().protocol_version())?;
        Ok(Self::new(listener, auth, service))
    }

    pub fn bind_with_identity_keystore<K>(
        runtime: &'runtime DaemonRuntime,
        auth: DaemonLocalAuth,
        keystore: K,
    ) -> Result<Self, DaemonServerError>
    where
        K: OsKeystore + Send + 'static,
    {
        let listener = DaemonUnixListener::bind(runtime)?;
        let service = DaemonGrpcService::with_identity_keystore(
            runtime.daemon().protocol_version(),
            keystore,
        );
        Ok(Self::new(listener, auth, service))
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
        let service = DaemonServiceServer::with_interceptor(self.service, self.auth.interceptor());
        Server::builder()
            .add_service(service)
            .serve_with_incoming_shutdown(incoming, shutdown)
            .await
            .map_err(DaemonServerError::Transport)
    }

    const fn new(
        listener: DaemonUnixListener<'runtime>,
        auth: DaemonLocalAuth,
        service: DaemonGrpcService,
    ) -> Self {
        Self {
            listener,
            auth,
            service,
        }
    }
}

#[cfg(test)]
mod tests {
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use hyper_util::rt::TokioIo;
    use tokio::{net::UnixStream, sync::oneshot};
    use tonic::{Code, Request, transport::Endpoint};
    use tower::service_fn;
    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_daemon_api::v1::{
        CreateIdentityRequest, CreateOrLoadIdentityRequest, GetIdentityRequest,
        IdentityInitialization, StartClientRequest, daemon_service_client::DaemonServiceClient,
    };
    use yeokcham_protocol::ProtocolVersion;

    use super::DaemonServer;
    use crate::{
        DaemonLocalAuth, DaemonLocalAuthToken, DaemonRuntime, LOCAL_AUTH_TOKEN_METADATA_KEY,
    };

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        PathBuf::from("/tmp").join(format!("ycsrv-{}-{number}", std::process::id()))
    }

    #[derive(Default)]
    struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

    impl OsKeystore for MemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .0
                .get(entry.as_str())
                .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
        }

        fn store(
            &mut self,
            entry: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.0
                .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
            Ok(())
        }

        fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.0.remove(entry.as_str());
            Ok(())
        }
    }

    fn authenticated_request<T>(value: T, token: &DaemonLocalAuthToken) -> Request<T> {
        let mut request = Request::new(value);
        request
            .metadata_mut()
            .insert_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, token.metadata_value());
        request
    }

    #[tokio::test]
    async fn serves_authenticated_client_start_and_shuts_down_cleanly() {
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
            let status = client
                .start_client(authenticated_request(StartClientRequest {}, &token))
                .await
                .unwrap()
                .into_inner();
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

    #[tokio::test]
    async fn serves_authenticated_identity_lifecycle_without_secret_material() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let server =
            DaemonServer::bind_with_identity_keystore(&runtime, auth, MemoryKeystore::default())
                .unwrap();
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
                    .get_identity(Request::new(GetIdentityRequest {}))
                    .await
                    .unwrap_err()
                    .code(),
                Code::Unauthenticated
            );
            assert_eq!(
                client
                    .get_identity(authenticated_request(GetIdentityRequest {}, &token))
                    .await
                    .unwrap_err()
                    .code(),
                Code::NotFound
            );
            let created = client
                .create_identity(authenticated_request(CreateIdentityRequest {}, &token))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(created.public_key.len(), 32);
            assert_eq!(
                IdentityInitialization::try_from(created.initialization),
                Ok(IdentityInitialization::Created)
            );
            assert_eq!(
                client
                    .create_identity(authenticated_request(CreateIdentityRequest {}, &token))
                    .await
                    .unwrap_err()
                    .code(),
                Code::AlreadyExists
            );
            let loaded = client
                .get_identity(authenticated_request(GetIdentityRequest {}, &token))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(loaded.public_key, created.public_key);
            assert_eq!(
                IdentityInitialization::try_from(loaded.initialization),
                Ok(IdentityInitialization::Loaded)
            );
            let loaded_again = client
                .create_or_load_identity(authenticated_request(
                    CreateOrLoadIdentityRequest {},
                    &token,
                ))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(loaded_again.public_key, created.public_key);
            assert_eq!(
                IdentityInitialization::try_from(loaded_again.initialization),
                Ok(IdentityInitialization::Loaded)
            );
            shutdown_sender.send(()).unwrap();
            created.public_key
        };
        let (server, public_key) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert_eq!(public_key.len(), 32);
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }
}
