#![deny(unsafe_op_in_unsafe_fn)]

#[allow(unsafe_code)]
mod c_abi;

pub const YEOKCHAM_ABI_VERSION_MAJOR: u32 = 1;
pub const YEOKCHAM_ABI_VERSION_MINOR: u32 = 0;
pub const YEOKCHAM_ABI_VERSION: u32 = 1;
pub const YEOKCHAM_ABI_NEGOTIATION_REJECTED: u32 = 0;
pub const YEOKCHAM_EVENT_VERSION: u32 = yeokcham_sdk::SDK_EVENT_ENVELOPE_VERSION;
pub const YEOKCHAM_EVENT_CLIENT_STARTED: u32 = 1;
pub const YEOKCHAM_EVENT_CLIENT_STOPPED: u32 = 2;
pub const YEOKCHAM_EVENT_MESSAGE_QUEUED: u32 = 3;
pub const YEOKCHAM_EVENT_MESSAGE_DELIVERED: u32 = 4;
pub const YEOKCHAM_EVENT_MESSAGE_DELIVERY_FAILED: u32 = 5;
pub const YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES: usize = yeokcham_core::ED25519_PUBLIC_KEY_BYTES;
pub const YEOKCHAM_RECOVERY_ARCHIVE_BYTES: usize = yeokcham_protocol::IDENTITY_EXPORT_BYTES;
pub const YEOKCHAM_MAX_RECOVERY_PASSPHRASE_BYTES: usize =
    yeokcham_sdk::MAX_SDK_RECOVERY_PASSPHRASE_BYTES;
pub const YEOKCHAM_CONTACT_INVITATION_BYTES: usize = yeokcham_protocol::CONTACT_INVITATION_BYTES;
pub const YEOKCHAM_CONTACT_STATUS_PENDING: u32 = 1;
pub const YEOKCHAM_CONTACT_STATUS_VERIFIED: u32 = 2;
pub const YEOKCHAM_CONTACT_STATUS_REVOKED: u32 = 3;
pub const YEOKCHAM_CONTACT_VERIFICATION_NONE: u32 = 0;
pub const YEOKCHAM_CONTACT_VERIFICATION_QR: u32 = 1;
pub const YEOKCHAM_CONTACT_VERIFICATION_SAFETY_NUMBER: u32 = 2;
pub const YEOKCHAM_QR_VERIFICATION_PAYLOAD_BYTES: usize =
    yeokcham_protocol::QR_VERIFICATION_PAYLOAD_BYTES;
pub const YEOKCHAM_SAFETY_NUMBER_FINGERPRINT_BYTES: usize =
    yeokcham_protocol::SAFETY_NUMBER_FINGERPRINT_BYTES;
pub const YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS: usize = 4;
pub const YEOKCHAM_DELIVERY_PROFILE_DIRECT: u32 = 1;
pub const YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP: u32 = 2;
pub const YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH: u32 = 3;
pub const YEOKCHAM_LOCAL_MESH_TRANSPORT_LAN: u32 = 1;
pub const YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT: u32 = 2;
pub const YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_DIRECT: u32 = 3;
pub const YEOKCHAM_LOCAL_MESH_TRANSPORT_BLUETOOTH: u32 = 4;
pub const YEOKCHAM_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED: u32 = 1;
pub const YEOKCHAM_DIRECT_IP_DISCLOSURE_WARNING: u32 = 1;
pub const YEOKCHAM_MESSAGE_IDENTIFIER_BYTES: usize = yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES;
pub const YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES: usize = yeokcham_protocol::MAX_MESSAGE_PAYLOAD_BYTES;
pub const YEOKCHAM_ATTACHMENT_IDENTIFIER_BYTES: usize =
    yeokcham_protocol::ATTACHMENT_IDENTIFIER_BYTES;
pub const YEOKCHAM_MAX_ATTACHMENT_CHUNKS: usize = yeokcham_sdk::MAX_SDK_ATTACHMENT_CHUNKS;
pub const YEOKCHAM_MAX_ATTACHMENT_CHUNKS_PER_CYCLE: usize =
    yeokcham_sdk::MAX_SDK_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE;
pub const YEOKCHAM_MAX_ATTACHMENT_MANIFEST_BYTES: usize =
    yeokcham_protocol::MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES;
