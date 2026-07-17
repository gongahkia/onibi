use std::future::Future;

use yeokcham_protocol::{
    AttachmentIdentifier, AttachmentUploadJournal, EncryptedAttachmentChunk,
    EncryptedAttachmentManifest,
};

pub const MAX_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE: usize = 64;

pub trait AttachmentChunkSource: Send {
    type Error: Send;

    fn load_chunk(
        &mut self,
        identifier: AttachmentIdentifier,
        index: u32,
    ) -> Result<EncryptedAttachmentChunk, Self::Error>;
}

pub trait AttachmentDeliveryTransport: Send {
    type Error: Send;

    fn upload_chunk(
        &mut self,
        manifest: &EncryptedAttachmentManifest,
        chunk: &EncryptedAttachmentChunk,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send;
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AttachmentDeliveryOutcome {
    Complete,
    Pending(u32),
    Retrying(u32),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AttachmentDeliveryCycle {
    uploaded: usize,
    outcome: AttachmentDeliveryOutcome,
}

impl AttachmentDeliveryCycle {
    #[must_use]
    pub const fn uploaded(self) -> usize {
        self.uploaded
    }

    #[must_use]
    pub const fn outcome(self) -> AttachmentDeliveryOutcome {
        self.outcome
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AttachmentDeliveryOrchestrator {
    maximum_chunks_per_cycle: usize,
}

impl AttachmentDeliveryOrchestrator {
    pub fn new(maximum_chunks_per_cycle: usize) -> Result<Self, AttachmentDeliveryError> {
        if !(1..=MAX_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE).contains(&maximum_chunks_per_cycle) {
            return Err(AttachmentDeliveryError::InvalidCycleLimit);
        }
        Ok(Self {
            maximum_chunks_per_cycle,
        })
    }

    #[must_use]
    pub const fn maximum_chunks_per_cycle(self) -> usize {
        self.maximum_chunks_per_cycle
    }

    pub async fn run_cycle<S: AttachmentChunkSource, T: AttachmentDeliveryTransport>(
        &self,
        manifest: &EncryptedAttachmentManifest,
        journal: &mut AttachmentUploadJournal,
        source: &mut S,
        transport: &mut T,
    ) -> Result<AttachmentDeliveryCycle, AttachmentDeliveryError> {
        if manifest.identifier() != journal.identifier() {
            return Err(AttachmentDeliveryError::ManifestJournalMismatch);
        }
        let mut uploaded = 0;
        for _ in 0..self.maximum_chunks_per_cycle {
            let Some(index) = journal.next_pending_index() else {
                return Ok(AttachmentDeliveryCycle {
                    uploaded,
                    outcome: AttachmentDeliveryOutcome::Complete,
                });
            };
            let chunk = source
                .load_chunk(journal.identifier(), index)
                .map_err(|_| AttachmentDeliveryError::Source)?;
            validate_chunk(journal, index, &chunk)?;
            if transport.upload_chunk(manifest, &chunk).await.is_err() {
                return Ok(AttachmentDeliveryCycle {
                    uploaded,
                    outcome: AttachmentDeliveryOutcome::Retrying(index),
                });
            }
            if !journal
                .mark_uploaded(&chunk)
                .map_err(|_| AttachmentDeliveryError::Journal)?
            {
                return Err(AttachmentDeliveryError::Journal);
            }
            uploaded += 1;
        }
        Ok(AttachmentDeliveryCycle {
            uploaded,
            outcome: journal.next_pending_index().map_or(
                AttachmentDeliveryOutcome::Complete,
                AttachmentDeliveryOutcome::Pending,
            ),
        })
    }
}

fn validate_chunk(
    journal: &AttachmentUploadJournal,
    expected_index: u32,
    chunk: &EncryptedAttachmentChunk,
) -> Result<(), AttachmentDeliveryError> {
    if chunk.identifier() != journal.identifier() {
        return Err(AttachmentDeliveryError::ChunkIdentifierMismatch);
    }
    if chunk.index() != expected_index {
        return Err(AttachmentDeliveryError::ChunkIndexMismatch);
    }
    let expected_hash = journal
        .expected_hash(expected_index)
        .ok_or(AttachmentDeliveryError::MissingExpectedChunk)?;
    chunk
        .validate_hash(expected_hash)
        .map_err(|_| AttachmentDeliveryError::ChunkHashMismatch)
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum AttachmentDeliveryError {
    #[error("attachment delivery cycle limit is outside the supported bounds")]
    InvalidCycleLimit,
    #[error("attachment manifest and upload journal identifiers differ")]
    ManifestJournalMismatch,
    #[error("attachment source returned a chunk for a different attachment")]
    ChunkIdentifierMismatch,
    #[error("attachment source returned an unexpected chunk index")]
    ChunkIndexMismatch,
    #[error("attachment upload journal has no expected chunk at the requested index")]
    MissingExpectedChunk,
    #[error("attachment source returned a chunk with an unexpected hash")]
    ChunkHashMismatch,
    #[error("attachment source operation failed")]
    Source,
    #[error("attachment upload journal update failed")]
    Journal,
}
