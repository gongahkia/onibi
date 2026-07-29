use arachne_core::{ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey};
use minicbor::{Decoder, Encoder};

use crate::{CryptoDomain, MessageIdentifier};

pub const DELIVERY_ACKNOWLEDGEMENT_SCHEMA_VERSION: u8 = 1;
const DELIVERY_ACKNOWLEDGEMENT_FIELDS: u64 = 5;
const DELIVERY_ACKNOWLEDGEMENT_UNSIGNED_FIELDS: u64 = 4;
const SIGNING_INPUT_FIELDS: u64 = 2;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeliveryAcknowledgement {
    recipient: IdentityPublicKey,
    message_identifier: MessageIdentifier,
    received_at: u64,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl DeliveryAcknowledgement {
    pub fn create(
        recipient: &IdentityKeypair,
        message_identifier: MessageIdentifier,
        received_at: u64,
    ) -> Result<Self, DeliveryAcknowledgementError> {
        let recipient_public = recipient.public_key();
        let signature = recipient.sign(&signing_input(
            &recipient_public,
            message_identifier,
            received_at,
        )?);
        Ok(Self {
            recipient: recipient_public,
            message_identifier,
            received_at,
            signature,
        })
    }

    #[must_use]
    pub const fn recipient(&self) -> &IdentityPublicKey {
        &self.recipient
    }

    #[must_use]
    pub const fn message_identifier(&self) -> MessageIdentifier {
        self.message_identifier
    }

    #[must_use]
    pub const fn received_at(&self) -> u64 {
        self.received_at
    }

    pub fn verify(&self) -> Result<(), DeliveryAcknowledgementError> {
        self.recipient
            .verify(
                &signing_input(&self.recipient, self.message_identifier, self.received_at)?,
                &self.signature,
            )
            .map_err(|_| DeliveryAcknowledgementError::InvalidSignature)
    }

    pub fn verify_for(
        &self,
        expected_recipient: &IdentityPublicKey,
        expected_message_identifier: MessageIdentifier,
    ) -> Result<(), DeliveryAcknowledgementError> {
        self.verify()?;
        if self.recipient != *expected_recipient {
            return Err(DeliveryAcknowledgementError::UnexpectedRecipient);
        }
        if self.message_identifier != expected_message_identifier {
            return Err(DeliveryAcknowledgementError::UnexpectedMessageIdentifier);
        }
        Ok(())
    }

    pub fn encode(&self) -> Result<Vec<u8>, DeliveryAcknowledgementError> {
        self.verify()?;
        encode_acknowledgement(
            &self.recipient,
            self.message_identifier,
            self.received_at,
            &self.signature,
        )
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, DeliveryAcknowledgementError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| DeliveryAcknowledgementError::Decode)?
            != Some(DELIVERY_ACKNOWLEDGEMENT_FIELDS)
        {
            return Err(DeliveryAcknowledgementError::InvalidShape);
        }
        let version = decoder
            .u8()
            .map_err(|_| DeliveryAcknowledgementError::Decode)?;
        if version != DELIVERY_ACKNOWLEDGEMENT_SCHEMA_VERSION {
            return Err(DeliveryAcknowledgementError::UnsupportedSchemaVersion(
                version,
            ));
        }
        let recipient = IdentityPublicKey::from_bytes(
            decoder
                .bytes()
                .map_err(|_| DeliveryAcknowledgementError::Decode)?
                .try_into()
                .map_err(|_| DeliveryAcknowledgementError::InvalidRecipient)?,
        )
        .map_err(|_| DeliveryAcknowledgementError::InvalidRecipient)?;
        let message_identifier = MessageIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| DeliveryAcknowledgementError::Decode)?
                .try_into()
                .map_err(|_| DeliveryAcknowledgementError::InvalidMessageIdentifier)?,
        )
        .map_err(|_| DeliveryAcknowledgementError::InvalidMessageIdentifier)?;
        let received_at = decoder
            .u64()
            .map_err(|_| DeliveryAcknowledgementError::Decode)?;
        let signature = decoder
            .bytes()
            .map_err(|_| DeliveryAcknowledgementError::Decode)?
            .try_into()
            .map_err(|_| DeliveryAcknowledgementError::InvalidSignature)?;
        if decoder.position() != encoded.len() {
            return Err(DeliveryAcknowledgementError::TrailingBytes);
        }
        let acknowledgement = Self {
            recipient,
            message_identifier,
            received_at,
            signature,
        };
        acknowledgement.verify()?;
        if acknowledgement.encode()? != encoded {
            return Err(DeliveryAcknowledgementError::NonCanonicalEncoding);
        }
        Ok(acknowledgement)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum DeliveryAcknowledgementError {
    #[error("unsupported delivery-acknowledgement schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("delivery acknowledgement has an invalid recipient")]
    InvalidRecipient,
    #[error("delivery acknowledgement has an invalid message identifier")]
    InvalidMessageIdentifier,
    #[error("delivery acknowledgement signature is invalid")]
    InvalidSignature,
    #[error("delivery acknowledgement was signed by an unexpected recipient")]
    UnexpectedRecipient,
    #[error("delivery acknowledgement is for an unexpected message identifier")]
    UnexpectedMessageIdentifier,
    #[error("delivery acknowledgement must be a five-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after delivery acknowledgement")]
    TrailingBytes,
    #[error("delivery acknowledgement is not canonically encoded")]
    NonCanonicalEncoding,
}

