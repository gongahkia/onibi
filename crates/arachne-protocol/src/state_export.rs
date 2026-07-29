use argon2::{Algorithm, AssociatedData, ParamsBuilder, Version};
use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use zeroize::Zeroizing;

use crate::{CryptoDomain, IdentityExportPassphrase};

pub const STATE_EXPORT_FORMAT_VERSION: u8 = 1;
pub const STATE_EXPORT_SALT_BYTES: usize = 16;
pub const MAX_STATE_EXPORT_PLAINTEXT_BYTES: usize = 4 * 1024 * 1024;
pub const MAX_STATE_EXPORT_BYTES: usize =
    STATE_EXPORT_HEADER_BYTES + MAX_STATE_EXPORT_PLAINTEXT_BYTES + STATE_EXPORT_TAG_BYTES;
const STATE_EXPORT_NONCE_BYTES: usize = 24;
const STATE_EXPORT_KEY_BYTES: usize = 32;
const STATE_EXPORT_TAG_BYTES: usize = 16;
const STATE_EXPORT_HEADER_BYTES: usize = 1 + STATE_EXPORT_SALT_BYTES + STATE_EXPORT_NONCE_BYTES;
const ARGON2_MEMORY_KIB: u32 = 64 * 1024;
const ARGON2_ITERATIONS: u32 = 3;
const ARGON2_PARALLELISM: u32 = 4;

pub fn export_state(
    plaintext: &[u8],
    passphrase: &IdentityExportPassphrase,
) -> Result<Vec<u8>, StateExportError> {
    validate_plaintext(plaintext)?;
    let mut salt = [0; STATE_EXPORT_SALT_BYTES];
    let mut nonce = [0; STATE_EXPORT_NONCE_BYTES];
    fill_random(&mut salt)?;
    fill_random(&mut nonce)?;

    let key = derive_key(passphrase, &salt)?;
    let cipher = XChaCha20Poly1305::new_from_slice(key.as_ref())
        .map_err(|_| StateExportError::Encryption)?;
    let associated_data = associated_data();
    let nonce = XNonce::try_from(nonce.as_slice()).map_err(|_| StateExportError::Encryption)?;
    let ciphertext = cipher
        .encrypt(
            &nonce,
            Payload {
                msg: plaintext,
                aad: &associated_data,
            },
        )
        .map_err(|_| StateExportError::Encryption)?;
    if ciphertext.len() != plaintext.len() + STATE_EXPORT_TAG_BYTES {
        return Err(StateExportError::Encryption);
    }

    let mut encoded = Vec::with_capacity(STATE_EXPORT_HEADER_BYTES + ciphertext.len());
    encoded.push(STATE_EXPORT_FORMAT_VERSION);
    encoded.extend_from_slice(&salt);
    encoded.extend_from_slice(nonce.as_slice());
    encoded.extend_from_slice(&ciphertext);
    Ok(encoded)
}

