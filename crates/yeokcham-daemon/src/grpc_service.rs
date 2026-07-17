use std::{
    path::PathBuf,
    pin::Pin,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, AtomicU64, Ordering},
    },
};

use tokio::sync::{Notify, broadcast};
use tokio_stream::{Stream, StreamExt, wrappers::BroadcastStream};
use tonic::{Request, Response, Status};
use yeokcham_core::{ED25519_PUBLIC_KEY_BYTES, IdentityPublicKey, OsKeystore};
use yeokcham_daemon_api::v1::{
    ApplyContactIdentityRotationRequest, ContactResponse, ContactStatus as RpcContactStatus,
    ContactVerificationMethod as RpcContactVerificationMethod, CreateIdentityRequest,
    CreateOrLoadIdentityRequest, DaemonEvent as RpcDaemonEvent,
    DaemonEventKind as RpcDaemonEventKind, DeliveryProfileKind as RpcDeliveryProfileKind,
    DeliveryProfileResponse, DeliveryStatus as RpcDeliveryStatus, ExportIdentityRecoveryRequest,
    ExportIdentityRecoveryResponse, GetContactRequest, GetContactResponse,
    GetDeliveryStatusRequest, GetDeliveryStatusResponse, GetIdentityRequest, GetStatusRequest,
    GetStatusResponse, IdentityInitialization, IdentityResponse, ImportContactInvitationRequest,
    ImportIdentityRecoveryRequest, ListContactsRequest, ListContactsResponse,
    LocalMeshTransportKind as RpcLocalMeshTransportKind, RevokeContactRequest,
    SelectDeliveryProfileRequest, SendMessageRequest, SendMessageResponse, ShutdownDaemonRequest,
    ShutdownDaemonResponse, StartClientRequest, StartClientResponse, SubscribeEventsRequest,
    VerifyContactQrRequest, VerifyContactSafetyNumberRequest, daemon_service_server::DaemonService,
};
use yeokcham_protocol::{
    CONTACT_INVITATION_BYTES, DeliveryProfile, DeliveryProfileConstraints, DirectProfileSelection,
    EncryptedMessageEnvelope, IDENTITY_EXPORT_BYTES, IDENTITY_ROTATION_BYTES,
    IdentityExportPassphrase, LocalMeshProfileConfig, LocalMeshProfileConstraints,
    LocalMeshProfileSelection, LocalMeshTransportKind, MAX_MESSAGE_PAYLOAD_BYTES,
    MESSAGE_IDENTIFIER_BYTES, MessageIdentifier, ProtocolVersion, QR_VERIFICATION_PAYLOAD_BYTES,
    SAFETY_NUMBER_FINGERPRINT_BYTES,
};

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
    DeliveryState as StoredDeliveryState, MessageExpiry, MessageExpiryError,
    PendingContactImportError, PendingContactImportService, QrContactVerificationError,
    QrContactVerificationService, SafetyNumberVerificationError, SafetyNumberVerificationService,
    SenderOutbox, SenderOutboxError,
};

pub const MAX_DAEMON_CONTACTS_RESPONSE: usize = 65_536;
pub const MAX_DAEMON_EVENT_BACKLOG: usize = 256;
pub const MAX_DAEMON_MESSAGE_ENVELOPE_BYTES: usize = MAX_MESSAGE_PAYLOAD_BYTES;
pub const DAEMON_EVENT_VERSION: u32 = 1;

#[derive(Clone, Copy)]
enum DaemonEvent {
    ClientStarted,
    ClientStopped,
    MessageQueued([u8; MESSAGE_IDENTIFIER_BYTES]),
}

#[derive(Clone, Copy)]
struct DaemonEventEnvelope {
    sequence: u64,
    event: DaemonEvent,
}

#[derive(Clone)]
pub struct DaemonGrpcService {
    version: ProtocolVersion,
    client_started: Arc<AtomicBool>,
    events: broadcast::Sender<DaemonEventEnvelope>,
    next_event_sequence: Arc<AtomicU64>,
    running: Arc<AtomicBool>,
    shutdown_signal: Arc<Notify>,
    identity: Arc<dyn DaemonIdentityOperations>,
    contacts: Arc<dyn DaemonContactOperations>,
    outbox: Arc<dyn DaemonOutboxOperations>,
}

impl DaemonGrpcService {
    #[must_use]
    pub fn new(version: ProtocolVersion) -> Self {
        let (events, _) = broadcast::channel(MAX_DAEMON_EVENT_BACKLOG);
        Self {
            version,
            client_started: Arc::new(AtomicBool::new(false)),
            events,
            next_event_sequence: Arc::new(AtomicU64::new(1)),
            running: Arc::new(AtomicBool::new(true)),
            shutdown_signal: Arc::new(Notify::new()),
            identity: Arc::new(UnavailableIdentityOperations),
            contacts: Arc::new(UnavailableContactOperations),
            outbox: Arc::new(UnavailableOutboxOperations),
        }
    }

