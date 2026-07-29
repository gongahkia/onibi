use arachne_core::IdentityPublicKey;
use sha2::{Digest, Sha256};

use crate::CryptoDomain;

pub const IDENTITY_IDENTIFIER_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct IdentityIdentifier([u8; IDENTITY_IDENTIFIER_BYTES]);

impl IdentityIdentifier {
    #[must_use]
    pub fn derive(public_key: &IdentityPublicKey) -> Self {
        let mut hasher = Sha256::new();
        hasher.update(CryptoDomain::IdentityIdentifier.context());
        hasher.update(public_key.as_bytes());
        let mut identifier = [0; IDENTITY_IDENTIFIER_BYTES];
        identifier.copy_from_slice(&hasher.finalize());
        Self(identifier)
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; IDENTITY_IDENTIFIER_BYTES] {
        &self.0
    }
}

#[cfg(test)]
mod tests {
    use arachne_core::IdentityPublicKey;
    use sha2::{Digest, Sha256};

    use super::IdentityIdentifier;
    #[test]
    fn derives_domain_separated_self_certifying_identifier() {
        let public_key = IdentityPublicKey::from_bytes([
            0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64,
            0x07, 0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68,
            0xf7, 0x07, 0x51, 0x1a,
        ])
        .unwrap();
        let identifier = IdentityIdentifier::derive(&public_key);
        let mut unscoped_hasher = Sha256::new();
        unscoped_hasher.update(public_key.as_bytes());

        assert_eq!(
            identifier.as_bytes(),
            &[
                0x5a, 0xc9, 0xa6, 0xc5, 0x42, 0x4c, 0xe1, 0xb1, 0x84, 0xa0, 0x04, 0x26, 0xc0, 0x32,
                0x2c, 0xb4, 0x4f, 0x0b, 0xb8, 0x08, 0x4c, 0x0a, 0xc3, 0x2c, 0x76, 0x5a, 0xa2, 0x3c,
                0x37, 0xfa, 0x6d, 0xb0,
            ]
        );
        assert_ne!(identifier.as_bytes(), &unscoped_hasher.finalize());
        assert_eq!(identifier, IdentityIdentifier::derive(&public_key));
    }
}
