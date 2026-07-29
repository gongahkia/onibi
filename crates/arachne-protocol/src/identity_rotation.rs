use arachne_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey,
};
use minicbor::{Decoder, Encoder};

use crate::CryptoDomain;

pub const IDENTITY_ROTATION_SCHEMA_VERSION: u8 = 1;
pub const IDENTITY_ROTATION_BYTES: usize = 136;
const ROTATION_FIELDS: u64 = 4;
const ROTATION_SIGNING_FIELDS: u64 = 3;
const ROTATION_SIGNING_INPUT_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct IdentityRotation {
    previous: IdentityPublicKey,
    replacement: IdentityPublicKey,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl IdentityRotation {
    pub fn create(
        previous: &IdentityKeypair,
        replacement: IdentityPublicKey,
    ) -> Result<Self, IdentityRotationError> {
        let previous_public = previous.public_key();
        if previous_public == replacement {
            return Err(IdentityRotationError::UnchangedIdentity);
        }
        let signing_input = signing_input(&previous_public, &replacement)?;
        Ok(Self {
            previous: previous_public,
            replacement,
            signature: previous.sign(&signing_input),
        })
    }

    #[must_use]
    pub const fn previous(&self) -> &IdentityPublicKey {
        &self.previous
    }

    #[must_use]
    pub const fn replacement(&self) -> &IdentityPublicKey {
        &self.replacement
    }

    pub fn encode(&self) -> Result<Vec<u8>, IdentityRotationError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ROTATION_FIELDS)
            .map_err(|_| IdentityRotationError::Encode)?
            .u8(IDENTITY_ROTATION_SCHEMA_VERSION)
            .map_err(|_| IdentityRotationError::Encode)?
            .bytes(self.previous.as_bytes())
            .map_err(|_| IdentityRotationError::Encode)?
            .bytes(self.replacement.as_bytes())
            .map_err(|_| IdentityRotationError::Encode)?
            .bytes(&self.signature)
            .map_err(|_| IdentityRotationError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() != IDENTITY_ROTATION_BYTES {
            return Err(IdentityRotationError::Encode);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, IdentityRotationError> {
        if encoded.len() > IDENTITY_ROTATION_BYTES {
            return Err(IdentityRotationError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| IdentityRotationError::Decode)? != Some(ROTATION_FIELDS) {
            return Err(IdentityRotationError::InvalidShape);
        }
        if decoder.u8().map_err(|_| IdentityRotationError::Decode)?
            != IDENTITY_ROTATION_SCHEMA_VERSION
        {
            return Err(IdentityRotationError::UnsupportedSchemaVersion);
        }
        let previous =
            decode_identity(decoder.bytes().map_err(|_| IdentityRotationError::Decode)?)?;
        let replacement =
            decode_identity(decoder.bytes().map_err(|_| IdentityRotationError::Decode)?)?;
        if previous == replacement {
            return Err(IdentityRotationError::UnchangedIdentity);
        }
        let signature =
            decode_signature(decoder.bytes().map_err(|_| IdentityRotationError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(IdentityRotationError::TrailingBytes);
        }
        let rotation = Self {
            previous,
            replacement,
            signature,
        };
        let signing_input = signing_input(&rotation.previous, &rotation.replacement)?;
        rotation
            .previous
            .verify(&signing_input, &rotation.signature)
            .map_err(|_| IdentityRotationError::InvalidSignature)?;
        if rotation.encode()? != encoded {
            return Err(IdentityRotationError::NonCanonicalEncoding);
        }
        Ok(rotation)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentityRotationError {
    #[error("identity rotation encoding failed")]
    Encode,
    #[error("identity rotation decoding failed")]
    Decode,
    #[error("identity rotation has an invalid shape")]
    InvalidShape,
    #[error("identity rotation schema version is unsupported")]
    UnsupportedSchemaVersion,
    #[error("identity rotation contains an invalid identity")]
    InvalidIdentity,
    #[error("identity rotation does not change the identity")]
    UnchangedIdentity,
    #[error("identity rotation exceeds the configured limit")]
    PayloadTooLarge,
    #[error("identity rotation signature is invalid")]
    InvalidSignature,
    #[error("identity rotation has trailing bytes")]
    TrailingBytes,
    #[error("identity rotation is not canonically encoded")]
    NonCanonicalEncoding,
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, IdentityRotationError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| IdentityRotationError::InvalidIdentity)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| IdentityRotationError::InvalidIdentity)
}

fn decode_signature(
    encoded: &[u8],
) -> Result<[u8; ED25519_SIGNATURE_BYTES], IdentityRotationError> {
    encoded
        .try_into()
        .map_err(|_| IdentityRotationError::InvalidSignature)
}

fn signing_input(
    previous: &IdentityPublicKey,
    replacement: &IdentityPublicKey,
) -> Result<Vec<u8>, IdentityRotationError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(ROTATION_SIGNING_FIELDS)
        .map_err(|_| IdentityRotationError::Encode)?
        .u8(IDENTITY_ROTATION_SCHEMA_VERSION)
        .map_err(|_| IdentityRotationError::Encode)?
        .bytes(previous.as_bytes())
        .map_err(|_| IdentityRotationError::Encode)?
        .bytes(replacement.as_bytes())
        .map_err(|_| IdentityRotationError::Encode)?;
    let unsigned = encoder.into_writer();
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(ROTATION_SIGNING_INPUT_FIELDS)
        .map_err(|_| IdentityRotationError::Encode)?
        .bytes(CryptoDomain::IdentityRotationSignature.context())
        .map_err(|_| IdentityRotationError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| IdentityRotationError::Encode)?;
    Ok(encoder.into_writer())
}

#[cfg(test)]
mod tests {
    use super::{IDENTITY_ROTATION_BYTES, IdentityRotation, IdentityRotationError};
    use arachne_core::IdentityKeypair;

    #[test]
    fn signs_and_validates_canonical_identity_rotations() {
        let previous = IdentityKeypair::generate().unwrap();
        let replacement = IdentityKeypair::generate().unwrap();
        let rotation = IdentityRotation::create(&previous, replacement.public_key()).unwrap();
        let encoded = rotation.encode().unwrap();

        assert_eq!(encoded.len(), IDENTITY_ROTATION_BYTES);
        assert_eq!(IdentityRotation::decode(&encoded).unwrap(), rotation);
        assert_eq!(rotation.previous(), &previous.public_key());
        assert_eq!(rotation.replacement(), &replacement.public_key());
    }

    #[test]
    fn rejects_tampered_or_unchanged_identity_rotations() {
        let previous = IdentityKeypair::generate().unwrap();
        let replacement = IdentityKeypair::generate().unwrap();
        let rotation = IdentityRotation::create(&previous, replacement.public_key()).unwrap();
        let mut encoded = rotation.encode().unwrap();
        let last = encoded.len() - 1;
        encoded[last] ^= 1;

        assert_eq!(
            IdentityRotation::decode(&encoded).unwrap_err(),
            IdentityRotationError::InvalidSignature
        );
        assert_eq!(
            IdentityRotation::create(&previous, previous.public_key()).unwrap_err(),
            IdentityRotationError::UnchangedIdentity
        );
        assert_eq!(
            IdentityRotation::decode(&[0; IDENTITY_ROTATION_BYTES + 1]).unwrap_err(),
            IdentityRotationError::PayloadTooLarge
        );
    }
}
