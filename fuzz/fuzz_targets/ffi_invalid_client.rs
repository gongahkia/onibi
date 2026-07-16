#![no_main]

use std::ffi::c_void;

use libfuzzer_sys::fuzz_target;
use yeokcham_ffi::{
    YeokchamStatus, yeokcham_client_complete_async, yeokcham_client_config_builder_build,
    yeokcham_client_release, yeokcham_client_start, yeokcham_client_stop,
};

extern "C" fn completion(_: i32, _: *mut c_void) {}

fn pointer_address(data: &[u8]) -> usize {
    let mut bytes = [0; std::mem::size_of::<usize>()];
    for (slot, byte) in bytes.iter_mut().zip(data) {
        *slot = *byte;
    }
    usize::from_le_bytes(bytes)
}

fuzz_target!(|data: &[u8]| {
    let address = pointer_address(data);
    let client = std::ptr::without_provenance_mut(address);
    assert_eq!(
        yeokcham_client_release(client),
        YeokchamStatus::InvalidInput
    );
    assert_eq!(
        yeokcham_client_start(client),
        YeokchamStatus::InvalidInput
    );
    assert_eq!(
        yeokcham_client_stop(client),
        YeokchamStatus::InvalidInput
    );
    assert_eq!(
        yeokcham_client_complete_async(
            client,
            Some(completion),
            std::ptr::null_mut(),
        ),
        YeokchamStatus::InvalidInput
    );
    assert_eq!(
        yeokcham_client_config_builder_build(std::ptr::without_provenance(address), client),
        YeokchamStatus::InvalidInput
    );
});
