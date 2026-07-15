use std::fmt;

use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use minicbor::{Decoder, Encoder};

use crate::{
    ATTACHMENT_CHUNK_BYTES, AttachmentChunkHash, AttachmentIdentifier, AttachmentKey, CryptoDomain,
};

pub const ATTACHMENT_MANIFEST_SCHEMA_VERSION: u8 = 1;
pub const ENCRYPTED_ATTACHMENT_MANIFEST_SCHEMA_VERSION: u8 = 1;
pub const ATTACHMENT_MANIFEST_NONCE_BYTES: usize = 24;
pub const ATTACHMENT_MANIFEST_TAG_BYTES: usize = 16;
pub const MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES: usize = 4 * 1024 * 1024;
const MANIFEST_FIELDS: u64 = 3;
const ENCRYPTED_MANIFEST_FIELDS: u64 = 4;
const MAX_MANIFEST_CHUNKS: usize =
    (MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES - ATTACHMENT_MANIFEST_TAG_BYTES - 64) / 40;

#[derive(Clone, Eq, PartialEq)]
pub struct AttachmentManifest {
    identifier: AttachmentIdentifier,
    plaintext_length: u64,
    chunk_hashes: Vec<AttachmentChunkHash>,
}

impl AttachmentManifest {
    pub fn new(
        identifier: AttachmentIdentifier,
        plaintext_length: u64,
        chunk_hashes: Vec<AttachmentChunkHash>,
    ) -> Result<Self, AttachmentManifestError> {
        if chunk_hashes.len() > MAX_MANIFEST_CHUNKS {
            return Err(AttachmentManifestError::TooManyChunks);
        }
        validate_plaintext_length(plaintext_length, chunk_hashes.len())?;
        Ok(Self {
            identifier,
            plaintext_length,
            chunk_hashes,
        })
    }

    #[must_use]
    pub const fn identifier(&self) -> AttachmentIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn plaintext_length(&self) -> u64 {
        self.plaintext_length
    }

    #[must_use]
    pub fn chunk_hashes(&self) -> &[AttachmentChunkHash] {
        &self.chunk_hashes
    }

    pub fn encrypt(
        &self,
        key: &AttachmentKey,
    ) -> Result<EncryptedAttachmentManifest, AttachmentManifestError> {
        let encoded = self.encode_plaintext()?;
        let mut nonce = [0; ATTACHMENT_MANIFEST_NONCE_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut nonce)
            .map_err(|_| AttachmentManifestError::Randomness)?;
        let cipher = XChaCha20Poly1305::new_from_slice(key.as_bytes())
            .map_err(|_| AttachmentManifestError::Encryption)?;
        let nonce_value =
            XNonce::try_from(nonce.as_slice()).map_err(|_| AttachmentManifestError::Encryption)?;
        let ciphertext = cipher
            .encrypt(
                &nonce_value,
                Payload {
                    msg: &encoded,
                    aad: &associated_data(self.identifier),
                },
            )
            .map_err(|_| AttachmentManifestError::Encryption)?;
        if ciphertext.len() < ATTACHMENT_MANIFEST_TAG_BYTES {
            return Err(AttachmentManifestError::Encryption);
        }
        Ok(EncryptedAttachmentManifest {
            identifier: self.identifier,
            nonce,
            ciphertext,
        })
    }

