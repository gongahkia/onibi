use std::fmt;

use minicbor::{Decoder, Encoder};

use crate::{
    AttachmentChunkError, AttachmentChunkHash, AttachmentIdentifier, EncryptedAttachmentChunk,
};

pub const ATTACHMENT_UPLOAD_JOURNAL_SCHEMA_VERSION: u8 = 1;
pub const MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES: usize = 4 * 1024 * 1024;
const JOURNAL_FIELDS: u64 = 3;
const JOURNAL_CHUNK_FIELDS: u64 = 2;
const MIN_ENCODED_JOURNAL_CHUNK_BYTES: usize = 36;

#[derive(Eq, PartialEq)]
pub struct AttachmentUploadJournal {
    identifier: AttachmentIdentifier,
    chunks: Vec<AttachmentUploadChunk>,
}

#[derive(Eq, PartialEq)]
struct AttachmentUploadChunk {
    hash: AttachmentChunkHash,
    uploaded: bool,
}

impl AttachmentUploadJournal {
    pub fn new(chunks: &[EncryptedAttachmentChunk]) -> Result<Self, AttachmentUploadError> {
        let first = chunks
            .first()
            .ok_or(AttachmentUploadError::EmptyAttachment)?;
        let identifier = first.identifier();
        let mut hashes = Vec::with_capacity(chunks.len());
        for (expected_index, chunk) in chunks.iter().enumerate() {
            let expected_index =
                u32::try_from(expected_index).map_err(|_| AttachmentUploadError::TooManyChunks)?;
            if chunk.identifier() != identifier {
                return Err(AttachmentUploadError::IdentifierMismatch);
            }
            if chunk.index() != expected_index {
                return Err(AttachmentUploadError::InvalidChunkOrder);
            }
            hashes.push(chunk.hash().map_err(AttachmentUploadError::Chunk)?);
        }
        Self::from_chunk_hashes(identifier, hashes)
    }

    pub fn from_chunk_hashes(
        identifier: AttachmentIdentifier,
        hashes: Vec<AttachmentChunkHash>,
    ) -> Result<Self, AttachmentUploadError> {
        if hashes.is_empty() {
            return Err(AttachmentUploadError::EmptyAttachment);
        }
        if hashes.len() > MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES / MIN_ENCODED_JOURNAL_CHUNK_BYTES {
            return Err(AttachmentUploadError::TooManyChunks);
        }
        let entries = hashes
            .into_iter()
            .map(|hash| AttachmentUploadChunk {
                hash,
                uploaded: false,
            })
            .collect();
        Ok(Self {
            identifier,
            chunks: entries,
        })
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
            .position(|chunk| !chunk.uploaded)
            .and_then(|index| u32::try_from(index).ok())
    }

    #[must_use]
    pub fn is_complete(&self) -> bool {
        self.chunks.iter().all(|chunk| chunk.uploaded)
    }

    pub fn mark_uploaded(
        &mut self,
        chunk: &EncryptedAttachmentChunk,
    ) -> Result<bool, AttachmentUploadError> {
        if chunk.identifier() != self.identifier {
            return Err(AttachmentUploadError::IdentifierMismatch);
        }
        let index =
            usize::try_from(chunk.index()).map_err(|_| AttachmentUploadError::UnknownChunk)?;
        let entry = self
            .chunks
            .get_mut(index)
            .ok_or(AttachmentUploadError::UnknownChunk)?;
        chunk
            .validate_hash(&entry.hash)
            .map_err(|_| AttachmentUploadError::ChunkHashMismatch)?;
        let changed = !entry.uploaded;
        entry.uploaded = true;
        Ok(changed)
    }

