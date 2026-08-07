use std::fmt;

use arachne_core::{ED25519_PUBLIC_KEY_BYTES, IdentityPublicKey};
use minicbor::{Decoder, Encoder, data::Type};

use crate::{
    DeliveryAcknowledgement, DeliveryAcknowledgementError, EncryptedMessageEnvelope,
    EncryptedMessageError, MessageIdentifier, MessageIdentifierError, RatchetMessageEnvelope,
    RatchetMessageError, X3dhError, X3dhInitialMessage,
};

pub const COURIER_FRAME_SCHEMA_VERSION: u8 = 1;
pub const MAX_COURIER_FRAME_BYTES: usize = 1_048_576;
const FRAME_HEADER_FIELDS: u64 = 5;
const BOOTSTRAP_KIND: u8 = 1;
const RATCHET_KIND: u8 = 2;
const ACKNOWLEDGEMENT_KIND: u8 = 3;

#[derive(Clone, Eq, PartialEq)]
pub enum CourierFrame {
    Bootstrap {
        sender: IdentityPublicKey,
        message_identifier: MessageIdentifier,
        initial: X3dhInitialMessage,
        message: RatchetMessageEnvelope,
    },
    Ratchet {
        sender: IdentityPublicKey,
        message_identifier: MessageIdentifier,
        message: RatchetMessageEnvelope,
    },
    Acknowledgement {
        sender: IdentityPublicKey,
        acknowledgement: DeliveryAcknowledgement,
    },
}

impl CourierFrame {
    #[must_use]
    pub const fn sender(&self) -> &IdentityPublicKey {
        match self {
            Self::Bootstrap { sender, .. }
            | Self::Ratchet { sender, .. }
            | Self::Acknowledgement { sender, .. } => sender,
        }
    }

    #[must_use]
    pub const fn message_identifier(&self) -> Option<MessageIdentifier> {
        match self {
            Self::Bootstrap {
                message_identifier, ..
            }
            | Self::Ratchet {
                message_identifier, ..
            } => Some(*message_identifier),
            Self::Acknowledgement { .. } => None,
        }
    }

    pub fn into_envelope(self) -> Result<EncryptedMessageEnvelope, CourierFrameError> {
        let (sender, kind, message_identifier, initial, payload) = match self {
            Self::Bootstrap {
                sender,
                message_identifier,
                initial,
                message,
            } => (
                sender,
                BOOTSTRAP_KIND,
                Some(message_identifier),
                Some(initial.encode().map_err(CourierFrameError::X3dh)?),
                message.encode().map_err(CourierFrameError::Ratchet)?,
            ),
            Self::Ratchet {
                sender,
                message_identifier,
                message,
            } => (
                sender,
                RATCHET_KIND,
                Some(message_identifier),
                None,
                message.encode().map_err(CourierFrameError::Ratchet)?,
            ),
            Self::Acknowledgement {
                sender,
                acknowledgement,
            } => (
                sender,
                ACKNOWLEDGEMENT_KIND,
                None,
                None,
                acknowledgement
                    .encode()
                    .map_err(CourierFrameError::Acknowledgement)?,
            ),
        };
        let header = encode_header(&sender, kind, message_identifier, initial.as_deref())?;
        EncryptedMessageEnvelope::new(header, payload).map_err(CourierFrameError::Envelope)
    }

    pub fn from_envelope(envelope: &EncryptedMessageEnvelope) -> Result<Self, CourierFrameError> {
        let (sender, kind, message_identifier, initial) =
            decode_header(envelope.encrypted_header())?;
        match kind {
            BOOTSTRAP_KIND => Ok(Self::Bootstrap {
                sender,
                message_identifier: message_identifier
                    .ok_or(CourierFrameError::MissingMessageIdentifier)?,
                initial: X3dhInitialMessage::decode(
                    initial
                        .ok_or(CourierFrameError::MissingInitialMessage)?
                        .as_slice(),
                )
                .map_err(CourierFrameError::X3dh)?,
                message: RatchetMessageEnvelope::decode(envelope.ciphertext())
                    .map_err(CourierFrameError::Ratchet)?,
            }),
            RATCHET_KIND => Ok(Self::Ratchet {
                sender,
                message_identifier: message_identifier
                    .ok_or(CourierFrameError::MissingMessageIdentifier)?,
                message: RatchetMessageEnvelope::decode(envelope.ciphertext())
                    .map_err(CourierFrameError::Ratchet)?,
            }),
            ACKNOWLEDGEMENT_KIND => Ok(Self::Acknowledgement {
                sender,
                acknowledgement: DeliveryAcknowledgement::decode(envelope.ciphertext())
                    .map_err(CourierFrameError::Acknowledgement)?,
            }),
            _ => Err(CourierFrameError::UnknownKind),
        }
    }
}

