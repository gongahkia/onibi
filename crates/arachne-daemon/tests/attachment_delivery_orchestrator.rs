use std::{
    collections::BTreeMap,
    future::{Future, ready},
};

use arachne_daemon::{
    AttachmentChunkSource, AttachmentDeliveryError, AttachmentDeliveryOrchestrator,
    AttachmentDeliveryOutcome, AttachmentDeliveryTransport,
};
use arachne_protocol::{
    ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, AttachmentManifest,
    AttachmentUploadJournal, EncryptedAttachmentChunk, EncryptedAttachmentManifest,
};

struct MemorySource {
    chunks: BTreeMap<u32, EncryptedAttachmentChunk>,
}

impl AttachmentChunkSource for MemorySource {
    type Error = ();

    fn load_chunk(
        &mut self,
        _identifier: AttachmentIdentifier,
        index: u32,
    ) -> Result<EncryptedAttachmentChunk, Self::Error> {
        self.chunks.get(&index).cloned().ok_or(())
    }
}

struct RecordingTransport {
    fail_at: Option<u32>,
    uploaded: Vec<u32>,
}

impl AttachmentDeliveryTransport for RecordingTransport {
    type Error = ();

    fn upload_chunk(
        &mut self,
        _manifest: &EncryptedAttachmentManifest,
        chunk: &EncryptedAttachmentChunk,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        if self.fail_at == Some(chunk.index()) {
            return ready(Err(()));
        }
        self.uploaded.push(chunk.index());
        ready(Ok(()))
    }
}

fn attachment(
    count: u32,
) -> (
    EncryptedAttachmentManifest,
    AttachmentUploadJournal,
    Vec<EncryptedAttachmentChunk>,
) {
    let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
    let key = AttachmentKey::derive(&[0x11; 32], identifier).unwrap();
    let chunks: Vec<_> = (0..count)
        .map(|index| {
            EncryptedAttachmentChunk::encrypt(
                identifier,
                index,
                &key.derive_chunk_key(index).unwrap(),
                &vec![u8::try_from(index).unwrap(); ATTACHMENT_CHUNK_BYTES],
            )
            .unwrap()
        })
        .collect();
    let manifest = AttachmentManifest::new(
        identifier,
        u64::from(count) * u64::try_from(ATTACHMENT_CHUNK_BYTES).unwrap(),
        chunks.iter().map(|chunk| chunk.hash().unwrap()).collect(),
    )
    .unwrap()
    .encrypt(&key)
    .unwrap();
    let journal = AttachmentUploadJournal::new(&chunks).unwrap();
    (manifest, journal, chunks)
}

fn source(chunks: &[EncryptedAttachmentChunk]) -> MemorySource {
    MemorySource {
        chunks: chunks
            .iter()
            .map(|chunk| (chunk.index(), chunk.clone()))
            .collect(),
    }
}

#[tokio::test]
async fn uploads_a_bounded_cycle_then_completes_the_attachment() {
    let (manifest, mut journal, chunks) = attachment(3);
    let mut source = source(&chunks);
    let mut transport = RecordingTransport {
        fail_at: None,
        uploaded: Vec::new(),
    };
    let orchestrator = AttachmentDeliveryOrchestrator::new(2).unwrap();

    let first = orchestrator
        .run_cycle(&manifest, &mut journal, &mut source, &mut transport)
        .await
        .unwrap();
    assert_eq!(first.uploaded(), 2);
    assert_eq!(first.outcome(), AttachmentDeliveryOutcome::Pending(2));
    assert_eq!(journal.next_pending_index(), Some(2));

    let second = orchestrator
        .run_cycle(&manifest, &mut journal, &mut source, &mut transport)
        .await
        .unwrap();
    assert_eq!(second.uploaded(), 1);
    assert_eq!(second.outcome(), AttachmentDeliveryOutcome::Complete);
    assert!(journal.is_complete());
    assert_eq!(transport.uploaded, vec![0, 1, 2]);
}

#[tokio::test]
async fn retains_the_failed_chunk_for_a_later_delivery_cycle() {
    let (manifest, mut journal, chunks) = attachment(3);
    let mut source = source(&chunks);
    let orchestrator = AttachmentDeliveryOrchestrator::new(3).unwrap();
    let mut failing_transport = RecordingTransport {
        fail_at: Some(1),
        uploaded: Vec::new(),
    };

    let failed = orchestrator
        .run_cycle(&manifest, &mut journal, &mut source, &mut failing_transport)
        .await
        .unwrap();
    assert_eq!(failed.uploaded(), 1);
    assert_eq!(failed.outcome(), AttachmentDeliveryOutcome::Retrying(1));
    assert_eq!(journal.next_pending_index(), Some(1));
    assert_eq!(failing_transport.uploaded, vec![0]);

    let mut transport = RecordingTransport {
        fail_at: None,
        uploaded: Vec::new(),
    };
    let resumed = orchestrator
        .run_cycle(&manifest, &mut journal, &mut source, &mut transport)
        .await
        .unwrap();
    assert_eq!(resumed.uploaded(), 2);
    assert_eq!(resumed.outcome(), AttachmentDeliveryOutcome::Complete);
    assert!(journal.is_complete());
    assert_eq!(transport.uploaded, vec![1, 2]);
}

#[tokio::test]
async fn rejects_a_source_chunk_with_the_wrong_index_before_uploading() {
    let (manifest, mut journal, chunks) = attachment(2);
    let mut source = MemorySource {
        chunks: BTreeMap::from([(0, chunks[1].clone())]),
    };
    let mut transport = RecordingTransport {
        fail_at: None,
        uploaded: Vec::new(),
    };

    assert_eq!(
        AttachmentDeliveryOrchestrator::new(1)
            .unwrap()
            .run_cycle(&manifest, &mut journal, &mut source, &mut transport)
            .await,
        Err(AttachmentDeliveryError::ChunkIndexMismatch)
    );
    assert_eq!(journal.next_pending_index(), Some(0));
    assert!(transport.uploaded.is_empty());
}
