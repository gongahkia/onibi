use std::fmt;

use arachne_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey,
};
use minicbor::{Decoder, Encoder};

use crate::{CryptoDomain, RelayInvitation, RelayInvitationError, X3dhError, X3dhPrekeyBundle};

pub const COURIER_BUNDLE_SCHEMA_VERSION: u8 = 2;
pub const RELAY_TLS_CERTIFICATE_PIN_BYTES: usize = 32;
pub const MAX_COURIER_BUNDLE_BYTES: usize = 16 * 1024;
const BUNDLE_FIELDS: u64 = 7;
const UNSIGNED_FIELDS: u64 = 6;
const SIGNING_INPUT_FIELDS: u64 = 2;

pub struct CourierBundle {
    publisher: IdentityPublicKey,
    prekey_bundle: X3dhPrekeyBundle,
    relay_invitation: RelayInvitation,
    relay_tls_pin: [u8; RELAY_TLS_CERTIFICATE_PIN_BYTES],
    generation: u64,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl CourierBundle {
    pub fn create(
        publisher: &IdentityKeypair,
        prekey_bundle: X3dhPrekeyBundle,
        relay_invitation: RelayInvitation,
        relay_tls_pin: [u8; RELAY_TLS_CERTIFICATE_PIN_BYTES],
        generation: u64,
    ) -> Result<Self, CourierBundleError> {
        let publisher_key = publisher.public_key();
        validate_parts(
            &publisher_key,
            &prekey_bundle,
            &relay_invitation,
            &relay_tls_pin,
        )?;
        if generation == 0 {
            return Err(CourierBundleError::InvalidGeneration);
        }
        let signature = publisher.sign(&signing_input(
            &publisher_key,
            &prekey_bundle,
            &relay_invitation,
            &relay_tls_pin,
            generation,
        )?);
        Ok(Self {
            publisher: publisher_key,
            prekey_bundle,
            relay_invitation,
            relay_tls_pin,
            generation,
            signature,
        })
    }

    #[must_use]
    pub const fn publisher(&self) -> &IdentityPublicKey {
        &self.publisher
    }

    #[must_use]
    pub const fn prekey_bundle(&self) -> &X3dhPrekeyBundle {
        &self.prekey_bundle
    }

    #[must_use]
    pub const fn relay_invitation(&self) -> &RelayInvitation {
        &self.relay_invitation
    }

    #[must_use]
    pub const fn relay_tls_pin(&self) -> &[u8; RELAY_TLS_CERTIFICATE_PIN_BYTES] {
        &self.relay_tls_pin
    }

    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }

    pub fn validate(&self, now_unix_seconds: u64) -> Result<(), CourierBundleError> {
        validate_parts(
            &self.publisher,
            &self.prekey_bundle,
            &self.relay_invitation,
            &self.relay_tls_pin,
        )?;
        if self.generation == 0 {
            return Err(CourierBundleError::InvalidGeneration);
        }
        self.relay_invitation
            .validate(&self.publisher, now_unix_seconds)
            .map_err(CourierBundleError::RelayInvitation)?;
        self.publisher
            .verify(
                &signing_input(
                    &self.publisher,
                    &self.prekey_bundle,
                    &self.relay_invitation,
                    &self.relay_tls_pin,
                    self.generation,
                )?,
                &self.signature,
            )
            .map_err(|_| CourierBundleError::InvalidSignature)
    }

