use std::fmt;

use minicbor::Encoder;
use yeokcham_core::{
    ED25519_SIGNATURE_BYTES, IdentityKeypair, X25519Prekey, X25519PrekeyError,
    X25519PrekeyPublicKey,
};

use crate::CryptoDomain;

pub const SIGNED_PREKEY_SCHEMA_VERSION: u8 = 1;
pub const SIGNED_PREKEY_INITIAL_GENERATION: u64 = 1;
const SIGNED_PREKEY_FIELDS: u64 = 3;
const SIGNED_PREKEY_SIGNING_INPUT_FIELDS: u64 = 2;

pub struct SignedPrekey {
    generation: u64,
    prekey: X25519Prekey,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl SignedPrekey {
    pub fn generate(identity: &IdentityKeypair) -> Result<Self, SignedPrekeyError> {
        Self::with_generation(identity, SIGNED_PREKEY_INITIAL_GENERATION)
    }

    pub fn rotate(&mut self, identity: &IdentityKeypair) -> Result<(), SignedPrekeyError> {
        let generation = next_generation(self.generation)?;
        let replacement = Self::with_generation(identity, generation)?;
        *self = replacement;
        Ok(())
    }

    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }

    #[must_use]
    pub fn public(&self) -> SignedPrekeyPublic {
        SignedPrekeyPublic {
            generation: self.generation,
            prekey: self.prekey.public_key(),
            signature: self.signature,
        }
    }

    fn with_generation(
        identity: &IdentityKeypair,
        generation: u64,
    ) -> Result<Self, SignedPrekeyError> {
        let prekey = X25519Prekey::generate()?;
        let public_key = prekey.public_key();
        let signature = identity.sign(&signing_input(generation, &public_key)?);
        Ok(Self {
            generation,
            prekey,
            signature,
        })
    }
}

impl fmt::Debug for SignedPrekey {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SignedPrekey")
            .field("generation", &self.generation)
            .field("public", &self.public())
            .field("signature", &self.signature)
            .field("prekey", &"REDACTED")
            .finish()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SignedPrekeyPublic {
    generation: u64,
    prekey: X25519PrekeyPublicKey,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl SignedPrekeyPublic {
    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }

    #[must_use]
    pub const fn prekey(&self) -> &X25519PrekeyPublicKey {
        &self.prekey
    }

    #[must_use]
    pub const fn signature(&self) -> &[u8; ED25519_SIGNATURE_BYTES] {
        &self.signature
    }

    pub fn verify(
        &self,
        identity: &yeokcham_core::IdentityPublicKey,
    ) -> Result<(), SignedPrekeyValidationError> {
        if self.generation == 0 {
            return Err(SignedPrekeyValidationError::InvalidGeneration);
        }
        let input = signing_input(self.generation, &self.prekey)
            .map_err(|_| SignedPrekeyValidationError::InvalidSignature)?;
        identity
            .verify(&input, &self.signature)
            .map_err(|_| SignedPrekeyValidationError::InvalidSignature)
    }

    pub(crate) fn from_parts(
        generation: u64,
        prekey: X25519PrekeyPublicKey,
        signature: [u8; ED25519_SIGNATURE_BYTES],
    ) -> Result<Self, SignedPrekeyValidationError> {
        if generation == 0 {
            return Err(SignedPrekeyValidationError::InvalidGeneration);
        }
        Ok(Self {
            generation,
            prekey,
            signature,
        })
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SignedPrekeyError {
    #[error("X25519 prekey generation failed: {0}")]
    Prekey(#[from] X25519PrekeyError),
    #[error("signed prekey generation is exhausted")]
    GenerationExhausted,
    #[error("signed prekey signing input encoding failed")]
    Encode,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SignedPrekeyValidationError {
    #[error("signed prekey generation is invalid")]
    InvalidGeneration,
    #[error("signed prekey signature is invalid")]
    InvalidSignature,
}

fn next_generation(generation: u64) -> Result<u64, SignedPrekeyError> {
    generation
        .checked_add(1)
        .ok_or(SignedPrekeyError::GenerationExhausted)
}

fn signing_input(
    generation: u64,
    prekey: &X25519PrekeyPublicKey,
) -> Result<Vec<u8>, SignedPrekeyError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNED_PREKEY_FIELDS)
        .map_err(|_| SignedPrekeyError::Encode)?
        .u8(SIGNED_PREKEY_SCHEMA_VERSION)
        .map_err(|_| SignedPrekeyError::Encode)?
        .u64(generation)
        .map_err(|_| SignedPrekeyError::Encode)?
        .bytes(prekey.as_bytes())
        .map_err(|_| SignedPrekeyError::Encode)?;
    let unsigned = encoder.into_writer();
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNED_PREKEY_SIGNING_INPUT_FIELDS)
        .map_err(|_| SignedPrekeyError::Encode)?
        .bytes(CryptoDomain::PrekeyBundleSignature.context())
        .map_err(|_| SignedPrekeyError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| SignedPrekeyError::Encode)?;
    Ok(encoder.into_writer())
}

#[cfg(test)]
mod tests {
    use super::{
        SIGNED_PREKEY_INITIAL_GENERATION, SignedPrekey, SignedPrekeyError,
        SignedPrekeyValidationError, next_generation, signing_input,
    };
    use yeokcham_core::IdentityKeypair;

    #[test]
    fn rotates_domain_separated_signed_prekeys() {
        let identity = IdentityKeypair::generate().unwrap();
        let mut signed_prekey = SignedPrekey::generate(&identity).unwrap();
        let first = signed_prekey.public();
        let first_input = signing_input(first.generation(), first.prekey()).unwrap();

        assert_eq!(first.generation(), SIGNED_PREKEY_INITIAL_GENERATION);
        assert_eq!(
            identity
                .public_key()
                .verify(&first_input, first.signature()),
            Ok(())
        );
        assert_eq!(first.verify(&identity.public_key()), Ok(()));
        signed_prekey.rotate(&identity).unwrap();
        let second = signed_prekey.public();
        let second_input = signing_input(second.generation(), second.prekey()).unwrap();

        assert_eq!(second.generation(), first.generation() + 1);
        assert_ne!(second.prekey(), first.prekey());
        assert_eq!(
            identity
                .public_key()
                .verify(&second_input, second.signature()),
            Ok(())
        );
        assert_eq!(second.verify(&identity.public_key()), Ok(()));
        let output = format!("{signed_prekey:?}");
        assert!(output.contains("prekey: \"REDACTED\""));
        assert!(!output.contains("StaticSecret"));
    }

    #[test]
    fn rejects_exhausted_generations() {
        assert_eq!(
            next_generation(u64::MAX).unwrap_err(),
            SignedPrekeyError::GenerationExhausted
        );
    }

    #[test]
    fn rejects_invalid_signed_prekey_generations() {
        let prekey = SignedPrekey::generate(&IdentityKeypair::generate().unwrap())
            .unwrap()
            .public();

        assert_eq!(
            super::SignedPrekeyPublic::from_parts(0, *prekey.prekey(), *prekey.signature())
                .unwrap_err(),
            SignedPrekeyValidationError::InvalidGeneration
        );
    }
}