    #[must_use]
    pub fn with_identity_keystore<K>(version: ProtocolVersion, keystore: K) -> Self
    where
        K: OsKeystore + Send + 'static,
    {
        let keystore = Arc::new(Mutex::new(keystore));
        let (events, _) = broadcast::channel(MAX_DAEMON_EVENT_BACKLOG);
        Self {
            version,
            client_started: Arc::new(AtomicBool::new(false)),
            events,
            next_event_sequence: Arc::new(AtomicU64::new(1)),
            running: Arc::new(AtomicBool::new(true)),
            shutdown_signal: Arc::new(Notify::new()),
            identity: Arc::new(KeystoreIdentityOperations { keystore }),
            contacts: Arc::new(UnavailableContactOperations),
            outbox: Arc::new(UnavailableOutboxOperations),
        }
    }

    #[must_use]
    pub fn with_state_keystore<K>(
        version: ProtocolVersion,
        contacts_path: PathBuf,
        outbox_path: PathBuf,
        keystore: K,
    ) -> Self
    where
        K: OsKeystore + Send + 'static,
    {
        let keystore = Arc::new(Mutex::new(keystore));
        let (events, _) = broadcast::channel(MAX_DAEMON_EVENT_BACKLOG);
        Self {
            version,
            client_started: Arc::new(AtomicBool::new(false)),
            events,
            next_event_sequence: Arc::new(AtomicU64::new(1)),
            running: Arc::new(AtomicBool::new(true)),
            shutdown_signal: Arc::new(Notify::new()),
            identity: Arc::new(KeystoreIdentityOperations {
                keystore: Arc::clone(&keystore),
            }),
            contacts: Arc::new(KeystoreContactOperations {
                keystore: Arc::clone(&keystore),
                contacts_path,
                operation_lock: Mutex::new(()),
            }),
            outbox: Arc::new(KeystoreOutboxOperations {
                keystore,
                outbox_path,
                operation_lock: Mutex::new(()),
            }),
        }
    }

    pub fn with_system_keystore(
        version: ProtocolVersion,
        contacts_path: PathBuf,
        outbox_path: PathBuf,
    ) -> Result<Self, DaemonGrpcServiceConfigurationError> {
        #[cfg(target_os = "linux")]
        {
            return LinuxKeystore::new()
                .map(|keystore| {
                    Self::with_state_keystore(version, contacts_path, outbox_path, keystore)
                })
                .map_err(|_| DaemonGrpcServiceConfigurationError::SystemKeystoreUnavailable);
        }
        #[cfg(target_os = "macos")]
        {
            return Ok(Self::with_state_keystore(
                version,
                contacts_path,
                outbox_path,
                MacOsKeystore::new(),
            ));
        }
        #[cfg(target_os = "windows")]
        {
            return WindowsKeystore::new()
                .map(|keystore| {
                    Self::with_state_keystore(version, contacts_path, outbox_path, keystore)
                })
                .map_err(|_| DaemonGrpcServiceConfigurationError::SystemKeystoreUnavailable);
        }
        #[allow(unreachable_code)]
        Err(DaemonGrpcServiceConfigurationError::UnsupportedPlatform)
    }

    #[must_use]
    pub fn shutdown(&self) -> bool {
        if self.running.swap(false, Ordering::AcqRel) {
            self.shutdown_signal.notify_waiters();
            true
        } else {
            false
        }
    }

    pub async fn wait_for_shutdown(&self) {
        while self.running.load(Ordering::Acquire) {
            let notified = self.shutdown_signal.notified();
            if !self.running.load(Ordering::Acquire) {
                return;
            }
            notified.await;
        }
    }

    fn status_response(&self) -> (u32, u32, bool) {
        (
            u32::from(self.version.get()),
            0,
            self.running.load(Ordering::Acquire),
        )
    }

    fn emit(&self, event: DaemonEvent) -> Result<(), Status> {
        let sequence = self
            .next_event_sequence
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |sequence| {
                sequence.checked_add(1)
            })
            .map_err(|_| Status::resource_exhausted("daemon event sequence is exhausted"))?;
        let _ = self.events.send(DaemonEventEnvelope { sequence, event });
        Ok(())
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
    fn export_recovery(
        &self,
        passphrase: &IdentityExportPassphrase,
    ) -> Result<ExportIdentityRecoveryResponse, Status>;
    fn import_recovery(
        &self,
        archive: &[u8],
        passphrase: &IdentityExportPassphrase,
    ) -> Result<IdentityResponse, Status>;
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

    fn export_recovery(
        &self,
        _passphrase: &IdentityExportPassphrase,
    ) -> Result<ExportIdentityRecoveryResponse, Status> {
        Err(Status::unavailable(
            "daemon identity service is unavailable",
        ))
    }

