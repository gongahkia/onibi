use std::{
    collections::BTreeSet,
    ffi::c_void,
    sync::{Mutex, OnceLock, atomic::AtomicUsize, atomic::Ordering},
};

use crate::YEOKCHAM_ABI_VERSION;
use crate::{YeokchamHandle, YeokchamStatus};
use zeroize::Zeroize;

pub const MAX_C_ABI_HANDLES: usize = 1024;
pub const MAX_C_ABI_PENDING_COMPLETIONS: usize = 1024;
pub const MAX_C_ABI_SECRET_BUFFER_BYTES: usize = 16 * 1024 * 1024;

static ACTIVE_HANDLES: OnceLock<Mutex<BTreeSet<usize>>> = OnceLock::new();
static NEXT_HANDLE_IDENTIFIER: AtomicUsize = AtomicUsize::new(1);
static PENDING_COMPLETIONS: AtomicUsize = AtomicUsize::new(0);

pub type YeokchamCompletionCallback = extern "C" fn(i32, *mut c_void);

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_abi_negotiate(requested_version: u32) -> u32 {
    if requested_version == YEOKCHAM_ABI_VERSION {
        YEOKCHAM_ABI_VERSION
    } else {
        0
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
pub extern "C" fn yeokcham_handle_create() -> *mut YeokchamHandle {
    let Ok(mut handles) = active_handles().lock() else {
        return std::ptr::null_mut();
    };
    if handles.len() >= MAX_C_ABI_HANDLES {
        return std::ptr::null_mut();
    }
    let Ok(identifier) =
        NEXT_HANDLE_IDENTIFIER.fetch_update(Ordering::Relaxed, Ordering::Relaxed, |identifier| {
            identifier.checked_add(1)
        })
    else {
        return std::ptr::null_mut();
    };
    if !handles.insert(identifier) {
        return std::ptr::null_mut();
    }
    std::ptr::without_provenance_mut(identifier)
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_handle_release(handle: *mut YeokchamHandle) -> YeokchamStatus {
    let identifier = handle.addr();
    if identifier == 0 {
        return YeokchamStatus::InvalidInput;
    }
    let Ok(mut handles) = active_handles().lock() else {
        return YeokchamStatus::State;
    };
    if !handles.remove(&identifier) {
        return YeokchamStatus::InvalidInput;
    }
    YeokchamStatus::Ok
}

#[unsafe(no_mangle)]
pub extern "C" fn yeokcham_handle_complete_async(
    handle: *const YeokchamHandle,
    callback: Option<YeokchamCompletionCallback>,
    context: *mut c_void,
) -> YeokchamStatus {
    let Some(callback) = callback else {
        return YeokchamStatus::InvalidInput;
    };
    match is_active_handle(handle) {
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

fn active_handles() -> &'static Mutex<BTreeSet<usize>> {
    ACTIVE_HANDLES.get_or_init(|| Mutex::new(BTreeSet::new()))
}

fn is_active_handle(handle: *const YeokchamHandle) -> Result<bool, YeokchamStatus> {
    let identifier = handle.addr();
    if identifier == 0 {
        return Ok(false);
    }
    let handles = active_handles().lock().map_err(|_| YeokchamStatus::State)?;
    Ok(handles.contains(&identifier))
}

#[cfg(test)]
mod tests {
    use super::{MAX_C_ABI_SECRET_BUFFER_BYTES, YeokchamStatus, yeokcham_secret_buffer_zeroize};

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

    unsafe extern "C" {
        fn yeokcham_c_consumer_conformance() -> i32;
    }

    #[test]
    fn c_consumer_conformance_passes() {
        assert_eq!(unsafe { yeokcham_c_consumer_conformance() }, 0);
    }
}
