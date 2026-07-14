use std::fmt;

use argon2::{Algorithm, AssociatedData, ParamsBuilder, Version};
use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use yeokcham_core::IdentityKeypair;
use zeroize::Zeroizing;

use crate::CryptoDomain;

pub const IDENTITY_EXPORT_FORMAT_VERSION: u8 = 1;
pub const IDENTITY_EXPORT_SALT_BYTES: usize = 16;
pub const MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES: usize = 1024;
pub const IDENTITY_EXPORT_BYTES: usize = 122;
const IDENTITY_EXPORT_NONCE_BYTES: usize = 24;
const IDENTITY_EXPORT_KEY_BYTES: usize = 32;
const IDENTITY_EXPORT_TAG_BYTES: usize = 16;
const IDENTITY_EXPORT_HEADER_BYTES: usize =
    1 + IDENTITY_EXPORT_SALT_BYTES + IDENTITY_EXPORT_NONCE_BYTES;
const IDENTITY_EXPORT_CIPHERTEXT_BYTES: usize =
    yeokcham_core::IDENTITY_SERIALIZED_BYTES + IDENTITY_EXPORT_TAG_BYTES;
const ARGON2_MEMORY_KIB: u32 = 64 * 1024;
const ARGON2_ITERATIONS: u32 = 3;
const ARGON2_PARALLELISM: u32 = 4;

pub struct IdentityExportPassphrase(Zeroizing<Vec<u8>>);

impl IdentityExportPassphrase {
    pub fn new(passphrase: Vec<u8>) -> Result<Self, IdentityExportPassphraseError> {
        let passphrase = Zeroizing::new(passphrase);
        if passphrase.is_empty() {
            return Err(IdentityExportPassphraseError::Empty);
        }
        if passphrase.len() > MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES {
            return Err(IdentityExportPassphraseError::TooLong);
        }
        Ok(Self(passphrase))
    }

    fn as_bytes(&self) -> &[u8] {
        &self.0
    }
}

impl fmt::Debug for IdentityExportPassphrase {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("IdentityExportPassphrase(REDACTED)")
    }
}

pub fn export_identity(
    identity: &IdentityKeypair,
    passphrase: &IdentityExportPassphrase,
) -> Result<Vec<u8>, IdentityExportError> {
    let mut salt = [0; IDENTITY_EXPORT_SALT_BYTES];
    let mut nonce = [0; IDENTITY_EXPORT_NONCE_BYTES];
    fill_random(&mut salt)?;
    fill_random(&mut nonce)?;

    let key = derive_key(passphrase, &salt)?;
    let cipher = XChaCha20Poly1305::new_from_slice(key.as_ref())
        .map_err(|_| IdentityExportError::Encryption)?;
    let plaintext = identity.serialize();
    let associated_data = associated_data();
    let nonce = XNonce::try_from(nonce.as_slice()).map_err(|_| IdentityExportError::Encryption)?;
    let ciphertext = cipher
        .encrypt(
            &nonce,
            Payload {
                msg: plaintext.as_ref(),
                aad: &associated_data,
            },
        )
        .map_err(|_| IdentityExportError::Encryption)?;
    if ciphertext.len() != IDENTITY_EXPORT_CIPHERTEXT_BYTES {
        return Err(IdentityExportError::Encryption);
    }

    let mut encoded = Vec::with_capacity(IDENTITY_EXPORT_BYTES);
    encoded.push(IDENTITY_EXPORT_FORMAT_VERSION);
    encoded.extend_from_slice(&salt);
    encoded.extend_from_slice(&nonce);
    encoded.extend_from_slice(&ciphertext);
    debug_assert_eq!(encoded.len(), IDENTITY_EXPORT_BYTES);
    Ok(encoded)
}

