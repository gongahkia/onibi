#![no_main]

use libfuzzer_sys::fuzz_target;
use yeokcham_protocol::DeliveryProfile;

fuzz_target!(|data: &[u8]| {
    let _ = DeliveryProfile::decode(data);
});
