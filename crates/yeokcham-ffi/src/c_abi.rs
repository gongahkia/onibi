use std::{
    collections::BTreeSet,
    sync::{Mutex, OnceLock, atomic::AtomicUsize, atomic::Ordering},
};

use crate::{YeokchamHandle, YeokchamStatus};

pub const MAX_C_ABI_HANDLES: usize = 1024;

static ACTIVE_HANDLES: OnceLock<Mutex<BTreeSet<usize>>> = OnceLock::new();
static NEXT_HANDLE_IDENTIFIER: AtomicUsize = AtomicUsize::new(1);

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

fn active_handles() -> &'static Mutex<BTreeSet<usize>> {
    ACTIVE_HANDLES.get_or_init(|| Mutex::new(BTreeSet::new()))
}
