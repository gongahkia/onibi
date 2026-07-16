use std::{
    collections::BTreeMap,
    ffi::c_void,
    path::PathBuf,
    sync::{Mutex, OnceLock, atomic::AtomicUsize, atomic::Ordering},
};

use crate::{YEOKCHAM_ABI_NEGOTIATION_REJECTED, YEOKCHAM_ABI_VERSION};
use crate::{YeokchamClient, YeokchamClientConfigBuilder, YeokchamStatus};
use yeokcham_sdk::{RuntimeMode, SdkConfig};
use zeroize::Zeroize;

pub const MAX_C_ABI_CLIENT_CONFIG_BUILDERS: usize = 1024;
pub const MAX_C_ABI_CLIENTS: usize = 1024;
pub const MAX_C_ABI_PENDING_COMPLETIONS: usize = 1024;
pub const MAX_C_ABI_SECRET_BUFFER_BYTES: usize = 16 * 1024 * 1024;
pub const MAX_C_ABI_STATE_DIRECTORY_BYTES: usize = 4096;

struct ClientHandle {
    configuration: Option<SdkConfig>,
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

#[cfg(test)]
mod tests {
    use super::{
        CLIENT_TEST_LOCK, MAX_C_ABI_SECRET_BUFFER_BYTES, MAX_C_ABI_STATE_DIRECTORY_BYTES,
        YeokchamStatus, yeokcham_client_config_builder_build,
        yeokcham_client_config_builder_create, yeokcham_client_config_builder_release,
        yeokcham_client_config_builder_set_event_buffer_capacity,
        yeokcham_client_config_builder_set_state_directory, yeokcham_client_create,
        yeokcham_client_release, yeokcham_secret_buffer_zeroize,
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
        let state_directory = b"/tmp/yeokcham-ffi-client";
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
        assert_eq!(yeokcham_client_release(client), YeokchamStatus::Ok);
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
    }

    #[test]
    fn c_consumer_conformance_passes() {
        let _guard = CLIENT_TEST_LOCK.lock().unwrap();
        assert_eq!(unsafe { yeokcham_c_consumer_conformance() }, 0);
    }
}
