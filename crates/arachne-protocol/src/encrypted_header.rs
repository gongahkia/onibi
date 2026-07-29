use minicbor::{Decoder, Encoder};

use crate::{MAX_ENCRYPTED_HEADER_BYTES, RecipientCapability, RecipientCapabilityError};

pub const ENCRYPTED_HEADER_SCHEMA_VERSION: u8 = 1;
const ENCRYPTED_HEADER_FIELDS: u64 = 2;

#[derive(Debug)]
pub struct EncryptedHeader {
    recipient_capability: RecipientCapability,
}

impl EncryptedHeader {
    #[must_use]
    pub const fn new(recipient_capability: RecipientCapability) -> Self {
        Self {
            recipient_capability,
        }
    }

    #[must_use]
    pub const fn recipient_capability(&self) -> &RecipientCapability {
        &self.recipient_capability
    }

    pub fn encode(&self) -> Result<Vec<u8>, EncryptedHeaderError> {
        let recipient_capability = self
            .recipient_capability
            .encode()
            .map_err(|_| EncryptedHeaderError::InvalidRecipientCapability)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ENCRYPTED_HEADER_FIELDS)
            .map_err(|_| EncryptedHeaderError::Encode)?
            .u8(ENCRYPTED_HEADER_SCHEMA_VERSION)
            .map_err(|_| EncryptedHeaderError::Encode)?
            .bytes(&recipient_capability)
            .map_err(|_| EncryptedHeaderError::Encode)?;
        let header_bytes = encoder.into_writer();
        if header_bytes.len() > MAX_ENCRYPTED_HEADER_BYTES {
            return Err(EncryptedHeaderError::HeaderTooLarge);
        }
        Ok(header_bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, EncryptedHeaderError> {
        if encoded.len() > MAX_ENCRYPTED_HEADER_BYTES {
            return Err(EncryptedHeaderError::HeaderTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| EncryptedHeaderError::Decode)?
            != Some(ENCRYPTED_HEADER_FIELDS)
        {
            return Err(EncryptedHeaderError::InvalidShape);
        }
        let schema_version = decoder.u8().map_err(|_| EncryptedHeaderError::Decode)?;
        if schema_version != ENCRYPTED_HEADER_SCHEMA_VERSION {
            return Err(EncryptedHeaderError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let recipient_capability = decoder.bytes().map_err(|_| EncryptedHeaderError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(EncryptedHeaderError::TrailingBytes);
        }
        let recipient_capability = RecipientCapability::decode(recipient_capability)
            .map_err(|_| EncryptedHeaderError::InvalidRecipientCapability)?;
        Ok(Self::new(recipient_capability))
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum EncryptedHeaderError {
    #[error("encrypted header exceeds the configured limit")]
    HeaderTooLarge,
    #[error("unsupported encrypted-header schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("encrypted header has an invalid recipient capability")]
    InvalidRecipientCapability,
    #[error("encrypted header must be a two-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after encrypted header")]
    TrailingBytes,
}

impl From<RecipientCapabilityError> for EncryptedHeaderError {
    fn from(_: RecipientCapabilityError) -> Self {
        Self::InvalidRecipientCapability
    }
}

#[cfg(test)]
mod tests {
    use super::{ENCRYPTED_HEADER_SCHEMA_VERSION, EncryptedHeader, EncryptedHeaderError};
    use crate::{
        DirectProfileConfig, MAX_ENCRYPTED_HEADER_BYTES, RECIPIENT_CAPABILITY_SCHEMA_VERSION,
        RecipientCapability,
    };

    fn header() -> EncryptedHeader {
        EncryptedHeader::new(RecipientCapability::Direct(
            DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap(),
        ))
    }

    #[test]
    fn canonical_header_round_trips() {
        let header = header();
        let recipient_capability = header.recipient_capability().encode().unwrap();
        let mut expected = vec![0x82, 0x01, 0x50];
        expected.extend(recipient_capability);
        assert_eq!(header.encode().unwrap(), expected);
        assert!(matches!(
            EncryptedHeader::decode(&expected)
                .unwrap()
                .recipient_capability(),
            RecipientCapability::Direct(_)
        ));
    }

    #[test]
    fn rejects_invalid_encodings_and_limits() {
        assert_eq!(
            EncryptedHeader::decode(&[0x82, 0x02, 0x40]).unwrap_err(),
            EncryptedHeaderError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            EncryptedHeader::decode(&[
                0x82,
                ENCRYPTED_HEADER_SCHEMA_VERSION,
                0x45,
                0x84,
                RECIPIENT_CAPABILITY_SCHEMA_VERSION,
                0x02,
                0x40,
                0x40,
            ])
            .unwrap_err(),
            EncryptedHeaderError::InvalidRecipientCapability
        );
        assert_eq!(
            EncryptedHeader::decode(&[0x9f, 0x01, 0x40, 0xff]).unwrap_err(),
            EncryptedHeaderError::InvalidShape
        );
        assert_eq!(
            EncryptedHeader::decode(&[0x82, 0x01, 0x40, 0]).unwrap_err(),
            EncryptedHeaderError::TrailingBytes
        );
        assert_eq!(
            EncryptedHeader::decode(&vec![0; MAX_ENCRYPTED_HEADER_BYTES + 1]).unwrap_err(),
            EncryptedHeaderError::HeaderTooLarge
        );
    }
}
