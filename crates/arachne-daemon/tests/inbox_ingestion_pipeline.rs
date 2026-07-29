use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    EncryptedStateStore, InboxDeduplicationResult, RecipientInboxDeduplication,
    RecipientInboxDeduplicationError, StateDocument,
};
use arachne_protocol::{EncryptedMessageEnvelope, MAX_ENCRYPTED_HEADER_BYTES};

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

#[derive(Default)]
struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

impl OsKeystore for MemoryKeystore {
    type Error = Infallible;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .0
            .get(entry.as_str())
            .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
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

fn database_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "arachne-inbox-ingestion-pipeline-{}-{number}",
            std::process::id()
        ))
        .join("inbox.sqlite")
}

#[test]
fn persists_incoming_messages_and_deduplicates_retries_after_reopen() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc3]).unwrap();
    {
        let mut inbox = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
        assert_eq!(
            inbox.record_at(&envelope, 123).unwrap(),
            InboxDeduplicationResult::Accepted
        );
    }
    let mut inbox = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
    assert_eq!(inbox.len(), 1);
    let message = inbox.messages().next().unwrap();
    assert_eq!(message.received_at(), 123);
    assert_eq!(message.encrypted_header_bytes(), 1);
    assert_eq!(message.ciphertext_bytes(), 2);
    assert_eq!(
        inbox.record_at(&envelope, 124).unwrap(),
        InboxDeduplicationResult::Duplicate
    );
    drop(inbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn fails_closed_for_malformed_persisted_inbox_state() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    EncryptedStateStore::open(&path, &mut keystore)
        .unwrap()
        .replace(&StateDocument::new(vec![0xa1]).unwrap())
        .unwrap();
    assert!(matches!(
        RecipientInboxDeduplication::open(&path, &mut keystore),
        Err(RecipientInboxDeduplicationError::InvalidState(_))
    ));
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn retains_an_envelope_at_the_public_header_limit() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let envelope =
        EncryptedMessageEnvelope::new(vec![0xa1; MAX_ENCRYPTED_HEADER_BYTES], vec![0xb2]).unwrap();
    let mut inbox = RecipientInboxDeduplication::open(&path, &mut keystore).unwrap();
    assert_eq!(
        inbox.record_at(&envelope, u64::MAX).unwrap(),
        InboxDeduplicationResult::Accepted
    );
    let message = inbox.messages().next().unwrap();
    assert_eq!(message.received_at(), u64::MAX);
    assert_eq!(message.encrypted_header_bytes(), MAX_ENCRYPTED_HEADER_BYTES);
    assert_eq!(message.ciphertext_bytes(), 1);
    drop(inbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
