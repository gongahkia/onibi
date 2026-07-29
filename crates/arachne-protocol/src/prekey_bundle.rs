use arachne_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey,
    OneTimePrekeyId, X25519Prekey, X25519PrekeyPublicKey,
};
use minicbor::{Decoder, Encoder};

use crate::{CryptoDomain, SignedPrekeyPublic, SignedPrekeyValidationError};

pub const PREKEY_BUNDLE_SCHEMA_VERSION: u8 = 1;
pub const MAX_ONE_TIME_PREKEYS: usize = 64;
pub const MAX_PREKEY_BUNDLE_BYTES: usize = 4_096;
const PREKEY_BUNDLE_FIELDS: u64 = 5;
const PREKEY_BUNDLE_UNSIGNED_FIELDS: u64 = 4;
const SIGNED_PREKEY_FIELDS: u64 = 3;
const ONE_TIME_PREKEY_FIELDS: u64 = 2;
const SIGNING_INPUT_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct OneTimePrekeyPublic {
    identifier: OneTimePrekeyId,
    prekey: X25519PrekeyPublicKey,
}

impl OneTimePrekeyPublic {
    #[must_use]
    pub fn new(identifier: OneTimePrekeyId, prekey: &X25519Prekey) -> Self {
        Self {
            identifier,
            prekey: prekey.public_key(),
        }
    }

    #[must_use]
    pub const fn from_public(identifier: OneTimePrekeyId, prekey: X25519PrekeyPublicKey) -> Self {
        Self { identifier, prekey }
    }

    #[must_use]
    pub const fn identifier(&self) -> OneTimePrekeyId {
        self.identifier
    }

    #[must_use]
    pub const fn prekey(&self) -> &X25519PrekeyPublicKey {
        &self.prekey
    }

