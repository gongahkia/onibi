use std::fmt;

use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use getrandom::{SysRng, rand_core::TryRng};
use zeroize::{Zeroize, Zeroizing};

pub const ED25519_PUBLIC_KEY_BYTES: usize = 32;
pub const ED25519_SIGNATURE_BYTES: usize = 64;
pub const IDENTITY_SERIALIZATION_VERSION: u8 = 1;
pub const IDENTITY_SERIALIZED_BYTES: usize = 1 + ED25519_PUBLIC_KEY_BYTES * 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct IdentityPublicKey([u8; ED25519_PUBLIC_KEY_BYTES]);

impl IdentityPublicKey {
    pub fn from_bytes(
        bytes: [u8; ED25519_PUBLIC_KEY_BYTES],
    ) -> Result<Self, IdentityPublicKeyError> {
        let verifying_key =
            VerifyingKey::from_bytes(&bytes).map_err(|_| IdentityPublicKeyError::Malformed)?;
        if verifying_key.is_weak() {
            return Err(IdentityPublicKeyError::Weak);
        }
        Ok(Self(bytes))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; ED25519_PUBLIC_KEY_BYTES] {
        &self.0
    }

    pub fn verify(
        &self,
        message: &[u8],
        signature: &[u8; ED25519_SIGNATURE_BYTES],
    ) -> Result<(), IdentitySignatureError> {
        let verifying_key =
            VerifyingKey::from_bytes(&self.0).map_err(|_| IdentitySignatureError::Invalid)?;
        verifying_key
            .verify_strict(message, &Signature::from_bytes(signature))
            .map_err(|_| IdentitySignatureError::Invalid)
    }
}

pub struct IdentityKeypair {
    signing_key: SigningKey,
}

impl IdentityKeypair {
    pub fn generate() -> Result<Self, IdentityKeyError> {
        let mut random_source = SysRng;
        let mut secret_key = [0; ED25519_PUBLIC_KEY_BYTES];
        if random_source.try_fill_bytes(&mut secret_key).is_err() {
            secret_key.zeroize();
            return Err(IdentityKeyError::Randomness);
        }
        let signing_key = SigningKey::from_bytes(&secret_key);
        secret_key.zeroize();
        Ok(Self { signing_key })
    }

    #[must_use]
    pub fn public_key(&self) -> IdentityPublicKey {
        IdentityPublicKey(self.signing_key.verifying_key().to_bytes())
    }

    #[must_use]
    pub fn sign(&self, message: &[u8]) -> [u8; ED25519_SIGNATURE_BYTES] {
        self.signing_key.sign(message).to_bytes()
    }

    #[must_use]
    pub fn serialize(&self) -> Zeroizing<[u8; IDENTITY_SERIALIZED_BYTES]> {
        let mut secret_key = self.signing_key.to_bytes();
        let mut serialized = Zeroizing::new([0; IDENTITY_SERIALIZED_BYTES]);
        serialized[0] = IDENTITY_SERIALIZATION_VERSION;
        serialized[1..=ED25519_PUBLIC_KEY_BYTES].copy_from_slice(&secret_key);
        serialized[1 + ED25519_PUBLIC_KEY_BYTES..].copy_from_slice(self.public_key().as_bytes());
        secret_key.zeroize();
        serialized
    }

    pub fn deserialize(encoded: &[u8]) -> Result<Self, IdentitySerializationError> {
        if encoded.len() != IDENTITY_SERIALIZED_BYTES {
            return Err(IdentitySerializationError::InvalidLength);
        }
        if encoded[0] != IDENTITY_SERIALIZATION_VERSION {
            return Err(IdentitySerializationError::UnsupportedVersion(encoded[0]));
        }
        let mut secret_key = [0; ED25519_PUBLIC_KEY_BYTES];
        secret_key.copy_from_slice(&encoded[1..=ED25519_PUBLIC_KEY_BYTES]);
        let signing_key = SigningKey::from_bytes(&secret_key);
        secret_key.zeroize();
        if signing_key.verifying_key().as_bytes() != &encoded[1 + ED25519_PUBLIC_KEY_BYTES..] {
            return Err(IdentitySerializationError::PublicKeyMismatch);
        }
        Ok(Self { signing_key })
    }
}

