use std::fmt;

use minicbor::{Decoder, Encoder};

use crate::{
    AttachmentChunkError, AttachmentChunkHash, AttachmentIdentifier, EncryptedAttachmentChunk,
};

pub const ATTACHMENT_DOWNLOAD_JOURNAL_SCHEMA_VERSION: u8 = 1;
pub const MAX_ATTACHMENT_DOWNLOAD_JOURNAL_BYTES: usize = 4 * 1024 * 1024;
const JOURNAL_FIELDS: u64 = 3;
const JOURNAL_CHUNK_FIELDS: u64 = 2;
const MIN_ENCODED_JOURNAL_CHUNK_BYTES: usize = 36;

#[derive(Eq, PartialEq)]
pub struct AttachmentDownloadJournal {
    identifier: AttachmentIdentifier,
    chunks: Vec<AttachmentDownloadChunk>,
}

#[derive(Eq, PartialEq)]
struct AttachmentDownloadChunk {
    hash: AttachmentChunkHash,
    downloaded: bool,
}

impl AttachmentDownloadJournal {
    pub fn new(
        identifier: AttachmentIdentifier,
        expected_hashes: &[AttachmentChunkHash],
    ) -> Result<Self, AttachmentDownloadError> {
        if expected_hashes.is_empty() {
            return Err(AttachmentDownloadError::EmptyAttachment);
        }
        if expected_hashes.len()
            > MAX_ATTACHMENT_DOWNLOAD_JOURNAL_BYTES / MIN_ENCODED_JOURNAL_CHUNK_BYTES
        {
            return Err(AttachmentDownloadError::TooManyChunks);
        }
        let chunks = expected_hashes
            .iter()
            .copied()
            .map(|hash| AttachmentDownloadChunk {
                hash,
                downloaded: false,
            })
            .collect();
        Ok(Self { identifier, chunks })
    }

    #[must_use]
    pub const fn identifier(&self) -> AttachmentIdentifier {
        self.identifier
    }

    #[must_use]
    pub fn chunk_count(&self) -> usize {
        self.chunks.len()
    }

    #[must_use]
    pub fn expected_hash(&self, index: u32) -> Option<&AttachmentChunkHash> {
        usize::try_from(index)
            .ok()
            .and_then(|index| self.chunks.get(index))
            .map(|chunk| &chunk.hash)
    }

    #[must_use]
    pub fn next_pending_index(&self) -> Option<u32> {
        self.chunks
            .iter()
            .position(|chunk| !chunk.downloaded)
            .and_then(|index| u32::try_from(index).ok())
    }

    #[must_use]
    pub fn is_complete(&self) -> bool {
        self.chunks.iter().all(|chunk| chunk.downloaded)
    }

    pub fn mark_downloaded(
        &mut self,
        chunk: &EncryptedAttachmentChunk,
    ) -> Result<bool, AttachmentDownloadError> {
        if chunk.identifier() != self.identifier {
            return Err(AttachmentDownloadError::IdentifierMismatch);
        }
        let index =
            usize::try_from(chunk.index()).map_err(|_| AttachmentDownloadError::UnknownChunk)?;
        let entry = self
            .chunks
            .get_mut(index)
            .ok_or(AttachmentDownloadError::UnknownChunk)?;
        chunk
            .validate_hash(&entry.hash)
            .map_err(|_| AttachmentDownloadError::ChunkHashMismatch)?;
        let changed = !entry.downloaded;
        entry.downloaded = true;
        Ok(changed)
    }

