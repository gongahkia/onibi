use minicbor::{Decoder, Encoder};

use crate::wire::MAX_FRAME_BYTES;

pub const MAX_ENCRYPTED_HEADER_BYTES: usize = 16_384;
const MAX_ENCRYPTED_MESSAGE_BYTES: usize = MAX_FRAME_BYTES - 32;
const ENCRYPTED_MESSAGE_FIELDS: u64 = 2;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EncryptedMessageEnvelope {
    encrypted_header: Vec<u8>,
    ciphertext: Vec<u8>,
}

impl EncryptedMessageEnvelope {
    pub fn new(
        encrypted_header: Vec<u8>,
        ciphertext: Vec<u8>,
    ) -> Result<Self, EncryptedMessageError> {
        validate_parts(&encrypted_header, &ciphertext)?;
        Ok(Self {
            encrypted_header,
            ciphertext,
        })
    }

    #[must_use]
    pub fn encrypted_header(&self) -> &[u8] {
        &self.encrypted_header
    }

    #[must_use]
    pub fn ciphertext(&self) -> &[u8] {
        &self.ciphertext
    }

    pub fn encode(&self) -> Result<Vec<u8>, EncryptedMessageError> {
        validate_parts(&self.encrypted_header, &self.ciphertext)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ENCRYPTED_MESSAGE_FIELDS)
            .map_err(|_| EncryptedMessageError::Encode)?
            .bytes(&self.encrypted_header)
            .map_err(|_| EncryptedMessageError::Encode)?
            .bytes(&self.ciphertext)
            .map_err(|_| EncryptedMessageError::Encode)?;
        let output = encoder.into_writer();
        if output.len() > MAX_ENCRYPTED_MESSAGE_BYTES {
            return Err(EncryptedMessageError::EnvelopeTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, EncryptedMessageError> {
        if encoded.len() > MAX_ENCRYPTED_MESSAGE_BYTES {
            return Err(EncryptedMessageError::EnvelopeTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| EncryptedMessageError::Decode)?
            != Some(ENCRYPTED_MESSAGE_FIELDS)
        {
            return Err(EncryptedMessageError::InvalidShape);
        }
        let encrypted_header = decoder.bytes().map_err(|_| EncryptedMessageError::Decode)?;
        let ciphertext = decoder.bytes().map_err(|_| EncryptedMessageError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(EncryptedMessageError::TrailingBytes);
        }
        validate_parts(encrypted_header, ciphertext)?;
        Ok(Self {
            encrypted_header: encrypted_header.to_vec(),
            ciphertext: ciphertext.to_vec(),
        })
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum EncryptedMessageError {
    #[error("encrypted message exceeds the configured limit")]
    EnvelopeTooLarge,
    #[error("encrypted header is required")]
    MissingHeader,
    #[error("encrypted header exceeds the configured limit")]
    HeaderTooLarge,
    #[error("ciphertext is required")]
    MissingCiphertext,
    #[error("encrypted message must be a two-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after encrypted message")]
    TrailingBytes,
}

fn validate_parts(encrypted_header: &[u8], ciphertext: &[u8]) -> Result<(), EncryptedMessageError> {
    if encrypted_header.is_empty() {
        return Err(EncryptedMessageError::MissingHeader);
    }
    if encrypted_header.len() > MAX_ENCRYPTED_HEADER_BYTES {
        return Err(EncryptedMessageError::HeaderTooLarge);
    }
    if ciphertext.is_empty() {
        return Err(EncryptedMessageError::MissingCiphertext);
    }
    let content_length = encrypted_header
        .len()
        .checked_add(ciphertext.len())
        .ok_or(EncryptedMessageError::EnvelopeTooLarge)?;
    if content_length > MAX_ENCRYPTED_MESSAGE_BYTES {
        return Err(EncryptedMessageError::EnvelopeTooLarge);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{EncryptedMessageEnvelope, EncryptedMessageError, MAX_ENCRYPTED_HEADER_BYTES};

    #[test]
    fn canonical_envelope_round_trip() {
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc3]).unwrap();
        let encoded = envelope.encode().unwrap();
        assert_eq!(encoded, [0x82, 0x41, 0xa1, 0x42, 0xb2, 0xc3]);
        assert_eq!(
            EncryptedMessageEnvelope::decode(&encoded).unwrap(),
            envelope
        );
    }

    #[test]
    fn rejects_empty_or_oversized_parts() {
        assert_eq!(
            EncryptedMessageEnvelope::new(Vec::new(), vec![1]).unwrap_err(),
            EncryptedMessageError::MissingHeader
        );
        assert_eq!(
            EncryptedMessageEnvelope::new(vec![1], Vec::new()).unwrap_err(),
            EncryptedMessageError::MissingCiphertext
        );
        assert_eq!(
            EncryptedMessageEnvelope::new(vec![0; MAX_ENCRYPTED_HEADER_BYTES + 1], vec![1])
                .unwrap_err(),
            EncryptedMessageError::HeaderTooLarge
        );
    }

    #[test]
    fn rejects_noncanonical_shapes_and_trailing_data() {
        assert_eq!(
            EncryptedMessageEnvelope::decode(&[0x9f, 0x41, 1, 0x41, 2, 0xff]).unwrap_err(),
            EncryptedMessageError::InvalidShape
        );
        assert_eq!(
            EncryptedMessageEnvelope::decode(&[0x82, 0x41, 1, 0x41, 2, 0]).unwrap_err(),
            EncryptedMessageError::TrailingBytes
        );
    }
}
