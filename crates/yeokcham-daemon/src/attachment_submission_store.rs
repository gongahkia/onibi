use std::{
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
};

use yeokcham_protocol::{
    ATTACHMENT_IDENTIFIER_BYTES, AttachmentIdentifier, AttachmentUploadJournal,
    EncryptedAttachmentChunk, EncryptedAttachmentManifest, MAX_ENCODED_ATTACHMENT_CHUNK_BYTES,
    MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES,
};

use crate::{AttachmentTransferJournalStore, ClientStateDirectory};

pub const MAX_DAEMON_ATTACHMENT_CHUNKS: usize = 1_600;
pub const MAX_DAEMON_ATTACHMENT_MANIFEST_BYTES: usize =
    MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES - 1024;
const MANIFEST_FILE: &str = "manifest.cbor";
const JOURNAL_FILE: &str = "journal.cbor";
const CHUNK_FILE_PREFIX: &str = "chunk-";
const CHUNK_FILE_SUFFIX: &str = ".cbor";
const HEX: &[u8; 16] = b"0123456789abcdef";

#[derive(Clone)]
pub struct AttachmentSubmissionStore {
    layout: ClientStateDirectory,
}

impl AttachmentSubmissionStore {
    #[must_use]
    pub const fn new(layout: ClientStateDirectory) -> Self {
        Self { layout }
    }

    pub fn begin(
        &self,
        encoded_manifest: &[u8],
    ) -> Result<AttachmentUploadSubmission, AttachmentSubmissionStoreError> {
        let manifest = decode_manifest(encoded_manifest)?;
        let identifier = manifest.identifier();
        let root = self.layout.attachment_uploads_path();
        ensure_directory(&root)?;
        let name = encode_identifier(identifier);
        let destination = root.join(&name);
        validate_destination(&destination)?;
        let staging = root.join(format!(".{name}.pending"));
        match fs::create_dir(&staging) {
            Ok(()) => {}
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                return Err(AttachmentSubmissionStoreError::AlreadyExists);
            }
            Err(_) => return Err(AttachmentSubmissionStoreError::Io),
        }
        let result = write_new_file(&staging.join(MANIFEST_FILE), encoded_manifest);
        if result.is_err() {
            let _ = fs::remove_dir_all(&staging);
        }
        result.map(|()| AttachmentUploadSubmission {
            identifier,
            destination,
            staging,
            hashes: Vec::new(),
            committed: false,
        })
    }

    pub fn status(
        &self,
        identifier: AttachmentIdentifier,
    ) -> Result<AttachmentSubmissionStatus, AttachmentSubmissionStoreError> {
        let directory = self
            .layout
            .attachment_uploads_path()
            .join(encode_identifier(identifier));
        match fs::symlink_metadata(&directory) {
            Ok(metadata) if metadata.file_type().is_symlink() || !metadata.is_dir() => {
                return Err(AttachmentSubmissionStoreError::InvalidDirectory);
            }
            Ok(_) => {}
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                return Err(AttachmentSubmissionStoreError::NotFound);
            }
            Err(_) => return Err(AttachmentSubmissionStoreError::Io),
        }
        let journal = AttachmentTransferJournalStore::open(&self.layout, identifier)
            .and_then(|store| store.load())
            .map_err(|_| AttachmentSubmissionStoreError::Journal)?;
        let chunk_count = u32::try_from(journal.chunk_count())
            .map_err(|_| AttachmentSubmissionStoreError::Journal)?;
        Ok(AttachmentSubmissionStatus {
            identifier,
            chunk_count,
            complete: journal.is_complete(),
            next_pending_index: journal.next_pending_index(),
        })
    }
}

pub struct AttachmentUploadSubmission {
    identifier: AttachmentIdentifier,
    destination: PathBuf,
    staging: PathBuf,
    hashes: Vec<yeokcham_protocol::AttachmentChunkHash>,
    committed: bool,
}

