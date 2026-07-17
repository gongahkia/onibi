use std::sync::{
    Arc, Mutex,
    atomic::{AtomicBool, Ordering},
};

use tonic::{Request, Response, Status};
use yeokcham_core::OsKeystore;
use yeokcham_daemon_api::v1::{
    CreateIdentityRequest, CreateOrLoadIdentityRequest, GetIdentityRequest, GetStatusRequest,
    GetStatusResponse, IdentityInitialization, IdentityResponse, StartClientRequest,
    StartClientResponse, daemon_service_server::DaemonService,
};
use yeokcham_protocol::ProtocolVersion;

#[cfg(target_os = "linux")]
use yeokcham_core::LinuxKeystore;
#[cfg(target_os = "macos")]
use yeokcham_core::MacOsKeystore;
#[cfg(target_os = "windows")]
use yeokcham_core::WindowsKeystore;

use crate::{ClientIdentity, ClientIdentityError, ClientIdentityInitialization};

#[derive(Clone)]
pub struct DaemonGrpcService {
    version: ProtocolVersion,
    running: Arc<AtomicBool>,
    identity: Arc<dyn DaemonIdentityOperations>,
}

impl DaemonGrpcService {
    #[must_use]
    pub fn new(version: ProtocolVersion) -> Self {
        Self {
            version,
            running: Arc::new(AtomicBool::new(true)),
            identity: Arc::new(UnavailableIdentityOperations),
        }
    }

    #[must_use]
    pub fn with_identity_keystore<K>(version: ProtocolVersion, keystore: K) -> Self
    where
        K: OsKeystore + Send + 'static,
    {
        Self {
            version,
            running: Arc::new(AtomicBool::new(true)),
            identity: Arc::new(KeystoreIdentityOperations {
                keystore: Mutex::new(keystore),
            }),
        }
    }

    pub fn with_system_keystore(
        version: ProtocolVersion,
    ) -> Result<Self, DaemonGrpcServiceConfigurationError> {
        #[cfg(target_os = "linux")]
        {
            return LinuxKeystore::new()
                .map(|keystore| Self::with_identity_keystore(version, keystore))
                .map_err(|_| DaemonGrpcServiceConfigurationError::SystemKeystoreUnavailable);
        }
        #[cfg(target_os = "macos")]
        {
            return Ok(Self::with_identity_keystore(version, MacOsKeystore::new()));
        }
        #[cfg(target_os = "windows")]
        {
            return WindowsKeystore::new()
                .map(|keystore| Self::with_identity_keystore(version, keystore))
                .map_err(|_| DaemonGrpcServiceConfigurationError::SystemKeystoreUnavailable);
        }
        #[allow(unreachable_code)]
        Err(DaemonGrpcServiceConfigurationError::UnsupportedPlatform)
    }

    pub fn shutdown(&self) {
        self.running.store(false, Ordering::Release);
    }