    fn from_parts(identifier: OneTimePrekeyId, prekey: X25519PrekeyPublicKey) -> Self {
        Self::from_public(identifier, prekey)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PrekeyBundle {
    identity: IdentityPublicKey,
    signed_prekey: SignedPrekeyPublic,
    one_time_prekeys: Vec<OneTimePrekeyPublic>,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl PrekeyBundle {
    pub fn create(
        identity: &IdentityKeypair,
        signed_prekey: SignedPrekeyPublic,
        mut one_time_prekeys: Vec<OneTimePrekeyPublic>,
    ) -> Result<Self, PrekeyBundleError> {
        let identity_public = identity.public_key();
        signed_prekey
            .verify(&identity_public)
            .map_err(PrekeyBundleError::SignedPrekey)?;
        normalize_one_time_prekeys(&mut one_time_prekeys)?;
        let signing_input = signing_input(&identity_public, &signed_prekey, &one_time_prekeys)?;
        Ok(Self {
            identity: identity_public,
            signed_prekey,
            one_time_prekeys,
            signature: identity.sign(&signing_input),
        })
    }

    #[must_use]
    pub const fn identity(&self) -> &IdentityPublicKey {
        &self.identity
    }

    #[must_use]
    pub const fn signed_prekey(&self) -> &SignedPrekeyPublic {
        &self.signed_prekey
    }

    #[must_use]
    pub fn one_time_prekeys(&self) -> &[OneTimePrekeyPublic] {
        &self.one_time_prekeys
    }

    #[must_use]
    pub const fn signature(&self) -> &[u8; ED25519_SIGNATURE_BYTES] {
        &self.signature
    }

    pub fn encode(&self) -> Result<Vec<u8>, PrekeyBundleError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(PREKEY_BUNDLE_FIELDS)
            .map_err(|_| PrekeyBundleError::Encode)?
            .u8(PREKEY_BUNDLE_SCHEMA_VERSION)
            .map_err(|_| PrekeyBundleError::Encode)?
            .bytes(self.identity.as_bytes())
            .map_err(|_| PrekeyBundleError::Encode)?;
        encode_signed_prekey(&mut encoder, &self.signed_prekey)?;
        encode_one_time_prekeys(&mut encoder, &self.one_time_prekeys)?;
        encoder
            .bytes(&self.signature)
            .map_err(|_| PrekeyBundleError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() > MAX_PREKEY_BUNDLE_BYTES {
            return Err(PrekeyBundleError::PayloadTooLarge);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, PrekeyBundleError> {
        if encoded.len() > MAX_PREKEY_BUNDLE_BYTES {
            return Err(PrekeyBundleError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| PrekeyBundleError::Decode)? != Some(PREKEY_BUNDLE_FIELDS) {
            return Err(PrekeyBundleError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| PrekeyBundleError::Decode)?;
        if version != PREKEY_BUNDLE_SCHEMA_VERSION {
            return Err(PrekeyBundleError::UnsupportedSchemaVersion(version));
        }
        let identity = decode_identity(decoder.bytes().map_err(|_| PrekeyBundleError::Decode)?)?;
        let signed_prekey = decode_signed_prekey(&mut decoder)?;
        let one_time_prekeys = decode_one_time_prekeys(&mut decoder)?;
        let signature = decode_signature(decoder.bytes().map_err(|_| PrekeyBundleError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(PrekeyBundleError::TrailingBytes);
        }
        let bundle = Self {
            identity,
            signed_prekey,
            one_time_prekeys,
            signature,
        };
        bundle
            .signed_prekey
            .verify(&bundle.identity)
            .map_err(PrekeyBundleError::SignedPrekey)?;
        let input = signing_input(
            &bundle.identity,
            &bundle.signed_prekey,
            &bundle.one_time_prekeys,
        )?;
        bundle
            .identity
            .verify(&input, &bundle.signature)
            .map_err(|_| PrekeyBundleError::InvalidSignature)?;
        if bundle.encode()? != encoded {
            return Err(PrekeyBundleError::NonCanonicalEncoding);
        }
        Ok(bundle)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum PrekeyBundleError {
    #[error("prekey bundle exceeds the configured limit")]
    PayloadTooLarge,
    #[error("unsupported prekey bundle schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("prekey bundle must be a five-element definite-length CBOR array")]
    InvalidShape,
    #[error("prekey bundle contains an invalid identity key")]
    InvalidIdentity,
    #[error("prekey bundle contains an invalid X25519 public key")]
    InvalidPrekey,
    #[error("prekey bundle contains an invalid one-time prekey identifier")]
    InvalidOneTimePrekeyIdentifier,
    #[error("prekey bundle contains duplicate one-time prekey identifiers")]
    DuplicateOneTimePrekeyIdentifier,
    #[error("prekey bundle contains too many one-time prekeys")]
    TooManyOneTimePrekeys,
    #[error("prekey bundle signature is invalid")]
    InvalidSignature,
    #[error("prekey bundle signed prekey is invalid: {0}")]
    SignedPrekey(#[from] SignedPrekeyValidationError),
    #[error("CBOR encoding failed")]
    Encode,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("trailing bytes after prekey bundle")]
    TrailingBytes,
    #[error("prekey bundle is not canonically encoded")]
    NonCanonicalEncoding,
}

fn normalize_one_time_prekeys(
    one_time_prekeys: &mut [OneTimePrekeyPublic],
) -> Result<(), PrekeyBundleError> {
    if one_time_prekeys.len() > MAX_ONE_TIME_PREKEYS {
        return Err(PrekeyBundleError::TooManyOneTimePrekeys);
    }
    one_time_prekeys.sort_unstable_by_key(|prekey| prekey.identifier);
    if one_time_prekeys
        .windows(2)
        .any(|pair| pair[0].identifier == pair[1].identifier)
    {
        return Err(PrekeyBundleError::DuplicateOneTimePrekeyIdentifier);
    }
    Ok(())
}

fn signing_input(
    identity: &IdentityPublicKey,
    signed_prekey: &SignedPrekeyPublic,
    one_time_prekeys: &[OneTimePrekeyPublic],
) -> Result<Vec<u8>, PrekeyBundleError> {
    let unsigned = encode_unsigned(identity, signed_prekey, one_time_prekeys)?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| PrekeyBundleError::Encode)?
        .bytes(CryptoDomain::PrekeyBundleSignature.context())
        .map_err(|_| PrekeyBundleError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| PrekeyBundleError::Encode)?;
    Ok(encoder.into_writer())
}

fn encode_unsigned(
    identity: &IdentityPublicKey,
    signed_prekey: &SignedPrekeyPublic,
    one_time_prekeys: &[OneTimePrekeyPublic],
) -> Result<Vec<u8>, PrekeyBundleError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(PREKEY_BUNDLE_UNSIGNED_FIELDS)
        .map_err(|_| PrekeyBundleError::Encode)?
        .u8(PREKEY_BUNDLE_SCHEMA_VERSION)
        .map_err(|_| PrekeyBundleError::Encode)?
        .bytes(identity.as_bytes())
        .map_err(|_| PrekeyBundleError::Encode)?;
    encode_signed_prekey(&mut encoder, signed_prekey)?;
    encode_one_time_prekeys(&mut encoder, one_time_prekeys)?;
    Ok(encoder.into_writer())
}

fn encode_signed_prekey(
    encoder: &mut Encoder<Vec<u8>>,
    signed_prekey: &SignedPrekeyPublic,
) -> Result<(), PrekeyBundleError> {
    encoder
        .array(SIGNED_PREKEY_FIELDS)
        .map_err(|_| PrekeyBundleError::Encode)?
        .u64(signed_prekey.generation())
        .map_err(|_| PrekeyBundleError::Encode)?
        .bytes(signed_prekey.prekey().as_bytes())
        .map_err(|_| PrekeyBundleError::Encode)?
        .bytes(signed_prekey.signature())
        .map_err(|_| PrekeyBundleError::Encode)?;
    Ok(())
}

fn encode_one_time_prekeys(
    encoder: &mut Encoder<Vec<u8>>,
    one_time_prekeys: &[OneTimePrekeyPublic],
) -> Result<(), PrekeyBundleError> {
    encoder
        .array(u64::try_from(one_time_prekeys.len()).map_err(|_| PrekeyBundleError::Encode)?)
        .map_err(|_| PrekeyBundleError::Encode)?;
    for prekey in one_time_prekeys {
        encoder
            .array(ONE_TIME_PREKEY_FIELDS)
            .map_err(|_| PrekeyBundleError::Encode)?
            .u64(prekey.identifier.get())
            .map_err(|_| PrekeyBundleError::Encode)?
            .bytes(prekey.prekey.as_bytes())
            .map_err(|_| PrekeyBundleError::Encode)?;
    }
    Ok(())
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, PrekeyBundleError> {
    if encoded.len() != ED25519_PUBLIC_KEY_BYTES {
        return Err(PrekeyBundleError::InvalidIdentity);
    }
    let mut bytes = [0; ED25519_PUBLIC_KEY_BYTES];
    bytes.copy_from_slice(encoded);
    IdentityPublicKey::from_bytes(bytes).map_err(|_| PrekeyBundleError::InvalidIdentity)
}

fn decode_signed_prekey(
    decoder: &mut Decoder<'_>,
) -> Result<SignedPrekeyPublic, PrekeyBundleError> {
    if decoder.array().map_err(|_| PrekeyBundleError::Decode)? != Some(SIGNED_PREKEY_FIELDS) {
        return Err(PrekeyBundleError::InvalidShape);
    }
    let generation = decoder.u64().map_err(|_| PrekeyBundleError::Decode)?;
    let prekey = decode_prekey(decoder.bytes().map_err(|_| PrekeyBundleError::Decode)?)?;
    let signature = decode_signature(decoder.bytes().map_err(|_| PrekeyBundleError::Decode)?)?;
    SignedPrekeyPublic::from_parts(generation, prekey, signature).map_err(PrekeyBundleError::from)
}

fn decode_one_time_prekeys(
    decoder: &mut Decoder<'_>,
) -> Result<Vec<OneTimePrekeyPublic>, PrekeyBundleError> {
    let count = decoder
        .array()
        .map_err(|_| PrekeyBundleError::Decode)?
        .ok_or(PrekeyBundleError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| PrekeyBundleError::TooManyOneTimePrekeys)?;
    if count > MAX_ONE_TIME_PREKEYS {
        return Err(PrekeyBundleError::TooManyOneTimePrekeys);
    }
    let mut prekeys = Vec::with_capacity(count);
    for _ in 0..count {
        if decoder.array().map_err(|_| PrekeyBundleError::Decode)? != Some(ONE_TIME_PREKEY_FIELDS) {
            return Err(PrekeyBundleError::InvalidShape);
        }
        let identifier =
            OneTimePrekeyId::new(decoder.u64().map_err(|_| PrekeyBundleError::Decode)?)
                .map_err(|_| PrekeyBundleError::InvalidOneTimePrekeyIdentifier)?;
        let prekey = decode_prekey(decoder.bytes().map_err(|_| PrekeyBundleError::Decode)?)?;
        prekeys.push(OneTimePrekeyPublic::from_parts(identifier, prekey));
    }
    normalize_one_time_prekeys(&mut prekeys)?;
    Ok(prekeys)
}

fn decode_prekey(encoded: &[u8]) -> Result<X25519PrekeyPublicKey, PrekeyBundleError> {
    if encoded.len() != arachne_core::X25519_KEY_BYTES {
        return Err(PrekeyBundleError::InvalidPrekey);
    }
    let mut bytes = [0; arachne_core::X25519_KEY_BYTES];
    bytes.copy_from_slice(encoded);
    X25519PrekeyPublicKey::from_bytes(bytes).map_err(|_| PrekeyBundleError::InvalidPrekey)
}

fn decode_signature(encoded: &[u8]) -> Result<[u8; ED25519_SIGNATURE_BYTES], PrekeyBundleError> {
    if encoded.len() != ED25519_SIGNATURE_BYTES {
        return Err(PrekeyBundleError::InvalidSignature);
    }
    let mut signature = [0; ED25519_SIGNATURE_BYTES];
    signature.copy_from_slice(encoded);
    Ok(signature)
}

#[cfg(test)]
mod tests {
    use super::{MAX_ONE_TIME_PREKEYS, OneTimePrekeyPublic, PrekeyBundle, PrekeyBundleError};
    use crate::SignedPrekey;
    use arachne_core::{IdentityKeypair, OneTimePrekeyId, X25519Prekey};

    #[test]
    fn publishes_and_parses_canonical_verified_bundles() {
        let identity = IdentityKeypair::generate().unwrap();
        let signed_prekey = SignedPrekey::generate(&identity).unwrap();
        let first = X25519Prekey::generate().unwrap();
        let second = X25519Prekey::generate().unwrap();
        let bundle = PrekeyBundle::create(
            &identity,
            signed_prekey.public(),
            vec![
                OneTimePrekeyPublic::new(OneTimePrekeyId::new(2).unwrap(), &second),
                OneTimePrekeyPublic::new(OneTimePrekeyId::new(1).unwrap(), &first),
            ],
        )
        .unwrap();
        let encoded = bundle.encode().unwrap();

        assert_eq!(bundle.one_time_prekeys()[0].identifier().get(), 1);
        assert_eq!(PrekeyBundle::decode(&encoded).unwrap(), bundle);
    }

    #[test]
    fn rejects_tampered_duplicate_and_oversized_bundles() {
        let identity = IdentityKeypair::generate().unwrap();
        let signed_prekey = SignedPrekey::generate(&identity).unwrap();
        let one_time = X25519Prekey::generate().unwrap();
        let public = OneTimePrekeyPublic::new(OneTimePrekeyId::new(1).unwrap(), &one_time);
        let bundle = PrekeyBundle::create(&identity, signed_prekey.public(), vec![public]).unwrap();
        let mut tampered = bundle.encode().unwrap();
        let last = tampered.len() - 1;
        tampered[last] ^= 1;
        let oversized = (1..=u64::try_from(MAX_ONE_TIME_PREKEYS).unwrap() + 1)
            .map(|identifier| {
                OneTimePrekeyPublic::new(OneTimePrekeyId::new(identifier).unwrap(), &one_time)
            })
            .collect();

        assert_eq!(
            PrekeyBundle::decode(&tampered).unwrap_err(),
            PrekeyBundleError::InvalidSignature
        );
        assert_eq!(
            PrekeyBundle::create(&identity, signed_prekey.public(), vec![public, public])
                .unwrap_err(),
            PrekeyBundleError::DuplicateOneTimePrekeyIdentifier
        );
        assert_eq!(
            PrekeyBundle::create(&identity, signed_prekey.public(), oversized).unwrap_err(),
            PrekeyBundleError::TooManyOneTimePrekeys
        );
    }
}
