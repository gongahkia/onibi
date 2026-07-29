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
                0xfb, 0xcc, 0x7b, 0xd5, 0x9b, 0x35, 0xde, 0x83, 0xc8, 0xea, 0x6d, 0x3f, 0xf0, 0x94,
                0x46, 0x3c, 0xda, 0x5c, 0x96, 0x2f, 0x1e, 0x71, 0xc9, 0xd6, 0xcd, 0xbe, 0xf0, 0x1f,
                0xc3, 0x61, 0x78, 0xae,
            ]
        );
        assert_ne!(identifier.as_bytes(), &unscoped_hasher.finalize());
        assert_eq!(identifier, IdentityIdentifier::derive(&public_key));
    }
}
