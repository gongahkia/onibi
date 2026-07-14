use std::fmt;

use hkdf::Hkdf;
use minicbor::{Decoder, Encoder, data::Type};
use sha2::Sha256;
use yeokcham_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey,
    OneTimePrekeyId, X25519IdentityKeypair, X25519IdentityPublicKey, X25519KeyAgreementError,
    X25519Prekey, X25519PrekeyPublicKey,
};
use zeroize::Zeroizing;

use crate::{CryptoDomain, OneTimePrekeyPublic, PrekeyBundle, PrekeyBundleError, SignedPrekey};

pub const X25519_IDENTITY_BINDING_SCHEMA_VERSION: u8 = 1;
pub const X3DH_PREKEY_BUNDLE_SCHEMA_VERSION: u8 = 1;
pub const X3DH_INITIAL_MESSAGE_SCHEMA_VERSION: u8 = 1;
pub const MAX_X3DH_PREKEY_BUNDLE_BYTES: usize = 8_192;
pub const MAX_X3DH_INITIAL_MESSAGE_BYTES: usize = 512;
pub const X3DH_ROOT_KEY_BYTES: usize = 32;
const BINDING_FIELDS: u64 = 4;
const BINDING_UNSIGNED_FIELDS: u64 = 3;
const SIGNING_INPUT_FIELDS: u64 = 2;
const X3DH_BUNDLE_FIELDS: u64 = 3;
const INITIAL_MESSAGE_FIELDS: u64 = 5;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct X25519IdentityBinding {
    signing_identity: IdentityPublicKey,
    exchange_identity: X25519IdentityPublicKey,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl X25519IdentityBinding {
    pub fn create(
        signing_identity: &IdentityKeypair,
        exchange_identity: &X25519IdentityKeypair,
    ) -> Result<Self, X3dhError> {
        let signing_public = signing_identity.public_key();
        let exchange_public = exchange_identity.public_key();
        let input = binding_signing_input(&signing_public, &exchange_public)?;
        Ok(Self {
            signing_identity: signing_public,
            exchange_identity: exchange_public,
            signature: signing_identity.sign(&input),
        })
    }

    #[must_use]
    pub const fn signing_identity(&self) -> &IdentityPublicKey {
        &self.signing_identity
    }

    #[must_use]
    pub const fn exchange_identity(&self) -> &X25519IdentityPublicKey {
        &self.exchange_identity
    }

    pub fn verify(&self) -> Result<(), X3dhError> {
        self.signing_identity
            .verify(
                &binding_signing_input(&self.signing_identity, &self.exchange_identity)?,
                &self.signature,
            )
            .map_err(|_| X3dhError::InvalidIdentityBinding)
    }

    pub fn encode(&self) -> Result<Vec<u8>, X3dhError> {
        self.verify()?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(BINDING_FIELDS)
            .map_err(|_| X3dhError::Encode)?
            .u8(X25519_IDENTITY_BINDING_SCHEMA_VERSION)
            .map_err(|_| X3dhError::Encode)?
            .bytes(self.signing_identity.as_bytes())
            .map_err(|_| X3dhError::Encode)?
            .bytes(self.exchange_identity.as_bytes())
            .map_err(|_| X3dhError::Encode)?
            .bytes(&self.signature)
            .map_err(|_| X3dhError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, X3dhError> {
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| X3dhError::Decode)? != Some(BINDING_FIELDS) {
            return Err(X3dhError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| X3dhError::Decode)?;
        if version != X25519_IDENTITY_BINDING_SCHEMA_VERSION {
            return Err(X3dhError::UnsupportedSchemaVersion(version));
        }
        let signing_identity = decode_ed_identity(
            decoder.bytes().map_err(|_| X3dhError::Decode)?,
            X3dhError::InvalidIdentityBinding,
        )?;
        let exchange_identity = decode_x_identity(
            decoder.bytes().map_err(|_| X3dhError::Decode)?,
            X3dhError::InvalidIdentityBinding,
        )?;
        let signature = decode_signature(decoder.bytes().map_err(|_| X3dhError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(X3dhError::TrailingBytes);
        }
        let binding = Self {
            signing_identity,
            exchange_identity,
            signature,
        };
        binding.verify()?;
        if binding.encode()? != encoded {
            return Err(X3dhError::NonCanonicalEncoding);
        }
        Ok(binding)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct X3dhPrekeyBundle {
    identity_binding: X25519IdentityBinding,
    prekeys: PrekeyBundle,
}

impl X3dhPrekeyBundle {
    pub fn create(
        signing_identity: &IdentityKeypair,
        exchange_identity: &X25519IdentityKeypair,
        signed_prekey: crate::SignedPrekeyPublic,
        one_time_prekeys: Vec<OneTimePrekeyPublic>,
    ) -> Result<Self, X3dhError> {
        let identity_binding = X25519IdentityBinding::create(signing_identity, exchange_identity)?;
        let prekeys = PrekeyBundle::create(signing_identity, signed_prekey, one_time_prekeys)
            .map_err(X3dhError::PrekeyBundle)?;
        let bundle = Self {
            identity_binding,
            prekeys,
        };
        bundle.verify()?;
        Ok(bundle)
    }

    #[must_use]
    pub const fn identity_binding(&self) -> &X25519IdentityBinding {
        &self.identity_binding
    }

    #[must_use]
    pub const fn signed_prekey(&self) -> &crate::SignedPrekeyPublic {
        self.prekeys.signed_prekey()
    }

    #[must_use]
    pub fn one_time_prekeys(&self) -> &[OneTimePrekeyPublic] {
        self.prekeys.one_time_prekeys()
    }

    pub fn verify(&self) -> Result<(), X3dhError> {
        self.identity_binding.verify()?;
        if self.identity_binding.signing_identity() != self.prekeys.identity() {
            return Err(X3dhError::IdentityBindingMismatch);
        }
        self.signed_prekey()
            .verify(self.identity_binding.signing_identity())
            .map_err(|_| X3dhError::InvalidSignedPrekey)
    }

    pub fn encode(&self) -> Result<Vec<u8>, X3dhError> {
        self.verify()?;
        let binding = self.identity_binding.encode()?;
        let prekeys = self.prekeys.encode().map_err(X3dhError::PrekeyBundle)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(X3DH_BUNDLE_FIELDS)
            .map_err(|_| X3dhError::Encode)?
            .u8(X3DH_PREKEY_BUNDLE_SCHEMA_VERSION)
            .map_err(|_| X3dhError::Encode)?
            .bytes(&binding)
            .map_err(|_| X3dhError::Encode)?
            .bytes(&prekeys)
            .map_err(|_| X3dhError::Encode)?;
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_X3DH_PREKEY_BUNDLE_BYTES {
            return Err(X3dhError::PayloadTooLarge);
        }
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, X3dhError> {
        if encoded.len() > MAX_X3DH_PREKEY_BUNDLE_BYTES {
            return Err(X3dhError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| X3dhError::Decode)? != Some(X3DH_BUNDLE_FIELDS) {
            return Err(X3dhError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| X3dhError::Decode)?;
        if version != X3DH_PREKEY_BUNDLE_SCHEMA_VERSION {
            return Err(X3dhError::UnsupportedSchemaVersion(version));
        }
        let identity_binding =
            X25519IdentityBinding::decode(decoder.bytes().map_err(|_| X3dhError::Decode)?)?;
        let prekeys = PrekeyBundle::decode(decoder.bytes().map_err(|_| X3dhError::Decode)?)
            .map_err(X3dhError::PrekeyBundle)?;
        if decoder.position() != encoded.len() {
            return Err(X3dhError::TrailingBytes);
        }
        let bundle = Self {
            identity_binding,
            prekeys,
        };
        bundle.verify()?;
        if bundle.encode()? != encoded {
            return Err(X3dhError::NonCanonicalEncoding);
        }
        Ok(bundle)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct X3dhInitialMessage {
    initiator_binding: X25519IdentityBinding,
    ephemeral: X25519PrekeyPublicKey,
    signed_prekey_generation: u64,
    one_time_prekey: Option<OneTimePrekeyId>,
}

impl X3dhInitialMessage {
    #[must_use]
    pub const fn initiator_binding(&self) -> &X25519IdentityBinding {
        &self.initiator_binding
    }

    #[must_use]
    pub const fn ephemeral(&self) -> &X25519PrekeyPublicKey {
        &self.ephemeral
    }

    #[must_use]
    pub const fn signed_prekey_generation(&self) -> u64 {
        self.signed_prekey_generation
    }

    #[must_use]
    pub const fn one_time_prekey(&self) -> Option<OneTimePrekeyId> {
        self.one_time_prekey
    }

    pub fn encode(&self) -> Result<Vec<u8>, X3dhError> {
        self.initiator_binding.verify()?;
        if self.signed_prekey_generation == 0 {
            return Err(X3dhError::InvalidSignedPrekey);
        }
        let binding = self.initiator_binding.encode()?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(INITIAL_MESSAGE_FIELDS)
            .map_err(|_| X3dhError::Encode)?
            .u8(X3DH_INITIAL_MESSAGE_SCHEMA_VERSION)
            .map_err(|_| X3dhError::Encode)?
            .bytes(&binding)
            .map_err(|_| X3dhError::Encode)?
            .bytes(self.ephemeral.as_bytes())
            .map_err(|_| X3dhError::Encode)?
            .u64(self.signed_prekey_generation)
            .map_err(|_| X3dhError::Encode)?;
        match self.one_time_prekey {
            Some(identifier) => encoder
                .u64(identifier.get())
                .map_err(|_| X3dhError::Encode)?,
            None => encoder.null().map_err(|_| X3dhError::Encode)?,
        };
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_X3DH_INITIAL_MESSAGE_BYTES {
            return Err(X3dhError::PayloadTooLarge);
        }
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, X3dhError> {
        if encoded.len() > MAX_X3DH_INITIAL_MESSAGE_BYTES {
            return Err(X3dhError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| X3dhError::Decode)? != Some(INITIAL_MESSAGE_FIELDS) {
            return Err(X3dhError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| X3dhError::Decode)?;
        if version != X3DH_INITIAL_MESSAGE_SCHEMA_VERSION {
            return Err(X3dhError::UnsupportedSchemaVersion(version));
        }
        let initiator_binding =
            X25519IdentityBinding::decode(decoder.bytes().map_err(|_| X3dhError::Decode)?)?;
        let ephemeral = decode_prekey(
            decoder.bytes().map_err(|_| X3dhError::Decode)?,
            X3dhError::InvalidEphemeral,
        )?;
        let signed_prekey_generation = decoder.u64().map_err(|_| X3dhError::Decode)?;
        if signed_prekey_generation == 0 {
            return Err(X3dhError::InvalidSignedPrekey);
        }
        let one_time_prekey = match decoder.datatype().map_err(|_| X3dhError::Decode)? {
            Type::Null => {
                decoder.null().map_err(|_| X3dhError::Decode)?;
                None
            }
            Type::U8 | Type::U16 | Type::U32 | Type::U64 => Some(
                OneTimePrekeyId::new(decoder.u64().map_err(|_| X3dhError::Decode)?)
                    .map_err(|_| X3dhError::InvalidOneTimePrekey)?,
            ),
            _ => return Err(X3dhError::InvalidOneTimePrekey),
        };
        if decoder.position() != encoded.len() {
            return Err(X3dhError::TrailingBytes);
        }
        let message = Self {
            initiator_binding,
            ephemeral,
            signed_prekey_generation,
            one_time_prekey,
        };
        if message.encode()? != encoded {
            return Err(X3dhError::NonCanonicalEncoding);
        }
        Ok(message)
    }
}

pub struct X3dhSession {
    root_key: Zeroizing<[u8; X3DH_ROOT_KEY_BYTES]>,
    associated_data: Vec<u8>,
    used_one_time_prekey: Option<OneTimePrekeyId>,
}

impl X3dhSession {
    #[must_use]
    pub fn root_key(&self) -> &[u8; X3DH_ROOT_KEY_BYTES] {
        &self.root_key
    }

    #[must_use]
    pub fn associated_data(&self) -> &[u8] {
        &self.associated_data
    }

    #[must_use]
    pub const fn used_one_time_prekey(&self) -> Option<OneTimePrekeyId> {
        self.used_one_time_prekey
    }
}

impl fmt::Debug for X3dhSession {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("X3dhSession")
            .field("root_key", &"REDACTED")
            .field("associated_data", &self.associated_data)
            .field("used_one_time_prekey", &self.used_one_time_prekey)
            .finish()
    }
}

pub fn initiate_x3dh(
    local_identity: &X25519IdentityKeypair,
    local_binding: X25519IdentityBinding,
    remote_bundle: &X3dhPrekeyBundle,
) -> Result<(X3dhInitialMessage, X3dhSession), X3dhError> {
    local_binding.verify()?;
    remote_bundle.verify()?;
    if local_binding.exchange_identity() != &local_identity.public_key() {
        return Err(X3dhError::LocalIdentityMismatch);
    }
    let ephemeral = X25519Prekey::generate().map_err(|_| X3dhError::Randomness)?;
    let one_time_prekey = remote_bundle.one_time_prekeys().first().copied();
    let mut secrets = vec![
        local_identity
            .shared_secret(remote_bundle.signed_prekey().prekey().as_bytes())
            .map_err(X3dhError::KeyAgreement)?,
        ephemeral
            .shared_secret(
                remote_bundle
                    .identity_binding
                    .exchange_identity()
                    .as_bytes(),
            )
            .map_err(X3dhError::KeyAgreement)?,
        ephemeral
            .shared_secret(remote_bundle.signed_prekey().prekey().as_bytes())
            .map_err(X3dhError::KeyAgreement)?,
    ];
    if let Some(one_time_prekey) = one_time_prekey {
        secrets.push(
            ephemeral
                .shared_secret(one_time_prekey.prekey().as_bytes())
                .map_err(X3dhError::KeyAgreement)?,
        );
    }
    let session = X3dhSession {
        root_key: derive_root_key(&secrets)?,
        associated_data: associated_data(&local_binding, remote_bundle.identity_binding())?,
        used_one_time_prekey: one_time_prekey.map(|prekey| prekey.identifier()),
    };
    let message = X3dhInitialMessage {
        initiator_binding: local_binding,
        ephemeral: ephemeral.public_key(),
        signed_prekey_generation: remote_bundle.signed_prekey().generation(),
        one_time_prekey: session.used_one_time_prekey,
    };
    secrets.clear();
    Ok((message, session))
}

pub fn respond_x3dh(
    local_identity: &X25519IdentityKeypair,
    local_binding: &X25519IdentityBinding,
    signed_prekey: &SignedPrekey,
    one_time_prekey: Option<(OneTimePrekeyId, &X25519Prekey)>,
    initial: &X3dhInitialMessage,
) -> Result<X3dhSession, X3dhError> {
    local_binding.verify()?;
    initial.initiator_binding.verify()?;
    if local_binding.exchange_identity() != &local_identity.public_key() {
        return Err(X3dhError::LocalIdentityMismatch);
    }
    if initial.signed_prekey_generation != signed_prekey.generation() {
        return Err(X3dhError::SignedPrekeyMismatch);
    }
    let selected_one_time = match (initial.one_time_prekey, one_time_prekey) {
        (Some(expected), Some((actual, prekey))) if expected == actual => Some(prekey),
        (Some(_), _) => return Err(X3dhError::MissingOneTimePrekey),
        (None, None) => None,
        (None, Some(_)) => return Err(X3dhError::UnexpectedOneTimePrekey),
    };
    let mut secrets = vec![
        signed_prekey
            .prekey()
            .shared_secret(initial.initiator_binding.exchange_identity().as_bytes())
            .map_err(X3dhError::KeyAgreement)?,
        local_identity
            .shared_secret(initial.ephemeral.as_bytes())
            .map_err(X3dhError::KeyAgreement)?,
        signed_prekey
            .prekey()
            .shared_secret(initial.ephemeral.as_bytes())
            .map_err(X3dhError::KeyAgreement)?,
    ];
    if let Some(prekey) = selected_one_time {
        secrets.push(
            prekey
                .shared_secret(initial.ephemeral.as_bytes())
                .map_err(X3dhError::KeyAgreement)?,
        );
    }
    let session = X3dhSession {
        root_key: derive_root_key(&secrets)?,
        associated_data: associated_data(&initial.initiator_binding, local_binding)?,
        used_one_time_prekey: initial.one_time_prekey,
    };
    secrets.clear();
    Ok(session)
}

#[derive(Debug, thiserror::Error)]
pub enum X3dhError {
    #[error("X3DH CBOR encoding failed")]
    Encode,
    #[error("X3DH CBOR decoding failed")]
    Decode,
    #[error("X3DH payload exceeds the configured limit")]
    PayloadTooLarge,
    #[error("unsupported X3DH schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("X3DH payload has an invalid shape")]
    InvalidShape,
    #[error("X3DH identity binding is invalid")]
    InvalidIdentityBinding,
    #[error("X3DH identity binding does not match the prekey bundle")]
    IdentityBindingMismatch,
    #[error("X3DH local identity does not match its binding")]
    LocalIdentityMismatch,
    #[error("X3DH signed prekey is invalid")]
    InvalidSignedPrekey,
    #[error("X3DH signed prekey does not match the initial message")]
    SignedPrekeyMismatch,
    #[error("X3DH ephemeral public key is invalid")]
    InvalidEphemeral,
    #[error("X3DH one-time prekey identifier is invalid")]
    InvalidOneTimePrekey,
    #[error("X3DH required one-time prekey is unavailable")]
    MissingOneTimePrekey,
    #[error("X3DH received an unexpected one-time prekey")]
    UnexpectedOneTimePrekey,
    #[error("X3DH key agreement failed: {0}")]
    KeyAgreement(#[source] X25519KeyAgreementError),
    #[error("X3DH root-key derivation failed")]
    KeyDerivation,
    #[error("X3DH prekey bundle is invalid: {0}")]
    PrekeyBundle(#[source] PrekeyBundleError),
    #[error("X3DH payload has trailing bytes")]
    TrailingBytes,
    #[error("X3DH payload is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("operating-system random source failed")]
    Randomness,
}

fn binding_signing_input(
    signing_identity: &IdentityPublicKey,
    exchange_identity: &X25519IdentityPublicKey,
) -> Result<Vec<u8>, X3dhError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(BINDING_UNSIGNED_FIELDS)
        .map_err(|_| X3dhError::Encode)?
        .u8(X25519_IDENTITY_BINDING_SCHEMA_VERSION)
        .map_err(|_| X3dhError::Encode)?
        .bytes(signing_identity.as_bytes())
        .map_err(|_| X3dhError::Encode)?
        .bytes(exchange_identity.as_bytes())
        .map_err(|_| X3dhError::Encode)?;
    let unsigned = encoder.into_writer();
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| X3dhError::Encode)?
        .bytes(CryptoDomain::X3dhIdentityBindingSignature.context())
        .map_err(|_| X3dhError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| X3dhError::Encode)?;
    Ok(encoder.into_writer())
}

fn derive_root_key(
    secrets: &[Zeroizing<[u8; X3DH_ROOT_KEY_BYTES]>],
) -> Result<Zeroizing<[u8; X3DH_ROOT_KEY_BYTES]>, X3dhError> {
    let mut key_material = Zeroizing::new(Vec::with_capacity(
        X3DH_ROOT_KEY_BYTES + secrets.len() * X3DH_ROOT_KEY_BYTES,
    ));
    key_material.extend_from_slice(&[0xff; X3DH_ROOT_KEY_BYTES]);
    for secret in secrets {
        key_material.extend_from_slice(secret.as_ref());
    }
    let hkdf = Hkdf::<Sha256>::new(None, key_material.as_ref());
    let mut root_key = Zeroizing::new([0; X3DH_ROOT_KEY_BYTES]);
    hkdf.expand(CryptoDomain::X3dhRootKey.context(), root_key.as_mut())
        .map_err(|_| X3dhError::KeyDerivation)?;
    Ok(root_key)
}

fn associated_data(
    initiator: &X25519IdentityBinding,
    recipient: &X25519IdentityBinding,
) -> Result<Vec<u8>, X3dhError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(5)
        .map_err(|_| X3dhError::Encode)?
        .bytes(CryptoDomain::X3dhAssociatedData.context())
        .map_err(|_| X3dhError::Encode)?
        .bytes(initiator.signing_identity.as_bytes())
        .map_err(|_| X3dhError::Encode)?
        .bytes(initiator.exchange_identity.as_bytes())
        .map_err(|_| X3dhError::Encode)?
        .bytes(recipient.signing_identity.as_bytes())
        .map_err(|_| X3dhError::Encode)?
        .bytes(recipient.exchange_identity.as_bytes())
        .map_err(|_| X3dhError::Encode)?;
    Ok(encoder.into_writer())
}

fn decode_ed_identity(encoded: &[u8], error: X3dhError) -> Result<IdentityPublicKey, X3dhError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded.try_into().map_err(|_| error)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| X3dhError::InvalidIdentityBinding)
}

fn decode_x_identity(
    encoded: &[u8],
    error: X3dhError,
) -> Result<X25519IdentityPublicKey, X3dhError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded.try_into().map_err(|_| error)?;
    X25519IdentityPublicKey::from_bytes(bytes).map_err(|_| X3dhError::InvalidIdentityBinding)
}

fn decode_prekey(encoded: &[u8], error: X3dhError) -> Result<X25519PrekeyPublicKey, X3dhError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded.try_into().map_err(|_| error)?;
    X25519PrekeyPublicKey::from_bytes(bytes).map_err(|_| X3dhError::InvalidEphemeral)
}

fn decode_signature(encoded: &[u8]) -> Result<[u8; ED25519_SIGNATURE_BYTES], X3dhError> {
    encoded
        .try_into()
        .map_err(|_| X3dhError::InvalidIdentityBinding)
}

#[cfg(test)]
mod tests {
    use yeokcham_core::{IdentityKeypair, OneTimePrekeyId, X25519IdentityKeypair, X25519Prekey};

    use super::{
        X3dhError, X3dhInitialMessage, X3dhPrekeyBundle, X25519IdentityBinding, initiate_x3dh,
        respond_x3dh,
    };
    use crate::{OneTimePrekeyPublic, SignedPrekey};

    #[test]
    fn establishes_matching_root_keys_with_a_serialized_initial_message() {
        let initiator_signing = IdentityKeypair::generate().unwrap();
        let initiator_exchange = X25519IdentityKeypair::generate().unwrap();
        let initiator_binding =
            X25519IdentityBinding::create(&initiator_signing, &initiator_exchange).unwrap();
        let recipient_signing = IdentityKeypair::generate().unwrap();
        let recipient_exchange = X25519IdentityKeypair::generate().unwrap();
        let recipient_binding =
            X25519IdentityBinding::create(&recipient_signing, &recipient_exchange).unwrap();
        let signed_prekey = SignedPrekey::generate(&recipient_signing).unwrap();
        let one_time = X25519Prekey::generate().unwrap();
        let one_time_id = OneTimePrekeyId::new(7).unwrap();
        let bundle = X3dhPrekeyBundle::create(
            &recipient_signing,
            &recipient_exchange,
            signed_prekey.public(),
            vec![OneTimePrekeyPublic::new(one_time_id, &one_time)],
        )
        .unwrap();
        let encoded_bundle = bundle.encode().unwrap();
        let (message, initiator_session) =
            initiate_x3dh(&initiator_exchange, initiator_binding, &bundle).unwrap();
        let encoded_message = message.encode().unwrap();
        let decoded_message = X3dhInitialMessage::decode(&encoded_message).unwrap();
        let responder_session = respond_x3dh(
            &recipient_exchange,
            &recipient_binding,
            &signed_prekey,
            Some((one_time_id, &one_time)),
            &decoded_message,
        )
        .unwrap();

        assert_eq!(X3dhPrekeyBundle::decode(&encoded_bundle).unwrap(), bundle);
        assert_eq!(decoded_message, message);
        assert_eq!(initiator_session.root_key(), responder_session.root_key());
        assert_eq!(
            initiator_session.associated_data(),
            responder_session.associated_data()
        );
        assert_eq!(initiator_session.used_one_time_prekey(), Some(one_time_id));
    }

    #[test]
    fn fails_closed_for_identity_or_one_time_prekey_mismatches() {
        let signing = IdentityKeypair::generate().unwrap();
        let exchange = X25519IdentityKeypair::generate().unwrap();
        let binding = X25519IdentityBinding::create(&signing, &exchange).unwrap();
        let signed_prekey = SignedPrekey::generate(&signing).unwrap();
        let bundle =
            X3dhPrekeyBundle::create(&signing, &exchange, signed_prekey.public(), Vec::new())
                .unwrap();
        let wrong_exchange = X25519IdentityKeypair::generate().unwrap();
        assert!(matches!(
            initiate_x3dh(&wrong_exchange, binding, &bundle),
            Err(X3dhError::LocalIdentityMismatch)
        ));
        let (message, _) = initiate_x3dh(&exchange, binding, &bundle).unwrap();
        assert!(matches!(
            respond_x3dh(
                &exchange,
                &binding,
                &signed_prekey,
                Some((
                    OneTimePrekeyId::new(1).unwrap(),
                    &X25519Prekey::generate().unwrap()
                )),
                &message
            ),
            Err(X3dhError::UnexpectedOneTimePrekey)
        ));
    }
}
