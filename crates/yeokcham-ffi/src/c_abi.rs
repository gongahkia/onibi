use std::{
    collections::BTreeMap,
    ffi::c_void,
    path::PathBuf,
    sync::{Mutex, OnceLock, atomic::AtomicUsize, atomic::Ordering},
};

use crate::{YEOKCHAM_ABI_NEGOTIATION_REJECTED, YEOKCHAM_ABI_VERSION};
use crate::{YeokchamClient, YeokchamClientConfigBuilder, YeokchamStatus};
use yeokcham_sdk::{RuntimeMode, SdkClient, SdkClientError, SdkConfig};
use zeroize::Zeroize;

pub const MAX_C_ABI_CLIENT_CONFIG_BUILDERS: usize = 1024;
pub const MAX_C_ABI_CLIENTS: usize = 1024;
pub const MAX_C_ABI_ERROR_DETAIL_BYTES: usize = 64;
pub const MAX_C_ABI_PENDING_COMPLETIONS: usize = 1024;
pub const MAX_C_ABI_SECRET_BUFFER_BYTES: usize = 16 * 1024 * 1024;
pub const MAX_C_ABI_STATE_DIRECTORY_BYTES: usize = 4096;

struct ClientHandle {
    configuration: Option<SdkConfig>,
    last_error_detail: Option<&'static [u8]>,
    runtime: Option<SdkClient>,
}

#[derive(Default)]
struct ClientConfigBuilder {
    state_directory: Option<PathBuf>,
    event_buffer_capacity: Option<usize>,
}

static ACTIVE_CLIENTS: OnceLock<Mutex<BTreeMap<usize, ClientHandle>>> = OnceLock::new();
static ACTIVE_CLIENT_CONFIG_BUILDERS: OnceLock<Mutex<BTreeMap<usize, ClientConfigBuilder>>> =
    OnceLock::new();
static NEXT_CLIENT_IDENTIFIER: AtomicUsize = AtomicUsize::new(1);
static NEXT_CLIENT_CONFIG_BUILDER_IDENTIFIER: AtomicUsize = AtomicUsize::new(1);
static PENDING_COMPLETIONS: AtomicUsize = AtomicUsize::new(0);

#[cfg(test)]
pub static CLIENT_TEST_LOCK: Mutex<()> = Mutex::new(());

pub type YeokchamCompletionCallback = extern "C" fn(i32, *mut c_void);

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
    let Ok(identifier) =
        NEXT_CLIENT_IDENTIFIER.fetch_update(Ordering::Relaxed, Ordering::Relaxed, |identifier| {
            identifier.checked_add(1)
        })
    else {
        return std::ptr::null_mut();
    };
    if clients
        .insert(
            identifier,
            ClientHandle {
                configuration: None,
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
    let Ok(_) = PENDING_COMPLETIONS.fetch_update(Ordering::Relaxed, Ordering::Relaxed, |pending| {
        (pending < MAX_C_ABI_PENDING_COMPLETIONS).then_some(pending + 1)
    }) else {
        return YeokchamStatus::ResourceLimit;
    };
    let context = context.expose_provenance();
    if std::thread::Builder::new()
        .spawn(move || {
            callback(
                YeokchamStatus::Ok as i32,
                std::ptr::with_exposed_provenance_mut(context),
            );
            PENDING_COMPLETIONS.fetch_sub(1, Ordering::Relaxed);
        })
        .is_err()
    {
        PENDING_COMPLETIONS.fetch_sub(1, Ordering::Relaxed);
        return YeokchamStatus::ResourceLimit;
    }
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_client_config_builder_create() -> *mut YeokchamClientConfigBuilder {
    let Ok(mut builders) = active_client_config_builders().lock() else {
        return std::ptr::null_mut();
    };
    if builders.len() >= MAX_C_ABI_CLIENT_CONFIG_BUILDERS {
        return std::ptr::null_mut();
    }
    let Ok(identifier) = NEXT_CLIENT_CONFIG_BUILDER_IDENTIFIER.fetch_update(
        Ordering::Relaxed,
        Ordering::Relaxed,
        |identifier| identifier.checked_add(1),
    ) else {
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

#[cfg(test)]
mod tests {
    use super::{
        CLIENT_TEST_LOCK, MAX_C_ABI_ERROR_DETAIL_BYTES, MAX_C_ABI_SECRET_BUFFER_BYTES,
        MAX_C_ABI_STATE_DIRECTORY_BYTES, SdkClientError, YeokchamStatus, map_sdk_client_error,
        sdk_client_error_detail, yeokcham_client_complete_async,
        yeokcham_client_config_builder_build, yeokcham_client_config_builder_create,
        yeokcham_client_config_builder_release,
        yeokcham_client_config_builder_set_event_buffer_capacity,
        yeokcham_client_config_builder_set_state_directory, yeokcham_client_copy_last_error_detail,
        yeokcham_client_create, yeokcham_client_release, yeokcham_client_start,
        yeokcham_client_stop, yeokcham_secret_buffer_zeroize,
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
        assert_eq!(yeokcham_client_start(client), YeokchamStatus::State);
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::Ok);
        assert_eq!(yeokcham_client_stop(client), YeokchamStatus::State);
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
        std::fs::remove_dir_all(state_directory).unwrap();
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
