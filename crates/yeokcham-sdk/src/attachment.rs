use std::{collections::BTreeMap, fmt, future::Future};

use yeokcham_daemon::{
    AttachmentChunkSource, AttachmentDeliveryCycle, AttachmentDeliveryError,
    AttachmentDeliveryOrchestrator, AttachmentDeliveryOutcome, AttachmentDeliveryTransport,
    MAX_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE,
};
use yeokcham_protocol::{
    AttachmentIdentifier, AttachmentUploadJournal, EncryptedAttachmentChunk,
    EncryptedAttachmentManifest,
};

pub const MAX_SDK_ATTACHMENT_CHUNKS: usize = 1_600;
pub const MAX_SDK_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE: usize =
    MAX_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE;

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct SdkAttachmentIdentifier(AttachmentIdentifier);

impl SdkAttachmentIdentifier {
    pub fn from_bytes(
        bytes: [u8; yeokcham_protocol::ATTACHMENT_IDENTIFIER_BYTES],
    ) -> Result<Self, SdkAttachmentIdentifierError> {
        AttachmentIdentifier::from_bytes(bytes)
            .map(Self)
            .map_err(|_| SdkAttachmentIdentifierError::Invalid)
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; yeokcham_protocol::ATTACHMENT_IDENTIFIER_BYTES] {
        self.0.as_bytes()
    }
}

impl From<AttachmentIdentifier> for SdkAttachmentIdentifier {
    fn from(identifier: AttachmentIdentifier) -> Self {
        Self(identifier)
    }
}

impl fmt::Debug for SdkAttachmentIdentifier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("SdkAttachmentIdentifier(REDACTED)")
    }
}

#[derive(Clone)]
pub struct SdkAttachmentManifest(EncryptedAttachmentManifest);

impl SdkAttachmentManifest {
    pub fn from_encoded(encoded: &[u8]) -> Result<Self, SdkAttachmentError> {
        EncryptedAttachmentManifest::decode(encoded)
            .map(Self)
            .map_err(|_| SdkAttachmentError::InvalidManifest)
    }

    #[must_use]
    pub fn identifier(&self) -> SdkAttachmentIdentifier {
        self.0.identifier().into()
    }

    pub fn encode(&self) -> Result<Vec<u8>, SdkAttachmentError> {
        self.0.encode().map_err(|_| SdkAttachmentError::State)
    }
}

impl fmt::Debug for SdkAttachmentManifest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SdkAttachmentManifest")
            .field("identifier", &self.identifier())
            .field("encrypted", &"REDACTED")
            .finish()
    }
}

#[derive(Clone)]
pub struct SdkAttachmentChunk(EncryptedAttachmentChunk);

impl SdkAttachmentChunk {
    pub fn from_encoded(encoded: &[u8]) -> Result<Self, SdkAttachmentError> {
        EncryptedAttachmentChunk::decode(encoded)
            .map(Self)
            .map_err(|_| SdkAttachmentError::InvalidChunk)
    }

    #[must_use]
    pub fn identifier(&self) -> SdkAttachmentIdentifier {
        self.0.identifier().into()
    }

    #[must_use]
    pub const fn index(&self) -> u32 {
        self.0.index()
    }

    pub fn encode(&self) -> Result<Vec<u8>, SdkAttachmentError> {
        self.0.encode().map_err(|_| SdkAttachmentError::State)
    }
}

impl fmt::Debug for SdkAttachmentChunk {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SdkAttachmentChunk")
            .field("identifier", &self.identifier())
            .field("index", &self.index())
            .field("encrypted", &"REDACTED")
            .finish()
    }
}

pub trait SdkAttachmentDeliveryTransport: Send {
    type Error: Send;

    fn upload_chunk(
        &mut self,
        manifest: &[u8],
        chunk: &[u8],
    ) -> impl Future<Output = Result<(), Self::Error>> + Send;
}

pub struct SdkAttachmentTransfer {
    delivery: AttachmentDeliveryOrchestrator,
    manifest: SdkAttachmentManifest,
    chunks: BTreeMap<u32, SdkAttachmentChunk>,
    journal: AttachmentUploadJournal,
}

impl SdkAttachmentTransfer {
    pub fn new(
        manifest: SdkAttachmentManifest,
        chunks: Vec<SdkAttachmentChunk>,
        maximum_chunks_per_cycle: usize,
    ) -> Result<Self, SdkAttachmentError> {
        if chunks.len() > MAX_SDK_ATTACHMENT_CHUNKS {
            return Err(SdkAttachmentError::TooManyChunks);
        }
        let delivery = AttachmentDeliveryOrchestrator::new(maximum_chunks_per_cycle)
            .map_err(|_| SdkAttachmentError::InvalidCycleLimit)?;
        let protocol_chunks: Vec<_> = chunks.iter().map(|chunk| chunk.0.clone()).collect();
        let journal = AttachmentUploadJournal::new(&protocol_chunks)
            .map_err(|_| SdkAttachmentError::InvalidTransfer)?;
        if manifest.0.identifier() != journal.identifier() {
            return Err(SdkAttachmentError::InvalidTransfer);
        }
        let chunks = chunks
            .into_iter()
            .map(|chunk| (chunk.index(), chunk))
            .collect();
        Ok(Self {
            delivery,
            manifest,
            chunks,
            journal,
        })
    }

