use std::{
    fs,
    path::{Path, PathBuf},
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_daemon::{
    AttachmentJournalStoreError, AttachmentTransferJournalStore, ClientStateDirectory,
};
use yeokcham_protocol::{
    ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, AttachmentUploadJournal,
    EncryptedAttachmentChunk,
};

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

fn state_directory() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-attachment-transfer-journal-{}-{number}",
        std::process::id()
    ))
}

fn identifier_directory(root: &Path, identifier: AttachmentIdentifier) -> PathBuf {
    let mut encoded = String::with_capacity(identifier.as_bytes().len() * 2);
    for byte in identifier.as_bytes() {
        encoded.push(char::from(b"0123456789abcdef"[usize::from(*byte >> 4)]));
        encoded.push(char::from(b"0123456789abcdef"[usize::from(*byte & 0x0f)]));
    }
    root.join("attachment-uploads").join(encoded)
}

fn chunks(identifier: AttachmentIdentifier, count: u32) -> Vec<EncryptedAttachmentChunk> {
    let key = AttachmentKey::derive(&[0x11; 32], identifier).unwrap();
    (0..count)
        .map(|index| {
            EncryptedAttachmentChunk::encrypt(
                identifier,
                index,
                &key.derive_chunk_key(index).unwrap(),
                &vec![u8::try_from(index).unwrap(); ATTACHMENT_CHUNK_BYTES],
            )
            .unwrap()
        })
        .collect()
}

#[test]
fn persists_uploaded_progress_and_restores_the_next_pending_chunk() {
    let root = state_directory();
    let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
    let chunks = chunks(identifier, 2);
    let mut journal = AttachmentUploadJournal::new(&chunks).unwrap();
    fs::create_dir_all(identifier_directory(&root, identifier)).unwrap();
    let layout = ClientStateDirectory::new(&root).unwrap();
    let store = AttachmentTransferJournalStore::open(&layout, identifier).unwrap();

    store.save(&journal).unwrap();
    assert!(store.mark_uploaded(&mut journal, &chunks[0]).unwrap());
    assert_eq!(journal.next_pending_index(), Some(1));

    let restored = AttachmentTransferJournalStore::open(&layout, identifier)
        .unwrap()
        .load()
        .unwrap();
    assert_eq!(restored.next_pending_index(), Some(1));
    assert!(!restored.is_complete());
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn rejects_a_journal_from_a_different_attachment_submission() {
    let root = state_directory();
    let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
    let foreign_identifier = AttachmentIdentifier::from_bytes([0x33; 16]).unwrap();
    let foreign_journal = AttachmentUploadJournal::new(&chunks(foreign_identifier, 1)).unwrap();
    let directory = identifier_directory(&root, identifier);
    fs::create_dir_all(&directory).unwrap();
    fs::write(
        directory.join("journal.cbor"),
        foreign_journal.encode().unwrap(),
    )
    .unwrap();
    let layout = ClientStateDirectory::new(&root).unwrap();

    assert_eq!(
        AttachmentTransferJournalStore::open(&layout, identifier)
            .unwrap()
            .load(),
        Err(AttachmentJournalStoreError::IdentifierMismatch)
    );
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn rejects_a_nonregular_journal_path_without_mutating_progress() {
    let root = state_directory();
    let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
    let chunks = chunks(identifier, 1);
    let mut journal = AttachmentUploadJournal::new(&chunks).unwrap();
    let directory = identifier_directory(&root, identifier);
    fs::create_dir_all(&directory).unwrap();
    fs::create_dir(directory.join("journal.cbor")).unwrap();
    let layout = ClientStateDirectory::new(&root).unwrap();
    let store = AttachmentTransferJournalStore::open(&layout, identifier).unwrap();

    assert_eq!(
        store.mark_uploaded(&mut journal, &chunks[0]),
        Err(AttachmentJournalStoreError::InvalidJournalPath)
    );
    assert_eq!(journal.next_pending_index(), Some(0));
    fs::remove_dir_all(root).unwrap();
}
