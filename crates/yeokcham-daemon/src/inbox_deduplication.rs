use std::path::Path;

use minicbor::{Decoder, Encoder};
use sha2::{Digest, Sha256};
use yeokcham_core::OsKeystore;
use yeokcham_protocol::{CryptoDomain, EncryptedMessageEnvelope};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub const INBOX_DEDUPLICATION_SCHEMA_VERSION: u8 = 1;
pub const MAX_INBOX_DEDUPLICATION_ENTRIES: usize = 65_536;
const INBOX_DEDUPLICATION_FIELDS: u64 = 2;
const ENVELOPE_FINGERPRINT_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum InboxDeduplicationResult {
    Accepted,
    Duplicate,
}

pub struct RecipientInboxDeduplication {
    state: EncryptedStateStore,
    fingerprints: Vec<[u8; ENVELOPE_FINGERPRINT_BYTES]>,
}

impl RecipientInboxDeduplication {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, RecipientInboxDeduplicationError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let fingerprints = match state.load()? {
            Some(document) => decode_fingerprints(document.as_bytes())?,
            None => Vec::new(),
        };
        Ok(Self {
            state,
            fingerprints,
        })
    }

    #[must_use]
    pub const fn len(&self) -> usize {
        self.fingerprints.len()
    }

    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.fingerprints.is_empty()
    }

    pub fn record(
        &mut self,
        envelope: &EncryptedMessageEnvelope,
    ) -> Result<InboxDeduplicationResult, RecipientInboxDeduplicationError> {
        let fingerprint = fingerprint(envelope)?;
        match self.fingerprints.binary_search(&fingerprint) {
            Ok(_) => Ok(InboxDeduplicationResult::Duplicate),
            Err(index) => {
                if self.fingerprints.len() >= MAX_INBOX_DEDUPLICATION_ENTRIES {
                    return Err(RecipientInboxDeduplicationError::CapacityExceeded);
                }
                self.fingerprints.insert(index, fingerprint);
                if let Err(error) = self.persist() {
                    self.fingerprints.remove(index);
                    return Err(error);
                }
                Ok(InboxDeduplicationResult::Accepted)
            }
        }
    }

    fn persist(&mut self) -> Result<(), RecipientInboxDeduplicationError> {
        let document = StateDocument::new(encode_fingerprints(&self.fingerprints)?)
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
    #[error("recipient inbox deduplication state document has duplicate fingerprints")]
    DuplicateFingerprint,
    #[error("recipient inbox deduplication state document has trailing bytes")]
    TrailingBytes,
    #[error("recipient inbox deduplication state document is not canonical")]
    NonCanonicalEncoding,
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

fn encode_fingerprints(
    fingerprints: &[[u8; ENVELOPE_FINGERPRINT_BYTES]],
) -> Result<Vec<u8>, RecipientInboxDeduplicationError> {
    if fingerprints.len() > MAX_INBOX_DEDUPLICATION_ENTRIES {
        return Err(RecipientInboxDeduplicationError::TooManyEntries);
    }
    if fingerprints.windows(2).any(|pair| pair[0] >= pair[1]) {
        return Err(RecipientInboxDeduplicationError::NonCanonicalEncoding);
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(INBOX_DEDUPLICATION_FIELDS)
        .map_err(|_| RecipientInboxDeduplicationError::Encode)?
        .u8(INBOX_DEDUPLICATION_SCHEMA_VERSION)
        .map_err(|_| RecipientInboxDeduplicationError::Encode)?
        .array(
            u64::try_from(fingerprints.len())
                .map_err(|_| RecipientInboxDeduplicationError::Encode)?,
        )
        .map_err(|_| RecipientInboxDeduplicationError::Encode)?;
    for fingerprint in fingerprints {
        encoder
            .bytes(fingerprint)
            .map_err(|_| RecipientInboxDeduplicationError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_fingerprints(
    encoded: &[u8],
) -> Result<Vec<[u8; ENVELOPE_FINGERPRINT_BYTES]>, RecipientInboxDeduplicationError> {
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
    if version != INBOX_DEDUPLICATION_SCHEMA_VERSION {
        return Err(RecipientInboxDeduplicationError::UnsupportedSchemaVersion(
            version,
        ));
    }
    let count = decoder
        .array()
        .map_err(RecipientInboxDeduplicationError::InvalidState)?
        .ok_or(RecipientInboxDeduplicationError::InvalidShape)?;
    let count =
        usize::try_from(count).map_err(|_| RecipientInboxDeduplicationError::TooManyEntries)?;
    if count > MAX_INBOX_DEDUPLICATION_ENTRIES {
        return Err(RecipientInboxDeduplicationError::TooManyEntries);
    }
    let mut fingerprints = Vec::with_capacity(count);
    for _ in 0..count {
        let fingerprint = decoder
            .bytes()
            .map_err(RecipientInboxDeduplicationError::InvalidState)?
            .try_into()
            .map_err(|_| RecipientInboxDeduplicationError::InvalidFingerprint)?;
        fingerprints.push(fingerprint);
    }
    if decoder.position() != encoded.len() {
        return Err(RecipientInboxDeduplicationError::TrailingBytes);
    }
    for pair in fingerprints.windows(2) {
        if pair[0] == pair[1] {
            return Err(RecipientInboxDeduplicationError::DuplicateFingerprint);
        }
        if pair[0] > pair[1] {
            return Err(RecipientInboxDeduplicationError::NonCanonicalEncoding);
        }
    }
    if encode_fingerprints(&fingerprints)? != encoded {
        return Err(RecipientInboxDeduplicationError::NonCanonicalEncoding);
    }
    Ok(fingerprints)
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_protocol::EncryptedMessageEnvelope;

    use super::{
        InboxDeduplicationResult, RecipientInboxDeduplication, RecipientInboxDeduplicationError,
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
            "yeokcham-recipient-inbox-{name}-{}.sqlite",
            std::process::id()
        ))
    }

    #[test]
    fn deduplicates_envelopes_across_restarts() {
        let path = path("persistence");
        let mut keystore = MemoryKeystore::default();
        let first = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap();
        let second = EncryptedMessageEnvelope::new(vec![0xc3], vec![0xd4]).unwrap();
        {
            let mut inbox = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
            assert!(matches!(
                inbox.record(&first),
                Ok(InboxDeduplicationResult::Accepted)
            ));
            assert!(matches!(
                inbox.record(&first),
                Ok(InboxDeduplicationResult::Duplicate)
            ));
            assert!(matches!(
                inbox.record(&second),
                Ok(InboxDeduplicationResult::Accepted)
            ));
            assert_eq!(inbox.len(), 2);
        }
        let mut restored = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
        assert!(matches!(
            restored.record(&first),
            Ok(InboxDeduplicationResult::Duplicate)
        ));
        assert!(matches!(
            restored.record(&second),
            Ok(InboxDeduplicationResult::Duplicate)
        ));
        assert_eq!(restored.len(), 2);
        drop(restored);
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
}
