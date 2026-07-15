use std::{fmt, path::Path};

use minicbor::{Decoder, Encoder};
use yeokcham_core::{IdentityPublicKey, OsKeystore};
use yeokcham_protocol::{
    DeliveryAcknowledgement, DeliveryAcknowledgementError, EncryptedMessageEnvelope,
    MessageIdentifier, MessageIdentifierError,
};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub const OUTBOX_STATE_SCHEMA_VERSION: u8 = 2;
pub const MAX_OUTBOX_MESSAGES: usize = 256;
const OUTBOX_FIELDS: u64 = 2;
const OUTBOX_STATE_SCHEMA_VERSION_V1: u8 = 1;
const OUTBOX_MESSAGE_FIELDS_V1: u64 = 2;
const OUTBOX_MESSAGE_FIELDS: u64 = 3;
const IDENTITY_PUBLIC_KEY_BYTES: usize = 32;

#[derive(Clone, Eq, PartialEq)]
pub struct OutboxMessage {
    identifier: MessageIdentifier,
    recipient: IdentityPublicKey,
    envelope: EncryptedMessageEnvelope,
}

impl OutboxMessage {
    #[must_use]
    pub const fn identifier(&self) -> MessageIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn recipient(&self) -> &IdentityPublicKey {
        &self.recipient
    }

    #[must_use]
    pub const fn envelope(&self) -> &EncryptedMessageEnvelope {
        &self.envelope
    }
}

impl fmt::Debug for OutboxMessage {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("OutboxMessage")
            .field("identifier", &self.identifier)
            .field("recipient", &self.recipient)
            .field("envelope", &"REDACTED")
            .finish()
    }
}

pub struct SenderOutbox {
    state: EncryptedStateStore,
    messages: Vec<OutboxMessage>,
}