    fn import_recovery(
        &self,
        _archive: &[u8],
        _passphrase: &IdentityExportPassphrase,
    ) -> Result<IdentityResponse, Status> {
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
    fn verify_qr(&self, encoded_payload: &[u8]) -> Result<ContactResponse, Status>;
    fn verify_safety_number(
        &self,
        encoded_identity: &[u8],
        encoded_fingerprint: &[u8],
    ) -> Result<ContactResponse, Status>;
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

    fn verify_qr(&self, _encoded_payload: &[u8]) -> Result<ContactResponse, Status> {
        Err(contact_unavailable())
    }

    fn verify_safety_number(
        &self,
        _encoded_identity: &[u8],
        _encoded_fingerprint: &[u8],
    ) -> Result<ContactResponse, Status> {
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

    fn verify_qr(&self, encoded_payload: &[u8]) -> Result<ContactResponse, Status> {
        if encoded_payload.len() != QR_VERIFICATION_PAYLOAD_BYTES {
            return Err(Status::invalid_argument(
                "QR verification payload is invalid",
            ));
        }
        self.with_contacts(|local_identity, contacts| {
            QrContactVerificationService::new(local_identity, contacts)
                .verify_encoded(encoded_payload)
                .map(contact_response)
                .map_err(map_qr_contact_verification_error)
        })
    }

    fn verify_safety_number(
        &self,
        encoded_identity: &[u8],
        encoded_fingerprint: &[u8],
    ) -> Result<ContactResponse, Status> {
        let identity = decode_contact_identity(encoded_identity)?;
        let fingerprint: &[u8; SAFETY_NUMBER_FINGERPRINT_BYTES] = encoded_fingerprint
            .try_into()
            .map_err(|_| Status::invalid_argument("safety number is invalid"))?;
        self.with_contacts(|local_identity, contacts| {
            SafetyNumberVerificationService::new(local_identity, contacts)
                .verify(&identity, fingerprint)
                .map(contact_response)
                .map_err(map_safety_number_verification_error)
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

trait DaemonOutboxOperations: Send + Sync {
    fn queue(
        &self,
        encoded_recipient: &[u8],
        encoded_envelope: &[u8],
        created_at: u64,
        ttl_seconds: u32,
    ) -> Result<SendMessageResponse, Status>;
    fn delivery_status(
        &self,
        encoded_identifier: &[u8],
    ) -> Result<GetDeliveryStatusResponse, Status>;
}

struct UnavailableOutboxOperations;

impl DaemonOutboxOperations for UnavailableOutboxOperations {
    fn queue(
        &self,
        _encoded_recipient: &[u8],
        _encoded_envelope: &[u8],
        _created_at: u64,
        _ttl_seconds: u32,
    ) -> Result<SendMessageResponse, Status> {
        Err(outbox_unavailable())
    }

    fn delivery_status(
        &self,
        _encoded_identifier: &[u8],
    ) -> Result<GetDeliveryStatusResponse, Status> {
        Err(outbox_unavailable())
    }
}

struct KeystoreOutboxOperations<K> {
    keystore: Arc<Mutex<K>>,
    outbox_path: PathBuf,
    operation_lock: Mutex<()>,
}

impl<K> KeystoreOutboxOperations<K>
where
    K: OsKeystore + Send,
{
    fn with_outbox<T>(
        &self,
        operation: impl FnOnce(&mut SenderOutbox) -> Result<T, Status>,
    ) -> Result<T, Status> {
        let operation_lock = self.operation_lock.lock().map_err(|_| outbox_failure())?;
        let mut keystore = self.keystore.lock().map_err(|_| outbox_failure())?;
        let mut outbox =
            SenderOutbox::open(&self.outbox_path, &mut *keystore).map_err(|_| outbox_failure())?;
        drop(keystore);
        let result = operation(&mut outbox);
        drop(operation_lock);
        result
    }
}

impl<K> DaemonOutboxOperations for KeystoreOutboxOperations<K>
where
    K: OsKeystore + Send,
{
    fn queue(
        &self,
        encoded_recipient: &[u8],
        encoded_envelope: &[u8],
        created_at: u64,
        ttl_seconds: u32,
    ) -> Result<SendMessageResponse, Status> {
        let recipient = decode_message_recipient(encoded_recipient)?;
        let envelope = decode_message_envelope(encoded_envelope)?;
        let expiry =
            MessageExpiry::new(created_at, ttl_seconds).map_err(map_message_expiry_error)?;
        self.with_outbox(|outbox| {
            outbox
                .enqueue(recipient, envelope, expiry)
                .map_err(|error| map_sender_outbox_error(&error))?;
            let identifier = outbox
                .messages()
                .last()
                .ok_or_else(outbox_failure)?
                .identifier();
            Ok(SendMessageResponse {
                message_identifier: identifier.as_bytes().to_vec(),
            })
        })
    }

    fn delivery_status(
        &self,
        encoded_identifier: &[u8],
    ) -> Result<GetDeliveryStatusResponse, Status> {
        let identifier = decode_message_identifier(encoded_identifier)?;
        self.with_outbox(|outbox| {
            outbox
                .delivery_state(identifier)
                .map(delivery_status_response)
                .ok_or_else(|| Status::not_found("message does not exist"))
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

    fn export_recovery(
        &self,
        passphrase: &IdentityExportPassphrase,
    ) -> Result<ExportIdentityRecoveryResponse, Status> {
        let keystore = self.keystore.lock().map_err(|_| identity_failure())?;
        ClientIdentity::load(&*keystore)
            .and_then(|identity| identity.export_recovery(passphrase))
            .map(|archive| ExportIdentityRecoveryResponse { archive })
            .map_err(|error| map_recovery_export_error(&error))
    }

    fn import_recovery(
        &self,
        archive: &[u8],
        passphrase: &IdentityExportPassphrase,
    ) -> Result<IdentityResponse, Status> {
        let mut keystore = self.keystore.lock().map_err(|_| identity_failure())?;
        ClientIdentity::import_recovery(&mut *keystore, archive, passphrase)
            .map(|identity| {
                identity_response(
                    identity.public_key().as_bytes(),
                    IdentityInitialization::Recovered,
                )
            })
            .map_err(|error| map_recovery_import_error(&error))
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
        | ClientIdentityError::RecoveryExport(_)
        | ClientIdentityError::RecoveryImport(_)
        | ClientIdentityError::KeystoreSecret(_)
        | ClientIdentityError::Keystore => identity_failure(),
    }
}

fn map_recovery_export_error(error: &ClientIdentityError) -> Status {
    match error {
        ClientIdentityError::NotInitialized => Status::not_found("daemon identity does not exist"),
        ClientIdentityError::AlreadyInitialized
        | ClientIdentityError::InvalidKeyEntry
        | ClientIdentityError::Generation(_)
        | ClientIdentityError::InvalidStoredIdentity(_)
        | ClientIdentityError::RecoveryExport(_)
        | ClientIdentityError::RecoveryImport(_)
        | ClientIdentityError::KeystoreSecret(_)
        | ClientIdentityError::Keystore => identity_failure(),
    }
}

fn map_recovery_import_error(error: &ClientIdentityError) -> Status {
    match error {
        ClientIdentityError::AlreadyInitialized => Status::already_exists("daemon identity exists"),
        ClientIdentityError::RecoveryImport(_) => {
            Status::invalid_argument("recovery archive is invalid")
        }
        ClientIdentityError::NotInitialized
        | ClientIdentityError::InvalidKeyEntry
        | ClientIdentityError::Generation(_)
        | ClientIdentityError::InvalidStoredIdentity(_)
        | ClientIdentityError::RecoveryExport(_)
        | ClientIdentityError::KeystoreSecret(_)
        | ClientIdentityError::Keystore => identity_failure(),
    }
}

fn recovery_passphrase(passphrase: Vec<u8>) -> Result<IdentityExportPassphrase, Status> {
    IdentityExportPassphrase::new(passphrase)
        .map_err(|_| Status::invalid_argument("recovery passphrase is invalid"))
}

fn recovery_archive(archive: &[u8]) -> Result<(), Status> {
    if archive.len() != IDENTITY_EXPORT_BYTES {
        return Err(Status::invalid_argument("recovery archive is invalid"));
    }
    Ok(())
}

fn contact_unavailable() -> Status {
    Status::unavailable("daemon contact service is unavailable")
}

fn contact_failure() -> Status {
    Status::internal("daemon contact operation failed")
}

fn outbox_unavailable() -> Status {
    Status::unavailable("daemon message service is unavailable")
}

fn outbox_failure() -> Status {
    Status::internal("daemon message operation failed")
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
        | ClientIdentityError::RecoveryExport(_)
        | ClientIdentityError::RecoveryImport(_)
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

fn decode_message_recipient(encoded: &[u8]) -> Result<IdentityPublicKey, Status> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| Status::invalid_argument("message recipient is invalid"))?;
    IdentityPublicKey::from_bytes(bytes)
        .map_err(|_| Status::invalid_argument("message recipient is invalid"))
}

fn decode_message_identifier(encoded: &[u8]) -> Result<MessageIdentifier, Status> {
    let bytes: [u8; MESSAGE_IDENTIFIER_BYTES] = encoded
        .try_into()
        .map_err(|_| Status::invalid_argument("message identifier is invalid"))?;
    MessageIdentifier::from_bytes(bytes)
        .map_err(|_| Status::invalid_argument("message identifier is invalid"))
}

fn decode_message_envelope(encoded: &[u8]) -> Result<EncryptedMessageEnvelope, Status> {
    if encoded.len() > MAX_DAEMON_MESSAGE_ENVELOPE_BYTES {
        return Err(Status::invalid_argument("message envelope is invalid"));
    }
    let envelope = EncryptedMessageEnvelope::decode(encoded)
        .map_err(|_| Status::invalid_argument("message envelope is invalid"))?;
    if envelope
        .encode()
        .map_err(|_| Status::invalid_argument("message envelope is invalid"))?
        != encoded
    {
        return Err(Status::invalid_argument("message envelope is invalid"));
    }
    Ok(envelope)
}

fn map_message_expiry_error(_error: MessageExpiryError) -> Status {
    Status::invalid_argument("message expiry is invalid")
}

fn map_sender_outbox_error(error: &SenderOutboxError) -> Status {
    match error {
        SenderOutboxError::QueueFull => Status::resource_exhausted("daemon outbox is full"),
        _ => outbox_failure(),
    }
}

fn delivery_status_response(state: StoredDeliveryState) -> GetDeliveryStatusResponse {
    let status = match state {
        StoredDeliveryState::Unknown => RpcDeliveryStatus::Queued,
        StoredDeliveryState::Delivered => RpcDeliveryStatus::Delivered,
        StoredDeliveryState::Expired => RpcDeliveryStatus::Expired,
        StoredDeliveryState::Failed => RpcDeliveryStatus::Failed,
    };
    GetDeliveryStatusResponse {
        status: status.into(),
    }
}

fn daemon_event_response(event: DaemonEventEnvelope) -> RpcDaemonEvent {
    let (kind, message_identifier) = match event.event {
        DaemonEvent::ClientStarted => (RpcDaemonEventKind::ClientStarted, Vec::new()),
        DaemonEvent::ClientStopped => (RpcDaemonEventKind::ClientStopped, Vec::new()),
        DaemonEvent::MessageQueued(identifier) => {
            (RpcDaemonEventKind::MessageQueued, identifier.to_vec())
        }
    };
    RpcDaemonEvent {
        version: DAEMON_EVENT_VERSION,
        sequence: event.sequence,
        kind: kind.into(),
        message_identifier,
    }
}

fn select_delivery_profile(
    request: &SelectDeliveryProfileRequest,
) -> Result<DeliveryProfileResponse, Status> {
    let local_mesh =
        delivery_profile_local_mesh_constraints(&request.allowed_local_mesh_transports)?;
    let constraints = DeliveryProfileConstraints::new(
        request.direct_allowed,
        request.tor_maildrop_allowed,
        local_mesh.is_some(),
    )
    .map_err(|_| delivery_profile_invalid())?;
    let kind =
        RpcDeliveryProfileKind::try_from(request.kind).map_err(|_| delivery_profile_invalid())?;
    let selected_local_mesh = RpcLocalMeshTransportKind::try_from(request.local_mesh_transport)
        .map_err(|_| delivery_profile_invalid())?;
    let profile = match kind {
        RpcDeliveryProfileKind::Direct => {
            if selected_local_mesh != RpcLocalMeshTransportKind::Unspecified
                || !request.direct_ip_disclosure_acknowledged
            {
                return Err(delivery_profile_invalid());
            }
            DeliveryProfile::direct(DirectProfileSelection::acknowledge_ip_disclosure())
        }
        RpcDeliveryProfileKind::TorMaildrop => {
            if selected_local_mesh != RpcLocalMeshTransportKind::Unspecified
                || request.direct_ip_disclosure_acknowledged
            {
                return Err(delivery_profile_invalid());
            }
            DeliveryProfile::tor_maildrop()
        }
        RpcDeliveryProfileKind::LocalMesh => {
            if request.direct_ip_disclosure_acknowledged {
                return Err(delivery_profile_invalid());
            }
            let transport = local_mesh_transport(selected_local_mesh)?;
            let Some(local_mesh) = local_mesh else {
                return Err(delivery_profile_disallowed());
            };
            local_mesh
                .validate(LocalMeshProfileConfig::new(transport))
                .map_err(|_| delivery_profile_disallowed())?;
            DeliveryProfile::local_mesh(LocalMeshProfileSelection::select(transport))
        }
        RpcDeliveryProfileKind::Unspecified => return Err(delivery_profile_invalid()),
    };
    constraints
        .validate(profile)
        .map_err(|_| delivery_profile_disallowed())?;
    Ok(DeliveryProfileResponse {
        kind: kind.into(),
        direct_ip_disclosure_warning: profile.privacy_warning().is_some(),
    })
}

fn delivery_profile_local_mesh_constraints(
    allowed: &[i32],
) -> Result<Option<LocalMeshProfileConstraints>, Status> {
    if allowed.is_empty() {
        return Ok(None);
    }
    if allowed.len() > 4 {
        return Err(delivery_profile_invalid());
    }
    let mut lan_allowed = false;
    let mut wifi_hotspot_allowed = false;
    let mut wifi_direct_allowed = false;
    let mut bluetooth_allowed = false;
    for &encoded_transport in allowed {
        match RpcLocalMeshTransportKind::try_from(encoded_transport)
            .map_err(|_| delivery_profile_invalid())?
        {
            RpcLocalMeshTransportKind::Lan if !lan_allowed => lan_allowed = true,
            RpcLocalMeshTransportKind::WifiHotspot if !wifi_hotspot_allowed => {
                wifi_hotspot_allowed = true;
            }
            RpcLocalMeshTransportKind::WifiDirect if !wifi_direct_allowed => {
                wifi_direct_allowed = true;
            }
            RpcLocalMeshTransportKind::Bluetooth if !bluetooth_allowed => bluetooth_allowed = true,
            RpcLocalMeshTransportKind::Unspecified
            | RpcLocalMeshTransportKind::Lan
            | RpcLocalMeshTransportKind::WifiHotspot
            | RpcLocalMeshTransportKind::WifiDirect
            | RpcLocalMeshTransportKind::Bluetooth => return Err(delivery_profile_invalid()),
        }
    }
    LocalMeshProfileConstraints::new(
        lan_allowed,
        wifi_hotspot_allowed,
        wifi_direct_allowed,
        bluetooth_allowed,
    )
    .map(Some)
    .map_err(|_| delivery_profile_invalid())
}

fn local_mesh_transport(kind: RpcLocalMeshTransportKind) -> Result<LocalMeshTransportKind, Status> {
    match kind {
        RpcLocalMeshTransportKind::Lan => Ok(LocalMeshTransportKind::Lan),
        RpcLocalMeshTransportKind::WifiHotspot => Ok(LocalMeshTransportKind::WifiHotspot),
        RpcLocalMeshTransportKind::WifiDirect => Ok(LocalMeshTransportKind::WifiDirect),
        RpcLocalMeshTransportKind::Bluetooth => Ok(LocalMeshTransportKind::Bluetooth),
        RpcLocalMeshTransportKind::Unspecified => Err(delivery_profile_invalid()),
    }
}

fn delivery_profile_invalid() -> Status {
    Status::invalid_argument("delivery profile selection is invalid")
}

fn delivery_profile_disallowed() -> Status {
    Status::failed_precondition("delivery profile selection is disallowed")
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

fn map_qr_contact_verification_error(error: QrContactVerificationError) -> Status {
    match error {
        QrContactVerificationError::Payload(_) => {
            Status::invalid_argument("QR verification payload is invalid")
        }
        QrContactVerificationError::ContactStore(error) => map_contact_store_error(&error),
    }
}

fn map_safety_number_verification_error(error: SafetyNumberVerificationError) -> Status {
    match error {
        SafetyNumberVerificationError::InvalidFingerprintLength => {
            Status::invalid_argument("safety number is invalid")
        }
        SafetyNumberVerificationError::ContactStore(error) => map_contact_store_error(&error),
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
    type SubscribeEventsStream =
        Pin<Box<dyn Stream<Item = Result<RpcDaemonEvent, Status>> + Send + 'static>>;

    async fn start_client(
        &self,
        _request: Request<StartClientRequest>,
    ) -> Result<Response<StartClientResponse>, Status> {
        let (api_major, api_minor, running) = self.status_response();
        if running
            && self
                .client_started
                .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
                .is_ok()
            && let Err(error) = self.emit(DaemonEvent::ClientStarted)
        {
            self.client_started.store(false, Ordering::Release);
            return Err(error);
        }
        Ok(Response::new(StartClientResponse {
            api_major,
            api_minor,
            running,
        }))
    }

    async fn shutdown_daemon(
        &self,
        _request: Request<ShutdownDaemonRequest>,
    ) -> Result<Response<ShutdownDaemonResponse>, Status> {
        if self.shutdown() && self.client_started.swap(false, Ordering::AcqRel) {
            self.emit(DaemonEvent::ClientStopped)?;
        }
        Ok(Response::new(ShutdownDaemonResponse { running: false }))
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

    async fn export_identity_recovery(
        &self,
        request: Request<ExportIdentityRecoveryRequest>,
    ) -> Result<Response<ExportIdentityRecoveryResponse>, Status> {
        let passphrase = recovery_passphrase(request.into_inner().passphrase)?;
        self.identity
            .export_recovery(&passphrase)
            .map(Response::new)
    }

    async fn import_identity_recovery(
        &self,
        request: Request<ImportIdentityRecoveryRequest>,
    ) -> Result<Response<IdentityResponse>, Status> {
        let request = request.into_inner();
        recovery_archive(&request.archive)?;
        let passphrase = recovery_passphrase(request.passphrase)?;
        self.identity
            .import_recovery(&request.archive, &passphrase)
            .map(Response::new)
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

    async fn verify_contact_qr(
        &self,
        request: Request<VerifyContactQrRequest>,
    ) -> Result<Response<ContactResponse>, Status> {
        self.contacts
            .verify_qr(&request.into_inner().payload)
            .map(Response::new)
    }

    async fn verify_contact_safety_number(
        &self,
        request: Request<VerifyContactSafetyNumberRequest>,
    ) -> Result<Response<ContactResponse>, Status> {
        let request = request.into_inner();
        self.contacts
            .verify_safety_number(&request.identity, &request.fingerprint)
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

    async fn send_message(
        &self,
        request: Request<SendMessageRequest>,
    ) -> Result<Response<SendMessageResponse>, Status> {
        let request = request.into_inner();
        let response = self.outbox.queue(
            &request.recipient,
            &request.envelope,
            request.created_at,
            request.ttl_seconds,
        )?;
        let identifier: [u8; MESSAGE_IDENTIFIER_BYTES] = response
            .message_identifier
            .as_slice()
            .try_into()
            .map_err(|_| Status::internal("daemon message operation failed"))?;
        self.emit(DaemonEvent::MessageQueued(identifier))?;
        Ok(Response::new(response))
    }

    async fn get_delivery_status(
        &self,
        request: Request<GetDeliveryStatusRequest>,
    ) -> Result<Response<GetDeliveryStatusResponse>, Status> {
        self.outbox
            .delivery_status(&request.into_inner().message_identifier)
            .map(Response::new)
    }

    async fn select_delivery_profile(
        &self,
        request: Request<SelectDeliveryProfileRequest>,
    ) -> Result<Response<DeliveryProfileResponse>, Status> {
        let request = request.into_inner();
        select_delivery_profile(&request).map(Response::new)
    }

    async fn subscribe_events(
        &self,
        _request: Request<SubscribeEventsRequest>,
    ) -> Result<Response<Self::SubscribeEventsStream>, Status> {
        let stream = BroadcastStream::new(self.events.subscribe()).map(|event| {
            event
                .map(daemon_event_response)
                .map_err(|_| Status::resource_exhausted("daemon event stream lagged"))
        });
        Ok(Response::new(Box::pin(stream)))
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

    use tokio_stream::StreamExt;
    use tonic::{Code, Request};
    use yeokcham_core::IdentityKeypair;
    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_daemon_api::v1::{
        DaemonEventKind as RpcDaemonEventKind, DeliveryProfileKind as RpcDeliveryProfileKind,
        GetIdentityRequest, GetStatusRequest, ListContactsRequest,
        LocalMeshTransportKind as RpcLocalMeshTransportKind, SelectDeliveryProfileRequest,
        SendMessageRequest, ShutdownDaemonRequest, StartClientRequest, SubscribeEventsRequest,
        daemon_service_server::DaemonService,
    };
    use yeokcham_protocol::{
        EncryptedMessageEnvelope, IDENTITY_EXPORT_BYTES, MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES,
        ProtocolVersion,
    };

    use super::{
        DAEMON_EVENT_VERSION, DaemonEvent, DaemonGrpcService, MAX_DAEMON_EVENT_BACKLOG,
        recovery_archive, recovery_passphrase, select_delivery_profile,
    };

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
        let _ = service.shutdown();
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
        let _ = service.shutdown();
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
            PathBuf::from("/tmp/yeokcham-outbox-redaction"),
            FailingKeystore,
        );
        let error = service
            .list_contacts(Request::new(ListContactsRequest {}))
            .await
            .unwrap_err();
        assert_eq!(error.code(), Code::Internal);
        assert_eq!(error.message(), "daemon contact operation failed");
    }

    #[tokio::test]
    async fn message_rpc_redacts_keystore_failures() {
        let service = DaemonGrpcService::with_state_keystore(
            ProtocolVersion::INITIAL,
            PathBuf::from("/tmp/yeokcham-contact-redaction"),
            PathBuf::from("/tmp/yeokcham-outbox-redaction"),
            FailingKeystore,
        );
        let recipient = IdentityKeypair::generate().unwrap();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();
        let error = service
            .send_message(Request::new(SendMessageRequest {
                recipient: recipient.public_key().as_bytes().to_vec(),
                envelope,
                created_at: 100,
                ttl_seconds: 60,
            }))
            .await
            .unwrap_err();
        assert_eq!(error.code(), Code::Internal);
        assert_eq!(error.message(), "daemon message operation failed");
    }

    #[test]
    fn delivery_profile_selection_requires_explicit_bounded_policy() {
        let direct = select_delivery_profile(&SelectDeliveryProfileRequest {
            direct_allowed: true,
            tor_maildrop_allowed: false,
            allowed_local_mesh_transports: Vec::new(),
            kind: RpcDeliveryProfileKind::Direct.into(),
            local_mesh_transport: RpcLocalMeshTransportKind::Unspecified.into(),
            direct_ip_disclosure_acknowledged: true,
        })
        .unwrap();
        assert_eq!(
            RpcDeliveryProfileKind::try_from(direct.kind),
            Ok(RpcDeliveryProfileKind::Direct)
        );
        assert!(direct.direct_ip_disclosure_warning);
        let local_mesh = select_delivery_profile(&SelectDeliveryProfileRequest {
            direct_allowed: false,
            tor_maildrop_allowed: false,
            allowed_local_mesh_transports: vec![RpcLocalMeshTransportKind::Lan.into()],
            kind: RpcDeliveryProfileKind::LocalMesh.into(),
            local_mesh_transport: RpcLocalMeshTransportKind::Lan.into(),
            direct_ip_disclosure_acknowledged: false,
        })
        .unwrap();
        assert_eq!(
            RpcDeliveryProfileKind::try_from(local_mesh.kind),
            Ok(RpcDeliveryProfileKind::LocalMesh)
        );
        assert!(!local_mesh.direct_ip_disclosure_warning);
        for request in [
            SelectDeliveryProfileRequest {
                direct_allowed: true,
                tor_maildrop_allowed: false,
                allowed_local_mesh_transports: Vec::new(),
                kind: RpcDeliveryProfileKind::Direct.into(),
                local_mesh_transport: RpcLocalMeshTransportKind::Unspecified.into(),
                direct_ip_disclosure_acknowledged: false,
            },
            SelectDeliveryProfileRequest {
                direct_allowed: true,
                tor_maildrop_allowed: false,
                allowed_local_mesh_transports: vec![RpcLocalMeshTransportKind::Lan.into(); 5],
                kind: RpcDeliveryProfileKind::Direct.into(),
                local_mesh_transport: RpcLocalMeshTransportKind::Unspecified.into(),
                direct_ip_disclosure_acknowledged: true,
            },
            SelectDeliveryProfileRequest {
                direct_allowed: true,
                tor_maildrop_allowed: false,
                allowed_local_mesh_transports: vec![RpcLocalMeshTransportKind::Lan.into(); 2],
                kind: RpcDeliveryProfileKind::Direct.into(),
                local_mesh_transport: RpcLocalMeshTransportKind::Unspecified.into(),
                direct_ip_disclosure_acknowledged: true,
            },
        ] {
            let error = select_delivery_profile(&request).unwrap_err();
            assert_eq!(error.code(), Code::InvalidArgument);
            assert_eq!(error.message(), "delivery profile selection is invalid");
        }
        let disallowed = select_delivery_profile(&SelectDeliveryProfileRequest {
            direct_allowed: false,
            tor_maildrop_allowed: true,
            allowed_local_mesh_transports: Vec::new(),
            kind: RpcDeliveryProfileKind::Direct.into(),
            local_mesh_transport: RpcLocalMeshTransportKind::Unspecified.into(),
            direct_ip_disclosure_acknowledged: true,
        })
        .unwrap_err();
        assert_eq!(disallowed.code(), Code::FailedPrecondition);
        assert_eq!(
            disallowed.message(),
            "delivery profile selection is disallowed"
        );
    }

    #[test]
    fn recovery_input_validation_is_bounded_and_redacted() {
        for passphrase in [
            Vec::new(),
            vec![0; MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES + 1],
        ] {
            let error = recovery_passphrase(passphrase).unwrap_err();
            assert_eq!(error.code(), Code::InvalidArgument);
            assert_eq!(error.message(), "recovery passphrase is invalid");
        }
        let error = recovery_archive(&[0; IDENTITY_EXPORT_BYTES - 1]).unwrap_err();
        assert_eq!(error.code(), Code::InvalidArgument);
        assert_eq!(error.message(), "recovery archive is invalid");
    }

    #[tokio::test]
    async fn event_stream_is_ordered_and_lifecycle_bounded() {
        let service = DaemonGrpcService::new(ProtocolVersion::INITIAL);
        let mut events = service
            .subscribe_events(Request::new(SubscribeEventsRequest {}))
            .await
            .unwrap()
            .into_inner();
        service
            .start_client(Request::new(StartClientRequest {}))
            .await
            .unwrap();
        service
            .shutdown_daemon(Request::new(ShutdownDaemonRequest {}))
            .await
            .unwrap();
        let started = events.next().await.unwrap().unwrap();
        let stopped = events.next().await.unwrap().unwrap();
        assert_eq!(started.version, DAEMON_EVENT_VERSION);
        assert_eq!(started.sequence, 1);
        assert_eq!(
            RpcDaemonEventKind::try_from(started.kind),
            Ok(RpcDaemonEventKind::ClientStarted)
        );
        assert!(started.message_identifier.is_empty());
        assert_eq!(stopped.sequence, 2);
        assert_eq!(
            RpcDaemonEventKind::try_from(stopped.kind),
            Ok(RpcDaemonEventKind::ClientStopped)
        );
        assert!(stopped.message_identifier.is_empty());
    }

    #[tokio::test]
    async fn event_stream_reports_lag_without_silent_gaps() {
        let service = DaemonGrpcService::new(ProtocolVersion::INITIAL);
        let mut events = service
            .subscribe_events(Request::new(SubscribeEventsRequest {}))
            .await
            .unwrap()
            .into_inner();
        for _ in 0..=MAX_DAEMON_EVENT_BACKLOG {
            service.emit(DaemonEvent::ClientStarted).unwrap();
        }
        let error = events.next().await.unwrap().unwrap_err();
        assert_eq!(error.code(), Code::ResourceExhausted);
        assert_eq!(error.message(), "daemon event stream lagged");
    }
}