impl fmt::Debug for IdentityKeypair {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("IdentityKeypair")
            .field("public_key", &self.public_key())
            .field("signing_key", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentityKeyError {
    #[error("operating-system random source failed")]
    Randomness,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentityPublicKeyError {
    #[error("Ed25519 public key encoding is malformed")]
    Malformed,
    #[error("Ed25519 public key is weak")]
    Weak,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentitySignatureError {
    #[error("Ed25519 signature verification failed")]
    Invalid,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentitySerializationError {
    #[error("identity serialization has an invalid length")]
    InvalidLength,
    #[error("unsupported identity serialization version: {0}")]
    UnsupportedVersion(u8),
    #[error("identity serialization public key does not match its private seed")]
    PublicKeyMismatch,
}

#[cfg(test)]
mod tests {
    use ed25519_dalek::{Signer, Verifier};

    use super::{
        ED25519_PUBLIC_KEY_BYTES, IDENTITY_SERIALIZATION_VERSION, IdentityKeypair,
        IdentityPublicKey, IdentityPublicKeyError, IdentitySerializationError,
        IdentitySignatureError,
    };

    #[test]
    fn generates_verified_distinct_keypairs_and_redacts_secrets() {
        let first = IdentityKeypair::generate().unwrap();
        let second = IdentityKeypair::generate().unwrap();
        let message = b"yeokcham identity key generation";
        let signature = first.sign(message);

        assert_ne!(first.public_key(), second.public_key());
        assert_eq!(first.public_key().verify(message, &signature), Ok(()));
        let mut altered_signature = signature;
        altered_signature[0] ^= 1;
        assert_eq!(
            first.public_key().verify(message, &altered_signature),
            Err(IdentitySignatureError::Invalid)
        );
        let output = format!("{first:?}");
        assert!(output.contains("signing_key: \"REDACTED\""));
        assert!(!output.contains("SigningKey"));
    }

    #[test]
    fn validates_public_key_encoding_and_strength() {
        let generated = IdentityKeypair::generate().unwrap();
        assert_eq!(
            IdentityPublicKey::from_bytes(*generated.public_key().as_bytes()).unwrap(),
            generated.public_key()
        );
        let mut weak_key = [0; 32];
        weak_key[0] = 1;
        assert_eq!(
            IdentityPublicKey::from_bytes(weak_key).unwrap_err(),
            IdentityPublicKeyError::Weak
        );
    }

    #[test]
    fn serializes_and_validates_identity_keypairs() {
        let identity = IdentityKeypair::generate().unwrap();
        let serialized = identity.serialize();
        let restored = IdentityKeypair::deserialize(&*serialized).unwrap();
        let message = b"yeokcham identity serialization";
        let signature = restored.signing_key.sign(message);

        assert_eq!(serialized[0], IDENTITY_SERIALIZATION_VERSION);
        assert_eq!(restored.public_key(), identity.public_key());
        assert!(
            identity
                .signing_key
                .verifying_key()
                .verify(message, &signature)
                .is_ok()
        );

        assert_eq!(
            IdentityKeypair::deserialize(&serialized[..serialized.len() - 1]).unwrap_err(),
            IdentitySerializationError::InvalidLength
        );
        let mut unsupported_version = *serialized;
        unsupported_version[0] = IDENTITY_SERIALIZATION_VERSION + 1;
        assert_eq!(
            IdentityKeypair::deserialize(&unsupported_version).unwrap_err(),
            IdentitySerializationError::UnsupportedVersion(2)
        );
        let mut mismatched_public_key = *serialized;
        mismatched_public_key[1 + ED25519_PUBLIC_KEY_BYTES] ^= 1;
        assert_eq!(
            IdentityKeypair::deserialize(&mismatched_public_key).unwrap_err(),
            IdentitySerializationError::PublicKeyMismatch
        );
    }
}
