use std::fmt;

use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use minicbor::{Decoder, Encoder};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use zeroize::Zeroizing;

use crate::{AttachmentChunkKey, AttachmentIdentifier, CryptoDomain};

pub const ENCRYPTED_ATTACHMENT_CHUNK_SCHEMA_VERSION: u8 = 1;
pub const ATTACHMENT_CHUNK_BYTES: usize = 64 * 1024;
pub const ATTACHMENT_CHUNK_NONCE_BYTES: usize = 24;
pub const ATTACHMENT_CHUNK_TAG_BYTES: usize = 16;
pub const ENCRYPTED_ATTACHMENT_CHUNK_BYTES: usize =
    ATTACHMENT_CHUNK_BYTES + ATTACHMENT_CHUNK_TAG_BYTES;
pub const MAX_ENCODED_ATTACHMENT_CHUNK_BYTES: usize =
    ENCRYPTED_ATTACHMENT_CHUNK_BYTES + ATTACHMENT_CHUNK_NONCE_BYTES + 64;
const ENCRYPTED_ATTACHMENT_CHUNK_FIELDS: u64 = 5;
pub const ATTACHMENT_CHUNK_HASH_BYTES: usize = 32;

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct AttachmentChunkHash([u8; ATTACHMENT_CHUNK_HASH_BYTES]);

impl AttachmentChunkHash {
    #[must_use]
    pub const fn from_bytes(hash: [u8; ATTACHMENT_CHUNK_HASH_BYTES]) -> Self {
        Self(hash)
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; ATTACHMENT_CHUNK_HASH_BYTES] {
        &self.0
    }
}

