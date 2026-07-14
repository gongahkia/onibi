use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::{Zeroize, Zeroizing};

use crate::{X25519KeyAgreementError, x25519_identity::shared_secret};

pub const X25519_KEY_BYTES: usize = 32;
pub const X25519_PREKEY_SERIALIZATION_VERSION: u8 = 1;
pub const X25519_PREKEY_SERIALIZED_BYTES: usize = 1 + X25519_KEY_BYTES;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct X25519PrekeyPublicKey([u8; X25519_KEY_BYTES]);

impl X25519PrekeyPublicKey {
    pub fn from_bytes(bytes: [u8; X25519_KEY_BYTES]) -> Result<Self, X25519PrekeyPublicKeyError> {
        let public_key = PublicKey::from(bytes);
        let verifier = StaticSecret::from([1; X25519_KEY_BYTES]);
        if !verifier.diffie_hellman(&public_key).was_contributory() {
            return Err(X25519PrekeyPublicKeyError::Weak);
        }
        Ok(Self(bytes))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; X25519_KEY_BYTES] {
        &self.0
    }
}

pub struct X25519Prekey {
    secret: StaticSecret,
}

impl X25519Prekey {
    pub fn generate() -> Result<Self, X25519PrekeyError> {
        let mut random_source = SysRng;
        let mut secret = [0; X25519_KEY_BYTES];
        if random_source.try_fill_bytes(&mut secret).is_err() {
            secret.zeroize();
            return Err(X25519PrekeyError::Randomness);
        }
        if secret.iter().all(|byte| *byte == 0) {
            secret.zeroize();
            return Err(X25519PrekeyError::Randomness);
        }
        let secret_key = StaticSecret::from(secret);
        secret.zeroize();
        Ok(Self { secret: secret_key })
    }

    #[must_use]
    pub fn public_key(&self) -> X25519PrekeyPublicKey {
        X25519PrekeyPublicKey(PublicKey::from(&self.secret).to_bytes())
    }

    pub fn shared_secret(
        &self,
        peer: &[u8; X25519_KEY_BYTES],
    ) -> Result<Zeroizing<[u8; X25519_KEY_BYTES]>, X25519KeyAgreementError> {
        shared_secret(&self.secret, peer)
    }

    #[must_use]
    pub fn serialize(&self) -> Zeroizing<[u8; X25519_PREKEY_SERIALIZED_BYTES]> {
        let mut secret = self.secret.to_bytes();
        let mut serialized = Zeroizing::new([0; X25519_PREKEY_SERIALIZED_BYTES]);
        serialized[0] = X25519_PREKEY_SERIALIZATION_VERSION;
        serialized[1..].copy_from_slice(&secret);
        secret.zeroize();
        serialized
    }

    pub fn deserialize(encoded: &[u8]) -> Result<Self, X25519PrekeySerializationError> {
        if encoded.len() != X25519_PREKEY_SERIALIZED_BYTES {
            return Err(X25519PrekeySerializationError::InvalidLength);
        }
        if encoded[0] != X25519_PREKEY_SERIALIZATION_VERSION {
            return Err(X25519PrekeySerializationError::UnsupportedVersion(
                encoded[0],
            ));
        }
        let mut secret = [0; X25519_KEY_BYTES];
        secret.copy_from_slice(&encoded[1..]);
        if secret.iter().all(|byte| *byte == 0) {
            secret.zeroize();
            return Err(X25519PrekeySerializationError::WeakSecret);
        }
        let prekey = Self {
            secret: StaticSecret::from(secret),
        };
        secret.zeroize();
        Ok(prekey)
    }
}

impl fmt::Debug for X25519Prekey {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("X25519Prekey")
            .field("public_key", &self.public_key())
            .field("secret", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519PrekeyError {
    #[error("operating-system random source failed")]
    Randomness,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519PrekeyPublicKeyError {
    #[error("X25519 public key is weak")]
    Weak,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519PrekeySerializationError {
    #[error("X25519 prekey serialization has an invalid length")]
    InvalidLength,
    #[error("unsupported X25519 prekey serialization version: {0}")]
    UnsupportedVersion(u8),
    #[error("X25519 prekey serialization contains a weak private key")]
    WeakSecret,
}

#[cfg(test)]
mod tests {
    use x25519_dalek::PublicKey;

    use super::{
        X25519_PREKEY_SERIALIZATION_VERSION, X25519Prekey, X25519PrekeyPublicKey,
        X25519PrekeyPublicKeyError, X25519PrekeySerializationError,
    };

    #[test]
    fn generates_distinct_prekeys_with_contributory_shared_secrets_and_redacts_secrets() {
        let first = X25519Prekey::generate().unwrap();
        let second = X25519Prekey::generate().unwrap();
        let first_public = PublicKey::from(*first.public_key().as_bytes());
        let second_public = PublicKey::from(*second.public_key().as_bytes());
        let first_shared = first.secret.diffie_hellman(&second_public);
        let second_shared = second.secret.diffie_hellman(&first_public);

        assert_ne!(first.public_key(), second.public_key());
        assert_eq!(first_shared.as_bytes(), second_shared.as_bytes());
        assert!(first_shared.was_contributory());
        let output = format!("{first:?}");
        assert!(output.contains("secret: \"REDACTED\""));
        assert!(!output.contains("StaticSecret"));
    }

    #[test]
    fn serializes_and_validates_private_prekeys() {
        let prekey = X25519Prekey::generate().unwrap();
        let serialized = prekey.serialize();
        let restored = X25519Prekey::deserialize(serialized.as_ref()).unwrap();

        assert_eq!(restored.public_key(), prekey.public_key());
        assert_eq!(serialized[0], X25519_PREKEY_SERIALIZATION_VERSION);
        assert_eq!(
            X25519Prekey::deserialize(&serialized[..serialized.len() - 1]).unwrap_err(),
            X25519PrekeySerializationError::InvalidLength
        );
        let mut unsupported = *serialized;
        unsupported[0] = X25519_PREKEY_SERIALIZATION_VERSION + 1;
        assert_eq!(
            X25519Prekey::deserialize(&unsupported).unwrap_err(),
            X25519PrekeySerializationError::UnsupportedVersion(2)
        );
        let mut weak = *serialized;
        weak[1..].fill(0);
        assert_eq!(
            X25519Prekey::deserialize(&weak).unwrap_err(),
            X25519PrekeySerializationError::WeakSecret
        );
    }

    #[test]
    fn validates_contributory_public_prekeys() {
        let public = X25519Prekey::generate().unwrap().public_key();

        assert_eq!(
            X25519PrekeyPublicKey::from_bytes(*public.as_bytes()).unwrap(),
            public
        );
        assert_eq!(
            X25519PrekeyPublicKey::from_bytes([0; 32]).unwrap_err(),
            X25519PrekeyPublicKeyError::Weak
        );
    }
}
