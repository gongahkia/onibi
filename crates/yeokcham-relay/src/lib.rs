#![forbid(unsafe_code)]

use yeokcham_core::{Error, Result};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MailboxQuota {
    bytes: u64,
}

impl MailboxQuota {
    pub fn new(bytes: u64) -> Result<Self> {
        if bytes == 0 {
            return Err(Error::InvalidInput("mailbox quota must be nonzero"));
        }
        Ok(Self { bytes })
    }

    #[must_use]
    pub const fn bytes(self) -> u64 {
        self.bytes
    }
}