impl fmt::Debug for AttachmentChunkHash {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("AttachmentChunkHash(REDACTED)")
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct EncryptedAttachmentChunk {
    identifier: AttachmentIdentifier,
    index: u32,
    nonce: [u8; ATTACHMENT_CHUNK_NONCE_BYTES],
    ciphertext: Vec<u8>,
}

impl EncryptedAttachmentChunk {
    pub fn encrypt(
        identifier: AttachmentIdentifier,
        index: u32,
        key: &AttachmentChunkKey,
        plaintext: &[u8],
    ) -> Result<Self, AttachmentChunkError> {
        if plaintext.len() != ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::InvalidPlaintextLength);
        }
        let mut nonce = [0; ATTACHMENT_CHUNK_NONCE_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut nonce)
            .map_err(|_| AttachmentChunkError::Randomness)?;
        let cipher = XChaCha20Poly1305::new_from_slice(key.as_bytes())
            .map_err(|_| AttachmentChunkError::Encryption)?;
        let nonce_value =
            XNonce::try_from(nonce.as_slice()).map_err(|_| AttachmentChunkError::Encryption)?;
        let ciphertext = cipher
            .encrypt(
                &nonce_value,
                Payload {
                    msg: plaintext,
                    aad: &associated_data(identifier, index),
                },
            )
            .map_err(|_| AttachmentChunkError::Encryption)?;
        if ciphertext.len() != ENCRYPTED_ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::Encryption);
        }
        Ok(Self {
            identifier,
            index,
            nonce,
            ciphertext,
        })
    }

    #[must_use]
    pub const fn identifier(&self) -> AttachmentIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn index(&self) -> u32 {
        self.index
    }

    pub fn decrypt(
        &self,
        key: &AttachmentChunkKey,
    ) -> Result<Zeroizing<Vec<u8>>, AttachmentChunkError> {
        if self.ciphertext.len() != ENCRYPTED_ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::InvalidCiphertextLength);
        }
        let cipher = XChaCha20Poly1305::new_from_slice(key.as_bytes())
            .map_err(|_| AttachmentChunkError::Authentication)?;
        let nonce = XNonce::try_from(self.nonce.as_slice())
            .map_err(|_| AttachmentChunkError::Authentication)?;
        let plaintext = Zeroizing::new(
            cipher
                .decrypt(
                    &nonce,
                    Payload {
                        msg: &self.ciphertext,
                        aad: &associated_data(self.identifier, self.index),
                    },
                )
                .map_err(|_| AttachmentChunkError::Authentication)?,
        );
        if plaintext.len() != ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::Authentication);
        }
        Ok(plaintext)
    }

    pub fn encode(&self) -> Result<Vec<u8>, AttachmentChunkError> {
        if self.ciphertext.len() != ENCRYPTED_ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::InvalidCiphertextLength);
        }
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ENCRYPTED_ATTACHMENT_CHUNK_FIELDS)
            .map_err(|_| AttachmentChunkError::Encode)?
            .u8(ENCRYPTED_ATTACHMENT_CHUNK_SCHEMA_VERSION)
            .map_err(|_| AttachmentChunkError::Encode)?
            .bytes(self.identifier.as_bytes())
            .map_err(|_| AttachmentChunkError::Encode)?
            .u32(self.index)
            .map_err(|_| AttachmentChunkError::Encode)?
            .bytes(&self.nonce)
            .map_err(|_| AttachmentChunkError::Encode)?
            .bytes(&self.ciphertext)
            .map_err(|_| AttachmentChunkError::Encode)?;
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_ENCODED_ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::EncodedChunkTooLarge);
        }
        Ok(encoded)
    }

    pub fn hash(&self) -> Result<AttachmentChunkHash, AttachmentChunkError> {
        let encoded = self.encode()?;
        let mut hasher = Sha256::new();
        hasher.update(CryptoDomain::AttachmentChunkHash.context());
        hasher.update(&encoded);
        let mut hash = [0; ATTACHMENT_CHUNK_HASH_BYTES];
        hash.copy_from_slice(&hasher.finalize());
        Ok(AttachmentChunkHash(hash))
    }

    pub fn validate_hash(
        &self,
        expected: &AttachmentChunkHash,
    ) -> Result<(), AttachmentChunkError> {
        if self.hash()?.as_bytes().ct_eq(expected.as_bytes()).into() {
            return Ok(());
        }
        Err(AttachmentChunkError::HashMismatch)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, AttachmentChunkError> {
        if encoded.len() > MAX_ENCODED_ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::EncodedChunkTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| AttachmentChunkError::Decode)?
            != Some(ENCRYPTED_ATTACHMENT_CHUNK_FIELDS)
        {
            return Err(AttachmentChunkError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| AttachmentChunkError::Decode)?;
        if version != ENCRYPTED_ATTACHMENT_CHUNK_SCHEMA_VERSION {
            return Err(AttachmentChunkError::UnsupportedSchemaVersion(version));
        }
        let identifier = AttachmentIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| AttachmentChunkError::Decode)?
                .try_into()
                .map_err(|_| AttachmentChunkError::InvalidIdentifier)?,
        )
        .map_err(|_| AttachmentChunkError::InvalidIdentifier)?;
        let index = decoder.u32().map_err(|_| AttachmentChunkError::Decode)?;
        let nonce: [u8; ATTACHMENT_CHUNK_NONCE_BYTES] = decoder
            .bytes()
            .map_err(|_| AttachmentChunkError::Decode)?
            .try_into()
            .map_err(|_| AttachmentChunkError::InvalidNonceLength)?;
        let ciphertext = decoder.bytes().map_err(|_| AttachmentChunkError::Decode)?;
        if ciphertext.len() != ENCRYPTED_ATTACHMENT_CHUNK_BYTES {
            return Err(AttachmentChunkError::InvalidCiphertextLength);
        }
        if decoder.position() != encoded.len() {
            return Err(AttachmentChunkError::TrailingBytes);
        }
        let chunk = Self {
            identifier,
            index,
            nonce,
            ciphertext: ciphertext.to_vec(),
        };
        if chunk.encode()? != encoded {
            return Err(AttachmentChunkError::NonCanonicalEncoding);
        }
        Ok(chunk)
    }
}