impl fmt::Debug for CourierFrame {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("CourierFrame")
            .field("sender", self.sender())
            .field("content", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierFrameError {
    #[error("courier frame header is invalid")]
    InvalidHeader,
    #[error("courier frame kind is unknown")]
    UnknownKind,
    #[error("courier bootstrap frame requires an X3DH initial message")]
    MissingInitialMessage,
    #[error("courier message frame requires a message identifier")]
    MissingMessageIdentifier,
    #[error("courier acknowledgement frame must not include a message identifier")]
    UnexpectedMessageIdentifier,
    #[error("courier non-bootstrap frame includes an X3DH initial message")]
    UnexpectedInitialMessage,
    #[error("courier frame sender identity is invalid")]
    InvalidSender,
    #[error("courier frame exceeds the configured limit")]
    TooLarge,
    #[error("courier frame CBOR encoding failed")]
    Encode,
    #[error("courier frame CBOR decoding failed")]
    Decode,
    #[error("courier frame has trailing bytes")]
    TrailingBytes,
    #[error("courier frame is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("courier X3DH material is invalid")]
    X3dh(#[source] X3dhError),
    #[error("courier ratchet message is invalid")]
    Ratchet(#[source] RatchetMessageError),
    #[error("courier delivery acknowledgement is invalid")]
    Acknowledgement(#[source] DeliveryAcknowledgementError),
    #[error("courier message identifier is invalid")]
    MessageIdentifier(#[source] MessageIdentifierError),
    #[error("courier transport envelope is invalid")]
    Envelope(#[source] EncryptedMessageError),
}

fn encode_header(
    sender: &IdentityPublicKey,
    kind: u8,
    message_identifier: Option<MessageIdentifier>,
    initial: Option<&[u8]>,
) -> Result<Vec<u8>, CourierFrameError> {
    validate_kind_fields(kind, message_identifier.is_some(), initial.is_some())?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(FRAME_HEADER_FIELDS)
        .map_err(|_| CourierFrameError::Encode)?
        .u8(COURIER_FRAME_SCHEMA_VERSION)
        .map_err(|_| CourierFrameError::Encode)?
        .u8(kind)
        .map_err(|_| CourierFrameError::Encode)?
        .bytes(sender.as_bytes())
        .map_err(|_| CourierFrameError::Encode)?;
    if let Some(message_identifier) = message_identifier {
        encoder
            .bytes(message_identifier.as_bytes())
            .map_err(|_| CourierFrameError::Encode)?;
    } else {
        encoder.null().map_err(|_| CourierFrameError::Encode)?;
    }
    if let Some(initial) = initial {
        encoder
            .bytes(initial)
            .map_err(|_| CourierFrameError::Encode)?;
    } else {
        encoder.null().map_err(|_| CourierFrameError::Encode)?;
    }
    let encoded = encoder.into_writer();
    if encoded.len() > MAX_COURIER_FRAME_BYTES {
        return Err(CourierFrameError::TooLarge);
    }
    Ok(encoded)
}

fn decode_header(
    encoded: &[u8],
) -> Result<
    (
        IdentityPublicKey,
        u8,
        Option<MessageIdentifier>,
        Option<Vec<u8>>,
    ),
    CourierFrameError,
> {
    if encoded.len() > MAX_COURIER_FRAME_BYTES {
        return Err(CourierFrameError::TooLarge);
    }
    let mut decoder = Decoder::new(encoded);
    if decoder.array().map_err(|_| CourierFrameError::Decode)? != Some(FRAME_HEADER_FIELDS) {
        return Err(CourierFrameError::InvalidHeader);
    }
    let version = decoder.u8().map_err(|_| CourierFrameError::Decode)?;
    if version != COURIER_FRAME_SCHEMA_VERSION {
        return Err(CourierFrameError::InvalidHeader);
    }
    let kind = decoder.u8().map_err(|_| CourierFrameError::Decode)?;
    let sender = decode_sender(decoder.bytes().map_err(|_| CourierFrameError::Decode)?)?;
    let message_identifier = match decoder.datatype().map_err(|_| CourierFrameError::Decode)? {
        Type::Null => {
            decoder.null().map_err(|_| CourierFrameError::Decode)?;
            None
        }
        Type::Bytes => Some(
            MessageIdentifier::from_bytes(
                decoder
                    .bytes()
                    .map_err(|_| CourierFrameError::Decode)?
                    .try_into()
                    .map_err(|_| CourierFrameError::InvalidHeader)?,
            )
            .map_err(CourierFrameError::MessageIdentifier)?,
        ),
        _ => return Err(CourierFrameError::InvalidHeader),
    };
    let initial = match decoder.datatype().map_err(|_| CourierFrameError::Decode)? {
        Type::Null => {
            decoder.null().map_err(|_| CourierFrameError::Decode)?;
            None
        }
        Type::Bytes => Some(
            decoder
                .bytes()
                .map_err(|_| CourierFrameError::Decode)?
                .to_vec(),
        ),
        _ => return Err(CourierFrameError::InvalidHeader),
    };
    if decoder.position() != encoded.len() {
        return Err(CourierFrameError::TrailingBytes);
    }
    validate_kind_fields(kind, message_identifier.is_some(), initial.is_some())?;
    if encode_header(&sender, kind, message_identifier, initial.as_deref())? != encoded {
        return Err(CourierFrameError::NonCanonicalEncoding);
    }
    Ok((sender, kind, message_identifier, initial))
}

fn validate_kind_fields(
    kind: u8,
    has_message_identifier: bool,
    has_initial: bool,
) -> Result<(), CourierFrameError> {
    match (kind, has_message_identifier, has_initial) {
        (BOOTSTRAP_KIND, true, true) => Ok(()),
        (BOOTSTRAP_KIND, _, false) => Err(CourierFrameError::MissingInitialMessage),
        (BOOTSTRAP_KIND, false, true) | (RATCHET_KIND, false, false) => {
            Err(CourierFrameError::MissingMessageIdentifier)
        }
        (RATCHET_KIND, true, false) | (ACKNOWLEDGEMENT_KIND, false, false) => Ok(()),
        (RATCHET_KIND | ACKNOWLEDGEMENT_KIND, _, true) => {
            Err(CourierFrameError::UnexpectedInitialMessage)
        }
        (ACKNOWLEDGEMENT_KIND, true, false) => Err(CourierFrameError::UnexpectedMessageIdentifier),
        _ => Err(CourierFrameError::UnknownKind),
    }
}

fn decode_sender(encoded: &[u8]) -> Result<IdentityPublicKey, CourierFrameError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| CourierFrameError::InvalidSender)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| CourierFrameError::InvalidSender)
}

#[cfg(test)]
mod tests {
    use arachne_core::IdentityKeypair;

    use super::CourierFrame;
    use crate::{DeliveryAcknowledgement, MessageIdentifier};

    #[test]
    fn transports_recipient_signed_acknowledgements_without_an_outer_message_identifier() {
        let recipient = IdentityKeypair::generate().unwrap();
        let identifier = MessageIdentifier::generate().unwrap();
        let acknowledgement = DeliveryAcknowledgement::create(&recipient, identifier, 100).unwrap();
        let frame = CourierFrame::Acknowledgement {
            sender: recipient.public_key(),
            acknowledgement,
        };
        assert_eq!(frame.message_identifier(), None);
        let envelope = frame.clone().into_envelope().unwrap();
        assert_eq!(CourierFrame::from_envelope(&envelope).unwrap(), frame);
    }
}