pub fn import_identity(
    encoded: &[u8],
    passphrase: &IdentityExportPassphrase,
) -> Result<IdentityKeypair, IdentityExportError> {
    if encoded.len() != IDENTITY_EXPORT_BYTES {
        return Err(IdentityExportError::InvalidLength);
    }
    if encoded[0] != IDENTITY_EXPORT_FORMAT_VERSION {
        return Err(IdentityExportError::UnsupportedVersion(encoded[0]));
    }
    let salt = &encoded[1..=IDENTITY_EXPORT_SALT_BYTES];
    let nonce = &encoded[1 + IDENTITY_EXPORT_SALT_BYTES..IDENTITY_EXPORT_HEADER_BYTES];
    let ciphertext = &encoded[IDENTITY_EXPORT_HEADER_BYTES..];
    let key = derive_key(passphrase, salt)?;
    let cipher = XChaCha20Poly1305::new_from_slice(key.as_ref())
        .map_err(|_| IdentityExportError::Authentication)?;
    let associated_data = associated_data();
    let nonce = XNonce::try_from(nonce).map_err(|_| IdentityExportError::Authentication)?;
    let plaintext = Zeroizing::new(
        cipher
            .decrypt(
                &nonce,
                Payload {
                    msg: ciphertext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| IdentityExportError::Authentication)?,
    );
    IdentityKeypair::deserialize(&plaintext).map_err(|_| IdentityExportError::InvalidIdentity)
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentityExportPassphraseError {
    #[error("identity export passphrase must not be empty")]
    Empty,
    #[error("identity export passphrase exceeds the configured limit")]
    TooLong,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum IdentityExportError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("identity export key derivation failed")]
    KeyDerivation,
    #[error("identity export encryption failed")]
    Encryption,
    #[error("identity export has an invalid length")]
    InvalidLength,
    #[error("unsupported identity export format version: {0}")]
    UnsupportedVersion(u8),
    #[error("identity export authentication failed")]
    Authentication,
    #[error("identity export contains an invalid serialized identity")]
    InvalidIdentity,
}

fn fill_random(bytes: &mut [u8]) -> Result<(), IdentityExportError> {
    let mut random_source = SysRng;
    random_source
        .try_fill_bytes(bytes)
        .map_err(|_| IdentityExportError::Randomness)
}

fn derive_key(
    passphrase: &IdentityExportPassphrase,
    salt: &[u8],
) -> Result<Zeroizing<[u8; IDENTITY_EXPORT_KEY_BYTES]>, IdentityExportError> {
    let associated_data = AssociatedData::new(CryptoDomain::IdentityExportKey.context())
        .map_err(|_| IdentityExportError::KeyDerivation)?;
    let mut parameters = ParamsBuilder::new();
    parameters
        .m_cost(ARGON2_MEMORY_KIB)
        .t_cost(ARGON2_ITERATIONS)
        .p_cost(ARGON2_PARALLELISM)
        .output_len(IDENTITY_EXPORT_KEY_BYTES)
        .data(associated_data);
    let argon2 = parameters
        .context(Algorithm::Argon2id, Version::V0x13)
        .map_err(|_| IdentityExportError::KeyDerivation)?;
    let mut key = Zeroizing::new([0; IDENTITY_EXPORT_KEY_BYTES]);
    argon2
        .hash_password_into(passphrase.as_bytes(), salt, key.as_mut())
        .map_err(|_| IdentityExportError::KeyDerivation)?;
    Ok(key)
}

fn associated_data() -> Vec<u8> {
    let context = CryptoDomain::IdentityExportKey.context();
    let mut data = Vec::with_capacity(context.len() + 1);
    data.extend_from_slice(context);
    data.push(IDENTITY_EXPORT_FORMAT_VERSION);
    data
}

#[cfg(test)]
mod tests {
    use super::{
        IDENTITY_EXPORT_BYTES, IDENTITY_EXPORT_FORMAT_VERSION, IDENTITY_EXPORT_HEADER_BYTES,
        IdentityExportError, IdentityExportPassphrase, IdentityExportPassphraseError,
        MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES, export_identity, import_identity,
    };
    use yeokcham_core::IdentityKeypair;

    fn passphrase() -> IdentityExportPassphrase {
        IdentityExportPassphrase::new(b"identity export test passphrase".to_vec()).unwrap()
    }

    #[test]
    fn encrypts_and_restores_identity_exports() {
        let identity = IdentityKeypair::generate().unwrap();
        let passphrase = passphrase();
        let first = export_identity(&identity, &passphrase).unwrap();
        let second = export_identity(&identity, &passphrase).unwrap();
        let serialized = identity.serialize();
        let restored = import_identity(&first, &passphrase).unwrap();

        assert_eq!(first.len(), IDENTITY_EXPORT_BYTES);
        assert_eq!(first[0], IDENTITY_EXPORT_FORMAT_VERSION);
        assert_ne!(first, second);
        assert_ne!(
            &first[IDENTITY_EXPORT_HEADER_BYTES..IDENTITY_EXPORT_HEADER_BYTES + serialized.len()],
            &*serialized
        );
        assert_eq!(restored.public_key(), identity.public_key());
    }

    #[test]
    fn fails_closed_for_invalid_exports_and_passphrases() {
        let identity = IdentityKeypair::generate().unwrap();
        let passphrase = passphrase();
        let exported = export_identity(&identity, &passphrase).unwrap();
        let wrong_passphrase =
            IdentityExportPassphrase::new(b"incorrect passphrase".to_vec()).unwrap();
        let mut tampered = exported.clone();
        tampered[IDENTITY_EXPORT_HEADER_BYTES] ^= 1;
        let mut unsupported = exported.clone();
        unsupported[0] = IDENTITY_EXPORT_FORMAT_VERSION + 1;

        assert_eq!(
            import_identity(&exported, &wrong_passphrase).unwrap_err(),
            IdentityExportError::Authentication
        );
        assert_eq!(
            import_identity(&tampered, &passphrase).unwrap_err(),
            IdentityExportError::Authentication
        );
        assert_eq!(
            import_identity(&unsupported, &passphrase).unwrap_err(),
            IdentityExportError::UnsupportedVersion(2)
        );
        assert_eq!(
            import_identity(&exported[..exported.len() - 1], &passphrase).unwrap_err(),
            IdentityExportError::InvalidLength
        );
        assert_eq!(
            IdentityExportPassphrase::new(Vec::new()).unwrap_err(),
            IdentityExportPassphraseError::Empty
        );
        assert_eq!(
            IdentityExportPassphrase::new(vec![0; MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES + 1])
                .unwrap_err(),
            IdentityExportPassphraseError::TooLong
        );
        assert_eq!(
            format!("{passphrase:?}"),
            "IdentityExportPassphrase(REDACTED)"
        );
    }
}
