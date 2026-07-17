use std::{
    path::PathBuf,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
};

use tonic::{Request, Response, Status};
use yeokcham_core::{ED25519_PUBLIC_KEY_BYTES, IdentityPublicKey, OsKeystore};
use yeokcham_daemon_api::v1::{
    ApplyContactIdentityRotationRequest, ContactResponse, ContactStatus as RpcContactStatus,
    ContactVerificationMethod as RpcContactVerificationMethod, CreateIdentityRequest,
    CreateOrLoadIdentityRequest, GetContactRequest, GetContactResponse, GetIdentityRequest,
    GetStatusRequest, GetStatusResponse, IdentityInitialization, IdentityResponse,
    ImportContactInvitationRequest, ListContactsRequest, ListContactsResponse,
    RevokeContactRequest, StartClientRequest, StartClientResponse,
    daemon_service_server::DaemonService,
};
use yeokcham_protocol::{CONTACT_INVITATION_BYTES, IDENTITY_ROTATION_BYTES, ProtocolVersion};

#[cfg(target_os = "linux")]
use yeokcham_core::LinuxKeystore;
#[cfg(target_os = "macos")]
use yeokcham_core::MacOsKeystore;
#[cfg(target_os = "windows")]
use yeokcham_core::WindowsKeystore;

use crate::{
    ClientIdentity, ClientIdentityError, ClientIdentityInitialization, Contact,
    ContactLifecycleError, ContactLifecycleService, ContactStatus as StoredContactStatus,
    ContactStore, ContactStoreError, ContactVerificationMethod as StoredContactVerificationMethod,
    PendingContactImportError, PendingContactImportService,
};

pub const MAX_DAEMON_CONTACTS_RESPONSE: usize = 65_536;

#[derive(Clone)]
pub struct DaemonGrpcService {
    version: ProtocolVersion,
    running: Arc<AtomicBool>,
    identity: Arc<dyn DaemonIdentityOperations>,
    contacts: Arc<dyn DaemonContactOperations>,
}

impl DaemonGrpcService {
    #[must_use]
    pub fn new(version: ProtocolVersion) -> Self {
        Self {
            version,
            running: Arc::new(AtomicBool::new(true)),
            identity: Arc::new(UnavailableIdentityOperations),
            contacts: Arc::new(UnavailableContactOperations),
        }
    }

    #[must_use]
    pub fn with_identity_keystore<K>(version: ProtocolVersion, keystore: K) -> Self
    where
        K: OsKeystore + Send + 'static,
    {
        let keystore = Arc::new(Mutex::new(keystore));
        Self {
            version,
            running: Arc::new(AtomicBool::new(true)),
            identity: Arc::new(KeystoreIdentityOperations { keystore }),
            contacts: Arc::new(UnavailableContactOperations),
        }
    }

    #[must_use]
    pub fn with_state_keystore<K>(
        version: ProtocolVersion,
        contacts_path: PathBuf,
        keystore: K,
    ) -> Self
    where
        K: OsKeystore + Send + 'static,
    {
        let keystore = Arc::new(Mutex::new(keystore));
        Self {
            version,
            running: Arc::new(AtomicBool::new(true)),
            identity: Arc::new(KeystoreIdentityOperations {
                keystore: Arc::clone(&keystore),
            }),
            contacts: Arc::new(KeystoreContactOperations {
                keystore,
                contacts_path,
                operation_lock: Mutex::new(()),
            }),
        }
    }

