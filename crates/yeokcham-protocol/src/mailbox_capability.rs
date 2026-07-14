use std::fmt;

use minicbor::{Decoder, Encoder};
use zeroize::Zeroize;

pub const MAILBOX_CAPABILITY_SCHEMA_VERSION: u8 = 1;
pub const MAILBOX_IDENTIFIER_BYTES: usize = 16;
pub const MAILBOX_CAPABILITY_TOKEN_BYTES: usize = 32;
const MAILBOX_CAPABILITY_FIELDS: u64 = 3;

pub struct MailboxCapability {
    mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
    token: [u8; MAILBOX_CAPABILITY_TOKEN_BYTES],
}

impl MailboxCapability {
    pub fn new(
        mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
        token: [u8; MAILBOX_CAPABILITY_TOKEN_BYTES],
    ) -> Result<Self, MailboxCapabilityError> {
        validate_parts(&mailbox_id, &token)?;
        Ok(Self { mailbox_id, token })
    }

    #[must_use]
    pub const fn mailbox_id(&self) -> &[u8; MAILBOX_IDENTIFIER_BYTES] {
        &self.mailbox_id
    }

    pub fn encode(&self) -> Result<Vec<u8>, MailboxCapabilityError> {
        validate_parts(&self.mailbox_id, &self.token)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(MAILBOX_CAPABILITY_FIELDS)
            .map_err(|_| MailboxCapabilityError::Encode)?
            .u8(MAILBOX_CAPABILITY_SCHEMA_VERSION)
            .map_err(|_| MailboxCapabilityError::Encode)?
            .bytes(&self.mailbox_id)
            .map_err(|_| MailboxCapabilityError::Encode)?
            .bytes(&self.token)
            .map_err(|_| MailboxCapabilityError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, MailboxCapabilityError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| MailboxCapabilityError::Decode)?
            != Some(MAILBOX_CAPABILITY_FIELDS)
        {
            return Err(MailboxCapabilityError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| MailboxCapabilityError::Decode)?;
        if version != MAILBOX_CAPABILITY_SCHEMA_VERSION {
            return Err(MailboxCapabilityError::UnsupportedSchemaVersion(version));
        }
        let mailbox_id = decoder
            .bytes()
            .map_err(|_| MailboxCapabilityError::Decode)?
            .try_into()
            .map_err(|_| MailboxCapabilityError::InvalidMailboxIdentifierLength)?;
        let token = decoder
            .bytes()
            .map_err(|_| MailboxCapabilityError::Decode)?
            .try_into()
            .map_err(|_| MailboxCapabilityError::InvalidTokenLength)?;
        if decoder.position() != encoded.len() {
            return Err(MailboxCapabilityError::TrailingBytes);
        }
        Self::new(mailbox_id, token)
    }
}

impl Drop for MailboxCapability {
    fn drop(&mut self) {
        self.token.zeroize();
    }
}

impl fmt::Debug for MailboxCapability {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "MailboxCapability {{ mailbox_id: {:?}, token: REDACTED }}",
            self.mailbox_id
        )
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MailboxCapabilityError {
    #[error("unsupported mailbox-capability schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("mailbox identifier must not be all zeroes")]
    InvalidMailboxIdentifier,
    #[error("mailbox identifier has an invalid length")]
    InvalidMailboxIdentifierLength,
    #[error("mailbox capability token must not be all zeroes")]
    InvalidToken,
    #[error("mailbox capability token has an invalid length")]
    InvalidTokenLength,
    #[error("mailbox capability must be a three-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after mailbox capability")]
    TrailingBytes,
}

fn validate_parts(
    mailbox_id: &[u8; MAILBOX_IDENTIFIER_BYTES],
    token: &[u8; MAILBOX_CAPABILITY_TOKEN_BYTES],
) -> Result<(), MailboxCapabilityError> {
    if mailbox_id.iter().all(|byte| *byte == 0) {
        return Err(MailboxCapabilityError::InvalidMailboxIdentifier);
    }
    if token.iter().all(|byte| *byte == 0) {
        return Err(MailboxCapabilityError::InvalidToken);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
        MailboxCapabilityError,
    };

    fn capability() -> MailboxCapability {
        MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x22; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
    }

    #[test]
    fn canonical_capability_round_trip_redacts_debug_output() {
        let capability = capability();
        let mut expected = vec![0x83, 0x01, 0x50];
        expected.extend([0x11; MAILBOX_IDENTIFIER_BYTES]);
        expected.extend([0x58, 0x20]);
        expected.extend([0x22; MAILBOX_CAPABILITY_TOKEN_BYTES]);

        assert_eq!(capability.encode().unwrap(), expected);
        let decoded = MailboxCapability::decode(&expected).unwrap();
        assert_eq!(decoded.mailbox_id(), &[0x11; MAILBOX_IDENTIFIER_BYTES]);
        assert_eq!(decoded.encode().unwrap(), expected);
        let output = format!("{decoded:?}");
        assert!(output.contains("token: REDACTED"));
        assert!(!output.contains("34"));
    }

    #[test]
    fn rejects_invalid_components_and_lengths() {
        assert_eq!(
            MailboxCapability::new(
                [0; MAILBOX_IDENTIFIER_BYTES],
                [1; MAILBOX_CAPABILITY_TOKEN_BYTES],
            )
            .unwrap_err(),
            MailboxCapabilityError::InvalidMailboxIdentifier
        );
        assert_eq!(
            MailboxCapability::new(
                [1; MAILBOX_IDENTIFIER_BYTES],
                [0; MAILBOX_CAPABILITY_TOKEN_BYTES],
            )
            .unwrap_err(),
            MailboxCapabilityError::InvalidToken
        );
        assert_eq!(
            MailboxCapability::decode(&[
                0x83, 0x01, 0x4f, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
            ])
            .unwrap_err(),
            MailboxCapabilityError::InvalidMailboxIdentifierLength
        );
    }

    #[test]
    fn rejects_unknown_versions_shapes_and_trailing_data() {
        assert_eq!(
            MailboxCapability::decode(&[0x83, 0x02, 0x50]).unwrap_err(),
            MailboxCapabilityError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            MailboxCapability::decode(&[0x9f, 0x01, 0xff]).unwrap_err(),
            MailboxCapabilityError::InvalidShape
        );
        let mut encoded = capability().encode().unwrap();
        encoded.push(0);
        assert_eq!(
            MailboxCapability::decode(&encoded).unwrap_err(),
            MailboxCapabilityError::TrailingBytes
        );
    }
}
