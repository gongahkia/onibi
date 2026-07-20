use std::fmt;

use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use minicbor::{Decoder, Encoder};
use yeokcham_core::{X25519Prekey, X25519PrekeyPublicKey};
use zeroize::Zeroizing;

use crate::{
    CryptoDomain, DoubleRatchetError, DoubleRatchetState, MessagePayload, MessagePayloadError,
    X3dhSession,
};

pub const RATCHET_MESSAGE_HEADER_SCHEMA_VERSION: u8 = 1;
pub const RATCHET_MESSAGE_ENVELOPE_SCHEMA_VERSION: u8 = 1;
pub const RATCHET_MESSAGE_SESSION_SCHEMA_VERSION: u8 = 1;
pub const RATCHET_MESSAGE_NONCE_BYTES: usize = 24;
pub const MAX_RATCHET_MESSAGE_CIPHERTEXT_BYTES: usize = 1_048_448;
pub const MAX_RATCHET_MESSAGE_ENVELOPE_BYTES: usize = 1_048_544;
pub const MAX_RATCHET_MESSAGE_SESSION_BYTES: usize = 128 * 1024;
const MAX_ASSOCIATED_DATA_BYTES: usize = 4 * 1024;
const AEAD_TAG_BYTES: usize = 16;
const HEADER_FIELDS: u64 = 5;
const ENVELOPE_FIELDS: u64 = 3;
const SESSION_FIELDS: u64 = 3;

#[derive(Clone, Eq, PartialEq)]
pub struct RatchetMessageHeader {
    ratchet_public: X25519PrekeyPublicKey,
    previous_sending_count: u32,
    message_number: u32,
    nonce: [u8; RATCHET_MESSAGE_NONCE_BYTES],
}

impl RatchetMessageHeader {
    #[must_use]
    pub const fn new(
        ratchet_public: X25519PrekeyPublicKey,
        previous_sending_count: u32,
        message_number: u32,
        nonce: [u8; RATCHET_MESSAGE_NONCE_BYTES],
    ) -> Self {
        Self {
            ratchet_public,
            previous_sending_count,
            message_number,
            nonce,
        }
    }

    #[must_use]
    pub const fn ratchet_public(&self) -> X25519PrekeyPublicKey {
        self.ratchet_public
    }

    #[must_use]
    pub const fn previous_sending_count(&self) -> u32 {
        self.previous_sending_count
    }

    #[must_use]
    pub const fn message_number(&self) -> u32 {
        self.message_number
    }

    #[must_use]
    pub const fn nonce(&self) -> &[u8; RATCHET_MESSAGE_NONCE_BYTES] {
        &self.nonce
    }

    pub fn encode(&self) -> Result<Vec<u8>, RatchetMessageError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(HEADER_FIELDS)
            .map_err(|_| RatchetMessageError::Encode)?
            .u8(RATCHET_MESSAGE_HEADER_SCHEMA_VERSION)
            .map_err(|_| RatchetMessageError::Encode)?
            .bytes(self.ratchet_public.as_bytes())
            .map_err(|_| RatchetMessageError::Encode)?
            .u32(self.previous_sending_count)
            .map_err(|_| RatchetMessageError::Encode)?
            .u32(self.message_number)
            .map_err(|_| RatchetMessageError::Encode)?
            .bytes(&self.nonce)
            .map_err(|_| RatchetMessageError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, RatchetMessageError> {
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| RatchetMessageError::Decode)? != Some(HEADER_FIELDS) {
            return Err(RatchetMessageError::InvalidHeaderShape);
        }
        let version = decoder.u8().map_err(|_| RatchetMessageError::Decode)?;
        if version != RATCHET_MESSAGE_HEADER_SCHEMA_VERSION {
            return Err(RatchetMessageError::UnsupportedHeaderSchemaVersion(version));
        }
        let ratchet_public =
            decode_ratchet_public(decoder.bytes().map_err(|_| RatchetMessageError::Decode)?)?;
        let previous_sending_count = decoder.u32().map_err(|_| RatchetMessageError::Decode)?;
        let message_number = decoder.u32().map_err(|_| RatchetMessageError::Decode)?;
        let nonce: [u8; RATCHET_MESSAGE_NONCE_BYTES] = decoder
            .bytes()
            .map_err(|_| RatchetMessageError::Decode)?
            .try_into()
            .map_err(|_| RatchetMessageError::InvalidNonce)?;
        if decoder.position() != encoded.len() {
            return Err(RatchetMessageError::TrailingBytes);
        }
        let header = Self {
            ratchet_public,
            previous_sending_count,
            message_number,
            nonce,
        };
        if header.encode()? != encoded {
            return Err(RatchetMessageError::NonCanonicalEncoding);
        }
        Ok(header)
    }
}

