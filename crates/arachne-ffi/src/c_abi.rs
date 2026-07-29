use std::{
    collections::BTreeMap,
    convert::Infallible,
    ffi::c_void,
    future::{Future, ready},
    path::PathBuf,
    sync::{
        Arc, Mutex, OnceLock,
        atomic::{AtomicUsize, Ordering},
        mpsc::{self, SyncSender, TrySendError},
    },
    time::Duration,
};

use crate::{
    ARACHNE_ABI_NEGOTIATION_REJECTED, ARACHNE_ABI_VERSION, ARACHNE_EVENT_CLIENT_STARTED,
    ARACHNE_EVENT_CLIENT_STOPPED, ARACHNE_EVENT_MESSAGE_DELIVERED,
    ARACHNE_EVENT_MESSAGE_DELIVERY_FAILED, ARACHNE_EVENT_MESSAGE_QUEUED,
    ArachneAttachmentDeliveryCycle, ArachneAttachmentTransfer, ArachneBuffer, ArachneByteSlice,
    ArachneCancellation, ArachneClient, ArachneClientConfigBuilder, ArachneContact,
    ArachneDeliveryProfile, ArachneDeliveryProfilePolicy, ArachneEvent, ArachneEventSubscription,
    ArachneStatus,
};
use arachne_core::{IdentityPublicKey, KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_sdk::{
    CancellationToken, RuntimeMode, SdkAsyncPolicy, SdkAttachmentChunk, SdkAttachmentDeliveryCycle,
    SdkAttachmentDeliveryOutcome, SdkAttachmentDeliveryTransport, SdkAttachmentManifest,
    SdkAttachmentTransfer, SdkClient, SdkClientError, SdkConfig, SdkContact, SdkContactError,
    SdkContactStatus, SdkContactVerificationMethod, SdkDeliveryProfile, SdkDeliveryProfileKind,
    SdkDeliveryProfilePolicy, SdkDeliveryProfilePolicyError, SdkDirectIpDisclosureAcknowledgement,
    SdkEvent, SdkEventEnvelope, SdkEventStream, SdkEventStreamError, SdkIdentityError,
    SdkIdentityManager, SdkLocalMeshPolicy, SdkLocalMeshTransportKind, SdkMessageEnvelope,
    SdkMessageError, SdkMessageExpiry, SdkMessageSendRequest, SdkRecoveryArchive, SdkRecoveryError,
    SdkRecoveryPassphrase,
};
use zeroize::{Zeroize, Zeroizing};

pub const MAX_C_ABI_CLIENT_CONFIG_BUILDERS: usize = 1024;
pub const MAX_C_ABI_CLIENTS: usize = 1024;
pub const MAX_C_ABI_ATTACHMENT_TRANSFERS: usize = 1024;
pub const MAX_C_ABI_CANCELLATIONS: usize = 1024;
pub const MAX_C_ABI_CALLBACK_WORKERS: usize = 4;
pub const MAX_C_ABI_BUFFERS: usize = 1024;
pub const MAX_C_ABI_EVENT_SUBSCRIPTIONS: usize = 1024;
pub const MAX_C_ABI_ERROR_DETAIL_BYTES: usize = 64;
pub const MAX_C_ABI_PENDING_COMPLETIONS: usize = 1024;
pub const MAX_C_ABI_SECRET_BUFFER_BYTES: usize = 16 * 1024 * 1024;
pub const MAX_C_ABI_STATE_DIRECTORY_BYTES: usize = 4096;

struct ClientHandle {
    configuration: Option<SdkConfig>,
    identity: SdkIdentityManager<ClientKeystore>,
    last_error_detail: Option<&'static [u8]>,
    runtime: Option<SdkClient>,
}

#[derive(Default)]
struct ClientConfigBuilder {
    state_directory: Option<PathBuf>,
    event_buffer_capacity: Option<usize>,
}

#[derive(Default)]
struct ClientKeystore(BTreeMap<String, Zeroizing<Vec<u8>>>);

impl OsKeystore for ClientKeystore {
    type Error = Infallible;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .0
            .get(entry.as_str())
            .and_then(|secret| KeystoreSecret::new(secret.to_vec()).ok()))
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error> {
        self.0.insert(
            entry.as_str().to_owned(),
            Zeroizing::new(secret.as_bytes().to_vec()),
        );
        Ok(())
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
        self.0.remove(entry.as_str());
        Ok(())
    }
}

static ACTIVE_CLIENTS: OnceLock<Mutex<BTreeMap<usize, ClientHandle>>> = OnceLock::new();
static ACTIVE_ATTACHMENT_TRANSFERS: OnceLock<Mutex<BTreeMap<usize, AttachmentTransferHandle>>> =
    OnceLock::new();
static ACTIVE_CANCELLATIONS: OnceLock<Mutex<BTreeMap<usize, CancellationToken>>> = OnceLock::new();
static ACTIVE_CLIENT_CONFIG_BUILDERS: OnceLock<Mutex<BTreeMap<usize, ClientConfigBuilder>>> =
    OnceLock::new();
static ACTIVE_BUFFERS: OnceLock<Mutex<BTreeMap<usize, Zeroizing<Vec<u8>>>>> = OnceLock::new();
static ACTIVE_EVENT_SUBSCRIPTIONS: OnceLock<Mutex<BTreeMap<usize, SdkEventStream>>> =
    OnceLock::new();
static NEXT_HANDLE_IDENTIFIER: AtomicUsize = AtomicUsize::new(1);
static PENDING_COMPLETIONS: AtomicUsize = AtomicUsize::new(0);
static CALLBACK_QUEUE: OnceLock<Option<SyncSender<PendingCompletion>>> = OnceLock::new();

#[cfg(test)]
pub static CLIENT_TEST_LOCK: Mutex<()> = Mutex::new(());

pub type ArachneCompletionCallback = extern "C" fn(i32, *mut c_void);
pub type ArachneAttachmentUploadCallback =
    extern "C" fn(*const u8, usize, *const u8, usize, *mut c_void) -> ArachneStatus;

enum AttachmentTransferHandle {
    Ready(SdkAttachmentTransfer),
    Running,
}

struct CAttachmentTransport {
    callback: ArachneAttachmentUploadCallback,
    context: *mut c_void,
}

unsafe impl Send for CAttachmentTransport {}

impl SdkAttachmentDeliveryTransport for CAttachmentTransport {
    type Error = ();

    fn upload_chunk(
        &mut self,
        manifest: &[u8],
        chunk: &[u8],
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        let status = (self.callback)(
            manifest.as_ptr(),
            manifest.len(),
            chunk.as_ptr(),
            chunk.len(),
            self.context,
        );
        ready((status == ArachneStatus::Ok).then_some(()).ok_or(()))
    }
}

