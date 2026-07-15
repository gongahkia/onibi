use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};

pub const MESSAGE_IDENTIFIER_BYTES: usize = 16;

#[derive(Clone, Copy, Eq, PartialEq, Ord, PartialOrd)]
pub struct MessageIdentifier([u8; MESSAGE_IDENTIFIER_BYTES]);

impl MessageIdentifier {
    pub fn generate() -> Result<Self, MessageIdentifierError> {
        let mut identifier = [0; MESSAGE_IDENTIFIER_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut identifier)
            .map_err(|_| MessageIdentifierError::Randomness)?;
        Self::from_bytes(identifier)
    }

    pub fn from_bytes(
        identifier: [u8; MESSAGE_IDENTIFIER_BYTES],
    ) -> Result<Self, MessageIdentifierError> {
        if identifier.iter().all(|byte| *byte == 0) {
            return Err(MessageIdentifierError::Zero);
        }
        Ok(Self(identifier))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; MESSAGE_IDENTIFIER_BYTES] {
        &self.0
    }
}

impl fmt::Debug for MessageIdentifier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("MessageIdentifier(REDACTED)")
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MessageIdentifierError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("message identifier must not be all zeroes")]
    Zero,
}

#[cfg(test)]
mod tests {
    use super::{MESSAGE_IDENTIFIER_BYTES, MessageIdentifier, MessageIdentifierError};

    #[test]
    fn generates_distinct_redacted_nonzero_message_identifiers() {
        let first = MessageIdentifier::generate().unwrap();
        let second = MessageIdentifier::generate().unwrap();

        assert_ne!(first, second);
        assert_eq!(first.as_bytes().len(), MESSAGE_IDENTIFIER_BYTES);
        assert!(format!("{first:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_zero_message_identifier() {
        assert_eq!(
            MessageIdentifier::from_bytes([0; MESSAGE_IDENTIFIER_BYTES]),
            Err(MessageIdentifierError::Zero)
        );
    }
}
