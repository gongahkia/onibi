#![no_main]

use libfuzzer_sys::fuzz_target;
use arachne_protocol::{EncryptedHeader, EncryptedMessageEnvelope};

fuzz_target!(|data: &[u8]| {
    let _ = EncryptedHeader::decode(data);
    let _ = EncryptedMessageEnvelope::decode(data);
});