pub const YEOKCHAM_MAX_ATTACHMENT_CHUNK_BYTES: usize =
    yeokcham_protocol::MAX_ENCODED_ATTACHMENT_CHUNK_BYTES;
pub const YEOKCHAM_ATTACHMENT_DELIVERY_COMPLETE: u32 = 1;
pub const YEOKCHAM_ATTACHMENT_DELIVERY_PENDING: u32 = 2;
pub const YEOKCHAM_ATTACHMENT_DELIVERY_RETRYING: u32 = 3;
pub const YEOKCHAM_MAX_CANCELLATION_DEADLINE_MILLISECONDS: u32 = 60_000;

#[repr(C)]
pub struct YeokchamClient {
    _private: u8,
}

#[repr(C)]
pub struct YeokchamClientConfigBuilder {
    _private: u8,
}

#[repr(C)]
pub struct YeokchamBuffer {
    _private: u8,
}

#[repr(C)]
pub struct YeokchamEventSubscription {
    _private: u8,
}

#[repr(C)]
pub struct YeokchamAttachmentTransfer {
    _private: u8,
}

#[repr(C)]
pub struct YeokchamCancellation {
    _private: u8,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamByteSlice {
    pub data: *const u8,
    pub length: usize,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamAttachmentDeliveryCycle {
    pub uploaded: u32,
    pub outcome: u32,
    pub next_pending_index: u32,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamEvent {
    pub version: u32,
    pub sequence: u64,
    pub kind: u32,
    pub message_identifier: [u8; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES],
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamContact {
    pub identity: [u8; yeokcham_core::ED25519_PUBLIC_KEY_BYTES],
    pub status: u32,
    pub verification: u32,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamDeliveryProfilePolicy {
    pub direct_allowed: u32,
    pub tor_maildrop_allowed: u32,
    pub local_mesh_transport_count: u32,
    pub local_mesh_transports: [u32; YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS],
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamDeliveryProfile {
    pub kind: u32,
    pub direct_ip_disclosure_warning: u32,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(i32)]
pub enum YeokchamStatus {
    Ok = 0,
    InvalidInput = 1,
    UnsupportedVersion = 2,
    ResourceLimit = 3,
    State = 4,
}

pub use c_abi::{
    MAX_C_ABI_ATTACHMENT_TRANSFERS, MAX_C_ABI_BUFFERS, MAX_C_ABI_CALLBACK_WORKERS,
    MAX_C_ABI_CANCELLATIONS, MAX_C_ABI_CLIENT_CONFIG_BUILDERS, MAX_C_ABI_CLIENTS,
    MAX_C_ABI_ERROR_DETAIL_BYTES, MAX_C_ABI_EVENT_SUBSCRIPTIONS, MAX_C_ABI_PENDING_COMPLETIONS,
    MAX_C_ABI_SECRET_BUFFER_BYTES, MAX_C_ABI_STATE_DIRECTORY_BYTES,
    YeokchamAttachmentUploadCallback, YeokchamCompletionCallback, yeokcham_abi_negotiate,
    yeokcham_attachment_transfer_create, yeokcham_attachment_transfer_release,
    yeokcham_attachment_transfer_run_cycle, yeokcham_buffer_data, yeokcham_buffer_length,
    yeokcham_buffer_release, yeokcham_cancellation_cancel, yeokcham_cancellation_create,
    yeokcham_cancellation_release, yeokcham_client_complete_async,
    yeokcham_client_config_builder_build, yeokcham_client_config_builder_create,
    yeokcham_client_config_builder_release,
    yeokcham_client_config_builder_set_event_buffer_capacity,
    yeokcham_client_config_builder_set_state_directory, yeokcham_client_contact_get,
    yeokcham_client_contact_import, yeokcham_client_contact_revoke,
    yeokcham_client_contact_verify_qr, yeokcham_client_contact_verify_safety_number,
    yeokcham_client_copy_last_error_detail, yeokcham_client_create,
    yeokcham_client_identity_create, yeokcham_client_identity_export_recovery,
    yeokcham_client_identity_import_recovery, yeokcham_client_identity_load,
    yeokcham_client_message_send, yeokcham_client_release, yeokcham_client_start,
    yeokcham_client_stop, yeokcham_client_subscribe_events, yeokcham_client_take_last_error_detail,
    yeokcham_delivery_profile_select, yeokcham_event_subscription_poll,
    yeokcham_event_subscription_release, yeokcham_event_subscription_wait,
    yeokcham_secret_buffer_zeroize,
};

#[cfg(test)]
mod tests {
    use super::c_abi::CLIENT_TEST_LOCK;
    use super::{
        MAX_C_ABI_ATTACHMENT_TRANSFERS, MAX_C_ABI_CANCELLATIONS, MAX_C_ABI_CLIENTS,
        MAX_C_ABI_PENDING_COMPLETIONS, YEOKCHAM_ABI_NEGOTIATION_REJECTED, YEOKCHAM_ABI_VERSION,
        YEOKCHAM_ABI_VERSION_MAJOR, YEOKCHAM_ABI_VERSION_MINOR, YEOKCHAM_DELIVERY_PROFILE_DIRECT,
        YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH, YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP,
        YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES, YEOKCHAM_MAX_ATTACHMENT_CHUNKS,
        YEOKCHAM_MAX_ATTACHMENT_CHUNKS_PER_CYCLE, YEOKCHAM_MAX_ATTACHMENT_MANIFEST_BYTES,
        YEOKCHAM_MAX_CANCELLATION_DEADLINE_MILLISECONDS, YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS,
        YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES, YEOKCHAM_MAX_RECOVERY_PASSPHRASE_BYTES,
        YEOKCHAM_MESSAGE_IDENTIFIER_BYTES, YEOKCHAM_QR_VERIFICATION_PAYLOAD_BYTES,
        YEOKCHAM_RECOVERY_ARCHIVE_BYTES, YEOKCHAM_SAFETY_NUMBER_FINGERPRINT_BYTES, YeokchamStatus,
        yeokcham_abi_negotiate, yeokcham_client_complete_async, yeokcham_client_create,
        yeokcham_client_release,
    };
    use std::{
        ffi::c_void,
        sync::{Arc, Barrier, Mutex, OnceLock, mpsc},
        thread,
        time::Duration,
    };

    const HEADER: &str = include_str!("../include/yeokcham.h");

    #[test]
    fn published_header_has_the_stable_abi_version() {
        assert_eq!(YEOKCHAM_ABI_VERSION_MAJOR, 1);
        assert_eq!(YEOKCHAM_ABI_VERSION_MINOR, 0);
        assert_eq!(YEOKCHAM_ABI_VERSION, 1);
        assert!(HEADER.contains("#ifndef YEOKCHAM_V1_H\n#define YEOKCHAM_V1_H"));
        assert!(HEADER.contains("#include <stdint.h>"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_VERSION_MAJOR UINT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_VERSION_MINOR UINT32_C(0)"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_VERSION UINT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_NEGOTIATION_REJECTED UINT32_C(0)"));
        assert!(HEADER.contains("#define YEOKCHAM_MAX_CALLBACK_WORKERS UINT32_C(4)"));
        assert!(HEADER.contains("#define YEOKCHAM_MAX_BUFFERS UINT32_C(1024)"));
        assert!(HEADER.contains("#define YEOKCHAM_MAX_ERROR_DETAIL_BYTES UINT32_C(64)"));
        assert!(HEADER.contains("#define YEOKCHAM_MAX_EVENT_SUBSCRIPTIONS UINT32_C(1024)"));
        assert!(HEADER.contains("#define YEOKCHAM_MAX_PENDING_COMPLETIONS UINT32_C(1024)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_VERSION UINT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_MESSAGE_IDENTIFIER_BYTES UINT32_C(16)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_CLIENT_STARTED UINT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_CLIENT_STOPPED UINT32_C(2)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_MESSAGE_QUEUED UINT32_C(3)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_MESSAGE_DELIVERED UINT32_C(4)"));
        assert!(HEADER.contains("#define YEOKCHAM_EVENT_MESSAGE_DELIVERY_FAILED UINT32_C(5)"));
        assert!(HEADER.contains("#define YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES UINT32_C(32)"));
        assert_eq!(YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES, 32);
        assert!(HEADER.contains("#define YEOKCHAM_RECOVERY_ARCHIVE_BYTES UINT32_C(122)"));
        assert_eq!(YEOKCHAM_RECOVERY_ARCHIVE_BYTES, 122);
        assert!(HEADER.contains("#define YEOKCHAM_MAX_RECOVERY_PASSPHRASE_BYTES UINT32_C(1024)"));
        assert_eq!(YEOKCHAM_MAX_RECOVERY_PASSPHRASE_BYTES, 1_024);
        assert!(HEADER.contains("#define YEOKCHAM_CONTACT_INVITATION_BYTES UINT32_C(136)"));
        assert!(HEADER.contains("#define YEOKCHAM_QR_VERIFICATION_PAYLOAD_BYTES UINT32_C(70)"));
        assert_eq!(YEOKCHAM_QR_VERIFICATION_PAYLOAD_BYTES, 70);
        assert!(HEADER.contains("#define YEOKCHAM_SAFETY_NUMBER_FINGERPRINT_BYTES UINT32_C(32)"));
        assert_eq!(YEOKCHAM_SAFETY_NUMBER_FINGERPRINT_BYTES, 32);
        assert!(HEADER.contains("#define YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS UINT32_C(4)"));
        assert_eq!(YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS, 4);
        assert!(HEADER.contains("#define YEOKCHAM_DELIVERY_PROFILE_DIRECT UINT32_C(1)"));
        assert_eq!(YEOKCHAM_DELIVERY_PROFILE_DIRECT, 1);
        assert!(HEADER.contains("#define YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP UINT32_C(2)"));
        assert_eq!(YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP, 2);
        assert!(HEADER.contains("#define YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH UINT32_C(3)"));
        assert_eq!(YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH, 3);
        assert!(HEADER.contains("#define YEOKCHAM_MESSAGE_IDENTIFIER_BYTES UINT32_C(16)"));
        assert_eq!(YEOKCHAM_MESSAGE_IDENTIFIER_BYTES, 16);
        assert!(HEADER.contains("#define YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES UINT32_C(1048544)"));
        assert_eq!(YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES, 1_048_544);
        assert!(HEADER.contains("#define YEOKCHAM_MAX_ATTACHMENT_TRANSFERS UINT32_C(1024)"));
        assert_eq!(MAX_C_ABI_ATTACHMENT_TRANSFERS, 1_024);
        assert!(HEADER.contains("#define YEOKCHAM_ATTACHMENT_IDENTIFIER_BYTES UINT32_C(16)"));
        assert!(HEADER.contains("#define YEOKCHAM_MAX_ATTACHMENT_CHUNKS UINT32_C(1600)"));
        assert_eq!(YEOKCHAM_MAX_ATTACHMENT_CHUNKS, 1_600);
        assert!(HEADER.contains("#define YEOKCHAM_MAX_ATTACHMENT_CHUNKS_PER_CYCLE UINT32_C(64)"));
        assert_eq!(YEOKCHAM_MAX_ATTACHMENT_CHUNKS_PER_CYCLE, 64);
        assert!(
            HEADER.contains("#define YEOKCHAM_MAX_ATTACHMENT_MANIFEST_BYTES UINT32_C(4194304)")
        );
        assert_eq!(YEOKCHAM_MAX_ATTACHMENT_MANIFEST_BYTES, 4 * 1024 * 1024);
        assert!(HEADER.contains("#define YEOKCHAM_MAX_CANCELLATIONS UINT32_C(1024)"));
        assert_eq!(MAX_C_ABI_CANCELLATIONS, 1_024);
        assert!(
            HEADER.contains(
                "#define YEOKCHAM_MAX_CANCELLATION_DEADLINE_MILLISECONDS UINT32_C(60000)"
            )
        );
        assert_eq!(YEOKCHAM_MAX_CANCELLATION_DEADLINE_MILLISECONDS, 60_000);
        assert!(HEADER.contains("typedef struct yeokcham_client yeokcham_client_t;"));
        assert!(HEADER.contains("typedef struct yeokcham_buffer yeokcham_buffer_t;"));
        assert!(
            HEADER.contains(
                "typedef struct yeokcham_event_subscription yeokcham_event_subscription_t;"
            )
        );
        assert!(HEADER.contains("typedef struct yeokcham_event {"));
        assert!(HEADER.contains(
            "typedef struct yeokcham_client_config_builder yeokcham_client_config_builder_t;"
        ));
        assert!(HEADER.ends_with("#endif\n"));
    }

    #[test]
    fn published_client_remains_opaque() {
        assert!(HEADER.contains("typedef struct yeokcham_client yeokcham_client_t;"));
        assert!(!HEADER.contains("struct yeokcham_client {"));
        assert!(HEADER.contains("typedef struct yeokcham_buffer yeokcham_buffer_t;"));
        assert!(!HEADER.contains("struct yeokcham_buffer {"));
        assert!(!HEADER.contains("struct yeokcham_event_subscription {"));
    }

    #[test]
    fn published_status_codes_match_rust() {
        assert_eq!(YeokchamStatus::Ok as i32, 0);
        assert_eq!(YeokchamStatus::InvalidInput as i32, 1);
        assert_eq!(YeokchamStatus::UnsupportedVersion as i32, 2);
        assert_eq!(YeokchamStatus::ResourceLimit as i32, 3);
        assert_eq!(YeokchamStatus::State as i32, 4);
        assert!(HEADER.contains("typedef int32_t yeokcham_status_t;"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_OK INT32_C(0)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_INVALID_INPUT INT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_UNSUPPORTED_VERSION INT32_C(2)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_RESOURCE_LIMIT INT32_C(3)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_STATE INT32_C(4)"));
        assert!(HEADER.contains("yeokcham_client_t *yeokcham_client_create(void);"));
        assert!(HEADER.contains("yeokcham_client_release(yeokcham_client_t *client);"));
        assert!(HEADER.contains("yeokcham_client_start(yeokcham_client_t *client);"));
        assert!(HEADER.contains("yeokcham_client_stop(yeokcham_client_t *client);"));
        assert!(HEADER.contains("yeokcham_client_identity_create("));
        assert!(HEADER.contains("yeokcham_client_identity_load("));
        assert!(HEADER.contains("yeokcham_client_contact_import("));
        assert!(HEADER.contains("yeokcham_client_contact_get("));
        assert!(HEADER.contains("yeokcham_client_contact_revoke("));
        assert!(HEADER.contains("yeokcham_client_contact_verify_qr("));
        assert!(HEADER.contains("yeokcham_client_contact_verify_safety_number("));
        assert!(HEADER.contains("yeokcham_delivery_profile_select("));
        assert!(HEADER.contains("yeokcham_client_message_send("));
        assert!(HEADER.contains("yeokcham_attachment_transfer_create("));
        assert!(HEADER.contains("yeokcham_attachment_transfer_run_cycle("));
        assert!(HEADER.contains("yeokcham_attachment_transfer_release("));
        assert!(HEADER.contains("yeokcham_cancellation_create(void);"));
        assert!(HEADER.contains("yeokcham_cancellation_cancel("));
        assert!(HEADER.contains("yeokcham_cancellation_release("));
        assert!(HEADER.contains("yeokcham_event_subscription_wait("));
        assert!(HEADER.contains("yeokcham_client_copy_last_error_detail("));
        assert!(HEADER.contains("yeokcham_client_take_last_error_detail("));
        assert!(HEADER.contains("yeokcham_buffer_data("));
        assert!(HEADER.contains("yeokcham_buffer_length("));
        assert!(HEADER.contains("yeokcham_buffer_release("));
        assert!(HEADER.contains("yeokcham_client_subscribe_events("));
        assert!(HEADER.contains("yeokcham_event_subscription_poll("));
        assert!(HEADER.contains("yeokcham_event_subscription_release("));
        assert!(HEADER.contains("typedef void (*yeokcham_completion_callback_t)("));
        assert!(HEADER.contains("yeokcham_client_complete_async("));
        assert!(HEADER.contains("yeokcham_client_config_builder_create(void);"));
        assert!(HEADER.contains("yeokcham_client_config_builder_set_state_directory("));
        assert!(HEADER.contains("yeokcham_client_config_builder_set_event_buffer_capacity("));
        assert!(HEADER.contains("yeokcham_client_config_builder_build("));
        assert!(HEADER.contains("uint32_t yeokcham_abi_negotiate(uint32_t requested_version);"));
        assert!(HEADER.contains("#include <stddef.h>"));
        assert!(HEADER.contains("yeokcham_secret_buffer_zeroize(uint8_t *buffer, size_t length);"));
    }

    #[test]
    fn created_clients_release_once_and_reject_invalid_inputs() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        assert!(!client.is_null());
        assert_eq!(
            yeokcham_client_release(std::ptr::null_mut()),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        assert_eq!(
            yeokcham_client_release(client),
            YeokchamStatus::InvalidInput
        );
        const {
            assert!(MAX_C_ABI_CLIENTS > 0);
        }
    }

    #[test]
    fn client_creation_fails_closed_at_capacity() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let clients = (0..MAX_C_ABI_CLIENTS)
            .map(|_| yeokcham_client_create())
            .collect::<Vec<_>>();
        assert!(clients.iter().all(|client| !client.is_null()));
        assert!(yeokcham_client_create().is_null());
        for client in clients {
            assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        }
    }

    static CALLBACK_TEST_LOCK: Mutex<()> = Mutex::new(());
    type Completion = (i32, usize);
    type CompletionSender = mpsc::Sender<Completion>;
    static COMPLETION_SENDER: OnceLock<Mutex<Option<CompletionSender>>> = OnceLock::new();

    extern "C" fn record_completion(status: i32, context: *mut c_void) {
        let sender = COMPLETION_SENDER
            .get_or_init(|| Mutex::new(None))
            .lock()
            .unwrap();
        sender
            .as_ref()
            .unwrap()
            .send((status, context.addr()))
            .unwrap();
    }

    extern "C" fn release_submitted_client(_: i32, context: *mut c_void) {
        let status = yeokcham_client_release(context.cast());
        let sender = COMPLETION_SENDER
            .get_or_init(|| Mutex::new(None))
            .lock()
            .unwrap();
        sender.as_ref().unwrap().send((status as i32, 0)).unwrap();
    }

    #[test]
    fn active_clients_complete_asynchronously_once() {
        let _client_guard = CLIENT_TEST_LOCK.lock().unwrap();
        let _callback_guard = CALLBACK_TEST_LOCK.lock().unwrap();
        let (sender, receiver) = mpsc::channel();
        *COMPLETION_SENDER
            .get_or_init(|| Mutex::new(None))
            .lock()
            .unwrap() = Some(sender);
        let client = yeokcham_client_create();
        assert_eq!(
            yeokcham_client_complete_async(
                client,
                Some(record_completion),
                std::ptr::without_provenance_mut(42),
            ),
            YeokchamStatus::Ok
        );
        assert_eq!(
            receiver.recv_timeout(Duration::from_secs(1)),
            Ok((YeokchamStatus::Ok as i32, 42))
        );
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        *COMPLETION_SENDER.get().unwrap().lock().unwrap() = None;
        const {
            assert!(MAX_C_ABI_PENDING_COMPLETIONS > 0);
        }
    }

    #[test]
    fn completion_callbacks_can_reenter_and_release_the_submitted_client() {
        let _client_guard = CLIENT_TEST_LOCK.lock().unwrap();
        let _callback_guard = CALLBACK_TEST_LOCK.lock().unwrap();
        let (sender, receiver) = mpsc::channel();
        *COMPLETION_SENDER
            .get_or_init(|| Mutex::new(None))
            .lock()
            .unwrap() = Some(sender);
        let client = yeokcham_client_create();
        assert_eq!(
            yeokcham_client_complete_async(client, Some(release_submitted_client), client.cast(),),
            YeokchamStatus::Ok
        );
        assert_eq!(
            receiver.recv_timeout(Duration::from_secs(1)),
            Ok((YeokchamStatus::Ok as i32, 0))
        );
        assert_eq!(
            yeokcham_client_release(client),
            YeokchamStatus::InvalidInput
        );
        *COMPLETION_SENDER.get().unwrap().lock().unwrap() = None;
    }

    #[test]
    fn asynchronous_completion_rejects_invalid_clients_and_callbacks() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        assert_eq!(
            yeokcham_client_complete_async(client, None, std::ptr::null_mut()),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            yeokcham_client_complete_async(
                std::ptr::null(),
                Some(record_completion),
                std::ptr::null_mut()
            ),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
    }

    #[test]
    fn pre_release_version_negotiation_accepts_only_the_published_abi_version() {
        assert_eq!(
            yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION),
            YEOKCHAM_ABI_VERSION
        );
        assert_eq!(
            yeokcham_abi_negotiate(YEOKCHAM_ABI_NEGOTIATION_REJECTED),
            YEOKCHAM_ABI_NEGOTIATION_REJECTED
        );
        assert_eq!(
            yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION + 1),
            YEOKCHAM_ABI_NEGOTIATION_REJECTED
        );
        assert_eq!(
            yeokcham_abi_negotiate(u32::MAX),
            YEOKCHAM_ABI_NEGOTIATION_REJECTED
        );
    }

    #[test]
    fn negotiation_contract_requires_exact_pre_release_compatibility() {
        const CONTRACT: &str = include_str!("../README.md");
        assert!(CONTRACT.contains("## ABI negotiation"));
        assert!(CONTRACT.contains("must equal `YEOKCHAM_ABI_VERSION`"));
        assert!(CONTRACT.contains("must not infer compatibility"));
        assert!(HEADER.contains("returns requested token only on exact pre-release match"));
    }

    #[test]
    fn engine_status_mapping_contract_is_documented() {
        const CONTRACT: &str = include_str!("../README.md");
        assert!(CONTRACT.contains("## Engine status mapping"));
        assert!(
            CONTRACT.contains("SDK configuration failures map to `YEOKCHAM_STATUS_INVALID_INPUT`")
        );
        assert!(CONTRACT.contains(
            "exhausted SDK event sequence space maps to `YEOKCHAM_STATUS_RESOURCE_LIMIT`"
        ));
        assert!(CONTRACT.contains(
            "failures map to `YEOKCHAM_STATUS_STATE` without exposing raw engine details"
        ));
        assert!(CONTRACT.contains("The payload contains no engine strings, paths, or secrets"));
    }

    extern "C" fn noop_completion(_: i32, _: *mut c_void) {}

    #[test]
    fn concurrent_client_lifecycle_accepts_each_client_once() {
        const WORKERS: usize = 32;
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let start = Arc::new(Barrier::new(WORKERS));
        let mut workers = Vec::with_capacity(WORKERS);
        for _ in 0..WORKERS {
            let start = Arc::clone(&start);
            workers.push(thread::spawn(move || {
                start.wait();
                let client = yeokcham_client_create();
                !client.is_null()
                    && yeokcham_client_release(client) == YeokchamStatus::Ok
                    && yeokcham_client_release(client) == YeokchamStatus::InvalidInput
            }));
        }
        assert!(workers.into_iter().all(|worker| worker.join().unwrap()));
    }

    #[test]
    fn concurrent_submission_and_release_return_only_defined_statuses() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        let client = yeokcham_client_create();
        let client_address = client.addr();
        let start = Arc::new(Barrier::new(2));
        let release_start = Arc::clone(&start);
        let release = thread::spawn(move || {
            release_start.wait();
            yeokcham_client_release(std::ptr::without_provenance_mut(client_address))
        });
        start.wait();
        let completion = yeokcham_client_complete_async(
            std::ptr::without_provenance(client_address),
            Some(noop_completion),
            std::ptr::null_mut(),
        );
        assert_eq!(release.join().unwrap(), YeokchamStatus::Ok);
        assert!(matches!(
            completion,
            YeokchamStatus::Ok | YeokchamStatus::InvalidInput
        ));
    }

    #[test]
    fn thread_safety_contract_covers_callbacks_and_client_lifecycle() {
        const THREAD_SAFETY: &str = include_str!("../README.md");
        assert!(THREAD_SAFETY.contains("## Thread safety"));
        assert!(THREAD_SAFETY.contains("Every public C ABI function may be called concurrently"));
        assert!(
            THREAD_SAFETY.contains(
                "Operations on one client, configuration builder, buffer, or event subscription are linearized"
            )
        );
        assert!(THREAD_SAFETY.contains("library-created background workers"));
        assert!(THREAD_SAFETY.contains("Callback-context synchronization"));
        assert!(THREAD_SAFETY.contains("A client remains opaque"));
        assert!(THREAD_SAFETY.contains("may synchronously call any C ABI function"));
        assert!(THREAD_SAFETY.contains("including `yeokcham_client_release`"));
        assert!(HEADER.contains("callback may reenter the ABI and release the submitted client"));
    }
}
