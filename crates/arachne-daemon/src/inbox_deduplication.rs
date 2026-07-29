use std::path::Path;

use arachne_core::OsKeystore;
use arachne_protocol::{CryptoDomain, EncryptedMessageEnvelope};
use minicbor::{Decoder, Encoder, data::Type};
use sha2::{Digest, Sha256};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub const INBOX_DEDUPLICATION_SCHEMA_VERSION: u8 = 2;
pub const MAX_INBOX_DEDUPLICATION_ENTRIES: usize = 65_536;
const INBOX_DEDUPLICATION_SCHEMA_VERSION_V1: u8 = 1;
const INBOX_DEDUPLICATION_FIELDS: u64 = 2;
const INBOX_ENTRY_FIELDS: u64 = 3;
const ENVELOPE_FINGERPRINT_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum InboxDeduplicationResult {
    Accepted,
    Duplicate,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct InboxMessage {
    received_at: u64,
    encrypted_header_bytes: usize,
    ciphertext_bytes: usize,
}

impl InboxMessage {
    #[must_use]
    pub const fn received_at(self) -> u64 {
        self.received_at
    }

    #[must_use]
    pub const fn encrypted_header_bytes(self) -> usize {
        self.encrypted_header_bytes
    }

    #[must_use]
    pub const fn ciphertext_bytes(self) -> usize {
        self.ciphertext_bytes
    }
}

struct InboxEntry {
    fingerprint: [u8; ENVELOPE_FINGERPRINT_BYTES],
    received_at: Option<u64>,
    envelope: Option<EncryptedMessageEnvelope>,
}

pub struct RecipientInboxDeduplication {
    state: EncryptedStateStore,
    entries: Vec<InboxEntry>,
}

impl RecipientInboxDeduplication {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, RecipientInboxDeduplicationError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let (entries, needs_migration) = match state.load()? {
            Some(document) => decode_entries(document.as_bytes())?,
            None => (Vec::new(), false),
        };
        let mut inbox = Self { state, entries };
        if needs_migration {
            inbox.persist()?;
        }
        Ok(inbox)
    }

    #[must_use]
    pub const fn len(&self) -> usize {
        self.entries.len()
    }

    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    pub fn messages(&self) -> impl Iterator<Item = InboxMessage> + '_ {
        self.entries.iter().filter_map(metadata)
    }

    pub fn record(
        &mut self,
        envelope: &EncryptedMessageEnvelope,
    ) -> Result<InboxDeduplicationResult, RecipientInboxDeduplicationError> {
        self.record_at(envelope, 0)
    }

    pub fn record_at(
        &mut self,
        envelope: &EncryptedMessageEnvelope,
        received_at: u64,
    ) -> Result<InboxDeduplicationResult, RecipientInboxDeduplicationError> {
        let fingerprint = fingerprint(envelope)?;
        match self
            .entries
            .binary_search_by_key(&fingerprint, |entry| entry.fingerprint)
        {
            Ok(_) => Ok(InboxDeduplicationResult::Duplicate),
            Err(index) => {
                if self.entries.len() >= MAX_INBOX_DEDUPLICATION_ENTRIES {
                    return Err(RecipientInboxDeduplicationError::CapacityExceeded);
                }
                self.entries.insert(
                    index,
                    InboxEntry {
                        fingerprint,
                        received_at: Some(received_at),
                        envelope: Some(envelope.clone()),
                    },
                );
                if let Err(error) = self.persist() {
                    self.entries.remove(index);
                    return Err(error);
                }
                Ok(InboxDeduplicationResult::Accepted)
            }
        }
    }

    fn persist(&mut self) -> Result<(), RecipientInboxDeduplicationError> {
        let document = StateDocument::new(encode_entries(&self.entries)?)
            .map_err(RecipientInboxDeduplicationError::InvalidDocument)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RecipientInboxDeduplicationError {
    #[error("encrypted state-store operation failed: {0}")]
    StateStore(#[from] StateStoreError),
    #[error("recipient inbox deduplication capacity is exhausted")]
    CapacityExceeded,
    #[error("recipient inbox deduplication received an invalid encrypted envelope")]
    InvalidEnvelope,
    #[error("recipient inbox deduplication state document is invalid")]
    InvalidState(#[source] minicbor::decode::Error),
    #[error("recipient inbox deduplication state document violates storage bounds: {0}")]
    InvalidDocument(#[source] StateDocumentError),
    #[error("recipient inbox deduplication encoding failed")]
    Encode,
    #[error("recipient inbox deduplication schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("recipient inbox deduplication state document has an invalid shape")]
    InvalidShape,
    #[error("recipient inbox deduplication state document has too many entries")]
    TooManyEntries,
    #[error("recipient inbox deduplication state document has an invalid fingerprint")]
    InvalidFingerprint,
    #[error("recipient inbox deduplication state document has an invalid message")]
    InvalidMessage,
    #[error("recipient inbox deduplication state document contains duplicate fingerprints")]
    DuplicateFingerprint,
    #[error("recipient inbox deduplication state document has trailing bytes")]
    TrailingBytes,
    #[error("recipient inbox deduplication state document is not canonical")]
    NonCanonicalEncoding,
}

fn metadata(entry: &InboxEntry) -> Option<InboxMessage> {
    Some(InboxMessage {
        received_at: entry.received_at?,
        encrypted_header_bytes: entry.envelope.as_ref()?.encrypted_header().len(),
        ciphertext_bytes: entry.envelope.as_ref()?.ciphertext().len(),
    })
}

fn fingerprint(
    envelope: &EncryptedMessageEnvelope,
) -> Result<[u8; ENVELOPE_FINGERPRINT_BYTES], RecipientInboxDeduplicationError> {
    let encoded = envelope
        .encode()
        .map_err(|_| RecipientInboxDeduplicationError::InvalidEnvelope)?;
    let mut hasher = Sha256::new();
    hasher.update(CryptoDomain::RecipientInboxDeduplication.context());
    hasher.update(encoded);
    Ok(hasher.finalize().into())
}

fn encode_entries(entries: &[InboxEntry]) -> Result<Vec<u8>, RecipientInboxDeduplicationError> {
    validate_entries(entries)?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(INBOX_DEDUPLICATION_FIELDS)
        .map_err(|_| RecipientInboxDeduplicationError::Encode)?
        .u8(INBOX_DEDUPLICATION_SCHEMA_VERSION)
        .map_err(|_| RecipientInboxDeduplicationError::Encode)?
        .array(u64::try_from(entries.len()).map_err(|_| RecipientInboxDeduplicationError::Encode)?)
        .map_err(|_| RecipientInboxDeduplicationError::Encode)?;
    for entry in entries {
        encoder
            .array(INBOX_ENTRY_FIELDS)
            .map_err(|_| RecipientInboxDeduplicationError::Encode)?
            .bytes(&entry.fingerprint)
            .map_err(|_| RecipientInboxDeduplicationError::Encode)?;
        match entry.received_at {
            Some(received_at) => encoder
                .u64(received_at)
                .map_err(|_| RecipientInboxDeduplicationError::Encode)?,
            None => encoder
                .null()
                .map_err(|_| RecipientInboxDeduplicationError::Encode)?,
        };
        match &entry.envelope {
            Some(envelope) => encoder
                .bytes(
                    &envelope
                        .encode()
                        .map_err(|_| RecipientInboxDeduplicationError::InvalidEnvelope)?,
                )
                .map_err(|_| RecipientInboxDeduplicationError::Encode)?,
            None => encoder
                .null()
                .map_err(|_| RecipientInboxDeduplicationError::Encode)?,
        };
    }
    Ok(encoder.into_writer())
}

fn decode_entries(
    encoded: &[u8],
) -> Result<(Vec<InboxEntry>, bool), RecipientInboxDeduplicationError> {
    let mut decoder = Decoder::new(encoded);
    if decoder
        .array()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?
        != Some(INBOX_DEDUPLICATION_FIELDS)
    {
        return Err(RecipientInboxDeduplicationError::InvalidShape);
    }
    let version = decoder
        .u8()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?;
    let (entries, needs_migration) = match version {
        INBOX_DEDUPLICATION_SCHEMA_VERSION_V1 => (decode_v1_entries(&mut decoder)?, true),
        INBOX_DEDUPLICATION_SCHEMA_VERSION => (decode_v2_entries(&mut decoder)?, false),
        _ => {
            return Err(RecipientInboxDeduplicationError::UnsupportedSchemaVersion(
                version,
            ));
        }
    };
    if decoder.position() != encoded.len() {
        return Err(RecipientInboxDeduplicationError::TrailingBytes);
    }
    validate_entries(&entries)?;
    if !needs_migration && encode_entries(&entries)? != encoded {
        return Err(RecipientInboxDeduplicationError::NonCanonicalEncoding);
    }
    Ok((entries, needs_migration))
}

fn decode_v1_entries(
    decoder: &mut Decoder<'_>,
) -> Result<Vec<InboxEntry>, RecipientInboxDeduplicationError> {
    let count = decoder
        .array()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?
        .ok_or(RecipientInboxDeduplicationError::InvalidShape)?;
    let count =
        usize::try_from(count).map_err(|_| RecipientInboxDeduplicationError::TooManyEntries)?;
    if count > MAX_INBOX_DEDUPLICATION_ENTRIES {
        return Err(RecipientInboxDeduplicationError::TooManyEntries);
    }
    let mut entries = Vec::with_capacity(count);
    for _ in 0..count {
        entries.push(InboxEntry {
            fingerprint: decoder
                .bytes()
                .map_err(RecipientInboxDeduplicationError::InvalidState)?
                .try_into()
                .map_err(|_| RecipientInboxDeduplicationError::InvalidFingerprint)?,
            received_at: None,
            envelope: None,
        });
    }
    Ok(entries)
}

fn decode_v2_entries(
    decoder: &mut Decoder<'_>,
) -> Result<Vec<InboxEntry>, RecipientInboxDeduplicationError> {
    let count = decoder
        .array()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?
        .ok_or(RecipientInboxDeduplicationError::InvalidShape)?;
    let count =
        usize::try_from(count).map_err(|_| RecipientInboxDeduplicationError::TooManyEntries)?;
    if count > MAX_INBOX_DEDUPLICATION_ENTRIES {
        return Err(RecipientInboxDeduplicationError::TooManyEntries);
    }
    let mut entries = Vec::with_capacity(count);
    for _ in 0..count {
        if decoder
            .array()
            .map_err(RecipientInboxDeduplicationError::InvalidState)?
            != Some(INBOX_ENTRY_FIELDS)
        {
            return Err(RecipientInboxDeduplicationError::InvalidShape);
        }
        let fingerprint = decoder
            .bytes()
            .map_err(RecipientInboxDeduplicationError::InvalidState)?
            .try_into()
            .map_err(|_| RecipientInboxDeduplicationError::InvalidFingerprint)?;
        let received_at = decode_received_at(decoder)?;
        let envelope = decode_envelope(decoder)?;
        if received_at.is_some() != envelope.is_some() {
            return Err(RecipientInboxDeduplicationError::InvalidMessage);
        }
        entries.push(InboxEntry {
            fingerprint,
            received_at,
            envelope,
        });
    }
    Ok(entries)
}

fn decode_received_at(
    decoder: &mut Decoder<'_>,
) -> Result<Option<u64>, RecipientInboxDeduplicationError> {
    match decoder
        .datatype()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?
    {
        Type::Null => {
            decoder
                .null()
                .map_err(RecipientInboxDeduplicationError::InvalidState)?;
            Ok(None)
        }
        Type::U8 | Type::U16 | Type::U32 | Type::U64 => decoder
            .u64()
            .map(Some)
            .map_err(RecipientInboxDeduplicationError::InvalidState),
        _ => Err(RecipientInboxDeduplicationError::InvalidMessage),
    }
}

fn decode_envelope(
    decoder: &mut Decoder<'_>,
) -> Result<Option<EncryptedMessageEnvelope>, RecipientInboxDeduplicationError> {
    match decoder
        .datatype()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?
    {
        Type::Null => {
            decoder
                .null()
                .map_err(RecipientInboxDeduplicationError::InvalidState)?;
            Ok(None)
        }
        Type::Bytes => EncryptedMessageEnvelope::decode(
            decoder
                .bytes()
                .map_err(RecipientInboxDeduplicationError::InvalidState)?,
        )
        .map(Some)
        .map_err(|_| RecipientInboxDeduplicationError::InvalidEnvelope),
        _ => Err(RecipientInboxDeduplicationError::InvalidMessage),
    }
}

fn validate_entries(entries: &[InboxEntry]) -> Result<(), RecipientInboxDeduplicationError> {
    if entries.len() > MAX_INBOX_DEDUPLICATION_ENTRIES {
        return Err(RecipientInboxDeduplicationError::TooManyEntries);
    }
    for pair in entries.windows(2) {
        if pair[0].fingerprint == pair[1].fingerprint {
            return Err(RecipientInboxDeduplicationError::DuplicateFingerprint);
        }
        if pair[0].fingerprint > pair[1].fingerprint {
            return Err(RecipientInboxDeduplicationError::NonCanonicalEncoding);
        }
    }
    if entries
        .iter()
        .any(|entry| entry.received_at.is_some() != entry.envelope.is_some())
    {
        return Err(RecipientInboxDeduplicationError::InvalidMessage);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
    use arachne_protocol::EncryptedMessageEnvelope;

    use super::{
        InboxDeduplicationResult, InboxEntry, InboxMessage, RecipientInboxDeduplication,
        RecipientInboxDeduplicationError, encode_entries,
    };
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
            "arachne-recipient-inbox-{name}-{}.sqlite",
            std::process::id()
        ))
    }

    #[test]
    fn deduplicates_and_retains_safe_message_metadata_across_restarts() {
        let path = path("persistence");
        let mut keystore = MemoryKeystore::default();
        let first = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap();
        let second = EncryptedMessageEnvelope::new(vec![0xc3], vec![0xd4, 0xe5]).unwrap();
        {
            let mut inbox = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
            assert_eq!(
                inbox.record_at(&first, 100).unwrap(),
                InboxDeduplicationResult::Accepted
            );
            assert_eq!(
                inbox.record_at(&first, 101).unwrap(),
                InboxDeduplicationResult::Duplicate
            );
            assert_eq!(
                inbox.record_at(&second, 102).unwrap(),
                InboxDeduplicationResult::Accepted
            );
            assert_eq!(inbox.len(), 2);
            let mut messages: Vec<_> = inbox.messages().collect();
            messages.sort_unstable_by_key(|message| message.received_at());
            assert_eq!(
                messages,
                vec![
                    InboxMessage {
                        received_at: 100,
                        encrypted_header_bytes: 1,
                        ciphertext_bytes: 1,
                    },
                    InboxMessage {
                        received_at: 102,
                        encrypted_header_bytes: 1,
                        ciphertext_bytes: 2,
                    },
                ]
            );
        }
        let mut restored = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
        assert_eq!(
            restored.record(&first).unwrap(),
            InboxDeduplicationResult::Duplicate
        );
        assert_eq!(
            restored.record(&second).unwrap(),
            InboxDeduplicationResult::Duplicate
        );
        assert_eq!(restored.messages().count(), 2);
        drop(restored);
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn migrates_legacy_fingerprints_without_losing_deduplication() {
        let path = path("migration");
        let mut keystore = MemoryKeystore::default();
        let fingerprint = [0x11; 32];
        let legacy = vec![0x82, 0x01, 0x81, 0x58, 0x20]
            .into_iter()
            .chain(fingerprint)
            .collect();
        let mut state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        state.replace(&StateDocument::new(legacy).unwrap()).unwrap();
        drop(state);

        let inbox = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
        assert_eq!(inbox.len(), 1);
        assert!(inbox.messages().next().is_none());
        drop(inbox);
        let state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        let migrated = state.load().unwrap().unwrap();
        assert_eq!(migrated.as_bytes()[1], 2);
        drop(state);
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_malformed_persisted_deduplication_state() {
        let path = path("malformed");
        let mut keystore = MemoryKeystore::default();
        let mut state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        state
            .replace(&StateDocument::new(vec![0x82, 0x01, 0x81, 0x41, 0x00]).unwrap())
            .unwrap();
        drop(state);

        assert!(matches!(
            RecipientInboxDeduplication::open(&path, &mut keystore),
            Err(RecipientInboxDeduplicationError::InvalidFingerprint)
        ));
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_message_metadata_without_an_envelope() {
        let entry = InboxEntry {
            fingerprint: [0x11; 32],
            received_at: Some(100),
            envelope: None,
        };
        assert!(matches!(
            encode_entries(&[entry]),
            Err(RecipientInboxDeduplicationError::InvalidMessage)
        ));
    }
}