impl AttachmentUploadSubmission {
    pub fn append_chunk(
        &mut self,
        encoded_chunk: &[u8],
    ) -> Result<(), AttachmentSubmissionStoreError> {
        if self.hashes.len() >= MAX_DAEMON_ATTACHMENT_CHUNKS {
            return Err(AttachmentSubmissionStoreError::TooManyChunks);
        }
        let chunk = decode_chunk(encoded_chunk)?;
        let expected_index = u32::try_from(self.hashes.len())
            .map_err(|_| AttachmentSubmissionStoreError::TooManyChunks)?;
        if chunk.identifier() != self.identifier {
            return Err(AttachmentSubmissionStoreError::IdentifierMismatch);
        }
        if chunk.index() != expected_index {
            return Err(AttachmentSubmissionStoreError::ChunkOrder);
        }
        let hash = chunk
            .hash()
            .map_err(|_| AttachmentSubmissionStoreError::InvalidChunk)?;
        write_new_file(
            &self.staging.join(chunk_file_name(expected_index)),
            encoded_chunk,
        )?;
        self.hashes.push(hash);
        Ok(())
    }

    pub fn finish(mut self) -> Result<AttachmentSubmissionStatus, AttachmentSubmissionStoreError> {
        let journal = AttachmentUploadJournal::from_chunk_hashes(
            self.identifier,
            std::mem::take(&mut self.hashes),
        )
        .map_err(|error| match error {
            yeokcham_protocol::AttachmentUploadError::EmptyAttachment => {
                AttachmentSubmissionStoreError::EmptyAttachment
            }
            _ => AttachmentSubmissionStoreError::Journal,
        })?;
        let encoded_journal = journal
            .encode()
            .map_err(|_| AttachmentSubmissionStoreError::Journal)?;
        write_new_file(&self.staging.join(JOURNAL_FILE), &encoded_journal)?;
        fs::rename(&self.staging, &self.destination)
            .map_err(|_| AttachmentSubmissionStoreError::Io)?;
        self.committed = true;
        let chunk_count = u32::try_from(journal.chunk_count())
            .map_err(|_| AttachmentSubmissionStoreError::Journal)?;
        Ok(AttachmentSubmissionStatus {
            identifier: self.identifier,
            chunk_count,
            complete: false,
            next_pending_index: Some(0),
        })
    }
}