pub fn import_state(
    encoded: &[u8],
    passphrase: &IdentityExportPassphrase,
) -> Result<Zeroizing<Vec<u8>>, StateExportError> {
    if !(STATE_EXPORT_HEADER_BYTES + STATE_EXPORT_TAG_BYTES..=MAX_STATE_EXPORT_BYTES)
        .contains(&encoded.len())
    {
        return Err(StateExportError::InvalidLength);
    }
    if encoded[0] != STATE_EXPORT_FORMAT_VERSION {
        return Err(StateExportError::UnsupportedVersion(encoded[0]));
    }
    let salt = &encoded[1..=STATE_EXPORT_SALT_BYTES];
    let nonce = &encoded[1 + STATE_EXPORT_SALT_BYTES..STATE_EXPORT_HEADER_BYTES];
    let ciphertext = &encoded[STATE_EXPORT_HEADER_BYTES..];
    let key = derive_key(passphrase, salt)?;
    let cipher = XChaCha20Poly1305::new_from_slice(key.as_ref())
        .map_err(|_| StateExportError::Authentication)?;
    let associated_data = associated_data();
    let nonce = XNonce::try_from(nonce).map_err(|_| StateExportError::Authentication)?;
    let plaintext = Zeroizing::new(
        cipher
            .decrypt(
                &nonce,
                Payload {
                    msg: ciphertext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| StateExportError::Authentication)?,
    );
    validate_plaintext(&plaintext)?;
    Ok(plaintext)
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum StateExportError {
    #[error("state export must not be empty")]
    EmptyState,
    #[error("state export plaintext exceeds the configured limit")]
    StateTooLarge,
    #[error("operating-system random source failed")]
    Randomness,
    #[error("state export key derivation failed")]
    KeyDerivation,
    #[error("state export encryption failed")]
    Encryption,
    #[error("state export has an invalid length")]
    InvalidLength,
    #[error("unsupported state export format version: {0}")]
    UnsupportedVersion(u8),
    #[error("state export authentication failed")]
    Authentication,
}

fn validate_plaintext(plaintext: &[u8]) -> Result<(), StateExportError> {
    if plaintext.is_empty() {
        return Err(StateExportError::EmptyState);
    }
    if plaintext.len() > MAX_STATE_EXPORT_PLAINTEXT_BYTES {
        return Err(StateExportError::StateTooLarge);
    }
    Ok(())
}

fn fill_random(bytes: &mut [u8]) -> Result<(), StateExportError> {
    let mut random_source = SysRng;
    random_source
        .try_fill_bytes(bytes)
        .map_err(|_| StateExportError::Randomness)
}

fn derive_key(
    passphrase: &IdentityExportPassphrase,
    salt: &[u8],
) -> Result<Zeroizing<[u8; STATE_EXPORT_KEY_BYTES]>, StateExportError> {
    let associated_data = AssociatedData::new(CryptoDomain::StateExportKey.context())
        .map_err(|_| StateExportError::KeyDerivation)?;
    let mut parameters = ParamsBuilder::new();
    parameters
        .m_cost(ARGON2_MEMORY_KIB)
        .t_cost(ARGON2_ITERATIONS)
        .p_cost(ARGON2_PARALLELISM)
        .output_len(STATE_EXPORT_KEY_BYTES)
        .data(associated_data);
    let argon2 = parameters
        .context(Algorithm::Argon2id, Version::V0x13)
        .map_err(|_| StateExportError::KeyDerivation)?;
    let mut key = Zeroizing::new([0; STATE_EXPORT_KEY_BYTES]);
    argon2
        .hash_password_into(passphrase.as_bytes(), salt, key.as_mut())
        .map_err(|_| StateExportError::KeyDerivation)?;
    Ok(key)
}

fn associated_data() -> Vec<u8> {
    let context = CryptoDomain::StateExportKey.context();
    let mut data = Vec::with_capacity(context.len() + 1);
    data.extend_from_slice(context);
    data.push(STATE_EXPORT_FORMAT_VERSION);
    data
}

#[cfg(test)]
mod tests {
    use super::{
        MAX_STATE_EXPORT_BYTES, MAX_STATE_EXPORT_PLAINTEXT_BYTES, STATE_EXPORT_FORMAT_VERSION,
        StateExportError, export_state, import_state,
    };
    use crate::IdentityExportPassphrase;

    fn passphrase() -> IdentityExportPassphrase {
        IdentityExportPassphrase::new(b"state export test passphrase".to_vec()).unwrap()
    }

    #[test]
    fn encrypts_and_restores_state_exports() {
        let passphrase = passphrase();
        let plaintext = b"durable state";
        let first = export_state(plaintext, &passphrase).unwrap();
        let second = export_state(plaintext, &passphrase).unwrap();
        let restored = import_state(&first, &passphrase).unwrap();

        assert_eq!(first[0], STATE_EXPORT_FORMAT_VERSION);
        assert_ne!(first, second);
        assert!(
            !first
                .windows(plaintext.len())
                .any(|window| window == plaintext)
        );
        assert_eq!(restored.as_slice(), plaintext);
    }

    #[test]
    fn fails_closed_for_invalid_state_exports() {
        let passphrase = passphrase();
        let encoded = export_state(b"durable state", &passphrase).unwrap();
        let wrong_passphrase =
            IdentityExportPassphrase::new(b"incorrect passphrase".to_vec()).unwrap();
        let mut tampered = encoded.clone();
        *tampered.last_mut().unwrap() ^= 0x01;
        let mut unsupported = encoded.clone();
        unsupported[0] = STATE_EXPORT_FORMAT_VERSION + 1;

        assert_eq!(
            export_state(&[], &passphrase).unwrap_err(),
            StateExportError::EmptyState
        );
        assert_eq!(
            export_state(&vec![0; MAX_STATE_EXPORT_PLAINTEXT_BYTES + 1], &passphrase).unwrap_err(),
            StateExportError::StateTooLarge
        );
        assert_eq!(
            import_state(&[STATE_EXPORT_FORMAT_VERSION], &passphrase).unwrap_err(),
            StateExportError::InvalidLength
        );
        assert_eq!(
            import_state(
                &vec![STATE_EXPORT_FORMAT_VERSION; MAX_STATE_EXPORT_BYTES + 1],
                &passphrase
            )
            .unwrap_err(),
            StateExportError::InvalidLength
        );
        assert_eq!(
            import_state(&tampered, &passphrase).unwrap_err(),
            StateExportError::Authentication
        );
        assert_eq!(
            import_state(&unsupported, &passphrase).unwrap_err(),
            StateExportError::UnsupportedVersion(STATE_EXPORT_FORMAT_VERSION + 1)
        );
        assert_eq!(
            import_state(&encoded, &wrong_passphrase).unwrap_err(),
            StateExportError::Authentication
        );
    }
}