    pub fn encode(&self) -> Result<Vec<u8>, AttachmentDownloadError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(JOURNAL_FIELDS)
            .map_err(|_| AttachmentDownloadError::Encode)?
            .u8(ATTACHMENT_DOWNLOAD_JOURNAL_SCHEMA_VERSION)
            .map_err(|_| AttachmentDownloadError::Encode)?
            .bytes(self.identifier.as_bytes())
            .map_err(|_| AttachmentDownloadError::Encode)?
            .array(
                u64::try_from(self.chunks.len())
                    .map_err(|_| AttachmentDownloadError::TooManyChunks)?,
            )
            .map_err(|_| AttachmentDownloadError::Encode)?;
        for chunk in &self.chunks {
            encoder
                .array(JOURNAL_CHUNK_FIELDS)
                .map_err(|_| AttachmentDownloadError::Encode)?
                .bytes(chunk.hash.as_bytes())
                .map_err(|_| AttachmentDownloadError::Encode)?
                .bool(chunk.downloaded)
                .map_err(|_| AttachmentDownloadError::Encode)?;
        }
        let output = encoder.into_writer();
        if output.len() > MAX_ATTACHMENT_DOWNLOAD_JOURNAL_BYTES {
            return Err(AttachmentDownloadError::JournalTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, AttachmentDownloadError> {
        if encoded.len() > MAX_ATTACHMENT_DOWNLOAD_JOURNAL_BYTES {
            return Err(AttachmentDownloadError::JournalTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| AttachmentDownloadError::Decode)?
            != Some(JOURNAL_FIELDS)
        {
            return Err(AttachmentDownloadError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| AttachmentDownloadError::Decode)?;
        if version != ATTACHMENT_DOWNLOAD_JOURNAL_SCHEMA_VERSION {
            return Err(AttachmentDownloadError::UnsupportedSchemaVersion(version));
        }
        let identifier = AttachmentIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| AttachmentDownloadError::Decode)?
                .try_into()
                .map_err(|_| AttachmentDownloadError::InvalidIdentifier)?,
        )
        .map_err(|_| AttachmentDownloadError::InvalidIdentifier)?;
        let count = decoder
            .array()
            .map_err(|_| AttachmentDownloadError::Decode)?
            .ok_or(AttachmentDownloadError::InvalidShape)?;
        let count = usize::try_from(count).map_err(|_| AttachmentDownloadError::TooManyChunks)?;
        if count > MAX_ATTACHMENT_DOWNLOAD_JOURNAL_BYTES / MIN_ENCODED_JOURNAL_CHUNK_BYTES {
            return Err(AttachmentDownloadError::TooManyChunks);
        }
        let mut chunks = Vec::with_capacity(count);
        for _ in 0..count {
            if decoder
                .array()
                .map_err(|_| AttachmentDownloadError::Decode)?
                != Some(JOURNAL_CHUNK_FIELDS)
            {
                return Err(AttachmentDownloadError::InvalidShape);
            }
            let hash = AttachmentChunkHash::from_bytes(
                decoder
                    .bytes()
                    .map_err(|_| AttachmentDownloadError::Decode)?
                    .try_into()
                    .map_err(|_| AttachmentDownloadError::InvalidChunkHash)?,
            );
            let downloaded = decoder
                .bool()
                .map_err(|_| AttachmentDownloadError::Decode)?;
            chunks.push(AttachmentDownloadChunk { hash, downloaded });
        }
        if chunks.is_empty() {
            return Err(AttachmentDownloadError::EmptyAttachment);
        }
        if decoder.position() != encoded.len() {
            return Err(AttachmentDownloadError::TrailingBytes);
        }
        let journal = Self { identifier, chunks };
        if journal.encode()? != encoded {
            return Err(AttachmentDownloadError::NonCanonicalEncoding);
        }
        Ok(journal)
    }
}

impl fmt::Debug for AttachmentDownloadJournal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("AttachmentDownloadJournal")
            .field("identifier", &self.identifier)
            .field("chunk_count", &self.chunks.len())
            .field("complete", &self.is_complete())
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentDownloadError {
    #[error("attachment download requires at least one chunk")]
    EmptyAttachment,
    #[error("attachment download contains too many chunks")]
    TooManyChunks,
    #[error("attachment download contains a mismatched attachment identifier")]
    IdentifierMismatch,
    #[error("attachment download references an unknown chunk")]
    UnknownChunk,
    #[error("attachment download chunk hash does not match the journal")]
    ChunkHashMismatch,
    #[error("attachment download journal contains an invalid attachment identifier")]
    InvalidIdentifier,
    #[error("attachment download journal contains an invalid chunk hash")]
    InvalidChunkHash,
    #[error("attachment download journal is too large")]
    JournalTooLarge,
    #[error("attachment download journal has an invalid shape")]
    InvalidShape,
    #[error("attachment download journal schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("attachment download journal decoding failed")]
    Decode,
    #[error("attachment download journal encoding failed")]
    Encode,
    #[error("attachment download journal has trailing bytes")]
    TrailingBytes,
    #[error("attachment download journal is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("attachment chunk validation failed")]
    Chunk(#[source] AttachmentChunkError),
}

#[cfg(test)]
mod tests {
    use super::{AttachmentDownloadError, AttachmentDownloadJournal};
    use crate::{
        ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, EncryptedAttachmentChunk,
    };

    fn chunk(identifier: AttachmentIdentifier, index: u32) -> EncryptedAttachmentChunk {
        let key = AttachmentKey::derive(&[0x11; 32], identifier)
            .unwrap()
            .derive_chunk_key(index)
            .unwrap();
        EncryptedAttachmentChunk::encrypt(
            identifier,
            index,
            &key,
            &vec![u8::try_from(index).unwrap(); ATTACHMENT_CHUNK_BYTES],
        )
        .unwrap()
    }

    #[test]
    fn resumes_an_attachment_download_from_a_persisted_journal() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let first = chunk(identifier, 0);
        let second = chunk(identifier, 1);
        let hashes = [first.hash().unwrap(), second.hash().unwrap()];
        let mut journal = AttachmentDownloadJournal::new(identifier, &hashes).unwrap();

        assert_eq!(journal.next_pending_index(), Some(0));
        assert_eq!(journal.expected_hash(1), Some(&hashes[1]));
        assert!(journal.mark_downloaded(&second).unwrap());
        let mut restored = AttachmentDownloadJournal::decode(&journal.encode().unwrap()).unwrap();
        assert_eq!(restored.next_pending_index(), Some(0));
        assert!(restored.mark_downloaded(&first).unwrap());
        assert!(restored.is_complete());
        assert!(!restored.mark_downloaded(&first).unwrap());
        assert_eq!(restored.next_pending_index(), None);
    }

    #[test]
    fn rejects_foreign_unknown_modified_or_malformed_downloads() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let first = chunk(identifier, 0);
        let second = chunk(identifier, 1);
        let mut journal =
            AttachmentDownloadJournal::new(identifier, &[first.hash().unwrap()]).unwrap();
        let foreign_identifier = AttachmentIdentifier::from_bytes([0x33; 16]).unwrap();
        assert_eq!(
            journal.mark_downloaded(&chunk(foreign_identifier, 0)),
            Err(AttachmentDownloadError::IdentifierMismatch)
        );
        assert_eq!(
            journal.mark_downloaded(&second),
            Err(AttachmentDownloadError::UnknownChunk)
        );
        assert_eq!(
            journal.mark_downloaded(&chunk(identifier, 0)),
            Err(AttachmentDownloadError::ChunkHashMismatch)
        );
        let mut encoded = journal.encode().unwrap();
        encoded.push(0);
        assert_eq!(
            AttachmentDownloadJournal::decode(&encoded),
            Err(AttachmentDownloadError::TrailingBytes)
        );
    }
}
