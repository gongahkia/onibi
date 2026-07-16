use std::{
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_protocol::{
    AttachmentIdentifier, AttachmentUploadJournal, EncryptedAttachmentChunk,
    MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES,
};

use crate::ClientStateDirectory;

const JOURNAL_FILE: &str = "journal.cbor";
const HEX: &[u8; 16] = b"0123456789abcdef";
static NEXT_TEMPORARY_FILE: AtomicU64 = AtomicU64::new(0);

pub struct AttachmentTransferJournalStore {
    directory: PathBuf,
    identifier: AttachmentIdentifier,
}

impl AttachmentTransferJournalStore {
    pub fn open(
        layout: &ClientStateDirectory,
        identifier: AttachmentIdentifier,
    ) -> Result<Self, AttachmentJournalStoreError> {
        let root = layout.attachment_uploads_path();
        require_directory(&root)?;
        let directory = root.join(encode_identifier(identifier));
        require_directory(&directory)?;
        Ok(Self {
            directory,
            identifier,
        })
    }

    pub fn load(&self) -> Result<AttachmentUploadJournal, AttachmentJournalStoreError> {
        let path = self.journal_path();
        require_regular_file(&path)?;
        let metadata = fs::metadata(&path).map_err(|_| AttachmentJournalStoreError::Io)?;
        if metadata.len() > MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES as u64 {
            return Err(AttachmentJournalStoreError::Journal);
        }
        let encoded = fs::read(&path).map_err(|_| AttachmentJournalStoreError::Io)?;
        if encoded.len() > MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES {
            return Err(AttachmentJournalStoreError::Journal);
        }
        let journal = AttachmentUploadJournal::decode(&encoded)
            .map_err(|_| AttachmentJournalStoreError::Journal)?;
        if journal.identifier() != self.identifier {
            return Err(AttachmentJournalStoreError::IdentifierMismatch);
        }
        Ok(journal)
    }

    pub fn save(
        &self,
        journal: &AttachmentUploadJournal,
    ) -> Result<(), AttachmentJournalStoreError> {
        if journal.identifier() != self.identifier {
            return Err(AttachmentJournalStoreError::IdentifierMismatch);
        }
        let encoded = journal
            .encode()
            .map_err(|_| AttachmentJournalStoreError::Journal)?;
        require_directory(&self.directory)?;
        validate_journal_destination(&self.journal_path())?;
        write_atomic(&self.directory, &self.journal_path(), &encoded)
    }

    pub fn mark_uploaded(
        &self,
        journal: &mut AttachmentUploadJournal,
        chunk: &EncryptedAttachmentChunk,
    ) -> Result<bool, AttachmentJournalStoreError> {
        let prior = journal
            .encode()
            .map_err(|_| AttachmentJournalStoreError::Journal)?;
        let changed = journal
            .mark_uploaded(chunk)
            .map_err(|_| AttachmentJournalStoreError::Journal)?;
        if !changed {
            return Ok(false);
        }
        if let Err(error) = self.save(journal) {
            *journal = AttachmentUploadJournal::decode(&prior)
                .map_err(|_| AttachmentJournalStoreError::Journal)?;
            return Err(error);
        }
        Ok(true)
    }

    fn journal_path(&self) -> PathBuf {
        self.directory.join(JOURNAL_FILE)
    }
}

fn require_directory(path: &Path) -> Result<(), AttachmentJournalStoreError> {
    let metadata = fs::symlink_metadata(path).map_err(|_| AttachmentJournalStoreError::Io)?;
    if metadata.file_type().is_symlink() || !metadata.is_dir() {
        return Err(AttachmentJournalStoreError::InvalidDirectory);
    }
    Ok(())
}

fn require_regular_file(path: &Path) -> Result<(), AttachmentJournalStoreError> {
    let metadata = fs::symlink_metadata(path).map_err(|_| AttachmentJournalStoreError::Io)?;
    if metadata.file_type().is_symlink() || !metadata.is_file() {
        return Err(AttachmentJournalStoreError::InvalidJournalPath);
    }
    Ok(())
}

fn validate_journal_destination(path: &Path) -> Result<(), AttachmentJournalStoreError> {
    match fs::symlink_metadata(path) {
        Ok(metadata) if metadata.file_type().is_symlink() || !metadata.is_file() => {
            Err(AttachmentJournalStoreError::InvalidJournalPath)
        }
        Ok(_) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(_) => Err(AttachmentJournalStoreError::Io),
    }
}

fn write_atomic(
    directory: &Path,
    journal_path: &Path,
    encoded: &[u8],
) -> Result<(), AttachmentJournalStoreError> {
    let sequence = NEXT_TEMPORARY_FILE.fetch_add(1, Ordering::Relaxed);
    let temporary = directory.join(format!(
        ".{JOURNAL_FILE}-{}-{sequence}.pending",
        std::process::id()
    ));
    let result = (|| {
        let mut file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&temporary)
            .map_err(|_| AttachmentJournalStoreError::Io)?;
        file.write_all(encoded)
            .map_err(|_| AttachmentJournalStoreError::Io)?;
        file.sync_all()
            .map_err(|_| AttachmentJournalStoreError::Io)?;
        drop(file);
        fs::rename(&temporary, journal_path).map_err(|_| AttachmentJournalStoreError::Io)
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}

fn encode_identifier(identifier: AttachmentIdentifier) -> String {
    let mut encoded = String::with_capacity(identifier.as_bytes().len() * 2);
    for byte in identifier.as_bytes() {
        encoded.push(char::from(HEX[usize::from(*byte >> 4)]));
        encoded.push(char::from(HEX[usize::from(*byte & 0x0f)]));
    }
    encoded
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentJournalStoreError {
    #[error("attachment journal I/O failed")]
    Io,
    #[error("attachment upload root or submission path is invalid")]
    InvalidDirectory,
    #[error("attachment journal path is not a regular file")]
    InvalidJournalPath,
    #[error("attachment journal is invalid")]
    Journal,
    #[error("attachment journal identifier does not match its submission")]
    IdentifierMismatch,
}