impl fmt::Debug for EncryptedAttachmentChunk {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("EncryptedAttachmentChunk")
            .field("identifier", &self.identifier)
            .field("index", &self.index)
            .field("nonce", &"REDACTED")
            .field("ciphertext", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentChunkError {
    #[error("attachment chunk plaintext must be exactly the fixed chunk size")]
    InvalidPlaintextLength,
    #[error("operating-system random source failed")]
    Randomness,
    #[error("attachment chunk encryption failed")]
    Encryption,
    #[error("attachment chunk authentication failed")]
    Authentication,
    #[error("encoded attachment chunk exceeds the configured limit")]
    EncodedChunkTooLarge,
    #[error("attachment chunk ciphertext has an invalid length")]
    InvalidCiphertextLength,
    #[error("attachment chunk nonce has an invalid length")]
    InvalidNonceLength,
    #[error("attachment chunk identifier is invalid")]
    InvalidIdentifier,
    #[error("attachment chunk has an invalid CBOR shape")]
    InvalidShape,
    #[error("attachment chunk schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("attachment chunk CBOR decoding failed")]
    Decode,
    #[error("attachment chunk CBOR encoding failed")]
    Encode,
    #[error("attachment chunk has trailing bytes")]
    TrailingBytes,
    #[error("attachment chunk is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("attachment chunk hash does not match the expected value")]
    HashMismatch,
}

fn associated_data(identifier: AttachmentIdentifier, index: u32) -> Vec<u8> {
    let context = CryptoDomain::AttachmentChunkEncryption.context();
    let mut data = Vec::with_capacity(context.len() + identifier.as_bytes().len() + 4);
    data.extend_from_slice(context);
    data.extend_from_slice(identifier.as_bytes());
    data.extend_from_slice(&index.to_be_bytes());
    data
}

#[cfg(test)]
mod tests {
    use minicbor::Encoder;

    use super::{ATTACHMENT_CHUNK_BYTES, AttachmentChunkError, EncryptedAttachmentChunk};
    use crate::{AttachmentIdentifier, AttachmentKey};

    fn key(identifier: AttachmentIdentifier, index: u32) -> crate::AttachmentChunkKey {
        AttachmentKey::derive(&[0x11; 32], identifier)
            .unwrap()
            .derive_chunk_key(index)
            .unwrap()
    }

    #[test]
    fn encrypts_fixed_size_chunks_with_attachment_bound_authentication() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let plaintext = vec![0x33; ATTACHMENT_CHUNK_BYTES];
        let chunk =
            EncryptedAttachmentChunk::encrypt(identifier, 7, &key(identifier, 7), &plaintext)
                .unwrap();
        let encoded = chunk.encode().unwrap();
        let decoded = EncryptedAttachmentChunk::decode(&encoded).unwrap();

        assert_eq!(decoded.identifier(), identifier);
        assert_eq!(decoded.index(), 7);
        assert_eq!(&*decoded.decrypt(&key(identifier, 7)).unwrap(), &plaintext);
        let hash = decoded.hash().unwrap();
        decoded.validate_hash(&hash).unwrap();
        assert!(matches!(
            decoded.decrypt(&key(identifier, 8)),
            Err(AttachmentChunkError::Authentication)
        ));
        let mut tampered = decoded.clone();
        tampered.index = 8;
        assert_eq!(
            tampered.validate_hash(&hash),
            Err(AttachmentChunkError::HashMismatch)
        );
        assert!(matches!(
            tampered.decrypt(&key(identifier, 7)),
            Err(AttachmentChunkError::Authentication)
        ));
        assert!(format!("{decoded:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_nonfixed_or_malformed_attachment_chunks() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        assert_eq!(
            EncryptedAttachmentChunk::encrypt(identifier, 0, &key(identifier, 0), &[0x33]),
            Err(AttachmentChunkError::InvalidPlaintextLength)
        );
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(5)
            .unwrap()
            .u8(1)
            .unwrap()
            .bytes(identifier.as_bytes())
            .unwrap()
            .u32(0)
            .unwrap()
            .bytes(&[0; 24])
            .unwrap()
            .bytes(&[0; 1])
            .unwrap();
        assert_eq!(
            EncryptedAttachmentChunk::decode(&encoder.into_writer()),
            Err(AttachmentChunkError::InvalidCiphertextLength)
        );
    }
}
