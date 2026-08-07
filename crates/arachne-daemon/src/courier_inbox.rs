use std::{collections::BTreeMap, path::Path};

use arachne_core::{IdentityPublicKey, OsKeystore};
use arachne_protocol::{MessageIdentifier, MessagePayload, MessagePayloadError};
use minicbor::{Decoder, Encoder};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub const COURIER_INBOX_STATE_SCHEMA_VERSION: u8 = 1;
pub const MAX_COURIER_INBOX_MESSAGES: usize = 4_096;
const INBOX_FIELDS: u64 = 2;
const MESSAGE_FIELDS: u64 = 4;
const IDENTIFIER_BYTES: usize = 16;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CourierInboxMessage {
    sender: IdentityPublicKey,
    identifier: MessageIdentifier,
    received_at: u64,
    payload: MessagePayload,
}

impl CourierInboxMessage {
    pub(crate) const fn new(
        sender: IdentityPublicKey,
        identifier: MessageIdentifier,
        received_at: u64,
        payload: MessagePayload,
    ) -> Self {
        Self {
            sender,
            identifier,
            received_at,
            payload,
        }
    }
    #[must_use]
    pub const fn sender(&self) -> &IdentityPublicKey {
        &self.sender
    }

    #[must_use]
    pub const fn identifier(&self) -> MessageIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn received_at(&self) -> u64 {
        self.received_at
    }

    #[must_use]
    pub const fn payload(&self) -> &MessagePayload {
        &self.payload
    }
}

pub struct CourierInboxStore {
    state: EncryptedStateStore,
    messages: BTreeMap<[u8; IDENTIFIER_BYTES], CourierInboxMessage>,
}

