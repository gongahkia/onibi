use minicbor::{Decoder, Encoder};
use yeokcham_core::IdentityPublicKey;

use crate::{SafetyNumberError, SafetyNumberFingerprint};

pub const QR_VERIFICATION_SCHEMA_VERSION: u8 = 1;
pub const QR_VERIFICATION_PAYLOAD_BYTES: usize = 70;
const QR_VERIFICATION_FIELDS: u64 = 3;
const IDENTITY_PUBLIC_KEY_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct QrVerificationPayload {
    first_identity: IdentityPublicKey,
    second_identity: IdentityPublicKey,
}

impl QrVerificationPayload {
    pub fn new(
        first_identity: IdentityPublicKey,
        second_identity: IdentityPublicKey,
    ) -> Result<Self, QrVerificationError> {
        if first_identity == second_identity {
            return Err(QrVerificationError::SameIdentity);
        }
        let (first_identity, second_identity) =
            if first_identity.as_bytes() < second_identity.as_bytes() {
                (first_identity, second_identity)
            } else {
                (second_identity, first_identity)
            };
        Ok(Self {
            first_identity,
            second_identity,
        })
    }

    #[must_use]
    pub const fn identities(&self) -> (&IdentityPublicKey, &IdentityPublicKey) {
        (&self.first_identity, &self.second_identity)
    }

    pub fn safety_number(&self) -> Result<SafetyNumberFingerprint, SafetyNumberError> {
        SafetyNumberFingerprint::derive(&self.first_identity, &self.second_identity)
    }

    pub fn encode(&self) -> Result<Vec<u8>, QrVerificationError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(QR_VERIFICATION_FIELDS)
            .map_err(|_| QrVerificationError::Encode)?
            .u8(QR_VERIFICATION_SCHEMA_VERSION)
            .map_err(|_| QrVerificationError::Encode)?
            .bytes(self.first_identity.as_bytes())
            .map_err(|_| QrVerificationError::Encode)?
            .bytes(self.second_identity.as_bytes())
            .map_err(|_| QrVerificationError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() != QR_VERIFICATION_PAYLOAD_BYTES {
            return Err(QrVerificationError::Encode);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, QrVerificationError> {
        if encoded.len() > QR_VERIFICATION_PAYLOAD_BYTES {
            return Err(QrVerificationError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| QrVerificationError::Decode)? != Some(QR_VERIFICATION_FIELDS)
        {
            return Err(QrVerificationError::InvalidShape);
        }
        let schema_version = decoder.u8().map_err(|_| QrVerificationError::Decode)?;
        if schema_version != QR_VERIFICATION_SCHEMA_VERSION {
            return Err(QrVerificationError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let first_identity =
            decode_identity(decoder.bytes().map_err(|_| QrVerificationError::Decode)?)?;
        let second_identity =
            decode_identity(decoder.bytes().map_err(|_| QrVerificationError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(QrVerificationError::TrailingBytes);
        }
        let payload = Self::new(first_identity, second_identity)?;
        if payload.encode()? != encoded {
            return Err(QrVerificationError::NonCanonicalEncoding);
        }
        Ok(payload)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum QrVerificationError {
    #[error("QR verification identities must be distinct")]
    SameIdentity,
    #[error("QR verification payload exceeds the configured limit")]
    PayloadTooLarge,
    #[error("unsupported QR verification schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("QR verification payload must be a three-element definite-length CBOR array")]
    InvalidShape,
    #[error("QR verification payload contains an invalid identity key")]
    InvalidIdentity,
    #[error("QR verification payload is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after QR verification payload")]
    TrailingBytes,
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, QrVerificationError> {
    if encoded.len() != IDENTITY_PUBLIC_KEY_BYTES {
        return Err(QrVerificationError::InvalidIdentity);
    }
    let mut bytes = [0; IDENTITY_PUBLIC_KEY_BYTES];
    bytes.copy_from_slice(encoded);
    IdentityPublicKey::from_bytes(bytes).map_err(|_| QrVerificationError::InvalidIdentity)
}

#[cfg(test)]
mod tests {
    use yeokcham_core::IdentityPublicKey;

    use super::{
        QR_VERIFICATION_PAYLOAD_BYTES, QR_VERIFICATION_SCHEMA_VERSION, QrVerificationError,
        QrVerificationPayload,
    };

    fn first_identity() -> IdentityPublicKey {
        IdentityPublicKey::from_bytes([
            0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64,
            0x07, 0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68,
            0xf7, 0x07, 0x51, 0x1a,
        ])
        .unwrap()
    }

    fn second_identity() -> IdentityPublicKey {
        IdentityPublicKey::from_bytes([
            0x3d, 0x40, 0x17, 0xc3, 0xe8, 0x43, 0x89, 0x5a, 0x92, 0xb7, 0x0a, 0xa7, 0x4d, 0x1b,
            0x7e, 0xbc, 0x9c, 0x98, 0x2c, 0xcf, 0x2e, 0xc4, 0x96, 0x8c, 0xc0, 0xcd, 0x55, 0xf1,
            0x2a, 0xf4, 0x66, 0x0c,
        ])
        .unwrap()
    }

    #[test]
    fn canonical_payloads_round_trip_and_match_safety_numbers() {
        let first = first_identity();
        let second = second_identity();
        let payload = QrVerificationPayload::new(first, second).unwrap();
        let encoded = payload.encode().unwrap();
        let mut expected = vec![0x83, QR_VERIFICATION_SCHEMA_VERSION, 0x58, 32];
        expected.extend_from_slice(second.as_bytes());
        expected.extend_from_slice(&[0x58, 32]);
        expected.extend_from_slice(first.as_bytes());

        assert_eq!(encoded.len(), QR_VERIFICATION_PAYLOAD_BYTES);
        assert_eq!(encoded, expected);
        assert_eq!(QrVerificationPayload::decode(&encoded).unwrap(), payload);
        assert_eq!(
            payload.safety_number().unwrap(),
            QrVerificationPayload::new(second, first)
                .unwrap()
                .safety_number()
                .unwrap()
        );
    }

    #[test]
    fn rejects_invalid_noncanonical_and_malformed_payloads() {
        let identity = first_identity();
        let payload = QrVerificationPayload::new(identity, second_identity()).unwrap();
        let encoded = payload.encode().unwrap();
        let mut invalid_identity = encoded;
        let mut noncanonical = invalid_identity.clone();
        invalid_identity[4..36].fill(0);
        let (prefix, suffix) = noncanonical.split_at_mut(38);
        prefix[4..36].swap_with_slice(&mut suffix[..32]);

        assert_eq!(
            QrVerificationPayload::new(identity, identity).unwrap_err(),
            QrVerificationError::SameIdentity
        );
        assert_eq!(
            QrVerificationPayload::decode(&noncanonical).unwrap_err(),
            QrVerificationError::NonCanonicalEncoding
        );
        assert_eq!(
            QrVerificationPayload::decode(&invalid_identity).unwrap_err(),
            QrVerificationError::InvalidIdentity
        );
        assert_eq!(
            QrVerificationPayload::decode(&[0x9f, 1, 0x40, 0x40, 0xff]).unwrap_err(),
            QrVerificationError::InvalidShape
        );
        assert_eq!(
            QrVerificationPayload::decode(&[0; QR_VERIFICATION_PAYLOAD_BYTES + 1]).unwrap_err(),
            QrVerificationError::PayloadTooLarge
        );
    }
}
