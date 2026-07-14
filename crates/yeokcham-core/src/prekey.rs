use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::Zeroize;

pub const X25519_KEY_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct X25519PrekeyPublicKey([u8; X25519_KEY_BYTES]);

impl X25519PrekeyPublicKey {
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
        let secret_key = StaticSecret::from(secret);
        secret.zeroize();
        Ok(Self { secret: secret_key })
    }

    #[must_use]
    pub fn public_key(&self) -> X25519PrekeyPublicKey {
        X25519PrekeyPublicKey(PublicKey::from(&self.secret).to_bytes())
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

#[cfg(test)]
mod tests {
    use x25519_dalek::PublicKey;

    use super::X25519Prekey;

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
}