    pub fn encode(&self) -> Result<Vec<u8>, CourierBundleError> {
        validate_parts(
            &self.publisher,
            &self.prekey_bundle,
            &self.relay_invitation,
            &self.relay_tls_pin,
        )?;
        let prekey_bundle = self
            .prekey_bundle
            .encode()
            .map_err(CourierBundleError::X3dh)?;
        let relay_invitation = self
            .relay_invitation
            .encode()
            .map_err(CourierBundleError::RelayInvitation)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(BUNDLE_FIELDS)
            .map_err(|_| CourierBundleError::Encode)?
            .u8(COURIER_BUNDLE_SCHEMA_VERSION)
            .map_err(|_| CourierBundleError::Encode)?
            .bytes(self.publisher.as_bytes())
            .map_err(|_| CourierBundleError::Encode)?
            .bytes(&prekey_bundle)
            .map_err(|_| CourierBundleError::Encode)?
            .bytes(&relay_invitation)
            .map_err(|_| CourierBundleError::Encode)?
            .bytes(&self.relay_tls_pin)
            .map_err(|_| CourierBundleError::Encode)?
            .u64(self.generation)
            .map_err(|_| CourierBundleError::Encode)?
            .bytes(&self.signature)
            .map_err(|_| CourierBundleError::Encode)?;
        let encoded = encoder.into_writer();
        if encoded.len() > MAX_COURIER_BUNDLE_BYTES {
            return Err(CourierBundleError::TooLarge);
        }
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, CourierBundleError> {
        if encoded.len() > MAX_COURIER_BUNDLE_BYTES {
            return Err(CourierBundleError::TooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| CourierBundleError::Decode)? != Some(BUNDLE_FIELDS) {
            return Err(CourierBundleError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| CourierBundleError::Decode)?;
        if version != COURIER_BUNDLE_SCHEMA_VERSION {
            return Err(CourierBundleError::UnsupportedSchemaVersion(version));
        }
        let publisher = decode_identity(decoder.bytes().map_err(|_| CourierBundleError::Decode)?)?;
        let prekey_bundle =
            X3dhPrekeyBundle::decode(decoder.bytes().map_err(|_| CourierBundleError::Decode)?)
                .map_err(CourierBundleError::X3dh)?;
        let relay_invitation =
            RelayInvitation::decode(decoder.bytes().map_err(|_| CourierBundleError::Decode)?)
                .map_err(CourierBundleError::RelayInvitation)?;
        let relay_tls_pin = decode_pin(decoder.bytes().map_err(|_| CourierBundleError::Decode)?)?;
        let generation = decoder.u64().map_err(|_| CourierBundleError::Decode)?;
        let signature = decode_signature(decoder.bytes().map_err(|_| CourierBundleError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(CourierBundleError::TrailingBytes);
        }
        let bundle = Self {
            publisher,
            prekey_bundle,
            relay_invitation,
            relay_tls_pin,
            generation,
            signature,
        };
        validate_parts(
            &bundle.publisher,
            &bundle.prekey_bundle,
            &bundle.relay_invitation,
            &bundle.relay_tls_pin,
        )?;
        bundle
            .publisher
            .verify(
                &signing_input(
                    &bundle.publisher,
                    &bundle.prekey_bundle,
                    &bundle.relay_invitation,
                    &bundle.relay_tls_pin,
                    bundle.generation,
                )?,
                &bundle.signature,
            )
            .map_err(|_| CourierBundleError::InvalidSignature)?;
        if bundle.encode()? != encoded {
            return Err(CourierBundleError::NonCanonicalEncoding);
        }
        Ok(bundle)
    }
}

#[must_use]
pub fn courier_directory_fetch_signing_input(recipient: &IdentityPublicKey) -> Vec<u8> {
    let mut input = Vec::with_capacity(
        CryptoDomain::CourierDirectoryFetchSignature.context().len() + recipient.as_bytes().len(),
    );
    input.extend_from_slice(CryptoDomain::CourierDirectoryFetchSignature.context());
    input.extend_from_slice(recipient.as_bytes());
    input
}

impl fmt::Debug for CourierBundle {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("CourierBundle")
            .field("publisher", &self.publisher)
            .field("prekey_bundle", &"REDACTED")
            .field("relay_invitation", &"REDACTED")
            .field("relay_tls_pin", &"REDACTED")
            .field("generation", &self.generation)
            .finish()
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierBundleError {
    #[error("courier bundle exceeds the configured limit")]
    TooLarge,
    #[error("unsupported courier bundle schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("courier bundle has an invalid shape")]
    InvalidShape,
    #[error("courier bundle contains an invalid publisher identity")]
    InvalidPublisher,
    #[error("courier bundle contains an invalid relay TLS pin")]
    InvalidRelayTlsPin,
    #[error("courier bundle generation must be nonzero")]
    InvalidGeneration,
    #[error("courier bundle prekey bundle is invalid")]
    X3dh(#[source] X3dhError),
    #[error("courier bundle relay invitation is invalid")]
    RelayInvitation(#[source] RelayInvitationError),
    #[error("courier bundle publisher does not match its prekey bundle")]
    PublisherMismatch,
    #[error("courier bundle signature is invalid")]
    InvalidSignature,
    #[error("courier bundle CBOR encoding failed")]
    Encode,
    #[error("courier bundle CBOR decoding failed")]
    Decode,
    #[error("courier bundle has trailing bytes")]
    TrailingBytes,
    #[error("courier bundle is not canonically encoded")]
    NonCanonicalEncoding,
}

fn validate_parts(
    publisher: &IdentityPublicKey,
    prekey_bundle: &X3dhPrekeyBundle,
    relay_invitation: &RelayInvitation,
    relay_tls_pin: &[u8; RELAY_TLS_CERTIFICATE_PIN_BYTES],
) -> Result<(), CourierBundleError> {
    prekey_bundle.verify().map_err(CourierBundleError::X3dh)?;
    if prekey_bundle.identity_binding().signing_identity() != publisher {
        return Err(CourierBundleError::PublisherMismatch);
    }
    if relay_invitation.recipient() != publisher {
        return Err(CourierBundleError::PublisherMismatch);
    }
    if relay_tls_pin.iter().all(|byte| *byte == 0) {
        return Err(CourierBundleError::InvalidRelayTlsPin);
    }
    Ok(())
}

fn signing_input(
    publisher: &IdentityPublicKey,
    prekey_bundle: &X3dhPrekeyBundle,
    relay_invitation: &RelayInvitation,
    relay_tls_pin: &[u8; RELAY_TLS_CERTIFICATE_PIN_BYTES],
    generation: u64,
) -> Result<Vec<u8>, CourierBundleError> {
    let prekey_bundle = prekey_bundle.encode().map_err(CourierBundleError::X3dh)?;
    let relay_invitation = relay_invitation
        .encode()
        .map_err(CourierBundleError::RelayInvitation)?;
    let mut unsigned = Encoder::new(Vec::new());
    unsigned
        .array(UNSIGNED_FIELDS)
        .map_err(|_| CourierBundleError::Encode)?
        .u8(COURIER_BUNDLE_SCHEMA_VERSION)
        .map_err(|_| CourierBundleError::Encode)?
        .bytes(publisher.as_bytes())
        .map_err(|_| CourierBundleError::Encode)?
        .bytes(&prekey_bundle)
        .map_err(|_| CourierBundleError::Encode)?
        .bytes(&relay_invitation)
        .map_err(|_| CourierBundleError::Encode)?
        .bytes(relay_tls_pin)
        .map_err(|_| CourierBundleError::Encode)?
        .u64(generation)
        .map_err(|_| CourierBundleError::Encode)?;
    let mut input = Encoder::new(Vec::new());
    input
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| CourierBundleError::Encode)?
        .bytes(CryptoDomain::CourierBundleSignature.context())
        .map_err(|_| CourierBundleError::Encode)?
        .bytes(&unsigned.into_writer())
        .map_err(|_| CourierBundleError::Encode)?;
    Ok(input.into_writer())
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, CourierBundleError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| CourierBundleError::InvalidPublisher)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| CourierBundleError::InvalidPublisher)
}

fn decode_pin(encoded: &[u8]) -> Result<[u8; RELAY_TLS_CERTIFICATE_PIN_BYTES], CourierBundleError> {
    let pin: [u8; RELAY_TLS_CERTIFICATE_PIN_BYTES] = encoded
        .try_into()
        .map_err(|_| CourierBundleError::InvalidRelayTlsPin)?;
    if pin.iter().all(|byte| *byte == 0) {
        return Err(CourierBundleError::InvalidRelayTlsPin);
    }
    Ok(pin)
}

fn decode_signature(encoded: &[u8]) -> Result<[u8; ED25519_SIGNATURE_BYTES], CourierBundleError> {
    encoded
        .try_into()
        .map_err(|_| CourierBundleError::InvalidSignature)
}

#[cfg(test)]
mod tests {
    use arachne_core::{IdentityKeypair, RelaySigningKeypair, X25519IdentityKeypair};

    use super::{CourierBundle, CourierBundleError, RELAY_TLS_CERTIFICATE_PIN_BYTES};
    use crate::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
        RelayInvitation, SignedPrekey, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
        TorMaildropProfileConfig, X3dhPrekeyBundle,
    };

    fn bundle() -> CourierBundle {
        let publisher = IdentityKeypair::generate().unwrap();
        let exchange_identity = X25519IdentityKeypair::generate().unwrap();
        let signed_prekey = SignedPrekey::generate(&publisher).unwrap();
        let prekeys = X3dhPrekeyBundle::create(
            &publisher,
            &exchange_identity,
            signed_prekey.public(),
            Vec::new(),
        )
        .unwrap();
        let relay = RelaySigningKeypair::generate().unwrap();
        let invitation = RelayInvitation::create(
            &relay,
            publisher.public_key(),
            TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 443).unwrap(),
            MailboxCapability::new(
                [0x22; MAILBOX_IDENTIFIER_BYTES],
                [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
            )
            .unwrap(),
            100,
            60,
        )
        .unwrap();
        CourierBundle::create(
            &publisher,
            prekeys,
            invitation,
            [0x44; RELAY_TLS_CERTIFICATE_PIN_BYTES],
            1,
        )
        .unwrap()
    }

    #[test]
    fn signs_canonically_encoded_recipient_bound_courier_bundles() {
        let bundle = bundle();
        let encoded = bundle.encode().unwrap();
        let decoded = CourierBundle::decode(&encoded).unwrap();
        decoded.validate(120).unwrap();
        assert_eq!(decoded.encode().unwrap(), encoded);
        assert!(format!("{decoded:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_tampered_courier_bundles() {
        let bundle = bundle();
        let mut encoded = bundle.encode().unwrap();
        let last = encoded.len() - 1;
        encoded[last] ^= 1;
        assert!(matches!(
            CourierBundle::decode(&encoded),
            Err(CourierBundleError::InvalidSignature)
        ));
    }
}
