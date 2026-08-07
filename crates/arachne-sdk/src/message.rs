use std::fmt;

use arachne_core::IdentityPublicKey;
use arachne_daemon::{
    DeliveryState, MessageExpiry, MessageExpiryError, OutboxMessage, SenderOutboxError,
};
use arachne_protocol::{EncryptedMessageEnvelope, MessageIdentifier};

#[derive(Clone, Eq, PartialEq)]
pub struct SdkMessageEnvelope(EncryptedMessageEnvelope);

impl SdkMessageEnvelope {
    pub fn new(
        encrypted_header: Vec<u8>,
        ciphertext: Vec<u8>,
    ) -> Result<Self, SdkMessageEnvelopeError> {
        EncryptedMessageEnvelope::new(encrypted_header, ciphertext)
            .map(Self)
            .map_err(|_| SdkMessageEnvelopeError::InvalidEnvelope)
    }

    pub fn from_encoded(encoded: &[u8]) -> Result<Self, SdkMessageEnvelopeError> {
        let envelope = EncryptedMessageEnvelope::decode(encoded)
            .map_err(|_| SdkMessageEnvelopeError::InvalidEnvelope)?;
        if envelope
            .encode()
            .map_err(|_| SdkMessageEnvelopeError::InvalidEnvelope)?
            != encoded
        {
            return Err(SdkMessageEnvelopeError::NonCanonicalEncoding);
        }
        Ok(Self(envelope))
    }

    pub(crate) fn into_inner(self) -> EncryptedMessageEnvelope {
        self.0
    }
}

impl fmt::Debug for SdkMessageEnvelope {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("SdkMessageEnvelope(REDACTED)")
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkMessageExpiry(MessageExpiry);

impl SdkMessageExpiry {
    pub fn new(created_at: u64, ttl_seconds: u32) -> Result<Self, SdkMessageExpiryError> {
        MessageExpiry::new(created_at, ttl_seconds)
            .map(Self)
            .map_err(|error| map_expiry_error(&error))
    }

    #[must_use]
    pub const fn created_at(self) -> u64 {
        self.0.created_at()
    }

    #[must_use]
    pub const fn ttl_seconds(self) -> u32 {
        self.0.ttl_seconds()
    }

    pub(crate) const fn into_inner(self) -> MessageExpiry {
        self.0
    }
}

pub struct SdkMessageSendRequest {
    recipient: IdentityPublicKey,
    envelope: SdkMessageEnvelope,
    expiry: SdkMessageExpiry,
}

impl SdkMessageSendRequest {
    #[must_use]
    pub const fn new(
        recipient: IdentityPublicKey,
        envelope: SdkMessageEnvelope,
        expiry: SdkMessageExpiry,
    ) -> Self {
        Self {
            recipient,
            envelope,
            expiry,
        }
    }

    pub(crate) fn into_parts(self) -> (IdentityPublicKey, EncryptedMessageEnvelope, MessageExpiry) {
        (
            self.recipient,
            self.envelope.into_inner(),
            self.expiry.into_inner(),
        )
    }
}

impl fmt::Debug for SdkMessageSendRequest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SdkMessageSendRequest")
            .field("recipient", &self.recipient)
            .field("envelope", &"REDACTED")
            .field("expiry", &self.expiry)
            .finish()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkQueuedMessage {
    identifier: SdkMessageIdentifier,
    recipient: IdentityPublicKey,
    expiry: SdkMessageExpiry,
}

impl SdkQueuedMessage {
    #[must_use]
    pub const fn identifier(self) -> SdkMessageIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn recipient(self) -> IdentityPublicKey {
        self.recipient
    }

