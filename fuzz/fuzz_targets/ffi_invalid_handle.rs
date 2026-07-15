#![no_main]

use std::ffi::c_void;

use libfuzzer_sys::fuzz_target;
use yeokcham_ffi::{YeokchamStatus, yeokcham_handle_complete_async, yeokcham_handle_release};

extern "C" fn completion(_: i32, _: *mut c_void) {}

fn handle_address(data: &[u8]) -> usize {
    let mut bytes = [0; std::mem::size_of::<usize>()];
    for (slot, byte) in bytes.iter_mut().zip(data) {
        *slot = *byte;
    }
    usize::from_le_bytes(bytes)
}

fuzz_target!(|data: &[u8]| {
    let address = handle_address(data);
    assert_eq!(
        yeokcham_handle_release(std::ptr::without_provenance_mut(address)),
        YeokchamStatus::InvalidInput
    );
    assert_eq!(
        yeokcham_handle_complete_async(
            std::ptr::without_provenance(address),
            Some(completion),
            std::ptr::null_mut(),
        ),
        YeokchamStatus::InvalidInput
    );
});