    pub fn encode(&self) -> Result<Vec<u8>, AttachmentUploadError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(JOURNAL_FIELDS)
            .map_err(|_| AttachmentUploadError::Encode)?
            .u8(ATTACHMENT_UPLOAD_JOURNAL_SCHEMA_VERSION)
            .map_err(|_| AttachmentUploadError::Encode)?
            .bytes(self.identifier.as_bytes())
            .map_err(|_| AttachmentUploadError::Encode)?
            .array(
                u64::try_from(self.chunks.len())
                    .map_err(|_| AttachmentUploadError::TooManyChunks)?,
            )
            .map_err(|_| AttachmentUploadError::Encode)?;
        for chunk in &self.chunks {
            encoder
                .array(JOURNAL_CHUNK_FIELDS)
                .map_err(|_| AttachmentUploadError::Encode)?
                .bytes(chunk.hash.as_bytes())
                .map_err(|_| AttachmentUploadError::Encode)?
                .bool(chunk.uploaded)
                .map_err(|_| AttachmentUploadError::Encode)?;
        }
        let output = encoder.into_writer();
        if output.len() > MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES {
            return Err(AttachmentUploadError::JournalTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, AttachmentUploadError> {
        if encoded.len() > MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES {
            return Err(AttachmentUploadError::JournalTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| AttachmentUploadError::Decode)? != Some(JOURNAL_FIELDS) {
            return Err(AttachmentUploadError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| AttachmentUploadError::Decode)?;
        if version != ATTACHMENT_UPLOAD_JOURNAL_SCHEMA_VERSION {
            return Err(AttachmentUploadError::UnsupportedSchemaVersion(version));
        }
        let identifier = AttachmentIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| AttachmentUploadError::Decode)?
                .try_into()
                .map_err(|_| AttachmentUploadError::InvalidIdentifier)?,
        )
        .map_err(|_| AttachmentUploadError::InvalidIdentifier)?;
        let count = decoder
            .array()
            .map_err(|_| AttachmentUploadError::Decode)?
            .ok_or(AttachmentUploadError::InvalidShape)?;
        let count = usize::try_from(count).map_err(|_| AttachmentUploadError::TooManyChunks)?;
        if count > MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES / MIN_ENCODED_JOURNAL_CHUNK_BYTES {
            return Err(AttachmentUploadError::TooManyChunks);
        }
        let mut chunks = Vec::with_capacity(count);
        for _ in 0..count {
            if decoder.array().map_err(|_| AttachmentUploadError::Decode)?
                != Some(JOURNAL_CHUNK_FIELDS)
            {
                return Err(AttachmentUploadError::InvalidShape);
            }
            let hash = AttachmentChunkHash::from_bytes(
                decoder
                    .bytes()
                    .map_err(|_| AttachmentUploadError::Decode)?
                    .try_into()
                    .map_err(|_| AttachmentUploadError::InvalidChunkHash)?,
            );
            let uploaded = decoder.bool().map_err(|_| AttachmentUploadError::Decode)?;
            chunks.push(AttachmentUploadChunk { hash, uploaded });
        }
        if chunks.is_empty() {
            return Err(AttachmentUploadError::EmptyAttachment);
        }
        if decoder.position() != encoded.len() {
            return Err(AttachmentUploadError::TrailingBytes);
        }
        let journal = Self { identifier, chunks };
        if journal.encode()? != encoded {
            return Err(AttachmentUploadError::NonCanonicalEncoding);
        }
        Ok(journal)
    }
}

impl fmt::Debug for AttachmentUploadJournal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("AttachmentUploadJournal")
            .field("identifier", &self.identifier)
            .field("chunk_count", &self.chunks.len())
            .field("complete", &self.is_complete())
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentUploadError {
    #[error("attachment upload requires at least one chunk")]
    EmptyAttachment,
    #[error("attachment upload contains too many chunks")]
    TooManyChunks,
    #[error("attachment upload contains mismatched attachment identifiers")]
    IdentifierMismatch,
    #[error("attachment upload chunks must be ordered from index zero")]
    InvalidChunkOrder,
    #[error("attachment upload references an unknown chunk")]
    UnknownChunk,
    #[error("attachment upload chunk hash does not match the journal")]
    ChunkHashMismatch,
    #[error("attachment upload journal contains an invalid attachment identifier")]
    InvalidIdentifier,
    #[error("attachment upload journal contains an invalid chunk hash")]
    InvalidChunkHash,
    #[error("attachment upload journal is too large")]
    JournalTooLarge,
    #[error("attachment upload journal has an invalid shape")]
    InvalidShape,
    #[error("attachment upload journal schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("attachment upload journal decoding failed")]
    Decode,
    #[error("attachment upload journal encoding failed")]
    Encode,
    #[error("attachment upload journal has trailing bytes")]
    TrailingBytes,
    #[error("attachment upload journal is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("attachment chunk validation failed")]
    Chunk(#[source] AttachmentChunkError),
}

#[cfg(test)]
mod tests {
    use super::{AttachmentUploadError, AttachmentUploadJournal};
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
    fn resumes_an_attachment_upload_from_a_persisted_journal() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let first = chunk(identifier, 0);
        let second = chunk(identifier, 1);
        let mut journal = AttachmentUploadJournal::new(&[first.clone(), second.clone()]).unwrap();

        assert_eq!(journal.next_pending_index(), Some(0));
        assert!(journal.mark_uploaded(&second).unwrap());
        let mut restored = AttachmentUploadJournal::decode(&journal.encode().unwrap()).unwrap();
        assert_eq!(restored.next_pending_index(), Some(0));
        assert!(restored.mark_uploaded(&first).unwrap());
        assert!(restored.is_complete());
        assert!(!restored.mark_uploaded(&first).unwrap());
        assert_eq!(restored.next_pending_index(), None);
    }

    #[test]
    fn rejects_foreign_out_of_order_or_modified_upload_chunks() {
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let first = chunk(identifier, 0);
        let second = chunk(identifier, 1);
        assert_eq!(
            AttachmentUploadJournal::new(&[second.clone(), first.clone()]),
            Err(AttachmentUploadError::InvalidChunkOrder)
        );
        let mut journal = AttachmentUploadJournal::new(std::slice::from_ref(&first)).unwrap();
        let foreign_identifier = AttachmentIdentifier::from_bytes([0x33; 16]).unwrap();
        assert_eq!(
            journal.mark_uploaded(&chunk(foreign_identifier, 0)),
            Err(AttachmentUploadError::IdentifierMismatch)
        );
        assert_eq!(
            journal.mark_uploaded(&second),
            Err(AttachmentUploadError::UnknownChunk)
        );
        assert_eq!(
            journal.mark_uploaded(&chunk(identifier, 0)),
            Err(AttachmentUploadError::ChunkHashMismatch)
        );
        let mut encoded = journal.encode().unwrap();
        encoded.push(0);
        assert_eq!(
            AttachmentUploadJournal::decode(&encoded),
            Err(AttachmentUploadError::TrailingBytes)
        );
    }
}