    pub fn with_system_keystore(
        version: ProtocolVersion,
        contacts_path: PathBuf,
    ) -> Result<Self, DaemonGrpcServiceConfigurationError> {
        #[cfg(target_os = "linux")]
        {
            return LinuxKeystore::new()
                .map(|keystore| Self::with_state_keystore(version, contacts_path, keystore))
                .map_err(|_| DaemonGrpcServiceConfigurationError::SystemKeystoreUnavailable);
        }
        #[cfg(target_os = "macos")]
        {
            return Ok(Self::with_state_keystore(
                version,
                contacts_path,
                MacOsKeystore::new(),
            ));
        }
        #[cfg(target_os = "windows")]
        {
            return WindowsKeystore::new()
                .map(|keystore| Self::with_state_keystore(version, contacts_path, keystore))
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
    keystore: Arc<Mutex<K>>,
}

trait DaemonContactOperations: Send + Sync {
    fn list(&self) -> Result<Vec<ContactResponse>, Status>;
    fn get(&self, encoded_identity: &[u8]) -> Result<ContactResponse, Status>;
    fn import(&self, encoded_invitation: &[u8]) -> Result<ContactResponse, Status>;
    fn apply_rotation(&self, encoded_rotation: &[u8]) -> Result<ContactResponse, Status>;
    fn revoke(&self, encoded_identity: &[u8]) -> Result<ContactResponse, Status>;
}

struct UnavailableContactOperations;

impl DaemonContactOperations for UnavailableContactOperations {
    fn list(&self) -> Result<Vec<ContactResponse>, Status> {
        Err(contact_unavailable())
    }

    fn get(&self, _encoded_identity: &[u8]) -> Result<ContactResponse, Status> {
        Err(contact_unavailable())
    }

    fn import(&self, _encoded_invitation: &[u8]) -> Result<ContactResponse, Status> {
        Err(contact_unavailable())
    }

    fn apply_rotation(&self, _encoded_rotation: &[u8]) -> Result<ContactResponse, Status> {
        Err(contact_unavailable())
    }

    fn revoke(&self, _encoded_identity: &[u8]) -> Result<ContactResponse, Status> {
        Err(contact_unavailable())
    }
}

struct KeystoreContactOperations<K> {
    keystore: Arc<Mutex<K>>,
    contacts_path: PathBuf,
    operation_lock: Mutex<()>,
}

impl<K> KeystoreContactOperations<K>
where
    K: OsKeystore + Send,
{
    fn with_contacts<T>(
        &self,
        operation: impl FnOnce(&IdentityPublicKey, &mut ContactStore) -> Result<T, Status>,
    ) -> Result<T, Status> {
        let operation_lock = self.operation_lock.lock().map_err(|_| contact_failure())?;
        let mut keystore = self.keystore.lock().map_err(|_| contact_failure())?;
        let local_identity = ClientIdentity::load(&*keystore)
            .map_err(|error| map_contact_identity_error(&error))?
            .public_key();
        let mut contacts = ContactStore::open(&self.contacts_path, &mut *keystore)
            .map_err(|_| contact_failure())?;
        drop(keystore);
        let result = operation(&local_identity, &mut contacts);
        drop(operation_lock);
        result
    }
}

impl<K> DaemonContactOperations for KeystoreContactOperations<K>
where
    K: OsKeystore + Send,
{
    fn list(&self) -> Result<Vec<ContactResponse>, Status> {
        self.with_contacts(|_, contacts| {
            if contacts.contacts().len() > MAX_DAEMON_CONTACTS_RESPONSE {
                return Err(Status::resource_exhausted(
                    "daemon contact response exceeds its limit",
                ));
            }
            Ok(contacts
                .contacts()
                .iter()
                .copied()
                .map(contact_response)
                .collect())
        })
    }

    fn get(&self, encoded_identity: &[u8]) -> Result<ContactResponse, Status> {
        let identity = decode_contact_identity(encoded_identity)?;
        self.with_contacts(|_, contacts| {
            contacts
                .contact(&identity)
                .map(contact_response)
                .ok_or_else(|| Status::not_found("contact does not exist"))
        })
    }

    fn import(&self, encoded_invitation: &[u8]) -> Result<ContactResponse, Status> {
        if encoded_invitation.len() != CONTACT_INVITATION_BYTES {
            return Err(Status::invalid_argument("contact invitation is invalid"));
        }
        self.with_contacts(|local_identity, contacts| {
            PendingContactImportService::new(local_identity, contacts)
                .import_encoded(encoded_invitation)
                .map(contact_response)
                .map_err(map_pending_contact_import_error)
        })
    }

    fn apply_rotation(&self, encoded_rotation: &[u8]) -> Result<ContactResponse, Status> {
        if encoded_rotation.len() > IDENTITY_ROTATION_BYTES {
            return Err(Status::invalid_argument(
                "contact identity rotation is invalid",
            ));
        }
        self.with_contacts(|local_identity, contacts| {
            ContactLifecycleService::new(local_identity, contacts)
                .apply_rotation_encoded(encoded_rotation)
                .map(contact_response)
                .map_err(map_contact_lifecycle_error)
        })
    }

    fn revoke(&self, encoded_identity: &[u8]) -> Result<ContactResponse, Status> {
        let identity = decode_contact_identity(encoded_identity)?;
        self.with_contacts(|local_identity, contacts| {
            ContactLifecycleService::new(local_identity, contacts)
                .revoke(&identity)
                .map(contact_response)
                .map_err(map_contact_lifecycle_error)
        })
    }
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

fn contact_unavailable() -> Status {
    Status::unavailable("daemon contact service is unavailable")
}

fn contact_failure() -> Status {
    Status::internal("daemon contact operation failed")
}

fn map_contact_identity_error(error: &ClientIdentityError) -> Status {
    match error {
        ClientIdentityError::NotInitialized => {
            Status::failed_precondition("daemon identity is not initialized")
        }
        ClientIdentityError::AlreadyInitialized
        | ClientIdentityError::InvalidKeyEntry
        | ClientIdentityError::Generation(_)
        | ClientIdentityError::InvalidStoredIdentity(_)
        | ClientIdentityError::KeystoreSecret(_)
        | ClientIdentityError::Keystore => contact_failure(),
    }
}

fn decode_contact_identity(encoded: &[u8]) -> Result<IdentityPublicKey, Status> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| Status::invalid_argument("contact identity is invalid"))?;
    IdentityPublicKey::from_bytes(bytes)
        .map_err(|_| Status::invalid_argument("contact identity is invalid"))
}

fn contact_response(contact: Contact) -> ContactResponse {
    let status = match contact.status() {
        StoredContactStatus::Pending => RpcContactStatus::Pending,
        StoredContactStatus::Verified => RpcContactStatus::Verified,
        StoredContactStatus::Revoked => RpcContactStatus::Revoked,
    };
    let verification_method = match contact.verification_method() {
        None => RpcContactVerificationMethod::Unspecified,
        Some(StoredContactVerificationMethod::Qr) => RpcContactVerificationMethod::Qr,
        Some(StoredContactVerificationMethod::SafetyNumber) => {
            RpcContactVerificationMethod::SafetyNumber
        }
    };
    ContactResponse {
        identity: contact.identity().as_bytes().to_vec(),
        status: status.into(),
        verification_method: verification_method.into(),
    }
}

fn map_pending_contact_import_error(error: PendingContactImportError) -> Status {
    match error {
        PendingContactImportError::Invitation(_) => {
            Status::invalid_argument("contact invitation is invalid")
        }
        PendingContactImportError::ContactStore(error) => map_contact_store_error(&error),
    }
}

fn map_contact_lifecycle_error(error: ContactLifecycleError) -> Status {
    match error {
        ContactLifecycleError::IdentityRotation(_) => {
            Status::invalid_argument("contact identity rotation is invalid")
        }
        ContactLifecycleError::ContactStore(error) => map_contact_store_error(&error),
    }
}

fn map_contact_store_error(error: &ContactStoreError) -> Status {
    match error {
        ContactStoreError::SelfContact => Status::invalid_argument("contact identity is invalid"),
        ContactStoreError::UnknownContact => Status::not_found("contact does not exist"),
        ContactStoreError::NotPending | ContactStoreError::NotVerified => {
            Status::failed_precondition("contact lifecycle state is invalid")
        }
        ContactStoreError::ReplacementAlreadyKnown => Status::already_exists("contact exists"),
        ContactStoreError::AlreadyRevoked => {
            Status::failed_precondition("contact is already revoked")
        }
        ContactStoreError::QrDoesNotContainLocalIdentity
        | ContactStoreError::SafetyNumberMismatch
        | ContactStoreError::StateStore(_)
        | ContactStoreError::InvalidState(_)
        | ContactStoreError::InvalidDocument(_)
        | ContactStoreError::Encode
        | ContactStoreError::UnsupportedSchemaVersion(_)
        | ContactStoreError::InvalidShape
        | ContactStoreError::InvalidIdentity
        | ContactStoreError::InvalidStatus
        | ContactStoreError::DuplicateIdentity
        | ContactStoreError::TrailingBytes
        | ContactStoreError::NonCanonicalEncoding => contact_failure(),
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

    async fn list_contacts(
        &self,
        _request: Request<ListContactsRequest>,
    ) -> Result<Response<ListContactsResponse>, Status> {
        self.contacts
            .list()
            .map(|contacts| Response::new(ListContactsResponse { contacts }))
    }

    async fn get_contact(
        &self,
        request: Request<GetContactRequest>,
    ) -> Result<Response<GetContactResponse>, Status> {
        self.contacts
            .get(&request.into_inner().identity)
            .map(|contact| {
                Response::new(GetContactResponse {
                    contact: Some(contact),
                })
            })
    }

    async fn import_contact_invitation(
        &self,
        request: Request<ImportContactInvitationRequest>,
    ) -> Result<Response<ContactResponse>, Status> {
        self.contacts
            .import(&request.into_inner().invitation)
            .map(Response::new)
    }

    async fn apply_contact_identity_rotation(
        &self,
        request: Request<ApplyContactIdentityRotationRequest>,
    ) -> Result<Response<ContactResponse>, Status> {
        self.contacts
            .apply_rotation(&request.into_inner().rotation)
            .map(Response::new)
    }

    async fn revoke_contact(
        &self,
        request: Request<RevokeContactRequest>,
    ) -> Result<Response<ContactResponse>, Status> {
        self.contacts
            .revoke(&request.into_inner().identity)
            .map(Response::new)
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
    use std::path::PathBuf;

    use tonic::{Code, Request};
    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_daemon_api::v1::{
        GetIdentityRequest, GetStatusRequest, ListContactsRequest, StartClientRequest,
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

    #[tokio::test]
    async fn contact_rpc_redacts_keystore_failures() {
        let service = DaemonGrpcService::with_state_keystore(
            ProtocolVersion::INITIAL,
            PathBuf::from("/tmp/yeokcham-contact-redaction"),
            FailingKeystore,
        );
        let error = service
            .list_contacts(Request::new(ListContactsRequest {}))
            .await
            .unwrap_err();
        assert_eq!(error.code(), Code::Internal);
        assert_eq!(error.message(), "daemon contact operation failed");
    }
}
