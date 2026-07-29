#![no_main]

use std::ffi::c_void;

use libfuzzer_sys::fuzz_target;
use arachne_ffi::{
    ArachneEvent, ArachneStatus, arachne_client_complete_async,
    arachne_client_config_builder_build, arachne_client_copy_last_error_detail,
    arachne_client_release, arachne_client_start, arachne_client_stop,
    arachne_client_subscribe_events, arachne_client_take_last_error_detail,
    arachne_event_subscription_poll,
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
        arachne_client_release(client),
        ArachneStatus::InvalidInput
    );
    assert_eq!(arachne_client_start(client), ArachneStatus::InvalidInput);
    assert_eq!(arachne_client_stop(client), ArachneStatus::InvalidInput);
    assert_eq!(
        arachne_client_complete_async(client, Some(completion), std::ptr::null_mut(),),
        ArachneStatus::InvalidInput
    );
    assert_eq!(
        arachne_client_config_builder_build(std::ptr::without_provenance(address), client),
        ArachneStatus::InvalidInput
    );
    let mut detail_length = 0;
    assert_eq!(
        unsafe {
            arachne_client_copy_last_error_detail(
                client,
                std::ptr::null_mut(),
                0,
                &raw mut detail_length,
            )
        },
        ArachneStatus::InvalidInput
    );
    let mut buffer = std::ptr::null_mut();
    assert_eq!(
        unsafe { arachne_client_take_last_error_detail(client, &raw mut buffer) },
        ArachneStatus::InvalidInput
    );
    assert!(arachne_client_subscribe_events(client).is_null());
    let mut event = ArachneEvent::default();
    let mut has_event = 1;
    assert_eq!(
        unsafe {
            arachne_event_subscription_poll(
                std::ptr::without_provenance_mut(address),
                &raw mut event,
                &raw mut has_event,
            )
        },
        ArachneStatus::InvalidInput
    );
    assert_eq!(event, ArachneEvent::default());
    assert_eq!(has_event, 0);
});