    #[must_use]
    pub fn identifier(&self) -> SdkAttachmentIdentifier {
        self.manifest.identifier()
    }

    #[must_use]
    pub const fn maximum_chunks_per_cycle(&self) -> usize {
        self.delivery.maximum_chunks_per_cycle()
    }

    #[must_use]
    pub fn next_pending_index(&self) -> Option<u32> {
        self.journal.next_pending_index()
    }

    #[must_use]
    pub fn is_complete(&self) -> bool {
        self.journal.is_complete()
    }

    pub async fn run_cycle<T: SdkAttachmentDeliveryTransport>(
        &mut self,
        transport: &mut T,
    ) -> Result<SdkAttachmentDeliveryCycle, SdkAttachmentError> {
        let mut source = SdkAttachmentSource {
            chunks: &self.chunks,
        };
        let mut transport = SdkAttachmentTransportAdapter(transport);
        self.delivery
            .run_cycle(
                &self.manifest.0,
                &mut self.journal,
                &mut source,
                &mut transport,
            )
            .await
            .map(SdkAttachmentDeliveryCycle::from)
            .map_err(|error| map_delivery_error(&error))
    }
}

impl fmt::Debug for SdkAttachmentTransfer {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SdkAttachmentTransfer")
            .field("identifier", &self.identifier())
            .field("chunk_count", &self.chunks.len())
            .field("next_pending_index", &self.next_pending_index())
            .finish_non_exhaustive()
    }
}

struct SdkAttachmentSource<'a> {
    chunks: &'a BTreeMap<u32, SdkAttachmentChunk>,
}

impl AttachmentChunkSource for SdkAttachmentSource<'_> {
    type Error = ();

    fn load_chunk(
        &mut self,
        _identifier: AttachmentIdentifier,
        index: u32,
    ) -> Result<EncryptedAttachmentChunk, Self::Error> {
        self.chunks
            .get(&index)
            .map(|chunk| chunk.0.clone())
            .ok_or(())
    }
}

struct SdkAttachmentTransportAdapter<'a, T>(&'a mut T);

impl<T: SdkAttachmentDeliveryTransport> AttachmentDeliveryTransport
    for SdkAttachmentTransportAdapter<'_, T>
{
    type Error = ();

    fn upload_chunk(
        &mut self,
        manifest: &EncryptedAttachmentManifest,
        chunk: &EncryptedAttachmentChunk,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        let manifest = manifest.encode();
        let chunk = chunk.encode();
        async move {
            let manifest = manifest.map_err(|_| ())?;
            let chunk = chunk.map_err(|_| ())?;
            self.0.upload_chunk(&manifest, &chunk).await.map_err(|_| ())
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkAttachmentDeliveryOutcome {
    Complete,
    Pending(u32),
    Retrying(u32),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkAttachmentDeliveryCycle {
    uploaded: usize,
    outcome: SdkAttachmentDeliveryOutcome,
}

impl SdkAttachmentDeliveryCycle {
    #[must_use]
    pub const fn uploaded(self) -> usize {
        self.uploaded
    }

    #[must_use]
    pub const fn outcome(self) -> SdkAttachmentDeliveryOutcome {
        self.outcome
    }
}

impl From<AttachmentDeliveryCycle> for SdkAttachmentDeliveryCycle {
    fn from(cycle: AttachmentDeliveryCycle) -> Self {
        let outcome = match cycle.outcome() {
            AttachmentDeliveryOutcome::Complete => SdkAttachmentDeliveryOutcome::Complete,
            AttachmentDeliveryOutcome::Pending(index) => {
                SdkAttachmentDeliveryOutcome::Pending(index)
            }
            AttachmentDeliveryOutcome::Retrying(index) => {
                SdkAttachmentDeliveryOutcome::Retrying(index)
            }
        };
        Self {
            uploaded: cycle.uploaded(),
            outcome,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkAttachmentIdentifierError {
    #[error("SDK attachment identifier is invalid")]
    Invalid,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkAttachmentError {
    #[error("SDK attachment manifest is invalid")]
    InvalidManifest,
    #[error("SDK attachment chunk is invalid")]
    InvalidChunk,
    #[error("SDK attachment transfer has too many chunks")]
    TooManyChunks,
    #[error("SDK attachment delivery cycle limit is invalid")]
    InvalidCycleLimit,
    #[error("SDK attachment transfer is invalid")]
    InvalidTransfer,
    #[error("SDK attachment transfer state is unavailable")]
    State,
}

fn map_delivery_error(error: &AttachmentDeliveryError) -> SdkAttachmentError {
    match error {
        AttachmentDeliveryError::InvalidCycleLimit => SdkAttachmentError::InvalidCycleLimit,
        AttachmentDeliveryError::ManifestJournalMismatch
        | AttachmentDeliveryError::ChunkIdentifierMismatch
        | AttachmentDeliveryError::ChunkIndexMismatch
        | AttachmentDeliveryError::MissingExpectedChunk
        | AttachmentDeliveryError::ChunkHashMismatch
        | AttachmentDeliveryError::Source
        | AttachmentDeliveryError::Journal => SdkAttachmentError::State,
    }
}
