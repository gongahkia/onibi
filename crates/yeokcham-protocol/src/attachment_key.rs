use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};
use hkdf::Hkdf;
use sha2::Sha256;
use zeroize::Zeroizing;

use crate::CryptoDomain;

pub const ATTACHMENT_IDENTIFIER_BYTES: usize = 16;
pub const ATTACHMENT_KEY_BYTES: usize = 32;

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct AttachmentIdentifier([u8; ATTACHMENT_IDENTIFIER_BYTES]);

impl AttachmentIdentifier {
    pub fn generate() -> Result<Self, AttachmentKeyError> {
        let mut identifier = [0; ATTACHMENT_IDENTIFIER_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut identifier)
            .map_err(|_| AttachmentKeyError::Randomness)?;
        Self::from_bytes(identifier)
    }

    pub fn from_bytes(
        identifier: [u8; ATTACHMENT_IDENTIFIER_BYTES],
    ) -> Result<Self, AttachmentKeyError> {
        if identifier.iter().all(|byte| *byte == 0) {
            return Err(AttachmentKeyError::ZeroIdentifier);
        }
        Ok(Self(identifier))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; ATTACHMENT_IDENTIFIER_BYTES] {
        &self.0
    }
}

impl fmt::Debug for AttachmentIdentifier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("AttachmentIdentifier(REDACTED)")
    }
}

pub struct AttachmentKey(Zeroizing<[u8; ATTACHMENT_KEY_BYTES]>);

impl AttachmentKey {
    pub fn derive(
        message_key: &[u8; ATTACHMENT_KEY_BYTES],
        identifier: AttachmentIdentifier,
    ) -> Result<Self, AttachmentKeyError> {
        if message_key.iter().all(|byte| *byte == 0) {
            return Err(AttachmentKeyError::InvalidMessageKey);
        }
        let hkdf = Hkdf::<Sha256>::new(Some(identifier.as_bytes()), message_key);
        let mut key = Zeroizing::new([0; ATTACHMENT_KEY_BYTES]);
        hkdf.expand(CryptoDomain::AttachmentKey.context(), key.as_mut())
            .map_err(|_| AttachmentKeyError::KeyDerivation)?;
        Ok(Self(key))
    }

    pub fn derive_chunk_key(
        &self,
        chunk_index: u32,
    ) -> Result<AttachmentChunkKey, AttachmentKeyError> {
        let hkdf = Hkdf::<Sha256>::new(None, self.0.as_ref());
        let context = CryptoDomain::AttachmentChunkKey.context();
        let mut info = Vec::with_capacity(context.len() + 4);
        info.extend_from_slice(context);
        info.extend_from_slice(&chunk_index.to_be_bytes());
        let mut key = Zeroizing::new([0; ATTACHMENT_KEY_BYTES]);
        hkdf.expand(&info, key.as_mut())
            .map_err(|_| AttachmentKeyError::KeyDerivation)?;
        Ok(AttachmentChunkKey(key))
    }
}

impl fmt::Debug for AttachmentKey {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("AttachmentKey(REDACTED)")
    }
}

pub struct AttachmentChunkKey(Zeroizing<[u8; ATTACHMENT_KEY_BYTES]>);

impl AttachmentChunkKey {
    #[must_use]
    pub fn as_bytes(&self) -> &[u8; ATTACHMENT_KEY_BYTES] {
        &self.0
    }
}

impl fmt::Debug for AttachmentChunkKey {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("AttachmentChunkKey(REDACTED)")
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentKeyError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("attachment identifier must not be all zeroes")]
    ZeroIdentifier,
    #[error("attachment message key is invalid")]
    InvalidMessageKey,
    #[error("attachment key derivation failed")]
    KeyDerivation,
}

#[cfg(test)]
mod tests {
    use super::{
        ATTACHMENT_IDENTIFIER_BYTES, AttachmentIdentifier, AttachmentKey, AttachmentKeyError,
    };

    #[test]
    fn derives_distinct_attachment_and_chunk_keys() {
        let message_key = [0x11; 32];
        let first_identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let second_identifier = AttachmentIdentifier::from_bytes([0x33; 16]).unwrap();
        let first = AttachmentKey::derive(&message_key, first_identifier).unwrap();
        let repeated = AttachmentKey::derive(&message_key, first_identifier).unwrap();
        let second = AttachmentKey::derive(&message_key, second_identifier).unwrap();
        let first_chunk = first.derive_chunk_key(0).unwrap();
        let repeated_chunk = repeated.derive_chunk_key(0).unwrap();
        let second_chunk = first.derive_chunk_key(1).unwrap();
        let other_attachment_chunk = second.derive_chunk_key(0).unwrap();

        assert_eq!(first_chunk.as_bytes(), repeated_chunk.as_bytes());
        assert_ne!(first_chunk.as_bytes(), second_chunk.as_bytes());
        assert_ne!(first_chunk.as_bytes(), other_attachment_chunk.as_bytes());
        assert!(format!("{first:?}").contains("REDACTED"));
        assert!(format!("{first_chunk:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_zero_key_material_and_identifiers() {
        assert_eq!(
            AttachmentIdentifier::from_bytes([0; ATTACHMENT_IDENTIFIER_BYTES]),
            Err(AttachmentKeyError::ZeroIdentifier)
        );
        let identifier =
            AttachmentIdentifier::from_bytes([1; ATTACHMENT_IDENTIFIER_BYTES]).unwrap();
        assert!(matches!(
            AttachmentKey::derive(&[0; 32], identifier),
            Err(AttachmentKeyError::InvalidMessageKey)
        ));
    }
}