fn signing_input(
    recipient: &IdentityPublicKey,
    message_identifier: MessageIdentifier,
    received_at: u64,
) -> Result<Vec<u8>, DeliveryAcknowledgementError> {
    let unsigned = encode_unsigned(recipient, message_identifier, received_at)?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(CryptoDomain::DeliveryAcknowledgementSignature.context())
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?;
    Ok(encoder.into_writer())
}

fn encode_unsigned(
    recipient: &IdentityPublicKey,
    message_identifier: MessageIdentifier,
    received_at: u64,
) -> Result<Vec<u8>, DeliveryAcknowledgementError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(DELIVERY_ACKNOWLEDGEMENT_UNSIGNED_FIELDS)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .u8(DELIVERY_ACKNOWLEDGEMENT_SCHEMA_VERSION)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(recipient.as_bytes())
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(message_identifier.as_bytes())
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .u64(received_at)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?;
    Ok(encoder.into_writer())
}

fn encode_acknowledgement(
    recipient: &IdentityPublicKey,
    message_identifier: MessageIdentifier,
    received_at: u64,
    signature: &[u8; ED25519_SIGNATURE_BYTES],
) -> Result<Vec<u8>, DeliveryAcknowledgementError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(DELIVERY_ACKNOWLEDGEMENT_FIELDS)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .u8(DELIVERY_ACKNOWLEDGEMENT_SCHEMA_VERSION)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(recipient.as_bytes())
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(message_identifier.as_bytes())
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .u64(received_at)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?
        .bytes(signature)
        .map_err(|_| DeliveryAcknowledgementError::Encode)?;
    Ok(encoder.into_writer())
}

#[cfg(test)]
mod tests {
    use super::{DeliveryAcknowledgement, DeliveryAcknowledgementError};
    use crate::MessageIdentifier;
    use arachne_core::IdentityKeypair;

    #[test]
    fn creates_and_canonically_round_trips_recipient_acknowledgements() {
        let recipient = IdentityKeypair::generate().unwrap();
        let identifier = MessageIdentifier::generate().unwrap();
        let acknowledgement = DeliveryAcknowledgement::create(&recipient, identifier, 100).unwrap();
        let encoded = acknowledgement.encode().unwrap();

        assert_eq!(
            DeliveryAcknowledgement::decode(&encoded).unwrap(),
            acknowledgement
        );
        assert_eq!(acknowledgement.recipient(), &recipient.public_key());
        assert_eq!(acknowledgement.message_identifier(), identifier);
        assert_eq!(acknowledgement.received_at(), 100);
    }

    #[test]
    fn rejects_tampered_or_mismatched_recipient_acknowledgements() {
        let recipient = IdentityKeypair::generate().unwrap();
        let other_recipient = IdentityKeypair::generate().unwrap();
        let identifier = MessageIdentifier::generate().unwrap();
        let other_identifier = MessageIdentifier::generate().unwrap();
        let acknowledgement = DeliveryAcknowledgement::create(&recipient, identifier, 100).unwrap();
        let mut encoded = acknowledgement.encode().unwrap();
        let last = encoded.len() - 1;
        encoded[last] ^= 1;

        assert_eq!(
            DeliveryAcknowledgement::decode(&encoded).unwrap_err(),
            DeliveryAcknowledgementError::InvalidSignature
        );
        assert_eq!(
            acknowledgement
                .verify_for(&other_recipient.public_key(), identifier)
                .unwrap_err(),
            DeliveryAcknowledgementError::UnexpectedRecipient
        );
        assert_eq!(
            acknowledgement
                .verify_for(&recipient.public_key(), other_identifier)
                .unwrap_err(),
            DeliveryAcknowledgementError::UnexpectedMessageIdentifier
        );
    }
}
