use std::fmt;

use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use getrandom::{SysRng, rand_core::TryRng};
use zeroize::Zeroize;

use crate::{ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayPublicKey([u8; ED25519_PUBLIC_KEY_BYTES]);

impl RelayPublicKey {
    pub fn from_bytes(bytes: [u8; ED25519_PUBLIC_KEY_BYTES]) -> Result<Self, RelayPublicKeyError> {
        let verifying_key =
            VerifyingKey::from_bytes(&bytes).map_err(|_| RelayPublicKeyError::Malformed)?;
        if verifying_key.is_weak() {
            return Err(RelayPublicKeyError::Weak);
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
    ) -> Result<(), RelaySignatureError> {
        let verifying_key =
            VerifyingKey::from_bytes(&self.0).map_err(|_| RelaySignatureError::Invalid)?;
        verifying_key
            .verify_strict(message, &Signature::from_bytes(signature))
            .map_err(|_| RelaySignatureError::Invalid)
    }
}

pub struct RelaySigningKeypair {
    signing_key: SigningKey,
}

impl RelaySigningKeypair {
    pub fn generate() -> Result<Self, RelayIdentityKeyError> {
        let mut random_source = SysRng;
        let mut secret_key = [0; ED25519_PUBLIC_KEY_BYTES];
        if random_source.try_fill_bytes(&mut secret_key).is_err() {
            secret_key.zeroize();
            return Err(RelayIdentityKeyError::Randomness);
        }
        let signing_key = SigningKey::from_bytes(&secret_key);
        secret_key.zeroize();
        Ok(Self { signing_key })
    }

    #[must_use]
    pub fn public_key(&self) -> RelayPublicKey {
        RelayPublicKey(self.signing_key.verifying_key().to_bytes())
    }

    #[must_use]
    pub fn sign(&self, message: &[u8]) -> [u8; ED25519_SIGNATURE_BYTES] {
        self.signing_key.sign(message).to_bytes()
    }
}

impl fmt::Debug for RelaySigningKeypair {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("RelaySigningKeypair")
            .field("public_key", &self.public_key())
            .field("signing_key", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayIdentityKeyError {
    #[error("operating-system random source failed")]
    Randomness,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayPublicKeyError {
    #[error("relay Ed25519 public key encoding is malformed")]
    Malformed,
    #[error("relay Ed25519 public key is weak")]
    Weak,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelaySignatureError {
    #[error("relay Ed25519 signature verification failed")]
    Invalid,
}

#[cfg(test)]
mod tests {
    use super::{RelayPublicKey, RelayPublicKeyError, RelaySignatureError, RelaySigningKeypair};

    #[test]
    fn generates_distinct_redacted_relay_signing_identities() {
        let first = RelaySigningKeypair::generate().unwrap();
        let second = RelaySigningKeypair::generate().unwrap();
        let signature = first.sign(b"relay invitation");

        assert_ne!(first.public_key(), second.public_key());
        assert_eq!(
            first.public_key().verify(b"relay invitation", &signature),
            Ok(())
        );
        assert!(format!("{first:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_invalid_relay_public_keys_and_signatures() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let mut signature = relay.sign(b"relay invitation");
        signature[0] ^= 1;

        assert_eq!(
            RelayPublicKey::from_bytes([0; 32]).unwrap_err(),
            RelayPublicKeyError::Weak
        );
        assert_eq!(
            relay.public_key().verify(b"relay invitation", &signature),
            Err(RelaySignatureError::Invalid)
        );
    }
}
