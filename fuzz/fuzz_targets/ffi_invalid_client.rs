#![no_main]

use std::ffi::c_void;

use libfuzzer_sys::fuzz_target;
use yeokcham_ffi::{
    YeokchamEvent, YeokchamStatus, yeokcham_client_complete_async,
    yeokcham_client_config_builder_build,
    yeokcham_client_copy_last_error_detail, yeokcham_client_release, yeokcham_client_start,
    yeokcham_client_stop, yeokcham_client_subscribe_events, yeokcham_client_take_last_error_detail,
    yeokcham_event_subscription_poll,
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
        YeokchamStatus::InvalidInput
    );
    let mut buffer = std::ptr::null_mut();
    assert_eq!(
        unsafe { yeokcham_client_take_last_error_detail(client, &raw mut buffer) },
        YeokchamStatus::InvalidInput
    );
    assert!(yeokcham_client_subscribe_events(client).is_null());
    let mut event = YeokchamEvent::default();
    let mut has_event = 1;
    assert_eq!(
        unsafe {
            yeokcham_event_subscription_poll(
                std::ptr::without_provenance_mut(address),
                &raw mut event,
                &raw mut has_event,
            )
        },
        YeokchamStatus::InvalidInput
    );
    assert_eq!(event, YeokchamEvent::default());
    assert_eq!(has_event, 0);
});
