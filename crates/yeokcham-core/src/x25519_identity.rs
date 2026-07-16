use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::{Zeroize, Zeroizing};

use crate::X25519_KEY_BYTES;

pub const X25519_IDENTITY_SERIALIZATION_VERSION: u8 = 1;
pub const X25519_IDENTITY_SERIALIZED_BYTES: usize = 1 + X25519_KEY_BYTES;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct X25519IdentityPublicKey([u8; X25519_KEY_BYTES]);

impl X25519IdentityPublicKey {
    pub fn from_bytes(bytes: [u8; X25519_KEY_BYTES]) -> Result<Self, X25519IdentityPublicKeyError> {
        validate_public_key(&bytes).map_err(|_| X25519IdentityPublicKeyError::Weak)?;
        Ok(Self(bytes))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; X25519_KEY_BYTES] {
        &self.0
    }
}

pub struct X25519IdentityKeypair {
    secret: StaticSecret,
}

impl X25519IdentityKeypair {
    pub fn generate() -> Result<Self, X25519IdentityKeyError> {
        let mut random_source = SysRng;
        let mut secret = [0; X25519_KEY_BYTES];
        if random_source.try_fill_bytes(&mut secret).is_err() {
            secret.zeroize();
            return Err(X25519IdentityKeyError::Randomness);
        }
        if secret.iter().all(|byte| *byte == 0) {
            secret.zeroize();
            return Err(X25519IdentityKeyError::Randomness);
        }
        let secret_key = StaticSecret::from(secret);
        secret.zeroize();
        Ok(Self { secret: secret_key })
    }

    #[must_use]
    pub fn public_key(&self) -> X25519IdentityPublicKey {
        X25519IdentityPublicKey(PublicKey::from(&self.secret).to_bytes())
    }

    pub fn shared_secret(
        &self,
        peer: &[u8; X25519_KEY_BYTES],
    ) -> Result<Zeroizing<[u8; X25519_KEY_BYTES]>, X25519KeyAgreementError> {
        shared_secret(&self.secret, peer)
    }

    #[must_use]
    pub fn serialize(&self) -> Zeroizing<[u8; X25519_IDENTITY_SERIALIZED_BYTES]> {
        let mut secret = self.secret.to_bytes();
        let mut serialized = Zeroizing::new([0; X25519_IDENTITY_SERIALIZED_BYTES]);
        serialized[0] = X25519_IDENTITY_SERIALIZATION_VERSION;
        serialized[1..].copy_from_slice(&secret);
        secret.zeroize();
        serialized
    }

    pub fn deserialize(encoded: &[u8]) -> Result<Self, X25519IdentitySerializationError> {
        if encoded.len() != X25519_IDENTITY_SERIALIZED_BYTES {
            return Err(X25519IdentitySerializationError::InvalidLength);
        }
        if encoded[0] != X25519_IDENTITY_SERIALIZATION_VERSION {
            return Err(X25519IdentitySerializationError::UnsupportedVersion(
                encoded[0],
            ));
        }
        let mut secret = [0; X25519_KEY_BYTES];
        secret.copy_from_slice(&encoded[1..]);
        if secret.iter().all(|byte| *byte == 0) {
            secret.zeroize();
            return Err(X25519IdentitySerializationError::WeakSecret);
        }
        let keypair = Self {
            secret: StaticSecret::from(secret),
        };
        secret.zeroize();
        Ok(keypair)
    }
}

impl fmt::Debug for X25519IdentityKeypair {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("X25519IdentityKeypair")
            .field("public_key", &self.public_key())
            .field("secret", &"REDACTED")
            .finish()
    }
}

#[allow(clippy::redundant_pub_crate)]
pub(crate) fn shared_secret(
    secret: &StaticSecret,
    peer: &[u8; X25519_KEY_BYTES],
) -> Result<Zeroizing<[u8; X25519_KEY_BYTES]>, X25519KeyAgreementError> {
    validate_public_key(peer)?;
    let shared = secret.diffie_hellman(&PublicKey::from(*peer));
    if !shared.was_contributory() {
        return Err(X25519KeyAgreementError::NonContributory);
    }
    Ok(Zeroizing::new(shared.to_bytes()))
}

fn validate_public_key(peer: &[u8; X25519_KEY_BYTES]) -> Result<(), X25519KeyAgreementError> {
    let verifier = StaticSecret::from([1; X25519_KEY_BYTES]);
    if !verifier
        .diffie_hellman(&PublicKey::from(*peer))
        .was_contributory()
    {
        return Err(X25519KeyAgreementError::WeakPeer);
    }
    Ok(())
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519IdentityKeyError {
    #[error("operating-system random source failed")]
    Randomness,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519IdentityPublicKeyError {
    #[error("X25519 identity public key is weak")]
    Weak,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519IdentitySerializationError {
    #[error("X25519 identity serialization has an invalid length")]
    InvalidLength,
    #[error("unsupported X25519 identity serialization version: {0}")]
    UnsupportedVersion(u8),
    #[error("X25519 identity serialization contains a weak private key")]
    WeakSecret,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum X25519KeyAgreementError {
    #[error("X25519 peer public key is weak")]
    WeakPeer,
    #[error("X25519 shared secret is non-contributory")]
    NonContributory,
}

#[cfg(test)]
mod tests {
    use super::{
        X25519_IDENTITY_SERIALIZATION_VERSION, X25519IdentityKeypair, X25519IdentityPublicKey,
        X25519IdentityPublicKeyError, X25519IdentitySerializationError, X25519KeyAgreementError,
    };

    #[test]
    fn generates_serializes_and_redacts_independent_identity_keys() {
        let first = X25519IdentityKeypair::generate().unwrap();
        let second = X25519IdentityKeypair::generate().unwrap();
        let serialized = first.serialize();
        let restored = X25519IdentityKeypair::deserialize(&*serialized).unwrap();

        assert_ne!(first.public_key(), second.public_key());
        assert_eq!(serialized[0], X25519_IDENTITY_SERIALIZATION_VERSION);
        assert_eq!(restored.public_key(), first.public_key());
        assert_eq!(
            first.shared_secret(second.public_key().as_bytes()).unwrap(),
            second.shared_secret(first.public_key().as_bytes()).unwrap()
        );
        assert!(format!("{first:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_weak_identity_key_material_and_peers() {
        let identity = X25519IdentityKeypair::generate().unwrap();
        let mut encoded = *identity.serialize();
        encoded[0] = X25519_IDENTITY_SERIALIZATION_VERSION + 1;

        assert_eq!(
            X25519IdentityPublicKey::from_bytes([0; 32]).unwrap_err(),
            X25519IdentityPublicKeyError::Weak
        );
        assert_eq!(
            identity.shared_secret(&[0; 32]).unwrap_err(),
            X25519KeyAgreementError::WeakPeer
        );
        assert_eq!(
            X25519IdentityKeypair::deserialize(&encoded).unwrap_err(),
            X25519IdentitySerializationError::UnsupportedVersion(2)
        );
    }
}