impl SenderOutbox {
    pub fn open<K: OsKeystore>(path: &Path, keystore: &mut K) -> Result<Self, SenderOutboxError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let (messages, needs_migration) = match state.load()? {
            Some(document) => decode_messages(document.as_bytes())?,
            None => (Vec::new(), false),
        };
        let mut outbox = Self { state, messages };
        if needs_migration {
            outbox.persist()?;
        }
        Ok(outbox)
    }

    #[must_use]
    pub fn messages(&self) -> &[OutboxMessage] {
        &self.messages
    }

    #[must_use]
    pub fn next(&self) -> Option<&OutboxMessage> {
        self.messages.first()
    }

    pub fn enqueue(
        &mut self,
        recipient: IdentityPublicKey,
        envelope: EncryptedMessageEnvelope,
    ) -> Result<(), SenderOutboxError> {
        if self.messages.len() >= MAX_OUTBOX_MESSAGES {
            return Err(SenderOutboxError::QueueFull);
        }
        let identifier = next_identifier(&self.messages)?;
        self.messages.push(OutboxMessage {
            identifier,
            recipient,
            envelope,
        });
        if let Err(error) = self.persist() {
            self.messages.pop();
            return Err(error);
        }
        Ok(())
    }

    pub fn acknowledge_next(&mut self) -> Result<OutboxMessage, SenderOutboxError> {
        let message = self
            .messages
            .first()
            .cloned()
            .ok_or(SenderOutboxError::EmptyQueue)?;
        self.messages.remove(0);
        if let Err(error) = self.persist() {
            self.messages.insert(0, message);
            return Err(error);
        }
        Ok(message)
    }

    pub fn acknowledge_delivery(
        &mut self,
        acknowledgement: &DeliveryAcknowledgement,
    ) -> Result<OutboxMessage, SenderOutboxError> {
        let index = self
            .messages
            .iter()
            .position(|message| message.identifier == acknowledgement.message_identifier())
            .ok_or(SenderOutboxError::UnknownMessageIdentifier)?;
        let message = self.messages[index].clone();
        acknowledgement
            .verify_for(&message.recipient, message.identifier)
            .map_err(SenderOutboxError::Acknowledgement)?;
        self.messages.remove(index);
        if let Err(error) = self.persist() {
            self.messages.insert(index, message);
            return Err(error);
        }
        Ok(message)
    }

    fn persist(&mut self) -> Result<(), SenderOutboxError> {
        let document = StateDocument::new(encode_messages(&self.messages)?)
            .map_err(SenderOutboxError::InvalidDocument)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SenderOutboxError {
    #[error("encrypted state-store operation failed: {0}")]
    StateStore(#[from] StateStoreError),
    #[error("sender outbox is full")]
    QueueFull,
    #[error("sender outbox is empty")]
    EmptyQueue,
    #[error("sender outbox message identifier is invalid")]
    Identifier(#[source] MessageIdentifierError),
    #[error("delivery acknowledgement verification failed")]
    Acknowledgement(#[source] DeliveryAcknowledgementError),
    #[error("delivery acknowledgement references no queued message")]
    UnknownMessageIdentifier,
    #[error("sender outbox state document is invalid")]
    InvalidState(#[source] minicbor::decode::Error),
    #[error("sender outbox state document violates storage bounds: {0}")]
    InvalidDocument(#[source] StateDocumentError),
    #[error("sender outbox encoding failed")]
    Encode,
    #[error("sender outbox schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("sender outbox state document has an invalid shape")]
    InvalidShape,
    #[error("sender outbox state document contains an invalid recipient")]
    InvalidRecipient,
    #[error("sender outbox state document contains an invalid message identifier")]
    InvalidIdentifier,
    #[error("sender outbox state document contains duplicate message identifiers")]
    DuplicateIdentifier,
    #[error("sender outbox state document contains an invalid encrypted envelope")]
    InvalidEnvelope,
    #[error("sender outbox state document has too many messages")]
    TooManyMessages,
    #[error("sender outbox state document has trailing bytes")]
    TrailingBytes,
    #[error("sender outbox state document is not canonical")]
    NonCanonicalEncoding,
}

fn encode_messages(messages: &[OutboxMessage]) -> Result<Vec<u8>, SenderOutboxError> {
    if messages.len() > MAX_OUTBOX_MESSAGES {
        return Err(SenderOutboxError::TooManyMessages);
    }
    for (index, message) in messages.iter().enumerate() {
        if messages[..index]
            .iter()
            .any(|previous| previous.identifier == message.identifier)
        {
            return Err(SenderOutboxError::DuplicateIdentifier);
        }
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(OUTBOX_FIELDS)
        .map_err(|_| SenderOutboxError::Encode)?
        .u8(OUTBOX_STATE_SCHEMA_VERSION)
        .map_err(|_| SenderOutboxError::Encode)?
        .array(u64::try_from(messages.len()).map_err(|_| SenderOutboxError::Encode)?)
        .map_err(|_| SenderOutboxError::Encode)?;
    for message in messages {
        let envelope = message
            .envelope
            .encode()
            .map_err(|_| SenderOutboxError::InvalidEnvelope)?;
        encoder
            .array(OUTBOX_MESSAGE_FIELDS)
            .map_err(|_| SenderOutboxError::Encode)?
            .bytes(message.identifier.as_bytes())
            .map_err(|_| SenderOutboxError::Encode)?
            .bytes(message.recipient.as_bytes())
            .map_err(|_| SenderOutboxError::Encode)?
            .bytes(&envelope)
            .map_err(|_| SenderOutboxError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_messages(encoded: &[u8]) -> Result<(Vec<OutboxMessage>, bool), SenderOutboxError> {
    let mut decoder = Decoder::new(encoded);
    if decoder.array().map_err(SenderOutboxError::InvalidState)? != Some(OUTBOX_FIELDS) {
        return Err(SenderOutboxError::InvalidShape);
    }
    let version = decoder.u8().map_err(SenderOutboxError::InvalidState)?;
    if version != OUTBOX_STATE_SCHEMA_VERSION && version != OUTBOX_STATE_SCHEMA_VERSION_V1 {
        return Err(SenderOutboxError::UnsupportedSchemaVersion(version));
    }
    let count = decoder
        .array()
        .map_err(SenderOutboxError::InvalidState)?
        .ok_or(SenderOutboxError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| SenderOutboxError::TooManyMessages)?;
    if count > MAX_OUTBOX_MESSAGES {
        return Err(SenderOutboxError::TooManyMessages);
    }
    let mut messages = Vec::with_capacity(count);
    for _ in 0..count {
        let expected_fields = if version == OUTBOX_STATE_SCHEMA_VERSION {
            OUTBOX_MESSAGE_FIELDS
        } else {
            OUTBOX_MESSAGE_FIELDS_V1
        };
        if decoder.array().map_err(SenderOutboxError::InvalidState)? != Some(expected_fields) {
            return Err(SenderOutboxError::InvalidShape);
        }
        let identifier = if version == OUTBOX_STATE_SCHEMA_VERSION {
            let identifier = decoder
                .bytes()
                .map_err(SenderOutboxError::InvalidState)?
                .try_into()
                .map_err(|_| SenderOutboxError::InvalidIdentifier)?;
            MessageIdentifier::from_bytes(identifier)
                .map_err(|_| SenderOutboxError::InvalidIdentifier)?
        } else {
            next_identifier(&messages)?
        };
        let recipient: [u8; IDENTITY_PUBLIC_KEY_BYTES] = decoder
            .bytes()
            .map_err(SenderOutboxError::InvalidState)?
            .try_into()
            .map_err(|_| SenderOutboxError::InvalidRecipient)?;
        let recipient = IdentityPublicKey::from_bytes(recipient)
            .map_err(|_| SenderOutboxError::InvalidRecipient)?;
        let envelope = EncryptedMessageEnvelope::decode(
            decoder.bytes().map_err(SenderOutboxError::InvalidState)?,
        )
        .map_err(|_| SenderOutboxError::InvalidEnvelope)?;
        messages.push(OutboxMessage {
            identifier,
            recipient,
            envelope,
        });
    }
    if decoder.position() != encoded.len() {
        return Err(SenderOutboxError::TrailingBytes);
    }
    if version == OUTBOX_STATE_SCHEMA_VERSION && encode_messages(&messages)? != encoded {
        return Err(SenderOutboxError::NonCanonicalEncoding);
    }
    Ok((messages, version == OUTBOX_STATE_SCHEMA_VERSION_V1))
}

fn next_identifier(messages: &[OutboxMessage]) -> Result<MessageIdentifier, SenderOutboxError> {
    for _ in 0..=MAX_OUTBOX_MESSAGES {
        let identifier = MessageIdentifier::generate().map_err(SenderOutboxError::Identifier)?;
        if messages
            .iter()
            .all(|message| message.identifier != identifier)
        {
            return Ok(identifier);
        }
    }
    Err(SenderOutboxError::DuplicateIdentifier)
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use minicbor::Encoder;
    use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_protocol::{
        DeliveryAcknowledgement, DeliveryAcknowledgementError, EncryptedMessageEnvelope,
    };

    use super::{SenderOutbox, SenderOutboxError};
    use crate::{EncryptedStateStore, StateDocument};

    #[derive(Default)]
    struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

    impl OsKeystore for MemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .0
                .get(entry.as_str())
                .map(|value| KeystoreSecret::new(value.clone()).unwrap()))
        }

        fn store(
            &mut self,
            entry: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.0
                .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
            Ok(())
        }

        fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.0.remove(entry.as_str());
            Ok(())
        }
    }

    fn path(name: &str) -> PathBuf {
        std::env::temp_dir().join(format!(
            "yeokcham-sender-outbox-{name}-{}.sqlite",
            std::process::id()
        ))
    }

    #[test]
    fn persists_sender_outbox_in_fifo_order() {
        let path = path("persistence");
        let mut keystore = MemoryKeystore::default();
        let first_recipient = IdentityKeypair::generate().unwrap().public_key();
        let second_recipient = IdentityKeypair::generate().unwrap().public_key();
        let first = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap();
        let second = EncryptedMessageEnvelope::new(vec![0xc3], vec![0xd4, 0xe5]).unwrap();
        let (first_identifier, second_identifier) = {
            let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
            outbox.enqueue(first_recipient, first.clone()).unwrap();
            outbox.enqueue(second_recipient, second.clone()).unwrap();
            assert_eq!(outbox.next().unwrap().envelope(), &first);
            assert!(format!("{:?}", outbox.next().unwrap()).contains("REDACTED"));
            (
                outbox.messages()[0].identifier(),
                outbox.messages()[1].identifier(),
            )
        };
        {
            let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
            assert_eq!(outbox.messages().len(), 2);
            assert_eq!(outbox.next().unwrap().recipient(), &first_recipient);
            assert_eq!(outbox.next().unwrap().identifier(), first_identifier);
            let acknowledged = outbox.acknowledge_next().unwrap();
            assert_eq!(acknowledged.envelope(), &first);
            assert_eq!(acknowledged.identifier(), first_identifier);
        }
        {
            let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
            assert_eq!(outbox.messages().len(), 1);
            assert_eq!(outbox.next().unwrap().recipient(), &second_recipient);
            assert_eq!(outbox.next().unwrap().envelope(), &second);
            assert_eq!(outbox.next().unwrap().identifier(), second_identifier);
        }
        assert_eq!(
            SenderOutbox::open(&path, &mut keystore)
                .unwrap()
                .acknowledge_next()
                .unwrap()
                .recipient(),
            &second_recipient
        );
        let mut empty = SenderOutbox::open(&path, &mut keystore).unwrap();
        assert!(matches!(
            empty.acknowledge_next(),
            Err(SenderOutboxError::EmptyQueue)
        ));
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_malformed_persisted_outbox_state() {
        let path = path("malformed");
        let mut keystore = MemoryKeystore::default();
        let mut state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        state
            .replace(&StateDocument::new(vec![0x82, 0x01, 0x80, 0x00]).unwrap())
            .unwrap();
        drop(state);

        assert!(matches!(
            SenderOutbox::open(&path, &mut keystore),
            Err(SenderOutboxError::TrailingBytes)
        ));
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn verifies_delivery_acknowledgements_before_removing_queued_messages() {
        let path = path("delivery-acknowledgement");
        let mut keystore = MemoryKeystore::default();
        let first_recipient = IdentityKeypair::generate().unwrap();
        let second_recipient = IdentityKeypair::generate().unwrap();
        let unrelated_recipient = IdentityKeypair::generate().unwrap();
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(
                first_recipient.public_key(),
                EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap(),
            )
            .unwrap();
        outbox
            .enqueue(
                second_recipient.public_key(),
                EncryptedMessageEnvelope::new(vec![0xc3], vec![0xd4]).unwrap(),
            )
            .unwrap();
        let second_identifier = outbox.messages()[1].identifier();
        let invalid =
            DeliveryAcknowledgement::create(&unrelated_recipient, second_identifier, 100).unwrap();

        assert!(matches!(
            outbox.acknowledge_delivery(&invalid),
            Err(SenderOutboxError::Acknowledgement(
                DeliveryAcknowledgementError::UnexpectedRecipient
            ))
        ));
        let valid =
            DeliveryAcknowledgement::create(&second_recipient, second_identifier, 100).unwrap();
        assert_eq!(
            outbox.acknowledge_delivery(&valid).unwrap().identifier(),
            second_identifier
        );
        assert_eq!(outbox.messages().len(), 1);
        assert_eq!(
            outbox.next().unwrap().recipient(),
            &first_recipient.public_key()
        );
        drop(outbox);
        assert_eq!(
            SenderOutbox::open(&path, &mut keystore)
                .unwrap()
                .messages()
                .len(),
            1
        );
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn migrates_v1_outbox_messages_to_persistent_identifiers() {
        let path = path("v1-migration");
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap();
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(2)
            .unwrap()
            .u8(1)
            .unwrap()
            .array(1)
            .unwrap()
            .array(2)
            .unwrap()
            .bytes(recipient.as_bytes())
            .unwrap()
            .bytes(&envelope.encode().unwrap())
            .unwrap();
        let mut state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        state
            .replace(&StateDocument::new(encoder.into_writer()).unwrap())
            .unwrap();
        drop(state);

        let migrated = SenderOutbox::open(&path, &mut keystore).unwrap();
        let identifier = migrated.next().unwrap().identifier();
        assert_eq!(migrated.next().unwrap().recipient(), &recipient);
        drop(migrated);
        let restored = SenderOutbox::open(&path, &mut keystore).unwrap();
        assert_eq!(restored.next().unwrap().identifier(), identifier);
        fs::remove_file(path).unwrap();
    }
}