impl fmt::Debug for RatchetMessageHeader {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("RatchetMessageHeader")
            .field("ratchet_public", &self.ratchet_public)
            .field("previous_sending_count", &self.previous_sending_count)
            .field("message_number", &self.message_number)
            .field("nonce", &self.nonce)
            .finish()
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct RatchetMessageEnvelope {
    header: RatchetMessageHeader,
    ciphertext: Vec<u8>,
}

impl RatchetMessageEnvelope {
    pub fn new(
        header: RatchetMessageHeader,
        ciphertext: Vec<u8>,
    ) -> Result<Self, RatchetMessageError> {
        validate_ciphertext(&ciphertext)?;
        Ok(Self { header, ciphertext })
    }

    #[must_use]
    pub const fn header(&self) -> &RatchetMessageHeader {
        &self.header
    }

    #[must_use]
    pub fn ciphertext(&self) -> &[u8] {
        &self.ciphertext
    }

    pub fn encode(&self) -> Result<Vec<u8>, RatchetMessageError> {
        validate_ciphertext(&self.ciphertext)?;
        let header = self.header.encode()?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ENVELOPE_FIELDS)
            .map_err(|_| RatchetMessageError::Encode)?
            .u8(RATCHET_MESSAGE_ENVELOPE_SCHEMA_VERSION)
            .map_err(|_| RatchetMessageError::Encode)?
            .bytes(&header)
            .map_err(|_| RatchetMessageError::Encode)?
            .bytes(&self.ciphertext)
            .map_err(|_| RatchetMessageError::Encode)?;
        let output = encoder.into_writer();
        if output.len() > MAX_RATCHET_MESSAGE_ENVELOPE_BYTES {
            return Err(RatchetMessageError::EnvelopeTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, RatchetMessageError> {
        if encoded.len() > MAX_RATCHET_MESSAGE_ENVELOPE_BYTES {
            return Err(RatchetMessageError::EnvelopeTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| RatchetMessageError::Decode)? != Some(ENVELOPE_FIELDS) {
            return Err(RatchetMessageError::InvalidEnvelopeShape);
        }
        let version = decoder.u8().map_err(|_| RatchetMessageError::Decode)?;
        if version != RATCHET_MESSAGE_ENVELOPE_SCHEMA_VERSION {
            return Err(RatchetMessageError::UnsupportedEnvelopeSchemaVersion(
                version,
            ));
        }
        let header = RatchetMessageHeader::decode(
            decoder.bytes().map_err(|_| RatchetMessageError::Decode)?,
        )?;
        let ciphertext = decoder.bytes().map_err(|_| RatchetMessageError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(RatchetMessageError::TrailingBytes);
        }
        let envelope = Self::new(header, ciphertext.to_vec())?;
        if envelope.encode()? != encoded {
            return Err(RatchetMessageError::NonCanonicalEncoding);
        }
        Ok(envelope)
    }
}

impl fmt::Debug for RatchetMessageEnvelope {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("RatchetMessageEnvelope")
            .field("header", &self.header)
            .field("ciphertext", &"REDACTED")
            .finish()
    }
}

pub struct RatchetMessageSession {
    ratchet: DoubleRatchetState,
    associated_data: Vec<u8>,
}

impl RatchetMessageSession {
    pub fn initiate(
        x3dh: &X3dhSession,
        remote_initial_ratchet: X25519PrekeyPublicKey,
    ) -> Result<Self, RatchetMessageError> {
        Self::from_parts(
            DoubleRatchetState::from_x3dh(x3dh, remote_initial_ratchet)
                .map_err(RatchetMessageError::Ratchet)?,
            x3dh.associated_data().to_vec(),
        )
    }

    pub fn respond(
        x3dh: &X3dhSession,
        initial_local_ratchet: &X25519Prekey,
        remote_initial_ratchet: X25519PrekeyPublicKey,
    ) -> Result<Self, RatchetMessageError> {
        Self::from_parts(
            DoubleRatchetState::initialize_responder(
                *x3dh.root_key(),
                initial_local_ratchet,
                remote_initial_ratchet,
            )
            .map_err(RatchetMessageError::Ratchet)?,
            x3dh.associated_data().to_vec(),
        )
    }

    fn from_parts(
        ratchet: DoubleRatchetState,
        associated_data: Vec<u8>,
    ) -> Result<Self, RatchetMessageError> {
        validate_associated_data(&associated_data)?;
        Ok(Self {
            ratchet,
            associated_data,
        })
    }

    #[must_use]
    pub fn local_ratchet_public(&self) -> X25519PrekeyPublicKey {
        self.ratchet.local_ratchet_public()
    }

    pub fn encrypt(
        &mut self,
        payload: &MessagePayload,
    ) -> Result<RatchetMessageEnvelope, RatchetMessageError> {
        let plaintext = payload.encode().map_err(RatchetMessageError::Payload)?;
        if plaintext.len() > MAX_RATCHET_MESSAGE_CIPHERTEXT_BYTES - AEAD_TAG_BYTES {
            return Err(RatchetMessageError::PayloadTooLarge);
        }
        let mut nonce = [0; RATCHET_MESSAGE_NONCE_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut nonce)
            .map_err(|_| RatchetMessageError::Randomness)?;
        let header = RatchetMessageHeader::new(
            self.ratchet.local_ratchet_public(),
            self.ratchet.previous_sending_count(),
            self.ratchet.sending_count(),
            nonce,
        );
        let mut candidate = clone_ratchet(&self.ratchet)?;
        let key = candidate
            .next_sending_key()
            .map_err(RatchetMessageError::Ratchet)?;
        let cipher = XChaCha20Poly1305::new_from_slice(&*key)
            .map_err(|_| RatchetMessageError::Encryption)?;
        let nonce = XNonce::try_from(header.nonce().as_slice())
            .map_err(|_| RatchetMessageError::Encryption)?;
        let associated_data = associated_data(&self.associated_data, &header)?;
        let ciphertext = cipher
            .encrypt(
                &nonce,
                Payload {
                    msg: &plaintext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| RatchetMessageError::Encryption)?;
        let envelope = RatchetMessageEnvelope::new(header, ciphertext)?;
        self.ratchet = candidate;
        Ok(envelope)
    }

    pub fn decrypt(
        &mut self,
        envelope: &RatchetMessageEnvelope,
    ) -> Result<MessagePayload, RatchetMessageError> {
        validate_ciphertext(envelope.ciphertext())?;
        let mut candidate = clone_ratchet(&self.ratchet)?;
        let key = candidate
            .receive_key(
                envelope.header.ratchet_public(),
                envelope.header.previous_sending_count(),
                envelope.header.message_number(),
            )
            .map_err(RatchetMessageError::Ratchet)?;
        let cipher = XChaCha20Poly1305::new_from_slice(&*key)
            .map_err(|_| RatchetMessageError::Authentication)?;
        let nonce = XNonce::try_from(envelope.header.nonce().as_slice())
            .map_err(|_| RatchetMessageError::Authentication)?;
        let associated_data = associated_data(&self.associated_data, envelope.header())?;
        let plaintext = Zeroizing::new(
            cipher
                .decrypt(
                    &nonce,
                    Payload {
                        msg: envelope.ciphertext(),
                        aad: &associated_data,
                    },
                )
                .map_err(|_| RatchetMessageError::Authentication)?,
        );
        let payload = MessagePayload::decode(&plaintext).map_err(RatchetMessageError::Payload)?;
        self.ratchet = candidate;
        Ok(payload)
    }

    pub fn encode(&self) -> Result<Zeroizing<Vec<u8>>, RatchetMessageError> {
        validate_associated_data(&self.associated_data)?;
        let ratchet = self
            .ratchet
            .encode()
            .map_err(RatchetMessageError::Ratchet)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(SESSION_FIELDS)
            .map_err(|_| RatchetMessageError::Encode)?
            .u8(RATCHET_MESSAGE_SESSION_SCHEMA_VERSION)
            .map_err(|_| RatchetMessageError::Encode)?
            .bytes(&ratchet)
            .map_err(|_| RatchetMessageError::Encode)?
            .bytes(&self.associated_data)
            .map_err(|_| RatchetMessageError::Encode)?;
        let output = encoder.into_writer();
        if output.len() > MAX_RATCHET_MESSAGE_SESSION_BYTES {
            return Err(RatchetMessageError::SessionTooLarge);
        }
        Ok(Zeroizing::new(output))
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, RatchetMessageError> {
        if encoded.len() > MAX_RATCHET_MESSAGE_SESSION_BYTES {
            return Err(RatchetMessageError::SessionTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| RatchetMessageError::Decode)? != Some(SESSION_FIELDS) {
            return Err(RatchetMessageError::InvalidSessionShape);
        }
        let version = decoder.u8().map_err(|_| RatchetMessageError::Decode)?;
        if version != RATCHET_MESSAGE_SESSION_SCHEMA_VERSION {
            return Err(RatchetMessageError::UnsupportedSessionSchemaVersion(
                version,
            ));
        }
        let ratchet =
            DoubleRatchetState::decode(decoder.bytes().map_err(|_| RatchetMessageError::Decode)?)
                .map_err(RatchetMessageError::Ratchet)?;
        let associated_data = decoder.bytes().map_err(|_| RatchetMessageError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(RatchetMessageError::TrailingBytes);
        }
        let session = Self::from_parts(ratchet, associated_data.to_vec())?;
        if *session.encode()? != encoded {
            return Err(RatchetMessageError::NonCanonicalEncoding);
        }
        Ok(session)
    }
}

impl fmt::Debug for RatchetMessageSession {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("RatchetMessageSession")
            .field("ratchet", &"REDACTED")
            .field("associated_data", &self.associated_data)
            .finish()
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RatchetMessageError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("ratchet transition failed")]
    Ratchet(#[source] DoubleRatchetError),
    #[error("message payload is invalid")]
    Payload(#[source] MessagePayloadError),
    #[error("X3DH associated data is required")]
    MissingAssociatedData,
    #[error("X3DH associated data exceeds the configured limit")]
    AssociatedDataTooLarge,
    #[error("ratchet message payload exceeds the encrypted-envelope limit")]
    PayloadTooLarge,
    #[error("ratchet message encryption failed")]
    Encryption,
    #[error("ratchet message authentication failed")]
    Authentication,
    #[error("ratchet message ciphertext is invalid")]
    InvalidCiphertext,
    #[error("ratchet message nonce is invalid")]
    InvalidNonce,
    #[error("ratchet message header has an invalid ratchet public key")]
    InvalidRatchetPublic,
    #[error("ratchet message header has an invalid shape")]
    InvalidHeaderShape,
    #[error("ratchet message envelope has an invalid shape")]
    InvalidEnvelopeShape,
    #[error("ratchet message session has an invalid shape")]
    InvalidSessionShape,
    #[error("ratchet message header schema version is unsupported: {0}")]
    UnsupportedHeaderSchemaVersion(u8),
    #[error("ratchet message envelope schema version is unsupported: {0}")]
    UnsupportedEnvelopeSchemaVersion(u8),
    #[error("ratchet message session schema version is unsupported: {0}")]
    UnsupportedSessionSchemaVersion(u8),
    #[error("ratchet message envelope exceeds the configured limit")]
    EnvelopeTooLarge,
    #[error("ratchet message session exceeds the configured limit")]
    SessionTooLarge,
    #[error("ratchet message CBOR encoding failed")]
    Encode,
    #[error("ratchet message CBOR decoding failed")]
    Decode,
    #[error("ratchet message has trailing bytes")]
    TrailingBytes,
    #[error("ratchet message is not canonically encoded")]
    NonCanonicalEncoding,
}

fn validate_associated_data(associated_data: &[u8]) -> Result<(), RatchetMessageError> {
    if associated_data.is_empty() {
        return Err(RatchetMessageError::MissingAssociatedData);
    }
    if associated_data.len() > MAX_ASSOCIATED_DATA_BYTES {
        return Err(RatchetMessageError::AssociatedDataTooLarge);
    }
    Ok(())
}

fn validate_ciphertext(ciphertext: &[u8]) -> Result<(), RatchetMessageError> {
    if !(AEAD_TAG_BYTES..=MAX_RATCHET_MESSAGE_CIPHERTEXT_BYTES).contains(&ciphertext.len()) {
        return Err(RatchetMessageError::InvalidCiphertext);
    }
    Ok(())
}

fn decode_ratchet_public(encoded: &[u8]) -> Result<X25519PrekeyPublicKey, RatchetMessageError> {
    let bytes = encoded
        .try_into()
        .map_err(|_| RatchetMessageError::InvalidRatchetPublic)?;
    X25519PrekeyPublicKey::from_bytes(bytes).map_err(|_| RatchetMessageError::InvalidRatchetPublic)
}

fn clone_ratchet(ratchet: &DoubleRatchetState) -> Result<DoubleRatchetState, RatchetMessageError> {
    let encoded = ratchet.encode().map_err(RatchetMessageError::Ratchet)?;
    DoubleRatchetState::decode(&encoded).map_err(RatchetMessageError::Ratchet)
}

fn associated_data(
    session_associated_data: &[u8],
    header: &RatchetMessageHeader,
) -> Result<Vec<u8>, RatchetMessageError> {
    let header = header.encode()?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(3)
        .map_err(|_| RatchetMessageError::Encode)?
        .bytes(CryptoDomain::RatchetMessageAead.context())
        .map_err(|_| RatchetMessageError::Encode)?
        .bytes(session_associated_data)
        .map_err(|_| RatchetMessageError::Encode)?
        .bytes(&header)
        .map_err(|_| RatchetMessageError::Encode)?;
    Ok(encoder.into_writer())
}

#[cfg(test)]
mod tests {
    use yeokcham_core::{IdentityKeypair, X25519IdentityKeypair, X25519Prekey};

    use super::{
        RATCHET_MESSAGE_SESSION_SCHEMA_VERSION, RatchetMessageEnvelope, RatchetMessageError,
        RatchetMessageHeader, RatchetMessageSession,
    };
    use crate::{
        MessageContentType, MessagePayload, SignedPrekey, X3dhPrekeyBundle, X25519IdentityBinding,
        initiate_x3dh, respond_x3dh,
    };

    fn pair() -> (RatchetMessageSession, RatchetMessageSession) {
        let initial_responder_ratchet = X25519Prekey::generate().unwrap();
        let associated_data = b"x3dh-associated-data".to_vec();
        let initiator = RatchetMessageSession::from_parts(
            crate::DoubleRatchetState::initialize(
                [0x44; 32],
                initial_responder_ratchet.public_key(),
            )
            .unwrap(),
            associated_data.clone(),
        )
        .unwrap();
        let responder = RatchetMessageSession::from_parts(
            crate::DoubleRatchetState::initialize_responder(
                [0x44; 32],
                &initial_responder_ratchet,
                initiator.local_ratchet_public(),
            )
            .unwrap(),
            associated_data,
        )
        .unwrap();
        (initiator, responder)
    }

    fn text(value: &str) -> MessagePayload {
        MessagePayload::new(MessageContentType::TextUtf8, value.as_bytes().to_vec()).unwrap()
    }

    #[test]
    fn encrypts_two_way_messages_with_canonical_persistent_state() {
        let (mut initiator, mut responder) = pair();
        let first = initiator.encrypt(&text("first")).unwrap();
        assert_eq!(responder.decrypt(&first).unwrap(), text("first"));

        let persisted = responder.encode().unwrap();
        let mut restored = RatchetMessageSession::decode(&persisted).unwrap();
        let second = restored.encrypt(&text("second")).unwrap();
        assert_eq!(initiator.decrypt(&second).unwrap(), text("second"));
    }

    #[test]
    fn establishes_the_ratchet_from_authenticated_x3dh() {
        let initiator_signing = IdentityKeypair::generate().unwrap();
        let initiator_exchange = X25519IdentityKeypair::generate().unwrap();
        let initiator_binding =
            X25519IdentityBinding::create(&initiator_signing, &initiator_exchange).unwrap();
        let responder_signing = IdentityKeypair::generate().unwrap();
        let responder_exchange = X25519IdentityKeypair::generate().unwrap();
        let responder_binding =
            X25519IdentityBinding::create(&responder_signing, &responder_exchange).unwrap();
        let signed_prekey = SignedPrekey::generate(&responder_signing).unwrap();
        let bundle = X3dhPrekeyBundle::create(
            &responder_signing,
            &responder_exchange,
            signed_prekey.public(),
            Vec::new(),
        )
        .unwrap();
        let (initial, initiator_x3dh) =
            initiate_x3dh(&initiator_exchange, initiator_binding, &bundle).unwrap();
        let responder_x3dh = respond_x3dh(
            &responder_exchange,
            &responder_binding,
            &signed_prekey,
            None,
            &initial,
        )
        .unwrap();
        let mut initiator =
            RatchetMessageSession::initiate(&initiator_x3dh, *signed_prekey.public().prekey())
                .unwrap();
        let mut responder = RatchetMessageSession::respond(
            &responder_x3dh,
            signed_prekey.prekey(),
            initiator.local_ratchet_public(),
        )
        .unwrap();

        let envelope = initiator.encrypt(&text("authenticated")).unwrap();
        assert_eq!(responder.decrypt(&envelope).unwrap(), text("authenticated"));
    }

    #[test]
    fn authentication_failures_do_not_advance_ratchet_state() {
        let (mut initiator, mut responder) = pair();
        let envelope = initiator.encrypt(&text("protected")).unwrap();
        let before = responder.encode().unwrap();
        let mut tampered_ciphertext = envelope.ciphertext().to_vec();
        tampered_ciphertext[0] ^= 1;
        let tampered_ciphertext =
            RatchetMessageEnvelope::new(envelope.header().clone(), tampered_ciphertext).unwrap();
        assert!(matches!(
            responder.decrypt(&tampered_ciphertext),
            Err(RatchetMessageError::Authentication)
        ));
        assert_eq!(&*responder.encode().unwrap(), &*before);

        let header = envelope.header();
        let tampered_header = RatchetMessageEnvelope::new(
            RatchetMessageHeader::new(
                header.ratchet_public(),
                header.previous_sending_count(),
                header.message_number().checked_add(1).unwrap(),
                *header.nonce(),
            ),
            envelope.ciphertext().to_vec(),
        )
        .unwrap();
        assert!(matches!(
            responder.decrypt(&tampered_header),
            Err(RatchetMessageError::Authentication)
        ));
        assert_eq!(&*responder.encode().unwrap(), &*before);
        assert_eq!(responder.decrypt(&envelope).unwrap(), text("protected"));
    }

    #[test]
    fn rejects_replay_without_changing_persisted_state() {
        let (mut initiator, mut responder) = pair();
        let envelope = initiator.encrypt(&text("once")).unwrap();
        assert_eq!(responder.decrypt(&envelope).unwrap(), text("once"));
        let before = responder.encode().unwrap();
        assert!(matches!(
            responder.decrypt(&envelope),
            Err(RatchetMessageError::Ratchet(_))
        ));
        assert_eq!(&*responder.encode().unwrap(), &*before);
    }

    #[test]
    fn rejects_malformed_or_noncanonical_session_state() {
        let (initiator, _) = pair();
        let encoded = initiator.encode().unwrap();
        let mut unsupported = encoded.to_vec();
        unsupported[1] = RATCHET_MESSAGE_SESSION_SCHEMA_VERSION + 1;
        assert!(matches!(
            RatchetMessageSession::decode(&unsupported),
            Err(RatchetMessageError::UnsupportedSessionSchemaVersion(_))
        ));
        let mut trailing = encoded.to_vec();
        trailing.push(0);
        assert!(matches!(
            RatchetMessageSession::decode(&trailing),
            Err(RatchetMessageError::TrailingBytes)
        ));
    }
}
