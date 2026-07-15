#![forbid(unsafe_code)]

use yeokcham_core::{Error, Result};
use yeokcham_protocol::{MAILBOX_IDENTIFIER_BYTES, MailboxCapability};

pub struct MailboxIngress {
    capability: MailboxCapability,
}

impl MailboxIngress {
    #[must_use]
    pub const fn new(capability: MailboxCapability) -> Self {
        Self { capability }
    }

    #[must_use]
    pub const fn mailbox_id(&self) -> &[u8; MAILBOX_IDENTIFIER_BYTES] {
        self.capability.mailbox_id()
    }

    pub fn validate(&self, presented: &[u8]) -> Result<(), MailboxIngressError> {
        let presented = MailboxCapability::decode(presented)
            .map_err(|_| MailboxIngressError::InvalidCapability)?;
        if self.capability.authorizes(&presented) {
            Ok(())
        } else {
            Err(MailboxIngressError::InvalidCapability)
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MailboxIngressError {
    #[error("invalid mailbox capability")]
    InvalidCapability,
}

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

#[cfg(test)]
mod tests {
    use super::{MailboxIngress, MailboxIngressError};
    use yeokcham_protocol::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
    };

    fn capability() -> MailboxCapability {
        MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x22; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
    }

    #[test]
    fn admits_only_the_registered_canonical_capability() {
        let capability = capability();
        let encoded = capability.encode().unwrap();
        let ingress = MailboxIngress::new(capability);

        assert_eq!(ingress.mailbox_id(), &[0x11; MAILBOX_IDENTIFIER_BYTES]);
        assert_eq!(ingress.validate(&encoded), Ok(()));
    }

    #[test]
    fn rejects_malformed_or_unregistered_capabilities() {
        let ingress = MailboxIngress::new(capability());
        let other = MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
        .encode()
        .unwrap();

        assert_eq!(
            ingress.validate(&[0x80]),
            Err(MailboxIngressError::InvalidCapability)
        );
        assert_eq!(
            ingress.validate(&other),
            Err(MailboxIngressError::InvalidCapability)
        );
    }
}
