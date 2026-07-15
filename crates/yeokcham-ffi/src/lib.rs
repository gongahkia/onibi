#![deny(unsafe_op_in_unsafe_fn)]

#[allow(unsafe_code)]
mod c_abi;

pub const YEOKCHAM_ABI_VERSION_MAJOR: u32 = 1;
pub const YEOKCHAM_ABI_VERSION_MINOR: u32 = 0;
pub const YEOKCHAM_ABI_VERSION: u32 = 1;

#[repr(C)]
pub struct YeokchamHandle {
    _private: u8,
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
    MAX_C_ABI_HANDLES, MAX_C_ABI_PENDING_COMPLETIONS, MAX_C_ABI_SECRET_BUFFER_BYTES,
    YeokchamCompletionCallback, yeokcham_abi_negotiate, yeokcham_handle_complete_async,
    yeokcham_handle_create, yeokcham_handle_release, yeokcham_secret_buffer_zeroize,
};

#[cfg(test)]
mod tests {
    use super::{
        MAX_C_ABI_HANDLES, MAX_C_ABI_PENDING_COMPLETIONS, YEOKCHAM_ABI_VERSION,
        YEOKCHAM_ABI_VERSION_MAJOR, YEOKCHAM_ABI_VERSION_MINOR, YeokchamStatus,
        yeokcham_abi_negotiate, yeokcham_handle_complete_async, yeokcham_handle_create,
        yeokcham_handle_release,
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
        assert!(HEADER.contains("typedef struct yeokcham_handle yeokcham_handle_t;"));
        assert!(HEADER.ends_with("#endif\n"));
    }

    #[test]
    fn published_handle_remains_opaque() {
        assert!(HEADER.contains("typedef struct yeokcham_handle yeokcham_handle_t;"));
        assert!(!HEADER.contains("struct yeokcham_handle {"));
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
        assert!(HEADER.contains("yeokcham_handle_t *yeokcham_handle_create(void);"));
        assert!(HEADER.contains("yeokcham_handle_release(yeokcham_handle_t *handle);"));
        assert!(HEADER.contains("typedef void (*yeokcham_completion_callback_t)("));
        assert!(HEADER.contains("yeokcham_handle_complete_async("));
        assert!(HEADER.contains("uint32_t yeokcham_abi_negotiate(uint32_t requested_version);"));
        assert!(HEADER.contains("#include <stddef.h>"));
        assert!(HEADER.contains("yeokcham_secret_buffer_zeroize(uint8_t *buffer, size_t length);"));
    }

    #[test]
    fn created_handles_release_once_and_reject_invalid_inputs() {
        let handle = yeokcham_handle_create();
        assert!(!handle.is_null());
        assert_eq!(
            yeokcham_handle_release(std::ptr::null_mut()),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_handle_release(handle), YeokchamStatus::Ok);
        assert_eq!(
            yeokcham_handle_release(handle),
            YeokchamStatus::InvalidInput
        );
        assert!(MAX_C_ABI_HANDLES > 0);
    }

    static CALLBACK_TEST_LOCK: Mutex<()> = Mutex::new(());
    static COMPLETION_SENDER: OnceLock<Mutex<Option<mpsc::Sender<(i32, usize)>>>> = OnceLock::new();

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

    #[test]
    fn active_handles_complete_asynchronously_once() {
        let _guard = CALLBACK_TEST_LOCK.lock().unwrap();
        let (sender, receiver) = mpsc::channel();
        *COMPLETION_SENDER
            .get_or_init(|| Mutex::new(None))
            .lock()
            .unwrap() = Some(sender);
        let handle = yeokcham_handle_create();
        assert_eq!(
            yeokcham_handle_complete_async(
                handle,
                Some(record_completion),
                std::ptr::without_provenance_mut(42),
            ),
            YeokchamStatus::Ok
        );
        assert_eq!(
            receiver.recv_timeout(Duration::from_secs(1)),
            Ok((YeokchamStatus::Ok as i32, 42))
        );
        assert_eq!(yeokcham_handle_release(handle), YeokchamStatus::Ok);
        *COMPLETION_SENDER.get().unwrap().lock().unwrap() = None;
        assert!(MAX_C_ABI_PENDING_COMPLETIONS > 0);
    }

    #[test]
    fn asynchronous_completion_rejects_invalid_handles_and_callbacks() {
        let handle = yeokcham_handle_create();
        assert_eq!(
            yeokcham_handle_complete_async(handle, None, std::ptr::null_mut()),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(
            yeokcham_handle_complete_async(
                std::ptr::null(),
                Some(record_completion),
                std::ptr::null_mut()
            ),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_handle_release(handle), YeokchamStatus::Ok);
    }

    #[test]
    fn version_negotiation_accepts_only_the_published_abi_version() {
        assert_eq!(
            yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION),
            YEOKCHAM_ABI_VERSION
        );
        assert_eq!(yeokcham_abi_negotiate(0), 0);
        assert_eq!(yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION + 1), 0);
        assert_eq!(yeokcham_abi_negotiate(u32::MAX), 0);
    }

    extern "C" fn noop_completion(_: i32, _: *mut c_void) {}

    #[test]
    fn concurrent_handle_lifecycle_accepts_each_handle_once() {
        const WORKERS: usize = 32;
        let start = Arc::new(Barrier::new(WORKERS));
        let workers: Vec<_> = (0..WORKERS)
            .map(|_| {
                let start = Arc::clone(&start);
                thread::spawn(move || {
                    start.wait();
                    let handle = yeokcham_handle_create();
                    !handle.is_null()
                        && yeokcham_handle_release(handle) == YeokchamStatus::Ok
                        && yeokcham_handle_release(handle) == YeokchamStatus::InvalidInput
                })
            })
            .collect();
        assert!(workers.into_iter().all(|worker| worker.join().unwrap()));
    }

    #[test]
    fn concurrent_submission_and_release_return_only_defined_statuses() {
        let handle = yeokcham_handle_create();
        let handle_address = handle.addr();
        let start = Arc::new(Barrier::new(2));
        let release_start = Arc::clone(&start);
        let release = thread::spawn(move || {
            release_start.wait();
            yeokcham_handle_release(std::ptr::without_provenance_mut(handle_address))
        });
        start.wait();
        let completion = yeokcham_handle_complete_async(
            std::ptr::without_provenance(handle_address),
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
    fn thread_safety_contract_covers_callbacks_and_handle_lifecycle() {
        const THREAD_SAFETY: &str = include_str!("../README.md");
        assert!(THREAD_SAFETY.contains("## Thread safety"));
        assert!(THREAD_SAFETY.contains("may be called concurrently"));
        assert!(THREAD_SAFETY.contains("library-created background thread"));
        assert!(THREAD_SAFETY.contains("Callback-context synchronization"));
    }
}
