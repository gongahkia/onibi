#![no_main]

use libfuzzer_sys::fuzz_target;
use arachne_protocol::DeliveryProfile;

fuzz_target!(|data: &[u8]| {
    let _ = DeliveryProfile::decode(data);
});