struct PendingCompletion {
    callback: ArachneCompletionCallback,
    context: usize,
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_abi_negotiate(requested_version: u32) -> u32 {
    if requested_version == ARACHNE_ABI_VERSION {
        ARACHNE_ABI_VERSION
    } else {
        ARACHNE_ABI_NEGOTIATION_REJECTED
    }
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_secret_buffer_zeroize(
    buffer: *mut u8,
    length: usize,
) -> ArachneStatus {
    if buffer.is_null() || length == 0 {
        return ArachneStatus::InvalidInput;
    }
    if length > MAX_C_ABI_SECRET_BUFFER_BYTES {
        return ArachneStatus::ResourceLimit;
    }
    let buffer = unsafe { std::slice::from_raw_parts_mut(buffer, length) };
    buffer.zeroize();
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_create() -> *mut ArachneClient {
    let Ok(mut clients) = active_clients().lock() else {
        return std::ptr::null_mut();
    };
    if clients.len() >= MAX_C_ABI_CLIENTS {
        return std::ptr::null_mut();
    }
    let Some(identifier) = next_handle_identifier() else {
        return std::ptr::null_mut();
    };
    if clients
        .insert(
            identifier,
            ClientHandle {
                configuration: None,
                identity: SdkIdentityManager::new(ClientKeystore::default()),
                last_error_detail: None,
                runtime: None,
            },
        )
        .is_some()
    {
        return std::ptr::null_mut();
    }
    std::ptr::without_provenance_mut(identifier)
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_release(client: *mut ArachneClient) -> ArachneStatus {
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    if clients.remove(&identifier).is_none() {
        return ArachneStatus::InvalidInput;
    }
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_start(client: *mut ArachneClient) -> ArachneStatus {
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    if client.runtime.is_some() {
        return ArachneStatus::State;
    }
    let Some(configuration) = client.configuration.clone() else {
        return ArachneStatus::State;
    };
    let runtime = match SdkClient::start(&configuration) {
        Ok(runtime) => runtime,
        Err(error) => {
            client.last_error_detail = Some(sdk_client_error_detail(&error));
            return map_sdk_client_error(&error);
        }
    };
    client.runtime = Some(runtime);
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_stop(client: *mut ArachneClient) -> ArachneStatus {
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(mut runtime) = client.runtime.take() else {
        return ArachneStatus::State;
    };
    if let Err(error) = runtime.shutdown() {
        client.runtime = Some(runtime);
        client.last_error_detail = Some(sdk_client_error_detail(&error));
        return map_sdk_client_error(&error);
    }
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_identity_create(
    client: *mut ArachneClient,
    public_key: *mut u8,
) -> ArachneStatus {
    if public_key.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { std::ptr::write_bytes(public_key, 0, arachne_core::ED25519_PUBLIC_KEY_BYTES) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let identity = match client.identity.create() {
        Ok(identity) => identity,
        Err(error) => {
            client.last_error_detail = Some(sdk_identity_error_detail(&error));
            return map_sdk_identity_error(&error);
        }
    };
    unsafe {
        std::ptr::copy_nonoverlapping(
            identity.public_key().as_bytes().as_ptr(),
            public_key,
            arachne_core::ED25519_PUBLIC_KEY_BYTES,
        );
    };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_identity_load(
    client: *mut ArachneClient,
    public_key: *mut u8,
) -> ArachneStatus {
    if public_key.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { std::ptr::write_bytes(public_key, 0, arachne_core::ED25519_PUBLIC_KEY_BYTES) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let identity = match client.identity.load() {
        Ok(identity) => identity,
        Err(error) => {
            client.last_error_detail = Some(sdk_identity_error_detail(&error));
            return map_sdk_identity_error(&error);
        }
    };
    unsafe {
        std::ptr::copy_nonoverlapping(
            identity.public_key().as_bytes().as_ptr(),
            public_key,
            arachne_core::ED25519_PUBLIC_KEY_BYTES,
        );
    };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_identity_export_recovery(
    client: *mut ArachneClient,
    passphrase: *const u8,
    passphrase_length: usize,
    archive: *mut *mut ArachneBuffer,
) -> ArachneStatus {
    if archive.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { archive.write(std::ptr::null_mut()) };
    if passphrase.is_null()
        || passphrase_length == 0
        || passphrase_length > arachne_sdk::MAX_SDK_RECOVERY_PASSPHRASE_BYTES
    {
        return ArachneStatus::InvalidInput;
    }
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let passphrase = unsafe { std::slice::from_raw_parts(passphrase, passphrase_length) };
    let Ok(passphrase) = SdkRecoveryPassphrase::new(passphrase.to_vec()) else {
        return ArachneStatus::InvalidInput;
    };
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let archive_value = match client.identity.export_recovery(&passphrase) {
        Ok(archive_value) => archive_value,
        Err(error) => {
            client.last_error_detail = Some(sdk_recovery_error_detail(&error));
            return map_sdk_recovery_error(&error);
        }
    };
    let archive_buffer = match allocate_buffer(archive_value.as_bytes()) {
        Ok(archive_buffer) => archive_buffer,
        Err(status) => {
            client.last_error_detail = Some(b"sdk_recovery_buffer");
            return status;
        }
    };
    client.last_error_detail = None;
    unsafe { archive.write(archive_buffer) };
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_identity_import_recovery(
    client: *mut ArachneClient,
    archive: *const u8,
    archive_length: usize,
    passphrase: *const u8,
    passphrase_length: usize,
    public_key: *mut u8,
) -> ArachneStatus {
    if public_key.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { std::ptr::write_bytes(public_key, 0, arachne_core::ED25519_PUBLIC_KEY_BYTES) };
    if archive.is_null()
        || archive_length != arachne_protocol::IDENTITY_EXPORT_BYTES
        || passphrase.is_null()
        || passphrase_length == 0
        || passphrase_length > arachne_sdk::MAX_SDK_RECOVERY_PASSPHRASE_BYTES
    {
        return ArachneStatus::InvalidInput;
    }
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let archive = unsafe { std::slice::from_raw_parts(archive, archive_length) };
    let Ok(archive) = SdkRecoveryArchive::from_bytes(archive.to_vec()) else {
        return ArachneStatus::InvalidInput;
    };
    let passphrase = unsafe { std::slice::from_raw_parts(passphrase, passphrase_length) };
    let Ok(passphrase) = SdkRecoveryPassphrase::new(passphrase.to_vec()) else {
        return ArachneStatus::InvalidInput;
    };
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let identity = match client.identity.import_recovery(&archive, &passphrase) {
        Ok(identity) => identity,
        Err(error) => {
            client.last_error_detail = Some(sdk_recovery_error_detail(&error));
            return map_sdk_recovery_error(&error);
        }
    };
    unsafe {
        std::ptr::copy_nonoverlapping(
            identity.public_key().as_bytes().as_ptr(),
            public_key,
            arachne_core::ED25519_PUBLIC_KEY_BYTES,
        );
    }
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_contact_import(
    client: *mut ArachneClient,
    invitation: *const u8,
    contact: *mut ArachneContact,
) -> ArachneStatus {
    if invitation.is_null() || contact.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { contact.write(ArachneContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let invitation = unsafe {
        std::slice::from_raw_parts(invitation, arachne_protocol::CONTACT_INVITATION_BYTES)
    };
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return ArachneStatus::State;
    };
    let result = runtime
        .contact_manager(&mut client.identity)
        .and_then(|mut contacts| contacts.import_invitation(invitation));
    let contact_value = match result {
        Ok(contact_value) => contact_value,
        Err(error) => {
            client.last_error_detail = Some(sdk_contact_error_detail(&error));
            return map_sdk_contact_error(&error);
        }
    };
    unsafe { contact.write(c_contact(contact_value)) };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_contact_get(
    client: *mut ArachneClient,
    identity: *const u8,
    contact: *mut ArachneContact,
) -> ArachneStatus {
    let Some(identity) = (unsafe { c_identity_public_key(identity) }) else {
        return ArachneStatus::InvalidInput;
    };
    if contact.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { contact.write(ArachneContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return ArachneStatus::State;
    };
    let result = runtime
        .contact_manager(&mut client.identity)
        .map(|contacts| contacts.contact(&identity));
    let Some(contact_value) = (match result {
        Ok(contact_value) => contact_value,
        Err(error) => {
            client.last_error_detail = Some(sdk_contact_error_detail(&error));
            return map_sdk_contact_error(&error);
        }
    }) else {
        client.last_error_detail = Some(b"sdk_contact_not_found");
        return ArachneStatus::State;
    };
    unsafe { contact.write(c_contact(contact_value)) };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_contact_revoke(
    client: *mut ArachneClient,
    identity: *const u8,
    contact: *mut ArachneContact,
) -> ArachneStatus {
    let Some(identity) = (unsafe { c_identity_public_key(identity) }) else {
        return ArachneStatus::InvalidInput;
    };
    if contact.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { contact.write(ArachneContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return ArachneStatus::State;
    };
    let result = runtime
        .contact_manager(&mut client.identity)
        .and_then(|mut contacts| contacts.revoke(&identity));
    let contact_value = match result {
        Ok(contact_value) => contact_value,
        Err(error) => {
            client.last_error_detail = Some(sdk_contact_error_detail(&error));
            return map_sdk_contact_error(&error);
        }
    };
    unsafe { contact.write(c_contact(contact_value)) };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_contact_verify_qr(
    client: *mut ArachneClient,
    payload: *const u8,
    contact: *mut ArachneContact,
) -> ArachneStatus {
    if payload.is_null() || contact.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { contact.write(ArachneContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let payload = unsafe {
        std::slice::from_raw_parts(payload, arachne_protocol::QR_VERIFICATION_PAYLOAD_BYTES)
    };
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return ArachneStatus::State;
    };
    let result = runtime
        .contact_manager(&mut client.identity)
        .and_then(|mut contacts| contacts.verify_qr(payload));
    let contact_value = match result {
        Ok(contact_value) => contact_value,
        Err(error) => {
            client.last_error_detail = Some(sdk_contact_error_detail(&error));
            return map_sdk_contact_error(&error);
        }
    };
    unsafe { contact.write(c_contact(contact_value)) };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_contact_verify_safety_number(
    client: *mut ArachneClient,
    identity: *const u8,
    fingerprint: *const u8,
    contact: *mut ArachneContact,
) -> ArachneStatus {
    if contact.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { contact.write(ArachneContact::default()) };
    let Some(identity) = (unsafe { c_identity_public_key(identity) }) else {
        return ArachneStatus::InvalidInput;
    };
    if fingerprint.is_null() {
        return ArachneStatus::InvalidInput;
    }
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let fingerprint = unsafe {
        std::slice::from_raw_parts(
            fingerprint,
            arachne_protocol::SAFETY_NUMBER_FINGERPRINT_BYTES,
        )
    };
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return ArachneStatus::State;
    };
    let result = runtime
        .contact_manager(&mut client.identity)
        .and_then(|mut contacts| contacts.verify_safety_number(&identity, fingerprint));
    let contact_value = match result {
        Ok(contact_value) => contact_value,
        Err(error) => {
            client.last_error_detail = Some(sdk_contact_error_detail(&error));
            return map_sdk_contact_error(&error);
        }
    };
    unsafe { contact.write(c_contact(contact_value)) };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_delivery_profile_select(
    policy: *const ArachneDeliveryProfilePolicy,
    kind: u32,
    local_mesh_transport: u32,
    direct_ip_disclosure_acknowledged: u32,
    profile: *mut ArachneDeliveryProfile,
) -> ArachneStatus {
    if policy.is_null() || profile.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { profile.write(ArachneDeliveryProfile::default()) };
    let policy = unsafe { *policy };
    let policy = match c_delivery_profile_policy(policy) {
        Ok(policy) => policy,
        Err(status) => return status,
    };
    let result = match kind {
        crate::ARACHNE_DELIVERY_PROFILE_DIRECT => {
            if local_mesh_transport != 0
                || direct_ip_disclosure_acknowledged
                    != crate::ARACHNE_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED
            {
                return ArachneStatus::InvalidInput;
            }
            policy.select_direct(SdkDirectIpDisclosureAcknowledgement::acknowledge())
        }
        crate::ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP => {
            if local_mesh_transport != 0 || direct_ip_disclosure_acknowledged != 0 {
                return ArachneStatus::InvalidInput;
            }
            policy.select_tor_maildrop()
        }
        crate::ARACHNE_DELIVERY_PROFILE_LOCAL_MESH => {
            if direct_ip_disclosure_acknowledged != 0 {
                return ArachneStatus::InvalidInput;
            }
            let Some(local_mesh_transport) = c_local_mesh_transport(local_mesh_transport) else {
                return ArachneStatus::InvalidInput;
            };
            policy.select_local_mesh(local_mesh_transport)
        }
        _ => return ArachneStatus::InvalidInput,
    };
    let profile_value = match result {
        Ok(profile_value) => profile_value,
        Err(error) => return map_sdk_delivery_profile_policy_error(error),
    };
    unsafe { profile.write(c_delivery_profile(profile_value)) };
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_message_send(
    client: *mut ArachneClient,
    recipient: *const u8,
    envelope: *const u8,
    envelope_length: usize,
    created_at: u64,
    ttl_seconds: u32,
    message_identifier: *mut u8,
) -> ArachneStatus {
    if message_identifier.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe {
        std::ptr::write_bytes(
            message_identifier,
            0,
            arachne_protocol::MESSAGE_IDENTIFIER_BYTES,
        );
    };
    let Some(recipient) = (unsafe { c_identity_public_key(recipient) }) else {
        return ArachneStatus::InvalidInput;
    };
    if envelope.is_null()
        || envelope_length == 0
        || envelope_length > crate::ARACHNE_MAX_MESSAGE_ENVELOPE_BYTES
    {
        return ArachneStatus::InvalidInput;
    }
    let envelope = unsafe { std::slice::from_raw_parts(envelope, envelope_length) };
    let Ok(envelope) = SdkMessageEnvelope::from_encoded(envelope) else {
        return ArachneStatus::InvalidInput;
    };
    let Ok(expiry) = SdkMessageExpiry::new(created_at, ttl_seconds) else {
        return ArachneStatus::InvalidInput;
    };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_mut() else {
        client.last_error_detail = Some(b"sdk_message_client_not_running");
        return ArachneStatus::State;
    };
    let result = runtime.send_message(
        &mut client.identity,
        SdkMessageSendRequest::new(recipient, envelope, expiry),
    );
    let queued = match result {
        Ok(queued) => queued,
        Err(error) => {
            client.last_error_detail = Some(sdk_message_error_detail(&error));
            return map_sdk_message_error(&error);
        }
    };
    unsafe {
        std::ptr::copy_nonoverlapping(
            queued.identifier().as_bytes().as_ptr(),
            message_identifier,
            arachne_protocol::MESSAGE_IDENTIFIER_BYTES,
        );
    };
    client.last_error_detail = None;
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_attachment_transfer_create(
    manifest: *const u8,
    manifest_length: usize,
    chunks: *const ArachneByteSlice,
    chunk_count: usize,
    maximum_chunks_per_cycle: u32,
) -> *mut ArachneAttachmentTransfer {
    if manifest.is_null()
        || chunks.is_null()
        || manifest_length == 0
        || manifest_length > crate::ARACHNE_MAX_ATTACHMENT_MANIFEST_BYTES
        || chunk_count == 0
        || chunk_count > crate::ARACHNE_MAX_ATTACHMENT_CHUNKS
    {
        return std::ptr::null_mut();
    }
    let Ok(maximum_chunks_per_cycle) = usize::try_from(maximum_chunks_per_cycle) else {
        return std::ptr::null_mut();
    };
    if maximum_chunks_per_cycle == 0
        || maximum_chunks_per_cycle > crate::ARACHNE_MAX_ATTACHMENT_CHUNKS_PER_CYCLE
    {
        return std::ptr::null_mut();
    }
    let manifest = unsafe { std::slice::from_raw_parts(manifest, manifest_length) };
    let Ok(manifest) = SdkAttachmentManifest::from_encoded(manifest) else {
        return std::ptr::null_mut();
    };
    let chunks = unsafe { std::slice::from_raw_parts(chunks, chunk_count) };
    let mut attachment_chunks = Vec::with_capacity(chunk_count);
    for chunk in chunks {
        if chunk.data.is_null()
            || chunk.length == 0
            || chunk.length > crate::ARACHNE_MAX_ATTACHMENT_CHUNK_BYTES
        {
            return std::ptr::null_mut();
        }
        let encoded = unsafe { std::slice::from_raw_parts(chunk.data, chunk.length) };
        let Ok(chunk) = SdkAttachmentChunk::from_encoded(encoded) else {
            return std::ptr::null_mut();
        };
        attachment_chunks.push(chunk);
    }
    let Ok(transfer) =
        SdkAttachmentTransfer::new(manifest, attachment_chunks, maximum_chunks_per_cycle)
    else {
        return std::ptr::null_mut();
    };
    let Ok(mut transfers) = active_attachment_transfers().lock() else {
        return std::ptr::null_mut();
    };
    if transfers.len() >= MAX_C_ABI_ATTACHMENT_TRANSFERS {
        return std::ptr::null_mut();
    }
    let Some(identifier) = next_handle_identifier() else {
        return std::ptr::null_mut();
    };
    transfers.insert(identifier, AttachmentTransferHandle::Ready(transfer));
    std::ptr::without_provenance_mut(identifier)
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_attachment_transfer_run_cycle(
    transfer: *mut ArachneAttachmentTransfer,
    upload: Option<ArachneAttachmentUploadCallback>,
    context: *mut c_void,
    cycle: *mut ArachneAttachmentDeliveryCycle,
) -> ArachneStatus {
    if cycle.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { cycle.write(ArachneAttachmentDeliveryCycle::default()) };
    let Some(upload) = upload else {
        return ArachneStatus::InvalidInput;
    };
    let identifier = transfer.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let transfer = {
        let Ok(mut transfers) = active_attachment_transfers().lock() else {
            return ArachneStatus::State;
        };
        let Some(transfer) = transfers.get_mut(&identifier) else {
            return ArachneStatus::InvalidInput;
        };
        match std::mem::replace(transfer, AttachmentTransferHandle::Running) {
            AttachmentTransferHandle::Ready(transfer) => transfer,
            AttachmentTransferHandle::Running => return ArachneStatus::State,
        }
    };
    let Ok(runtime) = tokio::runtime::Builder::new_current_thread().build() else {
        return restore_attachment_transfer(identifier, transfer);
    };
    let mut transport = CAttachmentTransport {
        callback: upload,
        context,
    };
    let mut transfer = transfer;
    let result = runtime.block_on(transfer.run_cycle(&mut transport));
    let status = result.map_or(ArachneStatus::State, |delivery_cycle| {
        unsafe { cycle.write(c_attachment_delivery_cycle(delivery_cycle)) };
        ArachneStatus::Ok
    });
    let restore_status = restore_attachment_transfer(identifier, transfer);
    if restore_status != ArachneStatus::Ok {
        unsafe { cycle.write(ArachneAttachmentDeliveryCycle::default()) };
        return restore_status;
    }
    status
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_attachment_transfer_release(
    transfer: *mut ArachneAttachmentTransfer,
) -> ArachneStatus {
    let identifier = transfer.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut transfers) = active_attachment_transfers().lock() else {
        return ArachneStatus::State;
    };
    match transfers.get(&identifier) {
        None => ArachneStatus::InvalidInput,
        Some(AttachmentTransferHandle::Running) => ArachneStatus::State,
        Some(AttachmentTransferHandle::Ready(_)) => {
            transfers.remove(&identifier);
            ArachneStatus::Ok
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_cancellation_create() -> *mut ArachneCancellation {
    let Ok(mut cancellations) = active_cancellations().lock() else {
        return std::ptr::null_mut();
    };
    if cancellations.len() >= MAX_C_ABI_CANCELLATIONS {
        return std::ptr::null_mut();
    }
    let Some(identifier) = next_handle_identifier() else {
        return std::ptr::null_mut();
    };
    cancellations.insert(identifier, CancellationToken::new());
    std::ptr::without_provenance_mut(identifier)
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_cancellation_cancel(
    cancellation: *mut ArachneCancellation,
) -> ArachneStatus {
    let identifier = cancellation.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(cancellations) = active_cancellations().lock() else {
        return ArachneStatus::State;
    };
    let Some(cancellation) = cancellations.get(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    cancellation.cancel();
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_cancellation_release(
    cancellation: *mut ArachneCancellation,
) -> ArachneStatus {
    let identifier = cancellation.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut cancellations) = active_cancellations().lock() else {
        return ArachneStatus::State;
    };
    if cancellations.remove(&identifier).is_none() {
        return ArachneStatus::InvalidInput;
    }
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_copy_last_error_detail(
    client: *const ArachneClient,
    buffer: *mut u8,
    buffer_capacity: usize,
    detail_length: *mut usize,
) -> ArachneStatus {
    if detail_length.is_null() || (buffer.is_null() && buffer_capacity != 0) {
        return ArachneStatus::InvalidInput;
    }
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(detail) = client.last_error_detail else {
        return ArachneStatus::State;
    };
    unsafe { detail_length.write(detail.len()) };
    if buffer_capacity < detail.len() {
        return ArachneStatus::ResourceLimit;
    }
    unsafe { std::ptr::copy_nonoverlapping(detail.as_ptr(), buffer, detail.len()) };
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_take_last_error_detail(
    client: *mut ArachneClient,
    detail: *mut *mut ArachneBuffer,
) -> ArachneStatus {
    if detail.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe { detail.write(std::ptr::null_mut()) };
    let identifier = client.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let Some(last_error_detail) = client.last_error_detail else {
        return ArachneStatus::State;
    };
    let buffer = match allocate_buffer(last_error_detail) {
        Ok(buffer) => buffer,
        Err(status) => return status,
    };
    client.last_error_detail = None;
    unsafe { detail.write(buffer) };
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_buffer_data(buffer: *const ArachneBuffer) -> *const u8 {
    let identifier = buffer.addr();
    if identifier == 0 {
        return std::ptr::null();
    }
    let Ok(buffers) = active_buffers().lock() else {
        return std::ptr::null();
    };
    let Some(buffer) = buffers.get(&identifier) else {
        return std::ptr::null();
    };
    buffer.as_ptr()
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_buffer_length(buffer: *const ArachneBuffer) -> usize {
    let identifier = buffer.addr();
    if identifier == 0 {
        return 0;
    }
    let Ok(buffers) = active_buffers().lock() else {
        return 0;
    };
    buffers.get(&identifier).map_or(0, |buffer| buffer.len())
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_buffer_release(buffer: *mut ArachneBuffer) -> ArachneStatus {
    let identifier = buffer.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut buffers) = active_buffers().lock() else {
        return ArachneStatus::State;
    };
    if buffers.remove(&identifier).is_none() {
        return ArachneStatus::InvalidInput;
    }
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_subscribe_events(
    client: *const ArachneClient,
) -> *mut ArachneEventSubscription {
    let identifier = client.addr();
    if identifier == 0 {
        return std::ptr::null_mut();
    }
    let Ok(clients) = active_clients().lock() else {
        return std::ptr::null_mut();
    };
    let Some(client) = clients.get(&identifier) else {
        return std::ptr::null_mut();
    };
    let Some(runtime) = client.runtime.as_ref() else {
        return std::ptr::null_mut();
    };
    let subscription = runtime.subscribe();
    let Ok(mut subscriptions) = active_event_subscriptions().lock() else {
        return std::ptr::null_mut();
    };
    if subscriptions.len() >= MAX_C_ABI_EVENT_SUBSCRIPTIONS {
        return std::ptr::null_mut();
    }
    let Some(subscription_identifier) = next_handle_identifier() else {
        return std::ptr::null_mut();
    };
    if subscriptions
        .insert(subscription_identifier, subscription)
        .is_some()
    {
        return std::ptr::null_mut();
    }
    std::ptr::without_provenance_mut(subscription_identifier)
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_event_subscription_release(
    subscription: *mut ArachneEventSubscription,
) -> ArachneStatus {
    let identifier = subscription.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut subscriptions) = active_event_subscriptions().lock() else {
        return ArachneStatus::State;
    };
    if subscriptions.remove(&identifier).is_none() {
        return ArachneStatus::InvalidInput;
    }
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_event_subscription_poll(
    subscription: *mut ArachneEventSubscription,
    event: *mut ArachneEvent,
    has_event: *mut u8,
) -> ArachneStatus {
    if event.is_null() || has_event.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe {
        std::ptr::write_bytes(event.cast::<u8>(), 0, std::mem::size_of::<ArachneEvent>());
        has_event.write(0);
    }
    let identifier = subscription.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut subscriptions) = active_event_subscriptions().lock() else {
        return ArachneStatus::State;
    };
    let Some(subscription) = subscriptions.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    match subscription.try_next() {
        Ok(None) => ArachneStatus::Ok,
        Ok(Some(envelope)) => {
            let c_event = c_event(envelope);
            unsafe {
                std::ptr::addr_of_mut!((*event).version).write(c_event.version);
                std::ptr::addr_of_mut!((*event).sequence).write(c_event.sequence);
                std::ptr::addr_of_mut!((*event).kind).write(c_event.kind);
                std::ptr::addr_of_mut!((*event).message_identifier)
                    .write(c_event.message_identifier);
                has_event.write(1);
            }
            ArachneStatus::Ok
        }
        Err(SdkEventStreamError::Lagged(_)) => ArachneStatus::ResourceLimit,
        Err(
            SdkEventStreamError::Cancelled
            | SdkEventStreamError::DeadlineExceeded
            | SdkEventStreamError::Closed,
        ) => ArachneStatus::State,
    }
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_event_subscription_wait(
    subscription: *mut ArachneEventSubscription,
    cancellation: *mut ArachneCancellation,
    deadline_milliseconds: u32,
    event: *mut ArachneEvent,
    has_event: *mut u8,
) -> ArachneStatus {
    if event.is_null() || has_event.is_null() {
        return ArachneStatus::InvalidInput;
    }
    unsafe {
        std::ptr::write_bytes(event.cast::<u8>(), 0, std::mem::size_of::<ArachneEvent>());
        has_event.write(0);
    }
    if deadline_milliseconds == 0
        || deadline_milliseconds > crate::ARACHNE_MAX_CANCELLATION_DEADLINE_MILLISECONDS
    {
        return ArachneStatus::InvalidInput;
    }
    let cancellation_identifier = cancellation.addr();
    if cancellation_identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let cancellation = {
        let Ok(cancellations) = active_cancellations().lock() else {
            return ArachneStatus::State;
        };
        let Some(cancellation) = cancellations.get(&cancellation_identifier) else {
            return ArachneStatus::InvalidInput;
        };
        cancellation.clone()
    };
    let Ok(policy) = SdkAsyncPolicy::new(Duration::from_millis(u64::from(deadline_milliseconds)))
    else {
        return ArachneStatus::InvalidInput;
    };
    let subscription_identifier = subscription.addr();
    if subscription_identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(runtime) = tokio::runtime::Builder::new_current_thread()
        .enable_time()
        .build()
    else {
        return ArachneStatus::State;
    };
    let Ok(mut subscriptions) = active_event_subscriptions().lock() else {
        return ArachneStatus::State;
    };
    let Some(subscription) = subscriptions.get_mut(&subscription_identifier) else {
        return ArachneStatus::InvalidInput;
    };
    match runtime.block_on(subscription.next_with_policy(policy, &cancellation)) {
        Ok(envelope) => {
            let c_event = c_event(envelope);
            unsafe {
                event.write(c_event);
                has_event.write(1);
            }
            ArachneStatus::Ok
        }
        Err(SdkEventStreamError::Lagged(_)) => ArachneStatus::ResourceLimit,
        Err(
            SdkEventStreamError::Cancelled
            | SdkEventStreamError::DeadlineExceeded
            | SdkEventStreamError::Closed,
        ) => ArachneStatus::State,
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_complete_async(
    client: *const ArachneClient,
    callback: Option<ArachneCompletionCallback>,
    context: *mut c_void,
) -> ArachneStatus {
    let Some(callback) = callback else {
        return ArachneStatus::InvalidInput;
    };
    match is_active_client(client) {
        Ok(true) => {}
        Ok(false) => return ArachneStatus::InvalidInput,
        Err(status) => return status,
    }
    let Ok(_) = PENDING_COMPLETIONS.fetch_update(Ordering::AcqRel, Ordering::Acquire, |pending| {
        (pending < MAX_C_ABI_PENDING_COMPLETIONS).then_some(pending + 1)
    }) else {
        return ArachneStatus::ResourceLimit;
    };
    let context = context.expose_provenance();
    let Some(queue) = callback_queue() else {
        PENDING_COMPLETIONS.fetch_sub(1, Ordering::AcqRel);
        return ArachneStatus::ResourceLimit;
    };
    match queue.try_send(PendingCompletion { callback, context }) {
        Ok(()) => ArachneStatus::Ok,
        Err(TrySendError::Full(_) | TrySendError::Disconnected(_)) => {
            PENDING_COMPLETIONS.fetch_sub(1, Ordering::AcqRel);
            ArachneStatus::ResourceLimit
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_config_builder_create() -> *mut ArachneClientConfigBuilder {
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return std::ptr::null_mut();
    };
    if builders.len() >= MAX_C_ABI_CLIENT_CONFIG_BUILDERS {
        return std::ptr::null_mut();
    }
    let Some(identifier) = next_handle_identifier() else {
        return std::ptr::null_mut();
    };
    if builders
        .insert(identifier, ClientConfigBuilder::default())
        .is_some()
    {
        return std::ptr::null_mut();
    }
    std::ptr::without_provenance_mut(identifier)
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_config_builder_release(
    builder: *mut ArachneClientConfigBuilder,
) -> ArachneStatus {
    let identifier = builder.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return ArachneStatus::State;
    };
    if builders.remove(&identifier).is_none() {
        return ArachneStatus::InvalidInput;
    }
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn arachne_client_config_builder_set_state_directory(
    builder: *mut ArachneClientConfigBuilder,
    state_directory: *const u8,
    state_directory_length: usize,
) -> ArachneStatus {
    if state_directory.is_null()
        || state_directory_length == 0
        || state_directory_length > MAX_C_ABI_STATE_DIRECTORY_BYTES
    {
        return ArachneStatus::InvalidInput;
    }
    let bytes = unsafe { std::slice::from_raw_parts(state_directory, state_directory_length) };
    let Ok(state_directory) = std::str::from_utf8(bytes) else {
        return ArachneStatus::InvalidInput;
    };
    if state_directory.contains('\0') {
        return ArachneStatus::InvalidInput;
    }
    let identifier = builder.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return ArachneStatus::State;
    };
    let Some(builder) = builders.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    builder.state_directory = Some(PathBuf::from(state_directory));
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_config_builder_set_event_buffer_capacity(
    builder: *mut ArachneClientConfigBuilder,
    event_buffer_capacity: u32,
) -> ArachneStatus {
    let identifier = builder.addr();
    if identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return ArachneStatus::State;
    };
    let Some(builder) = builders.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    builder.event_buffer_capacity = Some(event_buffer_capacity as usize);
    ArachneStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn arachne_client_config_builder_build(
    builder: *const ArachneClientConfigBuilder,
    client: *const ArachneClient,
) -> ArachneStatus {
    let builder_identifier = builder.addr();
    let client_identifier = client.addr();
    if builder_identifier == 0 || client_identifier == 0 {
        return ArachneStatus::InvalidInput;
    }
    let Ok(builders) = active_client_config_builders().lock() else {
        return ArachneStatus::State;
    };
    let Some(builder) = builders.get(&builder_identifier) else {
        return ArachneStatus::InvalidInput;
    };
    let (Some(state_directory), Some(event_buffer_capacity)) = (
        builder.state_directory.clone(),
        builder.event_buffer_capacity,
    ) else {
        return ArachneStatus::InvalidInput;
    };
    drop(builders);
    let Ok(configuration) = SdkConfig::new(
        state_directory,
        RuntimeMode::Embedded,
        event_buffer_capacity,
    ) else {
        return ArachneStatus::InvalidInput;
    };
    let Ok(mut clients) = active_clients().lock() else {
        return ArachneStatus::State;
    };
    let Some(client) = clients.get_mut(&client_identifier) else {
        return ArachneStatus::InvalidInput;
    };
    if client.runtime.is_some() {
        return ArachneStatus::State;
    }
    client.configuration = Some(configuration);
    ArachneStatus::Ok
}

fn active_clients() -> &'static Mutex<BTreeMap<usize, ClientHandle>> {
    ACTIVE_CLIENTS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_attachment_transfers() -> &'static Mutex<BTreeMap<usize, AttachmentTransferHandle>> {
    ACTIVE_ATTACHMENT_TRANSFERS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_cancellations() -> &'static Mutex<BTreeMap<usize, CancellationToken>> {
    ACTIVE_CANCELLATIONS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_client_config_builders() -> &'static Mutex<BTreeMap<usize, ClientConfigBuilder>> {
    ACTIVE_CLIENT_CONFIG_BUILDERS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_buffers() -> &'static Mutex<BTreeMap<usize, Zeroizing<Vec<u8>>>> {
    ACTIVE_BUFFERS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_event_subscriptions() -> &'static Mutex<BTreeMap<usize, SdkEventStream>> {
    ACTIVE_EVENT_SUBSCRIPTIONS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn next_handle_identifier() -> Option<usize> {
    NEXT_HANDLE_IDENTIFIER
        .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |identifier| {
            identifier.checked_add(1)
        })
        .ok()
}

fn c_contact(contact: SdkContact) -> ArachneContact {
    let status = match contact.status() {
        SdkContactStatus::Pending => crate::ARACHNE_CONTACT_STATUS_PENDING,
        SdkContactStatus::Verified => crate::ARACHNE_CONTACT_STATUS_VERIFIED,
        SdkContactStatus::Revoked => crate::ARACHNE_CONTACT_STATUS_REVOKED,
    };
    let verification = match contact.verification_method() {
        None => crate::ARACHNE_CONTACT_VERIFICATION_NONE,
        Some(SdkContactVerificationMethod::Qr) => crate::ARACHNE_CONTACT_VERIFICATION_QR,
        Some(SdkContactVerificationMethod::SafetyNumber) => {
            crate::ARACHNE_CONTACT_VERIFICATION_SAFETY_NUMBER
        }
    };
    ArachneContact {
        identity: *contact.identity().as_bytes(),
        status,
        verification,
    }
}

fn restore_attachment_transfer(
    identifier: usize,
    transfer: SdkAttachmentTransfer,
) -> ArachneStatus {
    let Ok(mut transfers) = active_attachment_transfers().lock() else {
        return ArachneStatus::State;
    };
    let Some(state) = transfers.get_mut(&identifier) else {
        return ArachneStatus::InvalidInput;
    };
    if !matches!(state, AttachmentTransferHandle::Running) {
        return ArachneStatus::State;
    }
    *state = AttachmentTransferHandle::Ready(transfer);
    ArachneStatus::Ok
}

fn c_attachment_delivery_cycle(
    cycle: SdkAttachmentDeliveryCycle,
) -> ArachneAttachmentDeliveryCycle {
    let (outcome, next_pending_index) = match cycle.outcome() {
        SdkAttachmentDeliveryOutcome::Complete => (crate::ARACHNE_ATTACHMENT_DELIVERY_COMPLETE, 0),
        SdkAttachmentDeliveryOutcome::Pending(index) => {
            (crate::ARACHNE_ATTACHMENT_DELIVERY_PENDING, index)
        }
        SdkAttachmentDeliveryOutcome::Retrying(index) => {
            (crate::ARACHNE_ATTACHMENT_DELIVERY_RETRYING, index)
        }
    };
    ArachneAttachmentDeliveryCycle {
        uploaded: u32::try_from(cycle.uploaded())
            .expect("attachment cycle upload count is bounded"),
        outcome,
        next_pending_index,
    }
}

fn c_delivery_profile_policy(
    policy: ArachneDeliveryProfilePolicy,
) -> Result<SdkDeliveryProfilePolicy, ArachneStatus> {
    let Some(direct_allowed) = c_boolean(policy.direct_allowed) else {
        return Err(ArachneStatus::InvalidInput);
    };
    let Some(tor_maildrop_allowed) = c_boolean(policy.tor_maildrop_allowed) else {
        return Err(ArachneStatus::InvalidInput);
    };
    let Ok(local_mesh_transport_count) = usize::try_from(policy.local_mesh_transport_count) else {
        return Err(ArachneStatus::InvalidInput);
    };
    if local_mesh_transport_count > crate::ARACHNE_MAX_LOCAL_MESH_TRANSPORTS {
        return Err(ArachneStatus::InvalidInput);
    }
    let mut local_mesh_transports =
        [SdkLocalMeshTransportKind::Lan; crate::ARACHNE_MAX_LOCAL_MESH_TRANSPORTS];
    for (transport, value) in local_mesh_transports
        .iter_mut()
        .zip(&policy.local_mesh_transports[..local_mesh_transport_count])
    {
        let Some(value) = c_local_mesh_transport(*value) else {
            return Err(ArachneStatus::InvalidInput);
        };
        *transport = value;
    }
    let local_mesh = if local_mesh_transport_count == 0 {
        None
    } else {
        match SdkLocalMeshPolicy::new(&local_mesh_transports[..local_mesh_transport_count]) {
            Ok(policy) => Some(policy),
            Err(error) => return Err(map_sdk_delivery_profile_policy_error(error)),
        }
    };
    SdkDeliveryProfilePolicy::new(direct_allowed, tor_maildrop_allowed, local_mesh)
        .map_err(map_sdk_delivery_profile_policy_error)
}

const fn c_boolean(value: u32) -> Option<bool> {
    match value {
        0 => Some(false),
        1 => Some(true),
        _ => None,
    }
}

const fn c_local_mesh_transport(value: u32) -> Option<SdkLocalMeshTransportKind> {
    match value {
        crate::ARACHNE_LOCAL_MESH_TRANSPORT_LAN => Some(SdkLocalMeshTransportKind::Lan),
        crate::ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT => {
            Some(SdkLocalMeshTransportKind::WifiHotspot)
        }
        crate::ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_DIRECT => {
            Some(SdkLocalMeshTransportKind::WifiDirect)
        }
        crate::ARACHNE_LOCAL_MESH_TRANSPORT_BLUETOOTH => Some(SdkLocalMeshTransportKind::Bluetooth),
        _ => None,
    }
}

const fn c_delivery_profile(profile: SdkDeliveryProfile) -> ArachneDeliveryProfile {
    let kind = match profile.kind() {
        SdkDeliveryProfileKind::Direct => crate::ARACHNE_DELIVERY_PROFILE_DIRECT,
        SdkDeliveryProfileKind::TorMaildrop => crate::ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP,
        SdkDeliveryProfileKind::LocalMesh => crate::ARACHNE_DELIVERY_PROFILE_LOCAL_MESH,
    };
    let direct_ip_disclosure_warning = if profile.has_direct_ip_disclosure_warning() {
        crate::ARACHNE_DIRECT_IP_DISCLOSURE_WARNING
    } else {
        0
    };
    ArachneDeliveryProfile {
        kind,
        direct_ip_disclosure_warning,
    }
}

unsafe fn c_identity_public_key(identity: *const u8) -> Option<IdentityPublicKey> {
    if identity.is_null() {
        return None;
    }
    let mut bytes = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];
    unsafe {
        std::ptr::copy_nonoverlapping(
            identity,
            bytes.as_mut_ptr(),
            arachne_core::ED25519_PUBLIC_KEY_BYTES,
        );
    };
    IdentityPublicKey::from_bytes(bytes).ok()
}

fn c_event(envelope: SdkEventEnvelope) -> ArachneEvent {
    let (kind, message_identifier) = match envelope.event() {
        SdkEvent::ClientStarted => (
            ARACHNE_EVENT_CLIENT_STARTED,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES],
        ),
        SdkEvent::ClientStopped => (
            ARACHNE_EVENT_CLIENT_STOPPED,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES],
        ),
        SdkEvent::MessageQueued(identifier) => {
            (ARACHNE_EVENT_MESSAGE_QUEUED, *identifier.as_bytes())
        }
        SdkEvent::MessageDelivered(identifier) => {
            (ARACHNE_EVENT_MESSAGE_DELIVERED, *identifier.as_bytes())
        }
        SdkEvent::MessageDeliveryFailed(identifier) => (
            ARACHNE_EVENT_MESSAGE_DELIVERY_FAILED,
            *identifier.as_bytes(),
        ),
    };
    ArachneEvent {
        version: envelope.version(),
        sequence: envelope.sequence(),
        kind,
        message_identifier,
    }
}

fn allocate_buffer(bytes: &[u8]) -> Result<*mut ArachneBuffer, ArachneStatus> {
    let active_buffers = active_buffers();
    let mut buffers = active_buffers.lock().map_err(|_| ArachneStatus::State)?;
    if buffers.len() >= MAX_C_ABI_BUFFERS {
        return Err(ArachneStatus::ResourceLimit);
    }
    let identifier = next_handle_identifier().ok_or(ArachneStatus::ResourceLimit)?;
    if buffers
        .insert(identifier, Zeroizing::new(bytes.to_vec()))
        .is_some()
    {
        return Err(ArachneStatus::ResourceLimit);
    }
    let buffer = std::ptr::without_provenance_mut(identifier);
    drop(buffers);
    Ok(buffer)
}

fn callback_queue() -> Option<&'static SyncSender<PendingCompletion>> {
    CALLBACK_QUEUE
        .get_or_init(|| {
            let (sender, receiver) = mpsc::sync_channel(MAX_C_ABI_PENDING_COMPLETIONS);
            let receiver = Arc::new(Mutex::new(receiver));
            for _ in 0..MAX_C_ABI_CALLBACK_WORKERS {
                let receiver = Arc::clone(&receiver);
                if std::thread::Builder::new()
                    .spawn(move || {
                        loop {
                            let completion = match receiver.lock() {
                                Ok(receiver) => receiver.recv(),
                                Err(_) => return,
                            };
                            let Ok(PendingCompletion { callback, context }) = completion else {
                                return;
                            };
                            callback(
                                ArachneStatus::Ok as i32,
                                std::ptr::with_exposed_provenance_mut(context),
                            );
                            PENDING_COMPLETIONS.fetch_sub(1, Ordering::AcqRel);
                        }
                    })
                    .is_err()
                {
                    return None;
                }
            }
            Some(sender)
        })
        .as_ref()
}

fn is_active_client(client: *const ArachneClient) -> Result<bool, ArachneStatus> {
    let identifier = client.addr();
    if identifier == 0 {
        return Ok(false);
    }
    let clients = active_clients().lock().map_err(|_| ArachneStatus::State)?;
    Ok(clients.contains_key(&identifier))
}

fn map_sdk_client_error(error: &SdkClientError) -> ArachneStatus {
    match error {
        SdkClientError::Configuration(_) => ArachneStatus::InvalidInput,
        SdkClientError::EventSequenceExhausted => ArachneStatus::ResourceLimit,
        SdkClientError::DaemonModeUnavailable
        | SdkClientError::AlreadyRunning
        | SdkClientError::NotRunning
        | SdkClientError::State
        | SdkClientError::Engine
        | SdkClientError::AsyncTask => ArachneStatus::State,
    }
}

fn sdk_client_error_detail(error: &SdkClientError) -> &'static [u8] {
    match error {
        SdkClientError::Configuration(_) => b"sdk_configuration",
        SdkClientError::DaemonModeUnavailable => b"sdk_daemon_mode_unavailable",
        SdkClientError::AlreadyRunning => b"sdk_already_running",
        SdkClientError::NotRunning => b"sdk_not_running",
        SdkClientError::State => b"sdk_state",
        SdkClientError::Engine => b"sdk_engine",
        SdkClientError::EventSequenceExhausted => b"sdk_event_sequence_exhausted",
        SdkClientError::AsyncTask => b"sdk_async_task",
    }
}

fn map_sdk_identity_error(error: &SdkIdentityError) -> ArachneStatus {
    match error {
        SdkIdentityError::AlreadyInitialized
        | SdkIdentityError::NotInitialized
        | SdkIdentityError::Generation
        | SdkIdentityError::InvalidStoredIdentity
        | SdkIdentityError::Keystore => ArachneStatus::State,
    }
}

fn sdk_identity_error_detail(error: &SdkIdentityError) -> &'static [u8] {
    match error {
        SdkIdentityError::AlreadyInitialized => b"sdk_identity_already_initialized",
        SdkIdentityError::NotInitialized => b"sdk_identity_not_initialized",
        SdkIdentityError::Generation => b"sdk_identity_generation",
        SdkIdentityError::InvalidStoredIdentity => b"sdk_identity_invalid_stored",
        SdkIdentityError::Keystore => b"sdk_identity_keystore",
    }
}

const fn map_sdk_recovery_error(error: &SdkRecoveryError) -> ArachneStatus {
    match error {
        SdkRecoveryError::InvalidArchive => ArachneStatus::InvalidInput,
        SdkRecoveryError::Identity(_) | SdkRecoveryError::Operation => ArachneStatus::State,
    }
}

const fn sdk_recovery_error_detail(error: &SdkRecoveryError) -> &'static [u8] {
    match error {
        SdkRecoveryError::Identity(_) => b"sdk_recovery_identity",
        SdkRecoveryError::InvalidArchive => b"sdk_recovery_invalid_archive",
        SdkRecoveryError::Operation => b"sdk_recovery_operation",
    }
}

fn map_sdk_contact_error(error: &SdkContactError) -> ArachneStatus {
    match error {
        SdkContactError::InvalidInvitation
        | SdkContactError::InvalidRotation
        | SdkContactError::InvalidVerification => ArachneStatus::InvalidInput,
        SdkContactError::Identity(_)
        | SdkContactError::SelfContact
        | SdkContactError::UnknownContact
        | SdkContactError::NotPending
        | SdkContactError::NotVerified
        | SdkContactError::ReplacementAlreadyKnown
        | SdkContactError::AlreadyRevoked
        | SdkContactError::State => ArachneStatus::State,
    }
}

const fn map_sdk_delivery_profile_policy_error(
    error: SdkDeliveryProfilePolicyError,
) -> ArachneStatus {
    match error {
        SdkDeliveryProfilePolicyError::NoAllowedProfiles
        | SdkDeliveryProfilePolicyError::NoAllowedLocalMeshTransports
        | SdkDeliveryProfilePolicyError::TooManyLocalMeshTransports => ArachneStatus::InvalidInput,
        SdkDeliveryProfilePolicyError::DirectDisallowed
        | SdkDeliveryProfilePolicyError::TorMaildropDisallowed
        | SdkDeliveryProfilePolicyError::LocalMeshDisallowed
        | SdkDeliveryProfilePolicyError::LocalMeshTransportDisallowed
        | SdkDeliveryProfilePolicyError::DirectToTorRequiresExplicitSelection => {
            ArachneStatus::State
        }
    }
}

const fn map_sdk_message_error(error: &SdkMessageError) -> ArachneStatus {
    match error {
        SdkMessageError::EventSequenceExhausted | SdkMessageError::QueueFull => {
            ArachneStatus::ResourceLimit
        }
        SdkMessageError::Identity(_)
        | SdkMessageError::ClientNotRunning
        | SdkMessageError::State => ArachneStatus::State,
    }
}

const fn sdk_message_error_detail(error: &SdkMessageError) -> &'static [u8] {
    match error {
        SdkMessageError::Identity(_) => b"sdk_message_identity",
        SdkMessageError::ClientNotRunning => b"sdk_message_client_not_running",
        SdkMessageError::EventSequenceExhausted => b"sdk_message_event_sequence_exhausted",
        SdkMessageError::QueueFull => b"sdk_message_queue_full",
        SdkMessageError::State => b"sdk_message_state",
    }
}

fn sdk_contact_error_detail(error: &SdkContactError) -> &'static [u8] {
    match error {
        SdkContactError::Identity(_) => b"sdk_contact_identity",
        SdkContactError::InvalidInvitation => b"sdk_contact_invalid_invitation",
        SdkContactError::InvalidRotation => b"sdk_contact_invalid_rotation",
        SdkContactError::SelfContact => b"sdk_contact_self",
        SdkContactError::UnknownContact => b"sdk_contact_unknown",
        SdkContactError::NotPending => b"sdk_contact_not_pending",
        SdkContactError::NotVerified => b"sdk_contact_not_verified",
        SdkContactError::ReplacementAlreadyKnown => b"sdk_contact_replacement_known",
        SdkContactError::AlreadyRevoked => b"sdk_contact_already_revoked",
        SdkContactError::InvalidVerification => b"sdk_contact_invalid_verification",
        SdkContactError::State => b"sdk_contact_state",
    }
}

#[cfg(test)]
mod tests {
    use std::{
        ffi::c_void,
        sync::atomic::{AtomicUsize, Ordering},
    };

    use arachne_core::{IdentityKeypair, IdentityPublicKey};
    use arachne_protocol::{
        ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, AttachmentManifest,
        ContactInvitation, EncryptedAttachmentChunk, EncryptedMessageEnvelope,
        QrVerificationPayload, SafetyNumberFingerprint,
    };

    use super::{
        ArachneAttachmentDeliveryCycle, ArachneByteSlice, ArachneContact, ArachneDeliveryProfile,
        ArachneDeliveryProfilePolicy, ArachneEvent, ArachneStatus, CLIENT_TEST_LOCK,
        MAX_C_ABI_BUFFERS, MAX_C_ABI_CALLBACK_WORKERS, MAX_C_ABI_CANCELLATIONS,
        MAX_C_ABI_ERROR_DETAIL_BYTES, MAX_C_ABI_EVENT_SUBSCRIPTIONS, MAX_C_ABI_PENDING_COMPLETIONS,
        MAX_C_ABI_SECRET_BUFFER_BYTES, MAX_C_ABI_STATE_DIRECTORY_BYTES, PENDING_COMPLETIONS,
        SdkClientError, SdkEvent, SdkEventEnvelope, arachne_attachment_transfer_create,
        arachne_attachment_transfer_release, arachne_attachment_transfer_run_cycle,
        arachne_buffer_data, arachne_buffer_length, arachne_buffer_release,
        arachne_cancellation_cancel, arachne_cancellation_create, arachne_cancellation_release,
        arachne_client_complete_async, arachne_client_config_builder_build,
        arachne_client_config_builder_create, arachne_client_config_builder_release,
        arachne_client_config_builder_set_event_buffer_capacity,
        arachne_client_config_builder_set_state_directory, arachne_client_contact_get,
        arachne_client_contact_import, arachne_client_contact_revoke,
        arachne_client_contact_verify_qr, arachne_client_contact_verify_safety_number,
        arachne_client_copy_last_error_detail, arachne_client_create,
        arachne_client_identity_create, arachne_client_identity_export_recovery,
        arachne_client_identity_import_recovery, arachne_client_identity_load,
        arachne_client_message_send, arachne_client_release, arachne_client_start,
        arachne_client_stop, arachne_client_subscribe_events,
        arachne_client_take_last_error_detail, arachne_delivery_profile_select,
        arachne_event_subscription_poll, arachne_event_subscription_release,
        arachne_event_subscription_wait, arachne_secret_buffer_zeroize, c_event,
        map_sdk_client_error, sdk_client_error_detail,
    };

    #[test]
    fn zeroizes_bounded_caller_owned_secret_buffers() {
        let mut secret = [0xA5; 32];
        assert_eq!(
            unsafe { arachne_secret_buffer_zeroize(secret.as_mut_ptr(), secret.len()) },
            ArachneStatus::Ok
        );
        assert_eq!(secret, [0; 32]);
    }

    #[test]
    fn zeroization_rejects_invalid_and_oversized_buffers() {
        let mut byte = 0xA5;
        assert_eq!(
            unsafe { arachne_secret_buffer_zeroize(std::ptr::null_mut(), 1) },
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            unsafe { arachne_secret_buffer_zeroize(&raw mut byte, 0) },
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            unsafe {
                arachne_secret_buffer_zeroize(&raw mut byte, MAX_C_ABI_SECRET_BUFFER_BYTES + 1)
            },
            ArachneStatus::ResourceLimit
        );
        assert_eq!(byte, 0xA5);
    }

    #[test]
    fn c_identity_operations_create_and_load_one_redacted_handle_identity() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = arachne_client_create();
        let mut public_key = [0xA5; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let mut loaded_key = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];

        assert!(!client.is_null());

        assert_eq!(
            unsafe { arachne_client_identity_load(client, public_key.as_mut_ptr()) },
            ArachneStatus::State
        );
        assert_eq!(public_key, [0; arachne_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe { arachne_client_identity_create(client, public_key.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_ne!(public_key, [0; arachne_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe { arachne_client_identity_load(client, loaded_key.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_eq!(loaded_key, public_key);
        assert_eq!(
            unsafe { arachne_client_identity_create(client, loaded_key.as_mut_ptr()) },
            ArachneStatus::State
        );
        assert_eq!(loaded_key, [0; arachne_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe { arachne_client_identity_load(client, std::ptr::null_mut()) },
            ArachneStatus::InvalidInput
        );
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
    }

    fn export_c_recovery(client: *mut crate::ArachneClient, passphrase: &[u8]) -> Vec<u8> {
        let mut archive = std::ptr::null_mut();
        assert_eq!(
            unsafe {
                arachne_client_identity_export_recovery(
                    client,
                    passphrase.as_ptr(),
                    passphrase.len(),
                    &raw mut archive,
                )
            },
            ArachneStatus::Ok
        );
        assert!(!archive.is_null());
        assert_eq!(
            arachne_buffer_length(archive),
            arachne_protocol::IDENTITY_EXPORT_BYTES
        );
        let archive_bytes = unsafe {
            std::slice::from_raw_parts(arachne_buffer_data(archive), arachne_buffer_length(archive))
        }
        .to_vec();
        assert_eq!(arachne_buffer_release(archive), ArachneStatus::Ok);
        archive_bytes
    }

    #[test]
    fn c_identity_recovery_exports_imports_and_rejects_invalid_archives() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let source = arachne_client_create();
        let target = arachne_client_create();
        let passphrase = b"ffi recovery passphrase";
        let mut source_key = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let mut recovered_key = [0xA5; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let mut loaded_key = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let mut rejected_archive = std::ptr::without_provenance_mut(1);
        let too_long = vec![0xA5; arachne_sdk::MAX_SDK_RECOVERY_PASSPHRASE_BYTES + 1];

        assert!(!source.is_null());
        assert!(!target.is_null());
        assert_eq!(
            unsafe { arachne_client_identity_create(source, source_key.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_eq!(
            unsafe {
                arachne_client_identity_export_recovery(
                    source,
                    too_long.as_ptr(),
                    too_long.len(),
                    &raw mut rejected_archive,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert!(rejected_archive.is_null());
        let archive_bytes = export_c_recovery(source, passphrase);
        let mut tampered_archive = archive_bytes.clone();
        tampered_archive[0] ^= 1;

        assert_eq!(
            unsafe {
                arachne_client_identity_import_recovery(
                    target,
                    tampered_archive.as_ptr(),
                    tampered_archive.len(),
                    passphrase.as_ptr(),
                    passphrase.len(),
                    recovered_key.as_mut_ptr(),
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(recovered_key, [0; arachne_core::ED25519_PUBLIC_KEY_BYTES]);
        let mut detail = [0; MAX_C_ABI_ERROR_DETAIL_BYTES];
        let mut detail_length = 0;
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    target,
                    detail.as_mut_ptr(),
                    detail.len(),
                    &raw mut detail_length,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(&detail[..detail_length], b"sdk_recovery_invalid_archive");
        recovered_key.fill(0xA5);
        assert_eq!(
            unsafe {
                arachne_client_identity_import_recovery(
                    target,
                    archive_bytes.as_ptr(),
                    archive_bytes.len() - 1,
                    passphrase.as_ptr(),
                    passphrase.len(),
                    recovered_key.as_mut_ptr(),
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(recovered_key, [0; arachne_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe {
                arachne_client_identity_import_recovery(
                    target,
                    archive_bytes.as_ptr(),
                    archive_bytes.len(),
                    passphrase.as_ptr(),
                    passphrase.len(),
                    recovered_key.as_mut_ptr(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(recovered_key, source_key);
        assert_eq!(
            unsafe { arachne_client_identity_load(target, loaded_key.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_eq!(loaded_key, source_key);
        assert_eq!(arachne_client_release(source), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(target), ArachneStatus::Ok);
    }

    #[test]
    fn c_contact_operations_import_verify_get_and_revoke_contacts() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory =
            std::env::temp_dir().join(format!("arachne-ffi-contacts-{}", std::process::id()));
        let directory = directory.to_string_lossy().into_owned();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let mut local_identity = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let remote = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote)
            .unwrap()
            .encode()
            .unwrap();
        let mut contact = ArachneContact::default();

        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(
            unsafe { arachne_client_identity_create(client, local_identity.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::Ok);
        assert_eq!(
            unsafe { arachne_client_contact_import(client, invitation.as_ptr(), &raw mut contact) },
            ArachneStatus::Ok
        );
        assert_eq!(contact.identity, *remote.public_key().as_bytes());
        assert_eq!(contact.status, crate::ARACHNE_CONTACT_STATUS_PENDING);
        assert_eq!(
            contact.verification,
            crate::ARACHNE_CONTACT_VERIFICATION_NONE
        );
        let local_identity = IdentityPublicKey::from_bytes(local_identity).unwrap();
        let qr_payload = QrVerificationPayload::new(local_identity, remote.public_key())
            .unwrap()
            .encode()
            .unwrap();
        assert_eq!(
            unsafe {
                arachne_client_contact_verify_qr(client, qr_payload.as_ptr(), &raw mut contact)
            },
            ArachneStatus::Ok
        );
        assert_eq!(contact.status, crate::ARACHNE_CONTACT_STATUS_VERIFIED);
        assert_eq!(contact.verification, crate::ARACHNE_CONTACT_VERIFICATION_QR);
        assert_eq!(
            unsafe {
                arachne_client_contact_get(
                    client,
                    remote.public_key().as_bytes().as_ptr(),
                    &raw mut contact,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            unsafe {
                arachne_client_contact_revoke(
                    client,
                    remote.public_key().as_bytes().as_ptr(),
                    &raw mut contact,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(contact.status, crate::ARACHNE_CONTACT_STATUS_REVOKED);
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_contact_operations_verify_safety_numbers() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory =
            std::env::temp_dir().join(format!("arachne-ffi-contact-safety-{}", std::process::id()));
        let directory = directory.to_string_lossy().into_owned();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let mut local_identity = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let remote = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote)
            .unwrap()
            .encode()
            .unwrap();
        let mut contact = ArachneContact::default();

        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(
            unsafe { arachne_client_identity_create(client, local_identity.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::Ok);
        assert_eq!(
            unsafe { arachne_client_contact_import(client, invitation.as_ptr(), &raw mut contact) },
            ArachneStatus::Ok
        );
        let local_identity = IdentityPublicKey::from_bytes(local_identity).unwrap();
        let fingerprint =
            SafetyNumberFingerprint::derive(&local_identity, &remote.public_key()).unwrap();
        assert_eq!(
            unsafe {
                arachne_client_contact_verify_safety_number(
                    client,
                    remote.public_key().as_bytes().as_ptr(),
                    fingerprint.as_bytes().as_ptr(),
                    &raw mut contact,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(contact.identity, *remote.public_key().as_bytes());
        assert_eq!(contact.status, crate::ARACHNE_CONTACT_STATUS_VERIFIED);
        assert_eq!(
            contact.verification,
            crate::ARACHNE_CONTACT_VERIFICATION_SAFETY_NUMBER
        );
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_delivery_profile_operations_select_explicit_bounded_profiles() {
        let max_local_mesh_transports =
            u32::try_from(crate::ARACHNE_MAX_LOCAL_MESH_TRANSPORTS).unwrap();
        let policy = ArachneDeliveryProfilePolicy {
            direct_allowed: 1,
            tor_maildrop_allowed: 1,
            local_mesh_transport_count: max_local_mesh_transports,
            local_mesh_transports: [
                crate::ARACHNE_LOCAL_MESH_TRANSPORT_LAN,
                crate::ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT,
                crate::ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_DIRECT,
                crate::ARACHNE_LOCAL_MESH_TRANSPORT_BLUETOOTH,
            ],
        };
        let mut profile = ArachneDeliveryProfile::default();

        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    &raw const policy,
                    crate::ARACHNE_DELIVERY_PROFILE_DIRECT,
                    0,
                    crate::ARACHNE_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED,
                    &raw mut profile,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(profile.kind, crate::ARACHNE_DELIVERY_PROFILE_DIRECT);
        assert_eq!(
            profile.direct_ip_disclosure_warning,
            crate::ARACHNE_DIRECT_IP_DISCLOSURE_WARNING
        );
        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    &raw const policy,
                    crate::ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(profile.kind, crate::ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP);
        assert_eq!(profile.direct_ip_disclosure_warning, 0);
        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    &raw const policy,
                    crate::ARACHNE_DELIVERY_PROFILE_LOCAL_MESH,
                    crate::ARACHNE_LOCAL_MESH_TRANSPORT_BLUETOOTH,
                    0,
                    &raw mut profile,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(profile.kind, crate::ARACHNE_DELIVERY_PROFILE_LOCAL_MESH);
        assert_eq!(profile.direct_ip_disclosure_warning, 0);
    }

    #[test]
    fn c_delivery_profile_operations_reject_invalid_and_disallowed_selection() {
        let max_local_mesh_transports =
            u32::try_from(crate::ARACHNE_MAX_LOCAL_MESH_TRANSPORTS).unwrap();
        let mut profile = ArachneDeliveryProfile {
            kind: u32::MAX,
            direct_ip_disclosure_warning: u32::MAX,
        };
        let disallow_direct = ArachneDeliveryProfilePolicy {
            direct_allowed: 0,
            tor_maildrop_allowed: 1,
            local_mesh_transport_count: 0,
            local_mesh_transports: [0; crate::ARACHNE_MAX_LOCAL_MESH_TRANSPORTS],
        };
        let unbounded = ArachneDeliveryProfilePolicy {
            direct_allowed: 1,
            tor_maildrop_allowed: 1,
            local_mesh_transport_count: max_local_mesh_transports + 1,
            local_mesh_transports: [0; crate::ARACHNE_MAX_LOCAL_MESH_TRANSPORTS],
        };

        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    &raw const disallow_direct,
                    crate::ARACHNE_DELIVERY_PROFILE_DIRECT,
                    0,
                    crate::ARACHNE_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED,
                    &raw mut profile,
                )
            },
            ArachneStatus::State
        );
        assert_eq!(profile, ArachneDeliveryProfile::default());
        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    &raw const disallow_direct,
                    crate::ARACHNE_DELIVERY_PROFILE_DIRECT,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(profile, ArachneDeliveryProfile::default());
        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    &raw const unbounded,
                    crate::ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(profile, ArachneDeliveryProfile::default());
        assert_eq!(
            unsafe {
                arachne_delivery_profile_select(
                    std::ptr::null(),
                    crate::ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            ArachneStatus::InvalidInput
        );
    }

    #[test]
    fn c_message_send_queues_a_canonical_bounded_envelope_and_emits_an_event() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory =
            std::env::temp_dir().join(format!("arachne-ffi-message-{}", std::process::id()));
        let directory = directory.to_string_lossy().into_owned();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let mut local_identity = [0; arachne_core::ED25519_PUBLIC_KEY_BYTES];
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();
        let mut message_identifier = [0xA5; arachne_protocol::MESSAGE_IDENTIFIER_BYTES];

        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(
            unsafe { arachne_client_identity_create(client, local_identity.as_mut_ptr()) },
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::Ok);
        let subscription = arachne_client_subscribe_events(client);
        assert!(!subscription.is_null());
        assert_eq!(
            unsafe {
                arachne_client_message_send(
                    client,
                    recipient.as_bytes().as_ptr(),
                    envelope.as_ptr(),
                    envelope.len(),
                    100,
                    60,
                    message_identifier.as_mut_ptr(),
                )
            },
            ArachneStatus::Ok
        );
        assert_ne!(
            message_identifier,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        let mut event = ArachneEvent::default();
        let mut has_event = 0;
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            ArachneStatus::Ok
        );
        assert_eq!(has_event, 1);
        assert_eq!(event.kind, crate::ARACHNE_EVENT_MESSAGE_QUEUED);
        assert_eq!(event.message_identifier, message_identifier);
        assert_eq!(
            arachne_event_subscription_release(subscription),
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_message_send_rejects_invalid_and_oversized_input_after_clearing_output() {
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let byte = 0;
        let mut message_identifier = [0xA5; arachne_protocol::MESSAGE_IDENTIFIER_BYTES];
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();

        assert_eq!(
            unsafe {
                arachne_client_message_send(
                    std::ptr::null_mut(),
                    recipient.as_bytes().as_ptr(),
                    &raw const byte,
                    crate::ARACHNE_MAX_MESSAGE_ENVELOPE_BYTES + 1,
                    100,
                    60,
                    message_identifier.as_mut_ptr(),
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            message_identifier,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        message_identifier.fill(0xA5);
        assert_eq!(
            unsafe {
                arachne_client_message_send(
                    std::ptr::null_mut(),
                    recipient.as_bytes().as_ptr(),
                    envelope.as_ptr(),
                    envelope.len(),
                    u64::MAX,
                    1,
                    message_identifier.as_mut_ptr(),
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            message_identifier,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
    }

    #[test]
    fn c_message_send_redacts_missing_identity_failures() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory = std::env::temp_dir().join(format!(
            "arachne-ffi-message-missing-identity-{}",
            std::process::id()
        ));
        let directory = directory.to_string_lossy().into_owned();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();
        let mut message_identifier = [0xA5; arachne_protocol::MESSAGE_IDENTIFIER_BYTES];
        let mut detail = [0; MAX_C_ABI_ERROR_DETAIL_BYTES];
        let mut detail_length = 0;

        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::Ok);
        assert_eq!(
            unsafe {
                arachne_client_message_send(
                    client,
                    recipient.as_bytes().as_ptr(),
                    envelope.as_ptr(),
                    envelope.len(),
                    100,
                    60,
                    message_identifier.as_mut_ptr(),
                )
            },
            ArachneStatus::State
        );
        assert_eq!(
            message_identifier,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    client,
                    detail.as_mut_ptr(),
                    detail.len(),
                    &raw mut detail_length,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(&detail[..detail_length], b"sdk_message_identity");
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_cancellation_interrupts_event_wait_and_clears_outputs() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory =
            std::env::temp_dir().join(format!("arachne-ffi-cancellation-{}", std::process::id()));
        let directory = directory.to_string_lossy().into_owned();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let mut event = ArachneEvent {
            version: u32::MAX,
            sequence: u64::MAX,
            kind: u32::MAX,
            message_identifier: [u8::MAX; arachne_protocol::MESSAGE_IDENTIFIER_BYTES],
        };
        let mut has_event = 1;

        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::Ok);
        let subscription = arachne_client_subscribe_events(client);
        let cancellation = arachne_cancellation_create();
        assert!(!subscription.is_null());
        assert!(!cancellation.is_null());
        assert_eq!(arachne_cancellation_cancel(cancellation), ArachneStatus::Ok);
        assert_eq!(arachne_cancellation_cancel(cancellation), ArachneStatus::Ok);
        assert_eq!(
            unsafe {
                arachne_event_subscription_wait(
                    subscription,
                    cancellation,
                    1,
                    &raw mut event,
                    &raw mut has_event,
                )
            },
            ArachneStatus::State
        );
        assert_eq!(event, ArachneEvent::default());
        assert_eq!(has_event, 0);
        event = ArachneEvent {
            version: u32::MAX,
            sequence: u64::MAX,
            kind: u32::MAX,
            message_identifier: [u8::MAX; arachne_protocol::MESSAGE_IDENTIFIER_BYTES],
        };
        has_event = 1;
        assert_eq!(
            unsafe {
                arachne_event_subscription_wait(
                    subscription,
                    cancellation,
                    0,
                    &raw mut event,
                    &raw mut has_event,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(event, ArachneEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            arachne_cancellation_release(cancellation),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_cancellation_release(cancellation),
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            arachne_event_subscription_release(subscription),
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_cancellation_creation_fails_closed_at_capacity() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let cancellations = (0..MAX_C_ABI_CANCELLATIONS)
            .map(|_| arachne_cancellation_create())
            .collect::<Vec<_>>();

        assert!(
            cancellations
                .iter()
                .all(|cancellation| !cancellation.is_null())
        );
        assert!(arachne_cancellation_create().is_null());
        for cancellation in cancellations {
            assert_eq!(
                arachne_cancellation_release(cancellation),
                ArachneStatus::Ok
            );
        }
    }

    fn attachment_transfer_parts() -> (Vec<u8>, Vec<u8>) {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let key = AttachmentKey::derive(&[0x11; 32], identifier).unwrap();
        let chunk = EncryptedAttachmentChunk::encrypt(
            identifier,
            0,
            &key.derive_chunk_key(0).unwrap(),
            &vec![0; ATTACHMENT_CHUNK_BYTES],
        )
        .unwrap();
        let manifest = AttachmentManifest::new(
            identifier,
            u64::try_from(ATTACHMENT_CHUNK_BYTES).unwrap(),
            vec![chunk.hash().unwrap()],
        )
        .unwrap()
        .encrypt(&key)
        .unwrap();
        (manifest.encode().unwrap(), chunk.encode().unwrap())
    }

    extern "C" fn release_running_attachment_upload(
        _: *const u8,
        _: usize,
        _: *const u8,
        _: usize,
        context: *mut c_void,
    ) -> ArachneStatus {
        if unsafe { arachne_attachment_transfer_release(context.cast()) } == ArachneStatus::State {
            ArachneStatus::Ok
        } else {
            ArachneStatus::State
        }
    }

    extern "C" fn reject_attachment_upload(
        _: *const u8,
        _: usize,
        _: *const u8,
        _: usize,
        _: *mut c_void,
    ) -> ArachneStatus {
        ArachneStatus::State
    }

    #[test]
    fn c_attachment_transfer_runs_bounded_cycles_and_rejects_reentrant_release() {
        let (manifest, chunk) = attachment_transfer_parts();
        let chunks = [ArachneByteSlice {
            data: chunk.as_ptr(),
            length: chunk.len(),
        }];
        let transfer = unsafe {
            arachne_attachment_transfer_create(
                manifest.as_ptr(),
                manifest.len(),
                chunks.as_ptr(),
                chunks.len(),
                1,
            )
        };
        let mut cycle = ArachneAttachmentDeliveryCycle::default();

        assert!(!transfer.is_null());
        assert_eq!(
            unsafe {
                arachne_attachment_transfer_run_cycle(
                    transfer,
                    Some(release_running_attachment_upload),
                    transfer.cast(),
                    &raw mut cycle,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(cycle.uploaded, 1);
        assert_eq!(cycle.outcome, crate::ARACHNE_ATTACHMENT_DELIVERY_COMPLETE);
        assert_eq!(cycle.next_pending_index, 0);
        assert_eq!(
            unsafe { arachne_attachment_transfer_release(transfer) },
            ArachneStatus::Ok
        );
        assert_eq!(
            unsafe { arachne_attachment_transfer_release(transfer) },
            ArachneStatus::InvalidInput
        );
    }

    #[test]
    fn c_attachment_transfer_retries_uploads_and_rejects_invalid_bounds() {
        let (manifest, chunk) = attachment_transfer_parts();
        let chunks = [ArachneByteSlice {
            data: chunk.as_ptr(),
            length: chunk.len(),
        }];
        let too_many_chunks = vec![chunks[0]; crate::ARACHNE_MAX_ATTACHMENT_CHUNKS + 1];
        let transfer = unsafe {
            arachne_attachment_transfer_create(
                manifest.as_ptr(),
                manifest.len(),
                chunks.as_ptr(),
                chunks.len(),
                1,
            )
        };
        let mut cycle = ArachneAttachmentDeliveryCycle {
            uploaded: u32::MAX,
            outcome: u32::MAX,
            next_pending_index: u32::MAX,
        };
        let byte = 0;

        assert!(!transfer.is_null());
        assert_eq!(
            unsafe {
                arachne_attachment_transfer_run_cycle(
                    transfer,
                    Some(reject_attachment_upload),
                    std::ptr::null_mut(),
                    &raw mut cycle,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(cycle.uploaded, 0);
        assert_eq!(cycle.outcome, crate::ARACHNE_ATTACHMENT_DELIVERY_RETRYING);
        assert_eq!(cycle.next_pending_index, 0);
        assert_eq!(
            unsafe {
                arachne_attachment_transfer_create(
                    &raw const byte,
                    crate::ARACHNE_MAX_ATTACHMENT_MANIFEST_BYTES + 1,
                    chunks.as_ptr(),
                    chunks.len(),
                    1,
                )
            },
            std::ptr::null_mut()
        );
        assert_eq!(
            unsafe {
                arachne_attachment_transfer_create(
                    manifest.as_ptr(),
                    manifest.len(),
                    too_many_chunks.as_ptr(),
                    too_many_chunks.len(),
                    1,
                )
            },
            std::ptr::null_mut()
        );
        assert_eq!(
            unsafe {
                arachne_attachment_transfer_run_cycle(
                    transfer,
                    None,
                    std::ptr::null_mut(),
                    &raw mut cycle,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(cycle, ArachneAttachmentDeliveryCycle::default());
        assert_eq!(
            unsafe { arachne_attachment_transfer_release(transfer) },
            ArachneStatus::Ok
        );
    }

    #[test]
    fn client_configuration_builder_attaches_a_validated_embedded_configuration() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-ffi-client-lifecycle-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    state_directory.as_bytes().as_ptr(),
                    state_directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::Ok);
        let subscriptions = (0..MAX_C_ABI_EVENT_SUBSCRIPTIONS)
            .map(|_| arachne_client_subscribe_events(client))
            .collect::<Vec<_>>();
        assert!(
            subscriptions
                .iter()
                .all(|subscription| !subscription.is_null())
        );
        assert!(arachne_client_subscribe_events(client).is_null());
        for subscription in subscriptions {
            assert_eq!(
                arachne_event_subscription_release(subscription),
                ArachneStatus::Ok
            );
            assert_eq!(
                arachne_event_subscription_release(subscription),
                ArachneStatus::InvalidInput
            );
        }
        assert_event_polling_contract(client);
        assert!(arachne_client_subscribe_events(client).is_null());
        assert_eq!(arachne_client_stop(client), ArachneStatus::State);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    fn assert_event_polling_contract(client: *mut crate::ArachneClient) {
        let subscription = arachne_client_subscribe_events(client);
        assert!(!subscription.is_null());
        let mut event = ArachneEvent {
            version: u32::MAX,
            sequence: u64::MAX,
            kind: u32::MAX,
            message_identifier: [u8::MAX; arachne_protocol::MESSAGE_IDENTIFIER_BYTES],
        };
        let mut has_event = 1;
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            ArachneStatus::Ok
        );
        assert_eq!(event, ArachneEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(client.cast(), &raw mut event, &raw mut has_event)
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(event, ArachneEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(
                    std::ptr::null_mut(),
                    &raw mut event,
                    &raw mut has_event,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(event, ArachneEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(
                    subscription,
                    std::ptr::null_mut(),
                    &raw mut has_event,
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(arachne_client_start(client), ArachneStatus::State);
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            ArachneStatus::Ok
        );
        assert_eq!(event.version, crate::ARACHNE_EVENT_VERSION);
        assert_eq!(event.sequence, 2);
        assert_eq!(event.kind, crate::ARACHNE_EVENT_CLIENT_STOPPED);
        assert_eq!(
            event.message_identifier,
            [0; arachne_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        assert_eq!(has_event, 1);
        assert_eq!(
            unsafe {
                arachne_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            ArachneStatus::State
        );
        assert_eq!(event, ArachneEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            arachne_event_subscription_release(subscription),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_event_subscription_release(subscription),
            ArachneStatus::InvalidInput
        );
    }

    #[test]
    fn client_lifecycle_rejects_unconfigured_and_invalid_clients() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = arachne_client_create();
        assert_eq!(arachne_client_start(client), ArachneStatus::State);
        assert_eq!(arachne_client_stop(client), ArachneStatus::State);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_start(client), ArachneStatus::InvalidInput);
        assert_eq!(
            arachne_client_stop(std::ptr::null_mut()),
            ArachneStatus::InvalidInput
        );
    }

    #[test]
    fn event_records_preserve_the_stable_kind_and_identifier_contract() {
        let identifier = arachne_protocol::MessageIdentifier::from_bytes([7; 16]).unwrap();
        let cases = [
            (
                SdkEvent::ClientStarted,
                crate::ARACHNE_EVENT_CLIENT_STARTED,
                [0; 16],
            ),
            (
                SdkEvent::ClientStopped,
                crate::ARACHNE_EVENT_CLIENT_STOPPED,
                [0; 16],
            ),
            (
                SdkEvent::MessageQueued(identifier),
                crate::ARACHNE_EVENT_MESSAGE_QUEUED,
                *identifier.as_bytes(),
            ),
            (
                SdkEvent::MessageDelivered(identifier),
                crate::ARACHNE_EVENT_MESSAGE_DELIVERED,
                *identifier.as_bytes(),
            ),
            (
                SdkEvent::MessageDeliveryFailed(identifier),
                crate::ARACHNE_EVENT_MESSAGE_DELIVERY_FAILED,
                *identifier.as_bytes(),
            ),
        ];
        for (event, kind, message_identifier) in cases {
            let record = c_event(SdkEventEnvelope::new(42, event).unwrap());
            assert_eq!(record.version, crate::ARACHNE_EVENT_VERSION);
            assert_eq!(record.sequence, 42);
            assert_eq!(record.kind, kind);
            assert_eq!(record.message_identifier, message_identifier);
        }
    }

    #[test]
    fn sdk_client_errors_have_stable_c_status_mappings() {
        assert_eq!(
            map_sdk_client_error(&SdkClientError::Configuration(
                arachne_sdk::SdkConfigError::InvalidStateDirectory
            )),
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            map_sdk_client_error(&SdkClientError::EventSequenceExhausted),
            ArachneStatus::ResourceLimit
        );
        for error in [
            SdkClientError::DaemonModeUnavailable,
            SdkClientError::AlreadyRunning,
            SdkClientError::NotRunning,
            SdkClientError::State,
            SdkClientError::Engine,
            SdkClientError::AsyncTask,
        ] {
            assert_eq!(map_sdk_client_error(&error), ArachneStatus::State);
        }
    }

    #[test]
    fn sdk_client_error_details_are_bounded_redacted_tokens() {
        for error in [
            SdkClientError::Configuration(arachne_sdk::SdkConfigError::InvalidStateDirectory),
            SdkClientError::DaemonModeUnavailable,
            SdkClientError::AlreadyRunning,
            SdkClientError::NotRunning,
            SdkClientError::State,
            SdkClientError::Engine,
            SdkClientError::EventSequenceExhausted,
            SdkClientError::AsyncTask,
        ] {
            let detail = sdk_client_error_detail(&error);
            assert!(!detail.is_empty());
            assert!(detail.len() <= MAX_C_ABI_ERROR_DETAIL_BYTES);
            assert!(
                detail
                    .iter()
                    .all(|byte| byte.is_ascii_lowercase() || *byte == b'_')
            );
        }
    }

    fn assert_library_buffer_detail_transfer_and_capacity(client: *mut crate::ArachneClient) {
        let mut owned_detail = std::ptr::null_mut();
        assert_eq!(
            unsafe { arachne_client_take_last_error_detail(client, &raw mut owned_detail) },
            ArachneStatus::Ok
        );
        assert!(!owned_detail.is_null());
        assert_eq!(
            arachne_buffer_length(owned_detail),
            b"sdk_already_running".len()
        );
        let owned_data = arachne_buffer_data(owned_detail);
        assert_eq!(
            unsafe { std::slice::from_raw_parts(owned_data, arachne_buffer_length(owned_detail)) },
            b"sdk_already_running"
        );
        let mut detail_length = 0;
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    client,
                    std::ptr::null_mut(),
                    0,
                    &raw mut detail_length,
                )
            },
            ArachneStatus::State
        );
        assert_eq!(arachne_buffer_release(owned_detail), ArachneStatus::Ok);
        assert_eq!(arachne_buffer_data(owned_detail), std::ptr::null());
        assert_eq!(arachne_buffer_length(owned_detail), 0);
        assert_eq!(
            arachne_buffer_release(owned_detail),
            ArachneStatus::InvalidInput
        );
        let mut buffers = Vec::with_capacity(MAX_C_ABI_BUFFERS);
        for _ in 0..MAX_C_ABI_BUFFERS {
            assert_eq!(arachne_client_start(client), ArachneStatus::State);
            let mut buffer = std::ptr::null_mut();
            assert_eq!(
                unsafe { arachne_client_take_last_error_detail(client, &raw mut buffer) },
                ArachneStatus::Ok
            );
            buffers.push(buffer);
        }
        assert_eq!(arachne_client_start(client), ArachneStatus::State);
        let mut overflow = std::ptr::null_mut();
        assert_eq!(
            unsafe { arachne_client_take_last_error_detail(client, &raw mut overflow) },
            ArachneStatus::ResourceLimit
        );
        assert!(overflow.is_null());
        for buffer in buffers {
            assert_eq!(arachne_buffer_release(buffer), ArachneStatus::Ok);
        }
    }

    #[test]
    fn client_start_maps_an_engine_state_directory_conflict_to_state() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-ffi-client-start-conflict-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        let first = arachne_client_create();
        let second = arachne_client_create();
        for client in [first, second] {
            let builder = arachne_client_config_builder_create();
            assert_eq!(
                unsafe {
                    arachne_client_config_builder_set_state_directory(
                        builder,
                        state_directory.as_bytes().as_ptr(),
                        state_directory.len(),
                    )
                },
                ArachneStatus::Ok
            );
            assert_eq!(
                arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
                ArachneStatus::Ok
            );
            assert_eq!(
                arachne_client_config_builder_build(builder, client),
                ArachneStatus::Ok
            );
            assert_eq!(
                arachne_client_config_builder_release(builder),
                ArachneStatus::Ok
            );
        }
        assert_eq!(arachne_client_start(first), ArachneStatus::Ok);
        assert_eq!(arachne_client_start(second), ArachneStatus::State);
        let mut detail_length = 0;
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    second,
                    std::ptr::null_mut(),
                    0,
                    &raw mut detail_length,
                )
            },
            ArachneStatus::ResourceLimit
        );
        assert_eq!(detail_length, b"sdk_already_running".len());
        let mut short_buffer = [0xA5; 32];
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    second,
                    short_buffer.as_mut_ptr(),
                    detail_length - 1,
                    &raw mut detail_length,
                )
            },
            ArachneStatus::ResourceLimit
        );
        assert!(short_buffer.iter().all(|byte| *byte == 0xA5));
        let mut detail = [0; MAX_C_ABI_ERROR_DETAIL_BYTES];
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    second,
                    detail.as_mut_ptr(),
                    detail.len(),
                    &raw mut detail_length,
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(&detail[..detail_length], b"sdk_already_running");
        assert_library_buffer_detail_transfer_and_capacity(second);
        assert_eq!(
            unsafe {
                arachne_client_copy_last_error_detail(
                    second,
                    std::ptr::null_mut(),
                    0,
                    std::ptr::null_mut(),
                )
            },
            ArachneStatus::InvalidInput
        );
        assert_eq!(arachne_client_stop(first), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(first), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(second), ArachneStatus::Ok);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    extern "C" fn noop_completion(_: i32, _: *mut std::ffi::c_void) {}

    #[test]
    fn client_operations_reject_null_and_maximum_address_inputs() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        for client in [
            std::ptr::null_mut(),
            std::ptr::without_provenance_mut(usize::MAX),
        ] {
            assert_eq!(arachne_client_release(client), ArachneStatus::InvalidInput);
            assert_eq!(arachne_client_start(client), ArachneStatus::InvalidInput);
            assert_eq!(arachne_client_stop(client), ArachneStatus::InvalidInput);
            assert_eq!(
                arachne_client_complete_async(client, Some(noop_completion), std::ptr::null_mut()),
                ArachneStatus::InvalidInput
            );
        }
    }

    static CALLBACK_BACKPRESSURE_RELEASED: std::sync::atomic::AtomicBool =
        std::sync::atomic::AtomicBool::new(false);
    static CALLBACK_BACKPRESSURE_STARTED: AtomicUsize = AtomicUsize::new(0);

    extern "C" fn block_completion(_: i32, _: *mut std::ffi::c_void) {
        CALLBACK_BACKPRESSURE_STARTED.fetch_add(1, Ordering::AcqRel);
        while !CALLBACK_BACKPRESSURE_RELEASED.load(Ordering::Acquire) {
            std::thread::yield_now();
        }
    }

    fn wait_for_pending_callbacks(expected: usize) {
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(2);
        while PENDING_COMPLETIONS.load(Ordering::Acquire) != expected {
            assert!(std::time::Instant::now() < deadline);
            std::thread::yield_now();
        }
    }

    #[test]
    fn callback_queue_applies_bounded_nonblocking_backpressure() {
        struct CallbackReleaseGuard;

        impl Drop for CallbackReleaseGuard {
            fn drop(&mut self) {
                CALLBACK_BACKPRESSURE_RELEASED.store(true, Ordering::Release);
            }
        }

        let _client_guard = CLIENT_TEST_LOCK.lock().unwrap();
        wait_for_pending_callbacks(0);
        CALLBACK_BACKPRESSURE_RELEASED.store(false, Ordering::Release);
        CALLBACK_BACKPRESSURE_STARTED.store(0, Ordering::Release);
        let _release_guard = CallbackReleaseGuard;
        let client = arachne_client_create();
        for _ in 0..MAX_C_ABI_PENDING_COMPLETIONS {
            assert_eq!(
                arachne_client_complete_async(client, Some(block_completion), std::ptr::null_mut()),
                ArachneStatus::Ok
            );
        }
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(2);
        while CALLBACK_BACKPRESSURE_STARTED.load(Ordering::Acquire) < MAX_C_ABI_CALLBACK_WORKERS {
            assert!(std::time::Instant::now() < deadline);
            std::thread::yield_now();
        }
        assert_eq!(
            arachne_client_complete_async(client, Some(block_completion), std::ptr::null_mut()),
            ArachneStatus::ResourceLimit
        );
        CALLBACK_BACKPRESSURE_RELEASED.store(true, Ordering::Release);
        wait_for_pending_callbacks(0);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
    }

    #[test]
    fn concurrent_client_start_linearizes_one_successful_transition() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-ffi-client-start-race-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    state_directory.as_bytes().as_ptr(),
                    state_directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 8),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        let client_address = client.addr();
        let start = std::sync::Arc::new(std::sync::Barrier::new(2));
        let mut workers = Vec::with_capacity(2);
        for _ in 0..2 {
            let start = std::sync::Arc::clone(&start);
            workers.push(std::thread::spawn(move || {
                start.wait();
                arachne_client_start(std::ptr::without_provenance_mut(client_address))
            }));
        }
        let statuses = workers
            .into_iter()
            .map(|worker| worker.join().unwrap())
            .collect::<Vec<_>>();
        assert_eq!(
            statuses
                .iter()
                .filter(|status| **status == ArachneStatus::Ok)
                .count(),
            1
        );
        assert_eq!(
            statuses
                .iter()
                .filter(|status| **status == ArachneStatus::State)
                .count(),
            1
        );
        assert_eq!(arachne_client_stop(client), ArachneStatus::Ok);
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn client_configuration_builder_rejects_missing_invalid_and_unbounded_input() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = arachne_client_create();
        let builder = arachne_client_config_builder_create();
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(builder, std::ptr::null(), 1)
            },
            ArachneStatus::InvalidInput
        );
        let invalid_utf8 = [0xFF];
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    invalid_utf8.as_ptr(),
                    invalid_utf8.len(),
                )
            },
            ArachneStatus::InvalidInput
        );
        let embedded_nul = b"/tmp/arachne\0ffi";
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    embedded_nul.as_ptr(),
                    embedded_nul.len(),
                )
            },
            ArachneStatus::InvalidInput
        );
        let byte = b'x';
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    &raw const byte,
                    MAX_C_ABI_STATE_DIRECTORY_BYTES + 1,
                )
            },
            ArachneStatus::InvalidInput
        );
        let state_directory = b"relative-state";
        assert_eq!(
            unsafe {
                arachne_client_config_builder_set_state_directory(
                    builder,
                    state_directory.as_ptr(),
                    state_directory.len(),
                )
            },
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_set_event_buffer_capacity(builder, 0),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::InvalidInput
        );
        assert_eq!(
            arachne_client_config_builder_release(builder),
            ArachneStatus::Ok
        );
        assert_eq!(
            arachne_client_config_builder_build(builder, client),
            ArachneStatus::InvalidInput
        );
        assert_eq!(arachne_client_release(client), ArachneStatus::Ok);
    }

    unsafe extern "C" {
        fn arachne_c_consumer_conformance() -> i32;
        fn arachne_c_delivery_profile_operations() -> i32;
        fn arachne_c_embedded_client_lifecycle(
            state_directory: *const u8,
            state_directory_length: usize,
        ) -> i32;
        fn arachne_c_recovery_operations() -> i32;
    }

    #[test]
    fn c_consumer_conformance_passes() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        assert_eq!(unsafe { arachne_c_consumer_conformance() }, 0);
    }

    #[test]
    fn c_consumer_runs_delivery_profile_operations() {
        assert_eq!(unsafe { arachne_c_delivery_profile_operations() }, 0);
    }

    #[test]
    fn c_consumer_runs_recovery_operations() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        assert_eq!(unsafe { arachne_c_recovery_operations() }, 0);
    }

    #[test]
    fn c_consumer_runs_the_embedded_client_lifecycle() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-ffi-c-embedded-client-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        let status = unsafe {
            arachne_c_embedded_client_lifecycle(
                state_directory.as_bytes().as_ptr(),
                state_directory.len(),
            )
        };
        let cleanup = std::fs::remove_dir_all(state_directory);
        assert_eq!(status, 0);
        cleanup.unwrap();
    }
}