impl CourierInboxStore {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, CourierInboxStoreError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let messages = match state.load()? {
            Some(document) => decode_messages(document.as_bytes())?,
            None => BTreeMap::new(),
        };
        Ok(Self { state, messages })
    }

    #[must_use]
    pub fn messages(&self) -> impl Iterator<Item = &CourierInboxMessage> {
        self.messages.values()
    }

    #[must_use]
    pub fn message(&self, identifier: MessageIdentifier) -> Option<&CourierInboxMessage> {
        self.messages.get(identifier.as_bytes())
    }

    #[must_use]
    pub fn contains(&self, identifier: MessageIdentifier) -> bool {
        self.messages.contains_key(identifier.as_bytes())
    }

    pub fn record(
        &mut self,
        sender: IdentityPublicKey,
        identifier: MessageIdentifier,
        received_at: u64,
        payload: MessagePayload,
    ) -> Result<bool, CourierInboxStoreError> {
        let key = *identifier.as_bytes();
        if self.messages.contains_key(&key) {
            return Ok(false);
        }
        if self.messages.len() >= MAX_COURIER_INBOX_MESSAGES {
            return Err(CourierInboxStoreError::CapacityExceeded);
        }
        let message = CourierInboxMessage::new(sender, identifier, received_at, payload);
        self.messages.insert(key, message);
        if let Err(error) = self.persist() {
            self.messages.remove(&key);
            return Err(error);
        }
        Ok(true)
    }

    fn persist(&mut self) -> Result<(), CourierInboxStoreError> {
        let document = StateDocument::new(encode_messages(&self.messages)?)
            .map_err(CourierInboxStoreError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierInboxStoreError {
    #[error("courier inbox encrypted state operation failed")]
    State(#[from] StateStoreError),
    #[error("courier inbox state document is invalid")]
    Document(#[source] StateDocumentError),
    #[error("courier inbox payload is invalid")]
    Payload(#[source] MessagePayloadError),
    #[error("courier inbox state encoding failed")]
    Encode,
    #[error("courier inbox state decoding failed")]
    Decode,
    #[error("courier inbox state has an invalid shape")]
    InvalidShape,
    #[error("courier inbox state schema is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier inbox state has too many messages")]
    TooManyMessages,
    #[error("courier inbox state has a duplicate message identifier")]
    DuplicateIdentifier,
    #[error("courier inbox state has an invalid sender identity")]
    InvalidSender,
    #[error("courier inbox state has an invalid message identifier")]
    InvalidIdentifier,
    #[error("courier inbox state has trailing bytes")]
    TrailingBytes,
    #[error("courier inbox state is not canonical")]
    NonCanonicalEncoding,
    #[error("courier inbox capacity is exhausted")]
    CapacityExceeded,
}

fn encode_messages(
    messages: &BTreeMap<[u8; IDENTIFIER_BYTES], CourierInboxMessage>,
) -> Result<Vec<u8>, CourierInboxStoreError> {
    if messages.len() > MAX_COURIER_INBOX_MESSAGES {
        return Err(CourierInboxStoreError::TooManyMessages);
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(INBOX_FIELDS)
        .map_err(|_| CourierInboxStoreError::Encode)?
        .u8(COURIER_INBOX_STATE_SCHEMA_VERSION)
        .map_err(|_| CourierInboxStoreError::Encode)?
        .array(u64::try_from(messages.len()).map_err(|_| CourierInboxStoreError::Encode)?)
        .map_err(|_| CourierInboxStoreError::Encode)?;
    for message in messages.values() {
        let payload = message
            .payload
            .encode()
            .map_err(CourierInboxStoreError::Payload)?;
        encoder
            .array(MESSAGE_FIELDS)
            .map_err(|_| CourierInboxStoreError::Encode)?
            .bytes(message.sender.as_bytes())
            .map_err(|_| CourierInboxStoreError::Encode)?
            .bytes(message.identifier.as_bytes())
            .map_err(|_| CourierInboxStoreError::Encode)?
            .u64(message.received_at)
            .map_err(|_| CourierInboxStoreError::Encode)?
            .bytes(&payload)
            .map_err(|_| CourierInboxStoreError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_messages(
    encoded: &[u8],
) -> Result<BTreeMap<[u8; IDENTIFIER_BYTES], CourierInboxMessage>, CourierInboxStoreError> {
    let mut decoder = Decoder::new(encoded);
    if decoder
        .array()
        .map_err(|_| CourierInboxStoreError::Decode)?
        != Some(INBOX_FIELDS)
    {
        return Err(CourierInboxStoreError::InvalidShape);
    }
    if decoder.u8().map_err(|_| CourierInboxStoreError::Decode)?
        != COURIER_INBOX_STATE_SCHEMA_VERSION
    {
        return Err(CourierInboxStoreError::UnsupportedSchemaVersion);
    }
    let count = decoder
        .array()
        .map_err(|_| CourierInboxStoreError::Decode)?
        .ok_or(CourierInboxStoreError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| CourierInboxStoreError::TooManyMessages)?;
    if count > MAX_COURIER_INBOX_MESSAGES {
        return Err(CourierInboxStoreError::TooManyMessages);
    }
    let mut messages = BTreeMap::new();
    for _ in 0..count {
        if decoder
            .array()
            .map_err(|_| CourierInboxStoreError::Decode)?
            != Some(MESSAGE_FIELDS)
        {
            return Err(CourierInboxStoreError::InvalidShape);
        }
        let sender = IdentityPublicKey::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierInboxStoreError::Decode)?
                .try_into()
                .map_err(|_| CourierInboxStoreError::InvalidSender)?,
        )
        .map_err(|_| CourierInboxStoreError::InvalidSender)?;
        let identifier = MessageIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierInboxStoreError::Decode)?
                .try_into()
                .map_err(|_| CourierInboxStoreError::InvalidIdentifier)?,
        )
        .map_err(|_| CourierInboxStoreError::InvalidIdentifier)?;
        let received_at = decoder.u64().map_err(|_| CourierInboxStoreError::Decode)?;
        let payload = MessagePayload::decode(
            decoder
                .bytes()
                .map_err(|_| CourierInboxStoreError::Decode)?,
        )
        .map_err(CourierInboxStoreError::Payload)?;
        let key = *identifier.as_bytes();
        if messages
            .insert(
                key,
                CourierInboxMessage {
                    sender,
                    identifier,
                    received_at,
                    payload,
                },
            )
            .is_some()
        {
            return Err(CourierInboxStoreError::DuplicateIdentifier);
        }
    }
    if decoder.position() != encoded.len() {
        return Err(CourierInboxStoreError::TrailingBytes);
    }
    if encode_messages(&messages)? != encoded {
        return Err(CourierInboxStoreError::NonCanonicalEncoding);
    }
    Ok(messages)
}
