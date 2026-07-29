#![no_main]

use libfuzzer_sys::fuzz_target;
use arachne_protocol::{EncryptedAttachmentChunk, EncryptedAttachmentManifest};

fuzz_target!(|data: &[u8]| {
    let _ = EncryptedAttachmentChunk::decode(data);
    let _ = EncryptedAttachmentManifest::decode(data);
});