    #[must_use]
    pub const fn expiry(self) -> SdkMessageExpiry {
        self.expiry
    }
}

impl From<&OutboxMessage> for SdkQueuedMessage {
    fn from(message: &OutboxMessage) -> Self {
        Self {
            identifier: SdkMessageIdentifier(message.identifier()),
            recipient: *message.recipient(),
            expiry: SdkMessageExpiry(message.expiry()),
        }
    }
}

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct SdkMessageIdentifier(MessageIdentifier);

impl SdkMessageIdentifier {
    pub fn from_bytes(
        bytes: [u8; arachne_protocol::MESSAGE_IDENTIFIER_BYTES],
    ) -> Result<Self, SdkMessageIdentifierError> {
        MessageIdentifier::from_bytes(bytes)
            .map(Self)
            .map_err(|_| SdkMessageIdentifierError::Invalid)
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; arachne_protocol::MESSAGE_IDENTIFIER_BYTES] {
        self.0.as_bytes()
    }

    pub(crate) const fn into_inner(self) -> MessageIdentifier {
        self.0
    }
}

impl fmt::Debug for SdkMessageIdentifier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("SdkMessageIdentifier(REDACTED)")
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkMessageEnvelopeError {
    #[error("encrypted message envelope is invalid")]
    InvalidEnvelope,
    #[error("encrypted message envelope is not canonically encoded")]
    NonCanonicalEncoding,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkMessageIdentifierError {
    #[error("message identifier is invalid")]
    Invalid,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkMessageExpiryError {
    #[error("message expiry TTL is invalid")]
    InvalidTtl,
    #[error("message expiry timestamp overflowed")]
    TimestampOverflow,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkMessageError {
    #[error("SDK identity operation failed")]
    Identity(#[from] crate::SdkIdentityError),
    #[error("SDK client is not running")]
    ClientNotRunning,
    #[error("SDK event sequence is exhausted")]
    EventSequenceExhausted,
    #[error("sender outbox is full")]
    QueueFull,
    #[error("sender outbox state is unavailable")]
    State,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkDeliveryStatus {
    Queued,
    Attempted,
    Delivered,
    Expired,
    Failed,
}

pub const fn map_delivery_state(state: DeliveryState) -> SdkDeliveryStatus {
    match state {
        DeliveryState::Queued => SdkDeliveryStatus::Queued,
        DeliveryState::Attempted => SdkDeliveryStatus::Attempted,
        DeliveryState::AwaitingRecipientAcknowledgement => SdkDeliveryStatus::Attempted,
        DeliveryState::Delivered => SdkDeliveryStatus::Delivered,
        DeliveryState::Expired => SdkDeliveryStatus::Expired,
        DeliveryState::Failed => SdkDeliveryStatus::Failed,
    }
}

pub fn map_outbox_error(error: &SenderOutboxError) -> SdkMessageError {
    match error {
        SenderOutboxError::QueueFull => SdkMessageError::QueueFull,
        SenderOutboxError::StateStore(_)
        | SenderOutboxError::EmptyQueue
        | SenderOutboxError::Identifier(_)
        | SenderOutboxError::Expiry(_)
        | SenderOutboxError::Acknowledgement(_)
        | SenderOutboxError::UnknownMessageIdentifier
        | SenderOutboxError::AttemptsExhausted
        | SenderOutboxError::InvalidState(_)
        | SenderOutboxError::InvalidDocument(_)
        | SenderOutboxError::Encode
        | SenderOutboxError::UnsupportedSchemaVersion(_)
        | SenderOutboxError::InvalidShape
        | SenderOutboxError::InvalidRecipient
        | SenderOutboxError::InvalidIdentifier
        | SenderOutboxError::DuplicateIdentifier
        | SenderOutboxError::InvalidEnvelope
        | SenderOutboxError::TooManyMessages
        | SenderOutboxError::TooManyDeliveryStatuses
        | SenderOutboxError::InvalidDeliveryState
        | SenderOutboxError::InvalidDeliveryAttempts
        | SenderOutboxError::TrailingBytes
        | SenderOutboxError::NonCanonicalEncoding => SdkMessageError::State,
    }
}

fn map_expiry_error(error: &MessageExpiryError) -> SdkMessageExpiryError {
    match error {
        MessageExpiryError::InvalidTtl => SdkMessageExpiryError::InvalidTtl,
        MessageExpiryError::TimestampOverflow => SdkMessageExpiryError::TimestampOverflow,
    }
}
