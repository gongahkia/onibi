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

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[repr(C)]
pub struct YeokchamEvent {
    pub version: u32,
    pub sequence: u64,
    pub kind: u32,
    pub message_identifier: [u8; yeokcham_protocol::MESSAGE_IDENTIFIER_BYTES],
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
    MAX_C_ABI_BUFFERS, MAX_C_ABI_CALLBACK_WORKERS, MAX_C_ABI_CLIENT_CONFIG_BUILDERS,
    MAX_C_ABI_CLIENTS, MAX_C_ABI_ERROR_DETAIL_BYTES, MAX_C_ABI_EVENT_SUBSCRIPTIONS,
    MAX_C_ABI_PENDING_COMPLETIONS, MAX_C_ABI_SECRET_BUFFER_BYTES, MAX_C_ABI_STATE_DIRECTORY_BYTES,
    YeokchamCompletionCallback, yeokcham_abi_negotiate, yeokcham_buffer_data,
    yeokcham_buffer_length, yeokcham_buffer_release, yeokcham_client_complete_async,
    yeokcham_client_config_builder_build, yeokcham_client_config_builder_create,
    yeokcham_client_config_builder_release,
    yeokcham_client_config_builder_set_event_buffer_capacity,
    yeokcham_client_config_builder_set_state_directory, yeokcham_client_copy_last_error_detail,
    yeokcham_client_create, yeokcham_client_release, yeokcham_client_start, yeokcham_client_stop,
    yeokcham_client_subscribe_events, yeokcham_client_take_last_error_detail,
    yeokcham_event_subscription_poll, yeokcham_event_subscription_release,
    yeokcham_secret_buffer_zeroize,
};

#[cfg(test)]
mod tests {
    use super::c_abi::CLIENT_TEST_LOCK;
    use super::{
        MAX_C_ABI_CLIENTS, MAX_C_ABI_PENDING_COMPLETIONS, YEOKCHAM_ABI_NEGOTIATION_REJECTED,
        YEOKCHAM_ABI_VERSION, YEOKCHAM_ABI_VERSION_MAJOR, YEOKCHAM_ABI_VERSION_MINOR,
        YeokchamStatus, yeokcham_abi_negotiate, yeokcham_client_complete_async,
        yeokcham_client_create, yeokcham_client_release,
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
