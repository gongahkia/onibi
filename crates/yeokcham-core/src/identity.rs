use std::fmt;

use ed25519_dalek::{SigningKey, VerifyingKey};
use getrandom::{SysRng, rand_core::TryRng};
use zeroize::Zeroize;

pub const ED25519_PUBLIC_KEY_BYTES: usize = 32;

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

#[cfg(test)]
mod tests {
    use ed25519_dalek::{Signer, Verifier};

    use super::{IdentityKeypair, IdentityPublicKey, IdentityPublicKeyError};

    #[test]
    fn generates_verified_distinct_keypairs_and_redacts_secrets() {
        let first = IdentityKeypair::generate().unwrap();
        let second = IdentityKeypair::generate().unwrap();
        let message = b"yeokcham identity key generation";
        let signature = first.signing_key.sign(message);

        assert_ne!(first.public_key(), second.public_key());
        assert!(
            first
                .signing_key
                .verifying_key()
                .verify(message, &signature)
                .is_ok()
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
}