impl Drop for AttachmentUploadSubmission {
    fn drop(&mut self) {
        if !self.committed {
            let _ = fs::remove_dir_all(&self.staging);
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AttachmentSubmissionStatus {
    identifier: AttachmentIdentifier,
    chunk_count: u32,
    complete: bool,
    next_pending_index: Option<u32>,
}

impl AttachmentSubmissionStatus {
    #[must_use]
    pub const fn identifier(self) -> AttachmentIdentifier {
        self.identifier
    }

    #[must_use]
    pub const fn chunk_count(self) -> u32 {
        self.chunk_count
    }

    #[must_use]
    pub const fn complete(self) -> bool {
        self.complete
    }

    #[must_use]
    pub const fn next_pending_index(self) -> Option<u32> {
        self.next_pending_index
    }
}

fn decode_manifest(
    encoded: &[u8],
) -> Result<EncryptedAttachmentManifest, AttachmentSubmissionStoreError> {
    if encoded.len() > MAX_DAEMON_ATTACHMENT_MANIFEST_BYTES {
        return Err(AttachmentSubmissionStoreError::InvalidManifest);
    }
    let manifest = EncryptedAttachmentManifest::decode(encoded)
        .map_err(|_| AttachmentSubmissionStoreError::InvalidManifest)?;
    if manifest
        .encode()
        .map_err(|_| AttachmentSubmissionStoreError::InvalidManifest)?
        != encoded
    {
        return Err(AttachmentSubmissionStoreError::InvalidManifest);
    }
    Ok(manifest)
}

fn decode_chunk(
    encoded: &[u8],
) -> Result<EncryptedAttachmentChunk, AttachmentSubmissionStoreError> {
    if encoded.len() > MAX_ENCODED_ATTACHMENT_CHUNK_BYTES {
        return Err(AttachmentSubmissionStoreError::InvalidChunk);
    }
    let chunk = EncryptedAttachmentChunk::decode(encoded)
        .map_err(|_| AttachmentSubmissionStoreError::InvalidChunk)?;
    if chunk
        .encode()
        .map_err(|_| AttachmentSubmissionStoreError::InvalidChunk)?
        != encoded
    {
        return Err(AttachmentSubmissionStoreError::InvalidChunk);
    }
    Ok(chunk)
}

fn ensure_directory(path: &Path) -> Result<(), AttachmentSubmissionStoreError> {
    match fs::symlink_metadata(path) {
        Ok(metadata) if metadata.file_type().is_symlink() || !metadata.is_dir() => {
            Err(AttachmentSubmissionStoreError::InvalidDirectory)
        }
        Ok(_) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            match fs::create_dir(path) {
                Ok(()) => {}
                Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {}
                Err(_) => return Err(AttachmentSubmissionStoreError::Io),
            }
            ensure_directory(path)
        }
        Err(_) => Err(AttachmentSubmissionStoreError::Io),
    }
}

fn validate_destination(path: &Path) -> Result<(), AttachmentSubmissionStoreError> {
    match fs::symlink_metadata(path) {
        Ok(metadata) if metadata.file_type().is_symlink() || !metadata.is_dir() => {
            Err(AttachmentSubmissionStoreError::InvalidDirectory)
        }
        Ok(_) => Err(AttachmentSubmissionStoreError::AlreadyExists),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(_) => Err(AttachmentSubmissionStoreError::Io),
    }
}

fn write_new_file(path: &Path, content: &[u8]) -> Result<(), AttachmentSubmissionStoreError> {
    let mut file = OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(path)
        .map_err(|_| AttachmentSubmissionStoreError::Io)?;
    file.write_all(content)
        .map_err(|_| AttachmentSubmissionStoreError::Io)?;
    file.sync_all()
        .map_err(|_| AttachmentSubmissionStoreError::Io)
}

fn chunk_file_name(index: u32) -> String {
    format!("{CHUNK_FILE_PREFIX}{index}{CHUNK_FILE_SUFFIX}")
}

fn encode_identifier(identifier: AttachmentIdentifier) -> String {
    let mut encoded = String::with_capacity(ATTACHMENT_IDENTIFIER_BYTES * 2);
    for byte in identifier.as_bytes() {
        encoded.push(char::from(HEX[usize::from(*byte >> 4)]));
        encoded.push(char::from(HEX[usize::from(*byte & 0x0f)]));
    }
    encoded
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentSubmissionStoreError {
    #[error("attachment submission I/O failed")]
    Io,
    #[error("attachment submission directory is invalid")]
    InvalidDirectory,
    #[error("attachment manifest is invalid")]
    InvalidManifest,
    #[error("attachment chunk is invalid")]
    InvalidChunk,
    #[error("attachment chunk identifier differs from its manifest")]
    IdentifierMismatch,
    #[error("attachment chunks must be ordered from index zero")]
    ChunkOrder,
    #[error("attachment submission exceeds its chunk limit")]
    TooManyChunks,
    #[error("attachment submission has no chunks")]
    EmptyAttachment,
    #[error("attachment submission already exists")]
    AlreadyExists,
    #[error("attachment submission does not exist")]
    NotFound,
    #[error("attachment submission journal is invalid")]
    Journal,
}

#[cfg(test)]
mod tests {
    use super::{
        AttachmentSubmissionStore, AttachmentSubmissionStoreError,
        MAX_DAEMON_ATTACHMENT_MANIFEST_BYTES,
    };
    use crate::ClientStateDirectory;

    #[test]
    fn rejects_manifest_records_above_the_grpc_message_boundary() {
        let store = AttachmentSubmissionStore::new(
            ClientStateDirectory::new("/tmp/yeokcham-attachment-submission-boundary").unwrap(),
        );
        assert!(matches!(
            store.begin(&vec![0; MAX_DAEMON_ATTACHMENT_MANIFEST_BYTES + 1]),
            Err(AttachmentSubmissionStoreError::InvalidManifest)
        ));
    }
}