    fn encode_plaintext(&self) -> Result<Vec<u8>, AttachmentManifestError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(MANIFEST_FIELDS)
            .map_err(|_| AttachmentManifestError::Encode)?
            .u8(ATTACHMENT_MANIFEST_SCHEMA_VERSION)
            .map_err(|_| AttachmentManifestError::Encode)?
            .u64(self.plaintext_length)
            .map_err(|_| AttachmentManifestError::Encode)?
            .array(
                u64::try_from(self.chunk_hashes.len())
                    .map_err(|_| AttachmentManifestError::TooManyChunks)?,
            )
            .map_err(|_| AttachmentManifestError::Encode)?;
        for hash in &self.chunk_hashes {
            encoder
                .bytes(hash.as_bytes())
                .map_err(|_| AttachmentManifestError::Encode)?;
        }
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES {
            return Err(AttachmentManifestError::ManifestTooLarge);
        }
        Ok(encoded)
    }

    fn decode_plaintext(
        identifier: AttachmentIdentifier,
        encoded: &[u8],
    ) -> Result<Self, AttachmentManifestError> {
        if encoded.len() > MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES {
            return Err(AttachmentManifestError::ManifestTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| AttachmentManifestError::Decode)?
            != Some(MANIFEST_FIELDS)
        {
            return Err(AttachmentManifestError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| AttachmentManifestError::Decode)?;
        if version != ATTACHMENT_MANIFEST_SCHEMA_VERSION {
            return Err(AttachmentManifestError::UnsupportedSchemaVersion(version));
        }
        let plaintext_length = decoder.u64().map_err(|_| AttachmentManifestError::Decode)?;
        let count = decoder
            .array()
            .map_err(|_| AttachmentManifestError::Decode)?
            .ok_or(AttachmentManifestError::InvalidShape)?;
        let count = usize::try_from(count).map_err(|_| AttachmentManifestError::TooManyChunks)?;
        if count > MAX_MANIFEST_CHUNKS {
            return Err(AttachmentManifestError::TooManyChunks);
        }
        let mut chunk_hashes = Vec::with_capacity(count);
        for _ in 0..count {
            chunk_hashes.push(AttachmentChunkHash::from_bytes(
                decoder
                    .bytes()
                    .map_err(|_| AttachmentManifestError::Decode)?
                    .try_into()
                    .map_err(|_| AttachmentManifestError::InvalidChunkHash)?,
            ));
        }
        if decoder.position() != encoded.len() {
            return Err(AttachmentManifestError::TrailingBytes);
        }
        let manifest = Self::new(identifier, plaintext_length, chunk_hashes)?;
        if manifest.encode_plaintext()? != encoded {
            return Err(AttachmentManifestError::NonCanonicalEncoding);
        }
        Ok(manifest)
    }
}

impl fmt::Debug for AttachmentManifest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("AttachmentManifest")
            .field("identifier", &self.identifier)
            .field("plaintext_length", &self.plaintext_length)
            .field("chunk_count", &self.chunk_hashes.len())
            .finish()
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct EncryptedAttachmentManifest {
    identifier: AttachmentIdentifier,
    nonce: [u8; ATTACHMENT_MANIFEST_NONCE_BYTES],
    ciphertext: Vec<u8>,
}

impl EncryptedAttachmentManifest {
    #[must_use]
    pub const fn identifier(&self) -> AttachmentIdentifier {
        self.identifier
    }

    pub fn decrypt(
        &self,
        key: &AttachmentKey,
    ) -> Result<AttachmentManifest, AttachmentManifestError> {
        if self.ciphertext.len() < ATTACHMENT_MANIFEST_TAG_BYTES {
            return Err(AttachmentManifestError::InvalidCiphertextLength);
        }
        let cipher = XChaCha20Poly1305::new_from_slice(key.as_bytes())
            .map_err(|_| AttachmentManifestError::Authentication)?;
        let nonce = XNonce::try_from(self.nonce.as_slice())
            .map_err(|_| AttachmentManifestError::Authentication)?;
        let plaintext = cipher
            .decrypt(
                &nonce,
                Payload {
                    msg: &self.ciphertext,
                    aad: &associated_data(self.identifier),
                },
            )
            .map_err(|_| AttachmentManifestError::Authentication)?;
        AttachmentManifest::decode_plaintext(self.identifier, &plaintext)
    }

    pub fn encode(&self) -> Result<Vec<u8>, AttachmentManifestError> {
        if self.ciphertext.len() < ATTACHMENT_MANIFEST_TAG_BYTES {
            return Err(AttachmentManifestError::InvalidCiphertextLength);
        }
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ENCRYPTED_MANIFEST_FIELDS)
            .map_err(|_| AttachmentManifestError::Encode)?
            .u8(ENCRYPTED_ATTACHMENT_MANIFEST_SCHEMA_VERSION)
            .map_err(|_| AttachmentManifestError::Encode)?
            .bytes(self.identifier.as_bytes())
            .map_err(|_| AttachmentManifestError::Encode)?
            .bytes(&self.nonce)
            .map_err(|_| AttachmentManifestError::Encode)?
            .bytes(&self.ciphertext)
            .map_err(|_| AttachmentManifestError::Encode)?;
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES {
            return Err(AttachmentManifestError::ManifestTooLarge);
        }
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, AttachmentManifestError> {
        if encoded.len() > MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES {
            return Err(AttachmentManifestError::ManifestTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| AttachmentManifestError::Decode)?
            != Some(ENCRYPTED_MANIFEST_FIELDS)
        {
            return Err(AttachmentManifestError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| AttachmentManifestError::Decode)?;
        if version != ENCRYPTED_ATTACHMENT_MANIFEST_SCHEMA_VERSION {
            return Err(AttachmentManifestError::UnsupportedEncryptedSchemaVersion(
                version,
            ));
        }
        let identifier = AttachmentIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| AttachmentManifestError::Decode)?
                .try_into()
                .map_err(|_| AttachmentManifestError::InvalidIdentifier)?,
        )
        .map_err(|_| AttachmentManifestError::InvalidIdentifier)?;
        let nonce: [u8; ATTACHMENT_MANIFEST_NONCE_BYTES] = decoder
            .bytes()
            .map_err(|_| AttachmentManifestError::Decode)?
            .try_into()
            .map_err(|_| AttachmentManifestError::InvalidNonceLength)?;
        let ciphertext = decoder
            .bytes()
            .map_err(|_| AttachmentManifestError::Decode)?;
        if ciphertext.len() < ATTACHMENT_MANIFEST_TAG_BYTES {
            return Err(AttachmentManifestError::InvalidCiphertextLength);
        }
        if decoder.position() != encoded.len() {
            return Err(AttachmentManifestError::TrailingBytes);
        }
        let manifest = Self {
            identifier,
            nonce,
            ciphertext: ciphertext.to_vec(),
        };
        if manifest.encode()? != encoded {
            return Err(AttachmentManifestError::NonCanonicalEncoding);
        }
        Ok(manifest)
    }
}

impl fmt::Debug for EncryptedAttachmentManifest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("EncryptedAttachmentManifest")
            .field("identifier", &self.identifier)
            .field("nonce", &"REDACTED")
            .field("ciphertext", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentManifestError {
    #[error("attachment manifest requires at least one chunk")]
    EmptyAttachment,
    #[error("attachment manifest contains too many chunks")]
    TooManyChunks,
    #[error("attachment manifest plaintext length does not match its fixed-size chunks")]
    InvalidPlaintextLength,
    #[error("operating-system random source failed")]
    Randomness,
    #[error("attachment manifest encryption failed")]
    Encryption,
    #[error("attachment manifest authentication failed")]
    Authentication,
    #[error("attachment manifest exceeds the configured limit")]
    ManifestTooLarge,
    #[error("attachment manifest ciphertext has an invalid length")]
    InvalidCiphertextLength,
    #[error("attachment manifest nonce has an invalid length")]
    InvalidNonceLength,
    #[error("attachment manifest identifier is invalid")]
    InvalidIdentifier,
    #[error("attachment manifest chunk hash is invalid")]
    InvalidChunkHash,
    #[error("attachment manifest has an invalid CBOR shape")]
    InvalidShape,
    #[error("attachment manifest schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("encrypted attachment manifest schema version is unsupported: {0}")]
    UnsupportedEncryptedSchemaVersion(u8),
    #[error("attachment manifest CBOR decoding failed")]
    Decode,
    #[error("attachment manifest CBOR encoding failed")]
    Encode,
    #[error("attachment manifest has trailing bytes")]
    TrailingBytes,
    #[error("attachment manifest is not canonically encoded")]
    NonCanonicalEncoding,
}

fn validate_plaintext_length(
    plaintext_length: u64,
    chunk_count: usize,
) -> Result<(), AttachmentManifestError> {
    let chunk_count =
        u64::try_from(chunk_count).map_err(|_| AttachmentManifestError::TooManyChunks)?;
    if chunk_count == 0 {
        return Err(AttachmentManifestError::EmptyAttachment);
    }
    let maximum = chunk_count
        .checked_mul(ATTACHMENT_CHUNK_BYTES as u64)
        .ok_or(AttachmentManifestError::TooManyChunks)?;
    if plaintext_length == 0
        || plaintext_length > maximum
        || plaintext_length <= maximum - ATTACHMENT_CHUNK_BYTES as u64
    {
        return Err(AttachmentManifestError::InvalidPlaintextLength);
    }
    Ok(())
}

fn associated_data(identifier: AttachmentIdentifier) -> Vec<u8> {
    let context = CryptoDomain::AttachmentManifestEncryption.context();
    let mut data = Vec::with_capacity(context.len() + identifier.as_bytes().len());
    data.extend_from_slice(context);
    data.extend_from_slice(identifier.as_bytes());
    data
}

#[cfg(test)]
mod tests {
    use super::{AttachmentManifest, AttachmentManifestError, EncryptedAttachmentManifest};
    use crate::{
        ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, EncryptedAttachmentChunk,
    };

    fn chunk(identifier: AttachmentIdentifier, index: u32) -> EncryptedAttachmentChunk {
        let key = AttachmentKey::derive(&[0x11; 32], identifier)
            .unwrap()
            .derive_chunk_key(index)
            .unwrap();
        EncryptedAttachmentChunk::encrypt(
            identifier,
            index,
            &key,
            &vec![u8::try_from(index).unwrap(); ATTACHMENT_CHUNK_BYTES],
        )
        .unwrap()
    }

    #[test]
    fn encrypts_and_authenticates_attachment_manifests() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let key = AttachmentKey::derive(&[0x11; 32], identifier).unwrap();
        let first = chunk(identifier, 0);
        let second = chunk(identifier, 1);
        let manifest = AttachmentManifest::new(
            identifier,
            ATTACHMENT_CHUNK_BYTES as u64 + 17,
            vec![first.hash().unwrap(), second.hash().unwrap()],
        )
        .unwrap();

        let encrypted = manifest.encrypt(&key).unwrap();
        let decoded = EncryptedAttachmentManifest::decode(&encrypted.encode().unwrap()).unwrap();
        assert_eq!(decoded.decrypt(&key).unwrap(), manifest);
        let wrong_key = AttachmentKey::derive(&[0x33; 32], identifier).unwrap();
        assert_eq!(
            decoded.decrypt(&wrong_key),
            Err(AttachmentManifestError::Authentication)
        );
    }

    #[test]
    fn rejects_invalid_lengths_and_attachment_identifier_tampering() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let key = AttachmentKey::derive(&[0x11; 32], identifier).unwrap();
        let hash = chunk(identifier, 0).hash().unwrap();
        assert_eq!(
            AttachmentManifest::new(identifier, 0, vec![hash]),
            Err(AttachmentManifestError::InvalidPlaintextLength)
        );
        let manifest = AttachmentManifest::new(identifier, 1, vec![hash]).unwrap();
        let mut encrypted = manifest.encrypt(&key).unwrap();
        encrypted.identifier = AttachmentIdentifier::from_bytes([0x33; 16]).unwrap();
        assert_eq!(
            encrypted.decrypt(&key),
            Err(AttachmentManifestError::Authentication)
        );
    }
}