    fn status_response(&self) -> (u32, u32, bool) {
        (
            u32::from(self.version.get()),
            0,
            self.running.load(Ordering::Acquire),
        )
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonGrpcServiceConfigurationError {
    #[error("daemon system keystore is unavailable")]
    SystemKeystoreUnavailable,
    #[error("daemon system keystore is unsupported on this platform")]
    UnsupportedPlatform,
}

trait DaemonIdentityOperations: Send + Sync {
    fn create(&self) -> Result<IdentityResponse, Status>;
    fn load(&self) -> Result<IdentityResponse, Status>;
    fn create_or_load(&self) -> Result<IdentityResponse, Status>;
}

struct UnavailableIdentityOperations;

impl DaemonIdentityOperations for UnavailableIdentityOperations {
    fn create(&self) -> Result<IdentityResponse, Status> {
        Err(Status::unavailable(
            "daemon identity service is unavailable",
        ))
    }

    fn load(&self) -> Result<IdentityResponse, Status> {
        Err(Status::unavailable(
            "daemon identity service is unavailable",
        ))
    }

    fn create_or_load(&self) -> Result<IdentityResponse, Status> {
        Err(Status::unavailable(
            "daemon identity service is unavailable",
        ))
    }
}

struct KeystoreIdentityOperations<K> {
    keystore: Mutex<K>,
}

impl<K> DaemonIdentityOperations for KeystoreIdentityOperations<K>
where
    K: OsKeystore + Send,
{
    fn create(&self) -> Result<IdentityResponse, Status> {
        let mut keystore = self.keystore.lock().map_err(|_| identity_failure())?;
        ClientIdentity::create(&mut *keystore)
            .map(|identity| {
                identity_response(
                    identity.public_key().as_bytes(),
                    IdentityInitialization::Created,
                )
            })
            .map_err(|error| map_identity_error(&error))
    }

    fn load(&self) -> Result<IdentityResponse, Status> {
        let keystore = self.keystore.lock().map_err(|_| identity_failure())?;
        ClientIdentity::load(&*keystore)
            .map(|identity| {
                identity_response(
                    identity.public_key().as_bytes(),
                    IdentityInitialization::Loaded,
                )
            })
            .map_err(|error| map_identity_error(&error))
    }

    fn create_or_load(&self) -> Result<IdentityResponse, Status> {
        let mut keystore = self.keystore.lock().map_err(|_| identity_failure())?;
        ClientIdentity::create_or_load(&mut *keystore)
            .map(|(identity, initialization)| {
                let initialization = match initialization {
                    ClientIdentityInitialization::Created => IdentityInitialization::Created,
                    ClientIdentityInitialization::Loaded => IdentityInitialization::Loaded,
                };
                identity_response(identity.public_key().as_bytes(), initialization)
            })
            .map_err(|error| map_identity_error(&error))
    }
}

fn identity_response(
    public_key: &[u8],
    initialization: IdentityInitialization,
) -> IdentityResponse {
    IdentityResponse {
        public_key: public_key.to_vec(),
        initialization: initialization.into(),
    }
}

fn identity_failure() -> Status {
    Status::internal("daemon identity operation failed")
}

fn map_identity_error(error: &ClientIdentityError) -> Status {
    match error {
        ClientIdentityError::AlreadyInitialized => Status::already_exists("daemon identity exists"),
        ClientIdentityError::NotInitialized => Status::not_found("daemon identity does not exist"),
        ClientIdentityError::InvalidKeyEntry
        | ClientIdentityError::Generation(_)
        | ClientIdentityError::InvalidStoredIdentity(_)
        | ClientIdentityError::KeystoreSecret(_)
        | ClientIdentityError::Keystore => identity_failure(),
    }
}

#[tonic::async_trait]
impl DaemonService for DaemonGrpcService {
    async fn start_client(
        &self,
        _request: Request<StartClientRequest>,
    ) -> Result<Response<StartClientResponse>, Status> {
        let (api_major, api_minor, running) = self.status_response();
        Ok(Response::new(StartClientResponse {
            api_major,
            api_minor,
            running,
        }))
    }

    async fn create_identity(
        &self,
        _request: Request<CreateIdentityRequest>,
    ) -> Result<Response<IdentityResponse>, Status> {
        self.identity.create().map(Response::new)
    }

    async fn get_identity(
        &self,
        _request: Request<GetIdentityRequest>,
    ) -> Result<Response<IdentityResponse>, Status> {
        self.identity.load().map(Response::new)
    }

    async fn create_or_load_identity(
        &self,
        _request: Request<CreateOrLoadIdentityRequest>,
    ) -> Result<Response<IdentityResponse>, Status> {
        self.identity.create_or_load().map(Response::new)
    }

    async fn get_status(
        &self,
        _request: Request<GetStatusRequest>,
    ) -> Result<Response<GetStatusResponse>, Status> {
        let (api_major, api_minor, running) = self.status_response();
        Ok(Response::new(GetStatusResponse {
            api_major,
            api_minor,
            running,
        }))
    }
}

#[cfg(test)]
mod tests {
    use tonic::{Code, Request};
    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_daemon_api::v1::{
        GetIdentityRequest, GetStatusRequest, StartClientRequest,
        daemon_service_server::DaemonService,
    };
    use yeokcham_protocol::ProtocolVersion;

    use super::DaemonGrpcService;

    #[derive(Debug, thiserror::Error)]
    #[error("sensitive keystore failure")]
    struct FailingKeystoreError;

    struct FailingKeystore;

    impl OsKeystore for FailingKeystore {
        type Error = FailingKeystoreError;

        fn load(&self, _entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Err(FailingKeystoreError)
        }

        fn store(
            &mut self,
            _entry: &KeystoreEntryName,
            _secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            Err(FailingKeystoreError)
        }

        fn delete(&mut self, _entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            Err(FailingKeystoreError)
        }
    }

    #[tokio::test]
    async fn status_rpc_reports_protocol_version_and_lifecycle() {
        let service = DaemonGrpcService::new(ProtocolVersion::INITIAL);
        let running = service
            .get_status(Request::new(GetStatusRequest {}))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(running.api_major, u32::from(ProtocolVersion::INITIAL.get()));
        assert_eq!(running.api_minor, 0);
        assert!(running.running);
        service.shutdown();
        let stopped = service
            .get_status(Request::new(GetStatusRequest {}))
            .await
            .unwrap()
            .into_inner();
        assert!(!stopped.running);
    }

    #[tokio::test]
    async fn client_start_rpc_is_idempotent_and_reports_lifecycle() {
        let service = DaemonGrpcService::new(ProtocolVersion::INITIAL);
        for _ in 0..2 {
            let response = service
                .start_client(Request::new(StartClientRequest {}))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(
                response.api_major,
                u32::from(ProtocolVersion::INITIAL.get())
            );
            assert_eq!(response.api_minor, 0);
            assert!(response.running);
        }
        service.shutdown();
        assert!(
            !service
                .start_client(Request::new(StartClientRequest {}))
                .await
                .unwrap()
                .into_inner()
                .running
        );
    }

    #[tokio::test]
    async fn identity_rpc_redacts_keystore_failures() {
        let service =
            DaemonGrpcService::with_identity_keystore(ProtocolVersion::INITIAL, FailingKeystore);
        let error = service
            .get_identity(Request::new(GetIdentityRequest {}))
            .await
            .unwrap_err();
        assert_eq!(error.code(), Code::Internal);
        assert_eq!(error.message(), "daemon identity operation failed");
    }
}
