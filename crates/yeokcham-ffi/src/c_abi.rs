use std::{
    collections::BTreeMap,
    convert::Infallible,
    ffi::c_void,
    path::PathBuf,
    sync::{
        Arc, Mutex, OnceLock,
        atomic::{AtomicUsize, Ordering},
        mpsc::{self, SyncSender, TrySendError},
    },
};

use crate::{
    YEOKCHAM_ABI_NEGOTIATION_REJECTED, YEOKCHAM_ABI_VERSION, YEOKCHAM_EVENT_CLIENT_STARTED,
    YEOKCHAM_EVENT_CLIENT_STOPPED, YEOKCHAM_EVENT_MESSAGE_DELIVERED,
    YEOKCHAM_EVENT_MESSAGE_DELIVERY_FAILED, YEOKCHAM_EVENT_MESSAGE_QUEUED, YeokchamBuffer,
    YeokchamClient, YeokchamClientConfigBuilder, YeokchamContact, YeokchamDeliveryProfile,
    YeokchamDeliveryProfilePolicy, YeokchamEvent, YeokchamEventSubscription, YeokchamStatus,
};
use yeokcham_core::{IdentityPublicKey, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_sdk::{
    RuntimeMode, SdkClient, SdkClientError, SdkConfig, SdkContact, SdkContactError,
    SdkContactStatus, SdkContactVerificationMethod, SdkDeliveryProfile, SdkDeliveryProfileKind,
    SdkDeliveryProfilePolicy, SdkDeliveryProfilePolicyError, SdkDirectIpDisclosureAcknowledgement,
    SdkEvent, SdkEventEnvelope, SdkEventStream, SdkEventStreamError, SdkIdentityError,
    SdkIdentityManager, SdkLocalMeshPolicy, SdkLocalMeshTransportKind, SdkMessageEnvelope,
    SdkMessageError, SdkMessageExpiry, SdkMessageSendRequest,
};
use zeroize::{Zeroize, Zeroizing};

pub const MAX_C_ABI_CLIENT_CONFIG_BUILDERS: usize = 1024;
pub const MAX_C_ABI_CLIENTS: usize = 1024;
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
static ACTIVE_CLIENT_CONFIG_BUILDERS: OnceLock<Mutex<BTreeMap<usize, ClientConfigBuilder>>> =
    OnceLock::new();
static ACTIVE_BUFFERS: OnceLock<Mutex<BTreeMap<usize, Vec<u8>>>> = OnceLock::new();
static ACTIVE_EVENT_SUBSCRIPTIONS: OnceLock<Mutex<BTreeMap<usize, SdkEventStream>>> =
    OnceLock::new();
static NEXT_HANDLE_IDENTIFIER: AtomicUsize = AtomicUsize::new(1);
static PENDING_COMPLETIONS: AtomicUsize = AtomicUsize::new(0);
static CALLBACK_QUEUE: OnceLock<Option<SyncSender<PendingCompletion>>> = OnceLock::new();

#[cfg(test)]
pub static CLIENT_TEST_LOCK: Mutex<()> = Mutex::new(());

pub type YeokchamCompletionCallback = extern "C" fn(i32, *mut c_void);

struct PendingCompletion {
    callback: YeokchamCompletionCallback,
    context: usize,
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_abi_negotiate(requested_version: u32) -> u32 {
    if requested_version == YEOKCHAM_ABI_VERSION {
        YEOKCHAM_ABI_VERSION
    } else {
        YEOKCHAM_ABI_NEGOTIATION_REJECTED
    }
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_secret_buffer_zeroize(
    buffer: *mut u8,
    length: usize,
) -> YeokchamStatus {
    if buffer.is_null() || length == 0 {
        return YeokchamStatus::InvalidInput;
    }
    if length > MAX_C_ABI_SECRET_BUFFER_BYTES {
        return YeokchamStatus::ResourceLimit;
    }
    let buffer = unsafe { std::slice::from_raw_parts_mut(buffer, length) };
    buffer.zeroize();
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_create() -> *mut YeokchamClient {
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
pub extern "C" fn yeokcham_client_release(client: *mut YeokchamClient) -> YeokchamStatus {
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    if clients.remove(&identifier).is_none() {
        return YeokchamStatus::InvalidInput;
    }
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_start(client: *mut YeokchamClient) -> YeokchamStatus {
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    if client.runtime.is_some() {
        return YeokchamStatus::State;
    }
    let Some(configuration) = client.configuration.clone() else {
        return YeokchamStatus::State;
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
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_stop(client: *mut YeokchamClient) -> YeokchamStatus {
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(mut runtime) = client.runtime.take() else {
        return YeokchamStatus::State;
    };
    if let Err(error) = runtime.shutdown() {
        client.runtime = Some(runtime);
        client.last_error_detail = Some(sdk_client_error_detail(&error));
        return map_sdk_client_error(&error);
    }
    client.last_error_detail = None;
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_identity_create(
    client: *mut YeokchamClient,
    public_key: *mut u8,
) -> YeokchamStatus {
    if public_key.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { std::ptr::write_bytes(public_key, 0, yeokcham_core::ED25519_PUBLIC_KEY_BYTES) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
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
            yeokcham_core::ED25519_PUBLIC_KEY_BYTES,
        );
    };
    client.last_error_detail = None;
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_identity_load(
    client: *mut YeokchamClient,
    public_key: *mut u8,
) -> YeokchamStatus {
    if public_key.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { std::ptr::write_bytes(public_key, 0, yeokcham_core::ED25519_PUBLIC_KEY_BYTES) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
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
            yeokcham_core::ED25519_PUBLIC_KEY_BYTES,
        );
    };
    client.last_error_detail = None;
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_contact_import(
    client: *mut YeokchamClient,
    invitation: *const u8,
    contact: *mut YeokchamContact,
) -> YeokchamStatus {
    if invitation.is_null() || contact.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { contact.write(YeokchamContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let invitation = unsafe {
        std::slice::from_raw_parts(invitation, yeokcham_protocol::CONTACT_INVITATION_BYTES)
    };
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return YeokchamStatus::State;
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
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_contact_get(
    client: *mut YeokchamClient,
    identity: *const u8,
    contact: *mut YeokchamContact,
) -> YeokchamStatus {
    let Some(identity) = (unsafe { c_identity_public_key(identity) }) else {
        return YeokchamStatus::InvalidInput;
    };
    if contact.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { contact.write(YeokchamContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return YeokchamStatus::State;
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
        return YeokchamStatus::State;
    };
    unsafe { contact.write(c_contact(contact_value)) };
    client.last_error_detail = None;
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_contact_revoke(
    client: *mut YeokchamClient,
    identity: *const u8,
    contact: *mut YeokchamContact,
) -> YeokchamStatus {
    let Some(identity) = (unsafe { c_identity_public_key(identity) }) else {
        return YeokchamStatus::InvalidInput;
    };
    if contact.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { contact.write(YeokchamContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return YeokchamStatus::State;
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
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_contact_verify_qr(
    client: *mut YeokchamClient,
    payload: *const u8,
    contact: *mut YeokchamContact,
) -> YeokchamStatus {
    if payload.is_null() || contact.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { contact.write(YeokchamContact::default()) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let payload = unsafe {
        std::slice::from_raw_parts(payload, yeokcham_protocol::QR_VERIFICATION_PAYLOAD_BYTES)
    };
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return YeokchamStatus::State;
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
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_contact_verify_safety_number(
    client: *mut YeokchamClient,
    identity: *const u8,
    fingerprint: *const u8,
    contact: *mut YeokchamContact,
) -> YeokchamStatus {
    if contact.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { contact.write(YeokchamContact::default()) };
    let Some(identity) = (unsafe { c_identity_public_key(identity) }) else {
        return YeokchamStatus::InvalidInput;
    };
    if fingerprint.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let fingerprint = unsafe {
        std::slice::from_raw_parts(
            fingerprint,
            yeokcham_protocol::SAFETY_NUMBER_FINGERPRINT_BYTES,
        )
    };
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_ref() else {
        client.last_error_detail = Some(b"sdk_contact_client_not_running");
        return YeokchamStatus::State;
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
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_delivery_profile_select(
    policy: *const YeokchamDeliveryProfilePolicy,
    kind: u32,
    local_mesh_transport: u32,
    direct_ip_disclosure_acknowledged: u32,
    profile: *mut YeokchamDeliveryProfile,
) -> YeokchamStatus {
    if policy.is_null() || profile.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { profile.write(YeokchamDeliveryProfile::default()) };
    let policy = unsafe { *policy };
    let policy = match c_delivery_profile_policy(policy) {
        Ok(policy) => policy,
        Err(status) => return status,
    };
    let result = match kind {
        crate::YEOKCHAM_DELIVERY_PROFILE_DIRECT => {
            if local_mesh_transport != 0
                || direct_ip_disclosure_acknowledged
                    != crate::YEOKCHAM_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED
            {
                return YeokchamStatus::InvalidInput;
            }
            policy.select_direct(SdkDirectIpDisclosureAcknowledgement::acknowledge())
        }
        crate::YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP => {
            if local_mesh_transport != 0 || direct_ip_disclosure_acknowledged != 0 {
                return YeokchamStatus::InvalidInput;
            }
            policy.select_tor_maildrop()
        }
        crate::YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH => {
            if direct_ip_disclosure_acknowledged != 0 {
                return YeokchamStatus::InvalidInput;
            }
            let Some(local_mesh_transport) = c_local_mesh_transport(local_mesh_transport) else {
                return YeokchamStatus::InvalidInput;
            };
            policy.select_local_mesh(local_mesh_transport)
        }
        _ => return YeokchamStatus::InvalidInput,
    };
    let profile_value = match result {
        Ok(profile_value) => profile_value,
        Err(error) => return map_sdk_delivery_profile_policy_error(error),
    };
    unsafe { profile.write(c_delivery_profile(profile_value)) };
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_message_send(
    client: *mut YeokchamClient,
    recipient: *const u8,
    envelope: *const u8,
    envelope_length: usize,
    created_at: u64,
    ttl_seconds: u32,
    message_identifier: *mut u8,
) -> YeokchamStatus {
    if message_identifier.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe {
        std::ptr::write_bytes(
            message_identifier,
            0,
            yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES,
        );
    };
    let Some(recipient) = (unsafe { c_identity_public_key(recipient) }) else {
        return YeokchamStatus::InvalidInput;
    };
    if envelope.is_null()
        || envelope_length == 0
        || envelope_length > crate::YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES
    {
        return YeokchamStatus::InvalidInput;
    }
    let envelope = unsafe { std::slice::from_raw_parts(envelope, envelope_length) };
    let Ok(envelope) = SdkMessageEnvelope::from_encoded(envelope) else {
        return YeokchamStatus::InvalidInput;
    };
    let Ok(expiry) = SdkMessageExpiry::new(created_at, ttl_seconds) else {
        return YeokchamStatus::InvalidInput;
    };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(runtime) = client.runtime.as_mut() else {
        client.last_error_detail = Some(b"sdk_message_client_not_running");
        return YeokchamStatus::State;
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
            yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES,
        );
    };
    client.last_error_detail = None;
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_copy_last_error_detail(
    client: *const YeokchamClient,
    buffer: *mut u8,
    buffer_capacity: usize,
    detail_length: *mut usize,
) -> YeokchamStatus {
    if detail_length.is_null() || (buffer.is_null() && buffer_capacity != 0) {
        return YeokchamStatus::InvalidInput;
    }
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(detail) = client.last_error_detail else {
        return YeokchamStatus::State;
    };
    unsafe { detail_length.write(detail.len()) };
    if buffer_capacity < detail.len() {
        return YeokchamStatus::ResourceLimit;
    }
    unsafe { std::ptr::copy_nonoverlapping(detail.as_ptr(), buffer, detail.len()) };
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_take_last_error_detail(
    client: *mut YeokchamClient,
    detail: *mut *mut YeokchamBuffer,
) -> YeokchamStatus {
    if detail.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe { detail.write(std::ptr::null_mut()) };
    let identifier = client.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let Some(last_error_detail) = client.last_error_detail else {
        return YeokchamStatus::State;
    };
    let buffer = match allocate_buffer(last_error_detail) {
        Ok(buffer) => buffer,
        Err(status) => return status,
    };
    client.last_error_detail = None;
    unsafe { detail.write(buffer) };
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_buffer_data(buffer: *const YeokchamBuffer) -> *const u8 {
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
pub extern "C" fn yeokcham_buffer_length(buffer: *const YeokchamBuffer) -> usize {
    let identifier = buffer.addr();
    if identifier == 0 {
        return 0;
    }
    let Ok(buffers) = active_buffers().lock() else {
        return 0;
    };
    buffers.get(&identifier).map_or(0, Vec::len)
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_buffer_release(buffer: *mut YeokchamBuffer) -> YeokchamStatus {
    let identifier = buffer.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut buffers) = active_buffers().lock() else {
        return YeokchamStatus::State;
    };
    if buffers.remove(&identifier).is_none() {
        return YeokchamStatus::InvalidInput;
    }
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_subscribe_events(
    client: *const YeokchamClient,
) -> *mut YeokchamEventSubscription {
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
pub extern "C" fn yeokcham_event_subscription_release(
    subscription: *mut YeokchamEventSubscription,
) -> YeokchamStatus {
    let identifier = subscription.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut subscriptions) = active_event_subscriptions().lock() else {
        return YeokchamStatus::State;
    };
    if subscriptions.remove(&identifier).is_none() {
        return YeokchamStatus::InvalidInput;
    }
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_event_subscription_poll(
    subscription: *mut YeokchamEventSubscription,
    event: *mut YeokchamEvent,
    has_event: *mut u8,
) -> YeokchamStatus {
    if event.is_null() || has_event.is_null() {
        return YeokchamStatus::InvalidInput;
    }
    unsafe {
        std::ptr::write_bytes(event.cast::<u8>(), 0, std::mem::size_of::<YeokchamEvent>());
        has_event.write(0);
    }
    let identifier = subscription.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut subscriptions) = active_event_subscriptions().lock() else {
        return YeokchamStatus::State;
    };
    let Some(subscription) = subscriptions.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    match subscription.try_next() {
        Ok(None) => YeokchamStatus::Ok,
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
            YeokchamStatus::Ok
        }
        Err(SdkEventStreamError::Lagged(_)) => YeokchamStatus::ResourceLimit,
        Err(
            SdkEventStreamError::Cancelled
            | SdkEventStreamError::DeadlineExceeded
            | SdkEventStreamError::Closed,
        ) => YeokchamStatus::State,
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_complete_async(
    client: *const YeokchamClient,
    callback: Option<YeokchamCompletionCallback>,
    context: *mut c_void,
) -> YeokchamStatus {
    let Some(callback) = callback else {
        return YeokchamStatus::InvalidInput;
    };
    match is_active_client(client) {
        Ok(true) => {}
        Ok(false) => return YeokchamStatus::InvalidInput,
        Err(status) => return status,
    }
    let Ok(_) = PENDING_COMPLETIONS.fetch_update(Ordering::AcqRel, Ordering::Acquire, |pending| {
        (pending < MAX_C_ABI_PENDING_COMPLETIONS).then_some(pending + 1)
    }) else {
        return YeokchamStatus::ResourceLimit;
    };
    let context = context.expose_provenance();
    let Some(queue) = callback_queue() else {
        PENDING_COMPLETIONS.fetch_sub(1, Ordering::AcqRel);
        return YeokchamStatus::ResourceLimit;
    };
    match queue.try_send(PendingCompletion { callback, context }) {
        Ok(()) => YeokchamStatus::Ok,
        Err(TrySendError::Full(_) | TrySendError::Disconnected(_)) => {
            PENDING_COMPLETIONS.fetch_sub(1, Ordering::AcqRel);
            YeokchamStatus::ResourceLimit
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_config_builder_create() -> *mut YeokchamClientConfigBuilder {
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
pub extern "C" fn yeokcham_client_config_builder_release(
    builder: *mut YeokchamClientConfigBuilder,
) -> YeokchamStatus {
    let identifier = builder.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return YeokchamStatus::State;
    };
    if builders.remove(&identifier).is_none() {
        return YeokchamStatus::InvalidInput;
    }
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
#[allow(clippy::missing_safety_doc)]
pub unsafe extern "C" fn yeokcham_client_config_builder_set_state_directory(
    builder: *mut YeokchamClientConfigBuilder,
    state_directory: *const u8,
    state_directory_length: usize,
) -> YeokchamStatus {
    if state_directory.is_null()
        || state_directory_length == 0
        || state_directory_length > MAX_C_ABI_STATE_DIRECTORY_BYTES
    {
        return YeokchamStatus::InvalidInput;
    }
    let bytes = unsafe { std::slice::from_raw_parts(state_directory, state_directory_length) };
    let Ok(state_directory) = std::str::from_utf8(bytes) else {
        return YeokchamStatus::InvalidInput;
    };
    if state_directory.contains('\0') {
        return YeokchamStatus::InvalidInput;
    }
    let identifier = builder.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return YeokchamStatus::State;
    };
    let Some(builder) = builders.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    builder.state_directory = Some(PathBuf::from(state_directory));
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_config_builder_set_event_buffer_capacity(
    builder: *mut YeokchamClientConfigBuilder,
    event_buffer_capacity: u32,
) -> YeokchamStatus {
    let identifier = builder.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return YeokchamStatus::State;
    };
    let Some(builder) = builders.get_mut(&identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    builder.event_buffer_capacity = Some(event_buffer_capacity as usize);
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_config_builder_build(
    builder: *const YeokchamClientConfigBuilder,
    client: *const YeokchamClient,
) -> YeokchamStatus {
    let builder_identifier = builder.addr();
    let client_identifier = client.addr();
    if builder_identifier == 0 || client_identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(builders) = active_client_config_builders().lock() else {
        return YeokchamStatus::State;
    };
    let Some(builder) = builders.get(&builder_identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    let (Some(state_directory), Some(event_buffer_capacity)) = (
        builder.state_directory.clone(),
        builder.event_buffer_capacity,
    ) else {
        return YeokchamStatus::InvalidInput;
    };
    drop(builders);
    let Ok(configuration) = SdkConfig::new(
        state_directory,
        RuntimeMode::Embedded,
        event_buffer_capacity,
    ) else {
        return YeokchamStatus::InvalidInput;
    };
    let Ok(mut clients) = active_clients().lock() else {
        return YeokchamStatus::State;
    };
    let Some(client) = clients.get_mut(&client_identifier) else {
        return YeokchamStatus::InvalidInput;
    };
    if client.runtime.is_some() {
        return YeokchamStatus::State;
    }
    client.configuration = Some(configuration);
    YeokchamStatus::Ok
}

fn active_clients() -> &'static Mutex<BTreeMap<usize, ClientHandle>> {
    ACTIVE_CLIENTS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_client_config_builders() -> &'static Mutex<BTreeMap<usize, ClientConfigBuilder>> {
    ACTIVE_CLIENT_CONFIG_BUILDERS.get_or_init(|| Mutex::new(BTreeMap::new()))
}

fn active_buffers() -> &'static Mutex<BTreeMap<usize, Vec<u8>>> {
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

fn c_contact(contact: SdkContact) -> YeokchamContact {
    let status = match contact.status() {
        SdkContactStatus::Pending => crate::YEOKCHAM_CONTACT_STATUS_PENDING,
        SdkContactStatus::Verified => crate::YEOKCHAM_CONTACT_STATUS_VERIFIED,
        SdkContactStatus::Revoked => crate::YEOKCHAM_CONTACT_STATUS_REVOKED,
    };
    let verification = match contact.verification_method() {
        None => crate::YEOKCHAM_CONTACT_VERIFICATION_NONE,
        Some(SdkContactVerificationMethod::Qr) => crate::YEOKCHAM_CONTACT_VERIFICATION_QR,
        Some(SdkContactVerificationMethod::SafetyNumber) => {
            crate::YEOKCHAM_CONTACT_VERIFICATION_SAFETY_NUMBER
        }
    };
    YeokchamContact {
        identity: *contact.identity().as_bytes(),
        status,
        verification,
    }
}

fn c_delivery_profile_policy(
    policy: YeokchamDeliveryProfilePolicy,
) -> Result<SdkDeliveryProfilePolicy, YeokchamStatus> {
    let Some(direct_allowed) = c_boolean(policy.direct_allowed) else {
        return Err(YeokchamStatus::InvalidInput);
    };
    let Some(tor_maildrop_allowed) = c_boolean(policy.tor_maildrop_allowed) else {
        return Err(YeokchamStatus::InvalidInput);
    };
    let Ok(local_mesh_transport_count) = usize::try_from(policy.local_mesh_transport_count) else {
        return Err(YeokchamStatus::InvalidInput);
    };
    if local_mesh_transport_count > crate::YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS {
        return Err(YeokchamStatus::InvalidInput);
    }
    let mut local_mesh_transports =
        [SdkLocalMeshTransportKind::Lan; crate::YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS];
    for (transport, value) in local_mesh_transports
        .iter_mut()
        .zip(&policy.local_mesh_transports[..local_mesh_transport_count])
    {
        let Some(value) = c_local_mesh_transport(*value) else {
            return Err(YeokchamStatus::InvalidInput);
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
        crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_LAN => Some(SdkLocalMeshTransportKind::Lan),
        crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT => {
            Some(SdkLocalMeshTransportKind::WifiHotspot)
        }
        crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_DIRECT => {
            Some(SdkLocalMeshTransportKind::WifiDirect)
        }
        crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_BLUETOOTH => {
            Some(SdkLocalMeshTransportKind::Bluetooth)
        }
        _ => None,
    }
}

const fn c_delivery_profile(profile: SdkDeliveryProfile) -> YeokchamDeliveryProfile {
    let kind = match profile.kind() {
        SdkDeliveryProfileKind::Direct => crate::YEOKCHAM_DELIVERY_PROFILE_DIRECT,
        SdkDeliveryProfileKind::TorMaildrop => crate::YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP,
        SdkDeliveryProfileKind::LocalMesh => crate::YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH,
    };
    let direct_ip_disclosure_warning = if profile.has_direct_ip_disclosure_warning() {
        crate::YEOKCHAM_DIRECT_IP_DISCLOSURE_WARNING
    } else {
        0
    };
    YeokchamDeliveryProfile {
        kind,
        direct_ip_disclosure_warning,
    }
}

unsafe fn c_identity_public_key(identity: *const u8) -> Option<IdentityPublicKey> {
    if identity.is_null() {
        return None;
    }
    let mut bytes = [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES];
    unsafe {
        std::ptr::copy_nonoverlapping(
            identity,
            bytes.as_mut_ptr(),
            yeokcham_core::ED25519_PUBLIC_KEY_BYTES,
        );
    };
    IdentityPublicKey::from_bytes(bytes).ok()
}

fn c_event(envelope: SdkEventEnvelope) -> YeokchamEvent {
    let (kind, message_identifier) = match envelope.event() {
        SdkEvent::ClientStarted => (
            YEOKCHAM_EVENT_CLIENT_STARTED,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES],
        ),
        SdkEvent::ClientStopped => (
            YEOKCHAM_EVENT_CLIENT_STOPPED,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES],
        ),
        SdkEvent::MessageQueued(identifier) => {
            (YEOKCHAM_EVENT_MESSAGE_QUEUED, *identifier.as_bytes())
        }
        SdkEvent::MessageDelivered(identifier) => {
            (YEOKCHAM_EVENT_MESSAGE_DELIVERED, *identifier.as_bytes())
        }
        SdkEvent::MessageDeliveryFailed(identifier) => (
            YEOKCHAM_EVENT_MESSAGE_DELIVERY_FAILED,
            *identifier.as_bytes(),
        ),
    };
    YeokchamEvent {
        version: envelope.version(),
        sequence: envelope.sequence(),
        kind,
        message_identifier,
    }
}

fn allocate_buffer(bytes: &[u8]) -> Result<*mut YeokchamBuffer, YeokchamStatus> {
    let active_buffers = active_buffers();
    let mut buffers = active_buffers.lock().map_err(|_| YeokchamStatus::State)?;
    if buffers.len() >= MAX_C_ABI_BUFFERS {
        return Err(YeokchamStatus::ResourceLimit);
    }
    let identifier = next_handle_identifier().ok_or(YeokchamStatus::ResourceLimit)?;
    if buffers.insert(identifier, bytes.to_vec()).is_some() {
        return Err(YeokchamStatus::ResourceLimit);
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
                                YeokchamStatus::Ok as i32,
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

fn is_active_client(client: *const YeokchamClient) -> Result<bool, YeokchamStatus> {
    let identifier = client.addr();
    if identifier == 0 {
        return Ok(false);
    }
    let clients = active_clients().lock().map_err(|_| YeokchamStatus::State)?;
    Ok(clients.contains_key(&identifier))
}

fn map_sdk_client_error(error: &SdkClientError) -> YeokchamStatus {
    match error {
        SdkClientError::Configuration(_) => YeokchamStatus::InvalidInput,
        SdkClientError::EventSequenceExhausted => YeokchamStatus::ResourceLimit,
        SdkClientError::DaemonModeUnavailable
        | SdkClientError::AlreadyRunning
        | SdkClientError::NotRunning
        | SdkClientError::State
        | SdkClientError::Engine
        | SdkClientError::AsyncTask => YeokchamStatus::State,
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

fn map_sdk_identity_error(error: &SdkIdentityError) -> YeokchamStatus {
    match error {
        SdkIdentityError::AlreadyInitialized
        | SdkIdentityError::NotInitialized
        | SdkIdentityError::Generation
        | SdkIdentityError::InvalidStoredIdentity
        | SdkIdentityError::Keystore => YeokchamStatus::State,
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

fn map_sdk_contact_error(error: &SdkContactError) -> YeokchamStatus {
    match error {
        SdkContactError::InvalidInvitation
        | SdkContactError::InvalidRotation
        | SdkContactError::InvalidVerification => YeokchamStatus::InvalidInput,
        SdkContactError::Identity(_)
        | SdkContactError::SelfContact
        | SdkContactError::UnknownContact
        | SdkContactError::NotPending
        | SdkContactError::NotVerified
        | SdkContactError::ReplacementAlreadyKnown
        | SdkContactError::AlreadyRevoked
        | SdkContactError::State => YeokchamStatus::State,
    }
}

const fn map_sdk_delivery_profile_policy_error(
    error: SdkDeliveryProfilePolicyError,
) -> YeokchamStatus {
    match error {
        SdkDeliveryProfilePolicyError::NoAllowedProfiles
        | SdkDeliveryProfilePolicyError::NoAllowedLocalMeshTransports
        | SdkDeliveryProfilePolicyError::TooManyLocalMeshTransports => YeokchamStatus::InvalidInput,
        SdkDeliveryProfilePolicyError::DirectDisallowed
        | SdkDeliveryProfilePolicyError::TorMaildropDisallowed
        | SdkDeliveryProfilePolicyError::LocalMeshDisallowed
        | SdkDeliveryProfilePolicyError::LocalMeshTransportDisallowed
        | SdkDeliveryProfilePolicyError::DirectToTorRequiresExplicitSelection => {
            YeokchamStatus::State
        }
    }
}

const fn map_sdk_message_error(error: &SdkMessageError) -> YeokchamStatus {
    match error {
        SdkMessageError::EventSequenceExhausted | SdkMessageError::QueueFull => {
            YeokchamStatus::ResourceLimit
        }
        SdkMessageError::Identity(_)
        | SdkMessageError::ClientNotRunning
        | SdkMessageError::State => YeokchamStatus::State,
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
    use std::sync::atomic::{AtomicUsize, Ordering};

    use yeokcham_core::{IdentityKeypair, IdentityPublicKey};
    use yeokcham_protocol::{
        ContactInvitation, EncryptedMessageEnvelope, QrVerificationPayload, SafetyNumberFingerprint,
    };

    use super::{
        CLIENT_TEST_LOCK, MAX_C_ABI_BUFFERS, MAX_C_ABI_CALLBACK_WORKERS,
        MAX_C_ABI_ERROR_DETAIL_BYTES, MAX_C_ABI_EVENT_SUBSCRIPTIONS, MAX_C_ABI_PENDING_COMPLETIONS,
        MAX_C_ABI_SECRET_BUFFER_BYTES, MAX_C_ABI_STATE_DIRECTORY_BYTES, PENDING_COMPLETIONS,
        SdkClientError, SdkEvent, SdkEventEnvelope, YeokchamContact, YeokchamDeliveryProfile,
        YeokchamDeliveryProfilePolicy, YeokchamEvent, YeokchamStatus, c_event,
        map_sdk_client_error, sdk_client_error_detail, yeokcham_buffer_data,
        yeokcham_buffer_length, yeokcham_buffer_release, yeokcham_client_complete_async,
        yeokcham_client_config_builder_build, yeokcham_client_config_builder_create,
        yeokcham_client_config_builder_release,
        yeokcham_client_config_builder_set_event_buffer_capacity,
        yeokcham_client_config_builder_set_state_directory, yeokcham_client_contact_get,
        yeokcham_client_contact_import, yeokcham_client_contact_revoke,
        yeokcham_client_contact_verify_qr, yeokcham_client_contact_verify_safety_number,
        yeokcham_client_copy_last_error_detail, yeokcham_client_create,
        yeokcham_client_identity_create, yeokcham_client_identity_load,
        yeokcham_client_message_send, yeokcham_client_release, yeokcham_client_start,
        yeokcham_client_stop, yeokcham_client_subscribe_events,
        yeokcham_client_take_last_error_detail, yeokcham_delivery_profile_select,
        yeokcham_event_subscription_poll, yeokcham_event_subscription_release,
        yeokcham_secret_buffer_zeroize,
    };

    #[test]
    fn zeroizes_bounded_caller_owned_secret_buffers() {
        let mut secret = [0xA5; 32];
        assert_eq!(
            unsafe { yeokcham_secret_buffer_zeroize(secret.as_mut_ptr(), secret.len()) },
            YeokchamStatus::Ok
        );
        assert_eq!(secret, [0; 32]);
    }

    #[test]
    fn zeroization_rejects_invalid_and_oversized_buffers() {
        let mut byte = 0xA5;
        assert_eq!(
            unsafe { yeokcham_secret_buffer_zeroize(std::ptr::null_mut(), 1) },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            unsafe { yeokcham_secret_buffer_zeroize(&raw mut byte, 0) },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            unsafe {
                yeokcham_secret_buffer_zeroize(&raw mut byte, MAX_C_ABI_SECRET_BUFFER_BYTES + 1)
            },
            YeokchamStatus::ResourceLimit
        );
        assert_eq!(byte, 0xA5);
    }

    #[test]
    fn c_identity_operations_create_and_load_one_redacted_handle_identity() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        let mut public_key = [0xA5; yeokcham_core::ED25519_PUBLIC_KEY_BYTES];
        let mut loaded_key = [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES];

        assert!(!client.is_null());

        assert_eq!(
            unsafe { yeokcham_client_identity_load(client, public_key.as_mut_ptr()) },
            YeokchamStatus::State
        );
        assert_eq!(public_key, [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe { yeokcham_client_identity_create(client, public_key.as_mut_ptr()) },
            YeokchamStatus::Ok
        );
        assert_ne!(public_key, [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe { yeokcham_client_identity_load(client, loaded_key.as_mut_ptr()) },
            YeokchamStatus::Ok
        );
        assert_eq!(loaded_key, public_key);
        assert_eq!(
            unsafe { yeokcham_client_identity_create(client, loaded_key.as_mut_ptr()) },
            YeokchamStatus::State
        );
        assert_eq!(loaded_key, [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES]);
        assert_eq!(
            unsafe { yeokcham_client_identity_load(client, std::ptr::null_mut()) },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
    }

    #[test]
    fn c_contact_operations_import_verify_get_and_revoke_contacts() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory =
            std::env::temp_dir().join(format!("yeokcham-ffi-contacts-{}", std::process::id()));
        let directory = directory.to_string_lossy().into_owned();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        let mut local_identity = [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES];
        let remote = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote)
            .unwrap()
            .encode()
            .unwrap();
        let mut contact = YeokchamContact::default();

        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        assert_eq!(
            unsafe { yeokcham_client_identity_create(client, local_identity.as_mut_ptr()) },
            YeokchamStatus::Ok
        );
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::Ok);
        assert_eq!(
            unsafe {
                yeokcham_client_contact_import(client, invitation.as_ptr(), &raw mut contact)
            },
            YeokchamStatus::Ok
        );
        assert_eq!(contact.identity, *remote.public_key().as_bytes());
        assert_eq!(contact.status, crate::YEOKCHAM_CONTACT_STATUS_PENDING);
        assert_eq!(
            contact.verification,
            crate::YEOKCHAM_CONTACT_VERIFICATION_NONE
        );
        let local_identity = IdentityPublicKey::from_bytes(local_identity).unwrap();
        let qr_payload = QrVerificationPayload::new(local_identity, remote.public_key())
            .unwrap()
            .encode()
            .unwrap();
        assert_eq!(
            unsafe {
                yeokcham_client_contact_verify_qr(client, qr_payload.as_ptr(), &raw mut contact)
            },
            YeokchamStatus::Ok
        );
        assert_eq!(contact.status, crate::YEOKCHAM_CONTACT_STATUS_VERIFIED);
        assert_eq!(
            contact.verification,
            crate::YEOKCHAM_CONTACT_VERIFICATION_QR
        );
        assert_eq!(
            unsafe {
                yeokcham_client_contact_get(
                    client,
                    remote.public_key().as_bytes().as_ptr(),
                    &raw mut contact,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            unsafe {
                yeokcham_client_contact_revoke(
                    client,
                    remote.public_key().as_bytes().as_ptr(),
                    &raw mut contact,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(contact.status, crate::YEOKCHAM_CONTACT_STATUS_REVOKED);
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_contact_operations_verify_safety_numbers() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory = std::env::temp_dir().join(format!(
            "yeokcham-ffi-contact-safety-{}",
            std::process::id()
        ));
        let directory = directory.to_string_lossy().into_owned();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        let mut local_identity = [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES];
        let remote = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote)
            .unwrap()
            .encode()
            .unwrap();
        let mut contact = YeokchamContact::default();

        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        assert_eq!(
            unsafe { yeokcham_client_identity_create(client, local_identity.as_mut_ptr()) },
            YeokchamStatus::Ok
        );
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::Ok);
        assert_eq!(
            unsafe {
                yeokcham_client_contact_import(client, invitation.as_ptr(), &raw mut contact)
            },
            YeokchamStatus::Ok
        );
        let local_identity = IdentityPublicKey::from_bytes(local_identity).unwrap();
        let fingerprint =
            SafetyNumberFingerprint::derive(&local_identity, &remote.public_key()).unwrap();
        assert_eq!(
            unsafe {
                yeokcham_client_contact_verify_safety_number(
                    client,
                    remote.public_key().as_bytes().as_ptr(),
                    fingerprint.as_bytes().as_ptr(),
                    &raw mut contact,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(contact.identity, *remote.public_key().as_bytes());
        assert_eq!(contact.status, crate::YEOKCHAM_CONTACT_STATUS_VERIFIED);
        assert_eq!(
            contact.verification,
            crate::YEOKCHAM_CONTACT_VERIFICATION_SAFETY_NUMBER
        );
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_delivery_profile_operations_select_explicit_bounded_profiles() {
        let max_local_mesh_transports =
            u32::try_from(crate::YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS).unwrap();
        let policy = YeokchamDeliveryProfilePolicy {
            direct_allowed: 1,
            tor_maildrop_allowed: 1,
            local_mesh_transport_count: max_local_mesh_transports,
            local_mesh_transports: [
                crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_LAN,
                crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT,
                crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_DIRECT,
                crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_BLUETOOTH,
            ],
        };
        let mut profile = YeokchamDeliveryProfile::default();

        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    &raw const policy,
                    crate::YEOKCHAM_DELIVERY_PROFILE_DIRECT,
                    0,
                    crate::YEOKCHAM_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED,
                    &raw mut profile,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(profile.kind, crate::YEOKCHAM_DELIVERY_PROFILE_DIRECT);
        assert_eq!(
            profile.direct_ip_disclosure_warning,
            crate::YEOKCHAM_DIRECT_IP_DISCLOSURE_WARNING
        );
        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    &raw const policy,
                    crate::YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(profile.kind, crate::YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP);
        assert_eq!(profile.direct_ip_disclosure_warning, 0);
        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    &raw const policy,
                    crate::YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH,
                    crate::YEOKCHAM_LOCAL_MESH_TRANSPORT_BLUETOOTH,
                    0,
                    &raw mut profile,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(profile.kind, crate::YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH);
        assert_eq!(profile.direct_ip_disclosure_warning, 0);
    }

    #[test]
    fn c_delivery_profile_operations_reject_invalid_and_disallowed_selection() {
        let max_local_mesh_transports =
            u32::try_from(crate::YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS).unwrap();
        let mut profile = YeokchamDeliveryProfile {
            kind: u32::MAX,
            direct_ip_disclosure_warning: u32::MAX,
        };
        let disallow_direct = YeokchamDeliveryProfilePolicy {
            direct_allowed: 0,
            tor_maildrop_allowed: 1,
            local_mesh_transport_count: 0,
            local_mesh_transports: [0; crate::YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS],
        };
        let unbounded = YeokchamDeliveryProfilePolicy {
            direct_allowed: 1,
            tor_maildrop_allowed: 1,
            local_mesh_transport_count: max_local_mesh_transports + 1,
            local_mesh_transports: [0; crate::YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS],
        };

        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    &raw const disallow_direct,
                    crate::YEOKCHAM_DELIVERY_PROFILE_DIRECT,
                    0,
                    crate::YEOKCHAM_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED,
                    &raw mut profile,
                )
            },
            YeokchamStatus::State
        );
        assert_eq!(profile, YeokchamDeliveryProfile::default());
        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    &raw const disallow_direct,
                    crate::YEOKCHAM_DELIVERY_PROFILE_DIRECT,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(profile, YeokchamDeliveryProfile::default());
        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    &raw const unbounded,
                    crate::YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(profile, YeokchamDeliveryProfile::default());
        assert_eq!(
            unsafe {
                yeokcham_delivery_profile_select(
                    std::ptr::null(),
                    crate::YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP,
                    0,
                    0,
                    &raw mut profile,
                )
            },
            YeokchamStatus::InvalidInput
        );
    }

    #[test]
    fn c_message_send_queues_a_canonical_bounded_envelope_and_emits_an_event() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory =
            std::env::temp_dir().join(format!("yeokcham-ffi-message-{}", std::process::id()));
        let directory = directory.to_string_lossy().into_owned();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        let mut local_identity = [0; yeokcham_core::ED25519_PUBLIC_KEY_BYTES];
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();
        let mut message_identifier = [0xA5; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES];

        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        assert_eq!(
            unsafe { yeokcham_client_identity_create(client, local_identity.as_mut_ptr()) },
            YeokchamStatus::Ok
        );
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::Ok);
        let subscription = yeokcham_client_subscribe_events(client);
        assert!(!subscription.is_null());
        assert_eq!(
            unsafe {
                yeokcham_client_message_send(
                    client,
                    recipient.as_bytes().as_ptr(),
                    envelope.as_ptr(),
                    envelope.len(),
                    100,
                    60,
                    message_identifier.as_mut_ptr(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_ne!(
            message_identifier,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        let mut event = YeokchamEvent::default();
        let mut has_event = 0;
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            YeokchamStatus::Ok
        );
        assert_eq!(has_event, 1);
        assert_eq!(event.kind, crate::YEOKCHAM_EVENT_MESSAGE_QUEUED);
        assert_eq!(event.message_identifier, message_identifier);
        assert_eq!(
            yeokcham_event_subscription_release(subscription),
            YeokchamStatus::Ok
        );
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn c_message_send_rejects_invalid_and_oversized_input_after_clearing_output() {
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let byte = 0;
        let mut message_identifier = [0xA5; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES];
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();

        assert_eq!(
            unsafe {
                yeokcham_client_message_send(
                    std::ptr::null_mut(),
                    recipient.as_bytes().as_ptr(),
                    &raw const byte,
                    crate::YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES + 1,
                    100,
                    60,
                    message_identifier.as_mut_ptr(),
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            message_identifier,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        message_identifier.fill(0xA5);
        assert_eq!(
            unsafe {
                yeokcham_client_message_send(
                    std::ptr::null_mut(),
                    recipient.as_bytes().as_ptr(),
                    envelope.as_ptr(),
                    envelope.len(),
                    u64::MAX,
                    1,
                    message_identifier.as_mut_ptr(),
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            message_identifier,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
    }

    #[test]
    fn c_message_send_redacts_missing_identity_failures() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let directory = std::env::temp_dir().join(format!(
            "yeokcham-ffi-message-missing-identity-{}",
            std::process::id()
        ));
        let directory = directory.to_string_lossy().into_owned();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();
        let mut message_identifier = [0xA5; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES];
        let mut detail = [0; MAX_C_ABI_ERROR_DETAIL_BYTES];
        let mut detail_length = 0;

        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    directory.as_bytes().as_ptr(),
                    directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::Ok);
        assert_eq!(
            unsafe {
                yeokcham_client_message_send(
                    client,
                    recipient.as_bytes().as_ptr(),
                    envelope.as_ptr(),
                    envelope.len(),
                    100,
                    60,
                    message_identifier.as_mut_ptr(),
                )
            },
            YeokchamStatus::State
        );
        assert_eq!(
            message_identifier,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        assert_eq!(
            unsafe {
                yeokcham_client_copy_last_error_detail(
                    client,
                    detail.as_mut_ptr(),
                    detail.len(),
                    &raw mut detail_length,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(&detail[..detail_length], b"sdk_message_identity");
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn client_configuration_builder_attaches_a_validated_embedded_configuration() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        let state_directory = std::env::temp_dir().join(format!(
            "yeokcham-ffi-client-lifecycle-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    state_directory.as_bytes().as_ptr(),
                    state_directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::Ok);
        let subscriptions = (0..MAX_C_ABI_EVENT_SUBSCRIPTIONS)
            .map(|_| yeokcham_client_subscribe_events(client))
            .collect::<Vec<_>>();
        assert!(
            subscriptions
                .iter()
                .all(|subscription| !subscription.is_null())
        );
        assert!(yeokcham_client_subscribe_events(client).is_null());
        for subscription in subscriptions {
            assert_eq!(
                yeokcham_event_subscription_release(subscription),
                YeokchamStatus::Ok
            );
            assert_eq!(
                yeokcham_event_subscription_release(subscription),
                YeokchamStatus::InvalidInput
            );
        }
        assert_event_polling_contract(client);
        assert!(yeokcham_client_subscribe_events(client).is_null());
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::State);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    fn assert_event_polling_contract(client: *mut crate::YeokchamClient) {
        let subscription = yeokcham_client_subscribe_events(client);
        assert!(!subscription.is_null());
        let mut event = YeokchamEvent {
            version: u32::MAX,
            sequence: u64::MAX,
            kind: u32::MAX,
            message_identifier: [u8::MAX; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES],
        };
        let mut has_event = 1;
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            YeokchamStatus::Ok
        );
        assert_eq!(event, YeokchamEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(client.cast(), &raw mut event, &raw mut has_event)
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(event, YeokchamEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(
                    std::ptr::null_mut(),
                    &raw mut event,
                    &raw mut has_event,
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(event, YeokchamEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(
                    subscription,
                    std::ptr::null_mut(),
                    &raw mut has_event,
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::State);
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            YeokchamStatus::Ok
        );
        assert_eq!(event.version, crate::YEOKCHAM_EVENT_VERSION);
        assert_eq!(event.sequence, 2);
        assert_eq!(event.kind, crate::YEOKCHAM_EVENT_CLIENT_STOPPED);
        assert_eq!(
            event.message_identifier,
            [0; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES]
        );
        assert_eq!(has_event, 1);
        assert_eq!(
            unsafe {
                yeokcham_event_subscription_poll(subscription, &raw mut event, &raw mut has_event)
            },
            YeokchamStatus::State
        );
        assert_eq!(event, YeokchamEvent::default());
        assert_eq!(has_event, 0);
        assert_eq!(
            yeokcham_event_subscription_release(subscription),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_event_subscription_release(subscription),
            YeokchamStatus::InvalidInput
        );
    }

    #[test]
    fn client_lifecycle_rejects_unconfigured_and_invalid_clients() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::State);
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::State);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::InvalidInput);
        assert_eq!(
            yeokcham_client_stop(std::ptr::null_mut()),
            YeokchamStatus::InvalidInput
        );
    }

    #[test]
    fn event_records_preserve_the_stable_kind_and_identifier_contract() {
        let identifier = yeokcham_protocol::MessageIdentifier::from_bytes([7; 16]).unwrap();
        let cases = [
            (
                SdkEvent::ClientStarted,
                crate::YEOKCHAM_EVENT_CLIENT_STARTED,
                [0; 16],
            ),
            (
                SdkEvent::ClientStopped,
                crate::YEOKCHAM_EVENT_CLIENT_STOPPED,
                [0; 16],
            ),
            (
                SdkEvent::MessageQueued(identifier),
                crate::YEOKCHAM_EVENT_MESSAGE_QUEUED,
                *identifier.as_bytes(),
            ),
            (
                SdkEvent::MessageDelivered(identifier),
                crate::YEOKCHAM_EVENT_MESSAGE_DELIVERED,
                *identifier.as_bytes(),
            ),
            (
                SdkEvent::MessageDeliveryFailed(identifier),
                crate::YEOKCHAM_EVENT_MESSAGE_DELIVERY_FAILED,
                *identifier.as_bytes(),
            ),
        ];
        for (event, kind, message_identifier) in cases {
            let record = c_event(SdkEventEnvelope::new(42, event).unwrap());
            assert_eq!(record.version, crate::YEOKCHAM_EVENT_VERSION);
            assert_eq!(record.sequence, 42);
            assert_eq!(record.kind, kind);
            assert_eq!(record.message_identifier, message_identifier);
        }
    }

    #[test]
    fn sdk_client_errors_have_stable_c_status_mappings() {
        assert_eq!(
            map_sdk_client_error(&SdkClientError::Configuration(
                yeokcham_sdk::SdkConfigError::InvalidStateDirectory
            )),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            map_sdk_client_error(&SdkClientError::EventSequenceExhausted),
            YeokchamStatus::ResourceLimit
        );
        for error in [
            SdkClientError::DaemonModeUnavailable,
            SdkClientError::AlreadyRunning,
            SdkClientError::NotRunning,
            SdkClientError::State,
            SdkClientError::Engine,
            SdkClientError::AsyncTask,
        ] {
            assert_eq!(map_sdk_client_error(&error), YeokchamStatus::State);
        }
    }

    #[test]
    fn sdk_client_error_details_are_bounded_redacted_tokens() {
        for error in [
            SdkClientError::Configuration(yeokcham_sdk::SdkConfigError::InvalidStateDirectory),
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

    fn assert_library_buffer_detail_transfer_and_capacity(client: *mut crate::YeokchamClient) {
        let mut owned_detail = std::ptr::null_mut();
        assert_eq!(
            unsafe { yeokcham_client_take_last_error_detail(client, &raw mut owned_detail) },
            YeokchamStatus::Ok
        );
        assert!(!owned_detail.is_null());
        assert_eq!(
            yeokcham_buffer_length(owned_detail),
            b"sdk_already_running".len()
        );
        let owned_data = yeokcham_buffer_data(owned_detail);
        assert_eq!(
            unsafe { std::slice::from_raw_parts(owned_data, yeokcham_buffer_length(owned_detail)) },
            b"sdk_already_running"
        );
        let mut detail_length = 0;
        assert_eq!(
            unsafe {
                yeokcham_client_copy_last_error_detail(
                    client,
                    std::ptr::null_mut(),
                    0,
                    &raw mut detail_length,
                )
            },
            YeokchamStatus::State
        );
        assert_eq!(yeokcham_buffer_release(owned_detail), YeokchamStatus::Ok);
        assert_eq!(yeokcham_buffer_data(owned_detail), std::ptr::null());
        assert_eq!(yeokcham_buffer_length(owned_detail), 0);
        assert_eq!(
            yeokcham_buffer_release(owned_detail),
            YeokchamStatus::InvalidInput
        );
        let mut buffers = Vec::with_capacity(MAX_C_ABI_BUFFERS);
        for _ in 0..MAX_C_ABI_BUFFERS {
            assert_eq!(yeokcham_client_start(client), YeokchamStatus::State);
            let mut buffer = std::ptr::null_mut();
            assert_eq!(
                unsafe { yeokcham_client_take_last_error_detail(client, &raw mut buffer) },
                YeokchamStatus::Ok
            );
            buffers.push(buffer);
        }
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::State);
        let mut overflow = std::ptr::null_mut();
        assert_eq!(
            unsafe { yeokcham_client_take_last_error_detail(client, &raw mut overflow) },
            YeokchamStatus::ResourceLimit
        );
        assert!(overflow.is_null());
        for buffer in buffers {
            assert_eq!(yeokcham_buffer_release(buffer), YeokchamStatus::Ok);
        }
    }

    #[test]
    fn client_start_maps_an_engine_state_directory_conflict_to_state() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let state_directory = std::env::temp_dir().join(format!(
            "yeokcham-ffi-client-start-conflict-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        let first = yeokcham_client_create();
        let second = yeokcham_client_create();
        for client in [first, second] {
            let builder = yeokcham_client_config_builder_create();
            assert_eq!(
                unsafe {
                    yeokcham_client_config_builder_set_state_directory(
                        builder,
                        state_directory.as_bytes().as_ptr(),
                        state_directory.len(),
                    )
                },
                YeokchamStatus::Ok
            );
            assert_eq!(
                yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
                YeokchamStatus::Ok
            );
            assert_eq!(
                yeokcham_client_config_builder_build(builder, client),
                YeokchamStatus::Ok
            );
            assert_eq!(
                yeokcham_client_config_builder_release(builder),
                YeokchamStatus::Ok
            );
        }
        assert_eq!(yeokcham_client_start(first), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_start(second), YeokchamStatus::State);
        let mut detail_length = 0;
        assert_eq!(
            unsafe {
                yeokcham_client_copy_last_error_detail(
                    second,
                    std::ptr::null_mut(),
                    0,
                    &raw mut detail_length,
                )
            },
            YeokchamStatus::ResourceLimit
        );
        assert_eq!(detail_length, b"sdk_already_running".len());
        let mut short_buffer = [0xA5; 32];
        assert_eq!(
            unsafe {
                yeokcham_client_copy_last_error_detail(
                    second,
                    short_buffer.as_mut_ptr(),
                    detail_length - 1,
                    &raw mut detail_length,
                )
            },
            YeokchamStatus::ResourceLimit
        );
        assert!(short_buffer.iter().all(|byte| *byte == 0xA5));
        let mut detail = [0; MAX_C_ABI_ERROR_DETAIL_BYTES];
        assert_eq!(
            unsafe {
                yeokcham_client_copy_last_error_detail(
                    second,
                    detail.as_mut_ptr(),
                    detail.len(),
                    &raw mut detail_length,
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(&detail[..detail_length], b"sdk_already_running");
        assert_library_buffer_detail_transfer_and_capacity(second);
        assert_eq!(
            unsafe {
                yeokcham_client_copy_last_error_detail(
                    second,
                    std::ptr::null_mut(),
                    0,
                    std::ptr::null_mut(),
                )
            },
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_client_stop(first), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(first), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(second), YeokchamStatus::Ok);
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
            assert_eq!(
                yeokcham_client_release(client),
                YeokchamStatus::InvalidInput
            );
            assert_eq!(yeokcham_client_start(client), YeokchamStatus::InvalidInput);
            assert_eq!(yeokcham_client_stop(client), YeokchamStatus::InvalidInput);
            assert_eq!(
                yeokcham_client_complete_async(client, Some(noop_completion), std::ptr::null_mut()),
                YeokchamStatus::InvalidInput
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
        let client = yeokcham_client_create();
        for _ in 0..MAX_C_ABI_PENDING_COMPLETIONS {
            assert_eq!(
                yeokcham_client_complete_async(
                    client,
                    Some(block_completion),
                    std::ptr::null_mut()
                ),
                YeokchamStatus::Ok
            );
        }
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(2);
        while CALLBACK_BACKPRESSURE_STARTED.load(Ordering::Acquire) < MAX_C_ABI_CALLBACK_WORKERS {
            assert!(std::time::Instant::now() < deadline);
            std::thread::yield_now();
        }
        assert_eq!(
            yeokcham_client_complete_async(client, Some(block_completion), std::ptr::null_mut()),
            YeokchamStatus::ResourceLimit
        );
        CALLBACK_BACKPRESSURE_RELEASED.store(true, Ordering::Release);
        wait_for_pending_callbacks(0);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
    }

    #[test]
    fn concurrent_client_start_linearizes_one_successful_transition() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        let state_directory = std::env::temp_dir().join(format!(
            "yeokcham-ffi-client-start-race-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    state_directory.as_bytes().as_ptr(),
                    state_directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 8),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        let client_address = client.addr();
        let start = std::sync::Arc::new(std::sync::Barrier::new(2));
        let mut workers = Vec::with_capacity(2);
        for _ in 0..2 {
            let start = std::sync::Arc::clone(&start);
            workers.push(std::thread::spawn(move || {
                start.wait();
                yeokcham_client_start(std::ptr::without_provenance_mut(client_address))
            }));
        }
        let statuses = workers
            .into_iter()
            .map(|worker| worker.join().unwrap())
            .collect::<Vec<_>>();
        assert_eq!(
            statuses
                .iter()
                .filter(|status| **status == YeokchamStatus::Ok)
                .count(),
            1
        );
        assert_eq!(
            statuses
                .iter()
                .filter(|status| **status == YeokchamStatus::State)
                .count(),
            1
        );
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn client_configuration_builder_rejects_missing_invalid_and_unbounded_input() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        let builder = yeokcham_client_config_builder_create();
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(builder, std::ptr::null(), 1)
            },
            YeokchamStatus::InvalidInput
        );
        let invalid_utf8 = [0xFF];
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    invalid_utf8.as_ptr(),
                    invalid_utf8.len(),
                )
            },
            YeokchamStatus::InvalidInput
        );
        let embedded_nul = b"/tmp/yeokcham\0ffi";
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    embedded_nul.as_ptr(),
                    embedded_nul.len(),
                )
            },
            YeokchamStatus::InvalidInput
        );
        let byte = b'x';
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    &raw const byte,
                    MAX_C_ABI_STATE_DIRECTORY_BYTES + 1,
                )
            },
            YeokchamStatus::InvalidInput
        );
        let state_directory = b"relative-state";
        assert_eq!(
            unsafe {
                yeokcham_client_config_builder_set_state_directory(
                    builder,
                    state_directory.as_ptr(),
                    state_directory.len(),
                )
            },
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_set_event_buffer_capacity(builder, 0),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            yeokcham_client_config_builder_release(builder),
            YeokchamStatus::Ok
        );
        assert_eq!(
            yeokcham_client_config_builder_build(builder, client),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
    }

    unsafe extern "C" {
        fn yeokcham_c_consumer_conformance() -> i32;
        fn yeokcham_c_delivery_profile_operations() -> i32;
        fn yeokcham_c_embedded_client_lifecycle(
            state_directory: *const u8,
            state_directory_length: usize,
        ) -> i32;
    }

    #[test]
    fn c_consumer_conformance_passes() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        assert_eq!(unsafe { yeokcham_c_consumer_conformance() }, 0);
    }

    #[test]
    fn c_consumer_runs_delivery_profile_operations() {
        assert_eq!(unsafe { yeokcham_c_delivery_profile_operations() }, 0);
    }

    #[test]
    fn c_consumer_runs_the_embedded_client_lifecycle() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let state_directory = std::env::temp_dir().join(format!(
            "yeokcham-ffi-c-embedded-client-{}",
            std::process::id()
        ));
        let state_directory = state_directory.to_string_lossy().into_owned();
        let status = unsafe {
            yeokcham_c_embedded_client_lifecycle(
                state_directory.as_bytes().as_ptr(),
                state_directory.len(),
            )
        };
        let cleanup = std::fs::remove_dir_all(state_directory);
        assert_eq!(status, 0);
        cleanup.unwrap();
    }
}
