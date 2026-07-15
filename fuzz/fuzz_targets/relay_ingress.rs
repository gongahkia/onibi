#![no_main]

use libfuzzer_sys::fuzz_target;
use yeokcham_protocol::{
    MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
};
use yeokcham_relay::MailboxIngress;

fn capability() -> MailboxCapability {
    MailboxCapability::new(
        [0x11; MAILBOX_IDENTIFIER_BYTES],
        [0x22; MAILBOX_CAPABILITY_TOKEN_BYTES],
    )
    .expect("fuzz mailbox capability must be valid")
}

fuzz_target!(|data: &[u8]| {
    let ingress = MailboxIngress::new(capability());
    let _ = ingress.validate(data);
});
