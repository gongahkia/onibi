use std::fmt;

use minicbor::{Decoder, Encoder};

use crate::{
    ATTACHMENT_KEY_BYTES, AttachmentIdentifier, AttachmentManifestError,
    EncryptedAttachmentManifest, MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES,
};

pub const COURIER_ATTACHMENT_REFERENCE_SCHEMA_VERSION: u8 = 1;
pub const MAX_COURIER_ATTACHMENT_REFERENCE_BYTES: usize =
    MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES + 128;
const REFERENCE_FIELDS: u64 = 4;

#[derive(Clone, Eq, PartialEq)]
pub struct CourierAttachmentReference {
    identifier: AttachmentIdentifier,
    message_key: [u8; ATTACHMENT_KEY_BYTES],
    manifest: EncryptedAttachmentManifest,
}

impl CourierAttachmentReference {
    pub fn new(
        identifier: AttachmentIdentifier,
        message_key: [u8; ATTACHMENT_KEY_BYTES],
        manifest: EncryptedAttachmentManifest,
    ) -> Result<Self, CourierAttachmentReferenceError> {
        if message_key.iter().all(|byte| *byte == 0) {
            return Err(CourierAttachmentReferenceError::InvalidMessageKey);
        }
        if manifest.identifier() != identifier {
            return Err(CourierAttachmentReferenceError::IdentifierMismatch);
        }
        Ok(Self {
            identifier,
            message_key,
            manifest,
        })
    }

    #[must_use]
    pub const fn identifier(&self) -> AttachmentIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn message_key(&self) -> &[u8; ATTACHMENT_KEY_BYTES] {
        &self.message_key
    }

    #[must_use]
    pub const fn manifest(&self) -> &EncryptedAttachmentManifest {
        &self.manifest
    }

    pub fn encode(&self) -> Result<Vec<u8>, CourierAttachmentReferenceError> {
        if self.message_key.iter().all(|byte| *byte == 0) {
            return Err(CourierAttachmentReferenceError::InvalidMessageKey);
        }
        if self.manifest.identifier() != self.identifier {
            return Err(CourierAttachmentReferenceError::IdentifierMismatch);
        }
        let manifest = self
            .manifest
            .encode()
            .map_err(CourierAttachmentReferenceError::Manifest)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(REFERENCE_FIELDS)
            .map_err(|_| CourierAttachmentReferenceError::Encode)?
            .u8(COURIER_ATTACHMENT_REFERENCE_SCHEMA_VERSION)
            .map_err(|_| CourierAttachmentReferenceError::Encode)?
            .bytes(self.identifier.as_bytes())
            .map_err(|_| CourierAttachmentReferenceError::Encode)?
            .bytes(&self.message_key)
            .map_err(|_| CourierAttachmentReferenceError::Encode)?
            .bytes(&manifest)
            .map_err(|_| CourierAttachmentReferenceError::Encode)?;
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_COURIER_ATTACHMENT_REFERENCE_BYTES {
            return Err(CourierAttachmentReferenceError::TooLarge);
        }
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, CourierAttachmentReferenceError> {
        if encoded.len() > MAX_COURIER_ATTACHMENT_REFERENCE_BYTES {
            return Err(CourierAttachmentReferenceError::TooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| CourierAttachmentReferenceError::Decode)?
            != Some(REFERENCE_FIELDS)
        {
            return Err(CourierAttachmentReferenceError::InvalidShape);
        }
        if decoder
            .u8()
            .map_err(|_| CourierAttachmentReferenceError::Decode)?
            != COURIER_ATTACHMENT_REFERENCE_SCHEMA_VERSION
        {
            return Err(CourierAttachmentReferenceError::UnsupportedSchemaVersion);
        }
        let identifier = AttachmentIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierAttachmentReferenceError::Decode)?
                .try_into()
                .map_err(|_| CourierAttachmentReferenceError::InvalidIdentifier)?,
        )
        .map_err(|_| CourierAttachmentReferenceError::InvalidIdentifier)?;
        let message_key = decoder
            .bytes()
            .map_err(|_| CourierAttachmentReferenceError::Decode)?
            .try_into()
            .map_err(|_| CourierAttachmentReferenceError::InvalidMessageKey)?;
        let manifest = EncryptedAttachmentManifest::decode(
            decoder
                .bytes()
                .map_err(|_| CourierAttachmentReferenceError::Decode)?,
        )
        .map_err(CourierAttachmentReferenceError::Manifest)?;
        if decoder.position() != encoded.len() {
            return Err(CourierAttachmentReferenceError::TrailingBytes);
        }
        let reference = Self::new(identifier, message_key, manifest)?;
        if reference.encode()? != encoded {
            return Err(CourierAttachmentReferenceError::NonCanonicalEncoding);
        }
        Ok(reference)
    }
}

impl fmt::Debug for CourierAttachmentReference {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("CourierAttachmentReference")
            .field("identifier", &self.identifier)
            .field("message_key", &"REDACTED")
            .field("manifest", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum CourierAttachmentReferenceError {
    #[error("courier attachment reference exceeds the configured limit")]
    TooLarge,
    #[error("courier attachment reference schema is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier attachment reference has an invalid shape")]
    InvalidShape,
    #[error("courier attachment reference has an invalid identifier")]
    InvalidIdentifier,
    #[error("courier attachment reference has an invalid message key")]
    InvalidMessageKey,
    #[error("courier attachment reference identifier does not match its manifest")]
    IdentifierMismatch,
    #[error("courier attachment reference manifest is invalid")]
    Manifest(#[source] AttachmentManifestError),
    #[error("courier attachment reference CBOR encoding failed")]
    Encode,
    #[error("courier attachment reference CBOR decoding failed")]
    Decode,
    #[error("courier attachment reference has trailing bytes")]
    TrailingBytes,
    #[error("courier attachment reference is not canonical")]
    NonCanonicalEncoding,
}

#[cfg(test)]
mod tests {
    use crate::{
        AttachmentIdentifier, AttachmentKey, AttachmentManifest, CourierAttachmentReference,
    };

    #[test]
    fn encodes_a_redacted_attachment_reference() {
        let identifier = AttachmentIdentifier::from_bytes([0x11; 16]).unwrap();
        let message_key = [0x22; 32];
        let key = AttachmentKey::derive(&message_key, identifier).unwrap();
        let manifest = AttachmentManifest::new_with_limit(
            identifier,
            65_536,
            vec![crate::AttachmentChunkHash::from_bytes([0x33; 32])],
            crate::AttachmentSizeLimit::default(),
        )
        .unwrap()
        .encrypt(&key)
        .unwrap();
        let reference = CourierAttachmentReference::new(identifier, message_key, manifest).unwrap();
        let encoded = reference.encode().unwrap();
        assert_eq!(
            CourierAttachmentReference::decode(&encoded).unwrap(),
            reference
        );
        assert!(format!("{reference:?}").contains("REDACTED"));
    }
}
