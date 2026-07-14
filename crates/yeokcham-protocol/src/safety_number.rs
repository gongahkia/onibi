use sha2::{Digest, Sha256};
use yeokcham_core::IdentityPublicKey;

use crate::CryptoDomain;

pub const SAFETY_NUMBER_FINGERPRINT_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SafetyNumberFingerprint([u8; SAFETY_NUMBER_FINGERPRINT_BYTES]);

impl SafetyNumberFingerprint {
    pub fn derive(
        first: &IdentityPublicKey,
        second: &IdentityPublicKey,
    ) -> Result<Self, SafetyNumberError> {
        if first == second {
            return Err(SafetyNumberError::SameIdentity);
        }
        let (first, second) = if first.as_bytes() < second.as_bytes() {
            (first, second)
        } else {
            (second, first)
        };
        let mut hasher = Sha256::new();
        hasher.update(CryptoDomain::SafetyNumber.context());
        hasher.update(first.as_bytes());
        hasher.update(second.as_bytes());
        let mut fingerprint = [0; SAFETY_NUMBER_FINGERPRINT_BYTES];
        fingerprint.copy_from_slice(&hasher.finalize());
        Ok(Self(fingerprint))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; SAFETY_NUMBER_FINGERPRINT_BYTES] {
        &self.0
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SafetyNumberError {
    #[error("safety-number identities must be distinct")]
    SameIdentity,
}

#[cfg(test)]
mod tests {
    use sha2::{Digest, Sha256};
    use yeokcham_core::IdentityPublicKey;

    use super::{SafetyNumberError, SafetyNumberFingerprint};

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
    fn derives_order_independent_domain_separated_fingerprints() {
        let first = first_identity();
        let second = second_identity();
        let fingerprint = SafetyNumberFingerprint::derive(&first, &second).unwrap();
        let mut unscoped_hasher = Sha256::new();
        unscoped_hasher.update(second.as_bytes());
        unscoped_hasher.update(first.as_bytes());

        assert_eq!(
            fingerprint.as_bytes(),
            &[
                0x4c, 0xef, 0x6c, 0x6d, 0x68, 0x3e, 0xb8, 0xef, 0x53, 0x4f, 0x09, 0x7f, 0x9f, 0x83,
                0xf9, 0x36, 0x45, 0xe3, 0x6a, 0x77, 0x9e, 0xae, 0x2e, 0xe9, 0xc4, 0x71, 0x41, 0xee,
                0x83, 0xe7, 0x32, 0x06,
            ]
        );
        assert_eq!(
            fingerprint,
            SafetyNumberFingerprint::derive(&second, &first).unwrap()
        );
        assert_ne!(fingerprint.as_bytes(), &unscoped_hasher.finalize());
    }

    #[test]
    fn rejects_same_identity() {
        let identity = first_identity();

        assert_eq!(
            SafetyNumberFingerprint::derive(&identity, &identity).unwrap_err(),
            SafetyNumberError::SameIdentity
        );
    }
}
