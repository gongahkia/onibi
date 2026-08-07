use std::{collections::BTreeMap, path::Path};

use arachne_core::{IdentityPublicKey, OsKeystore};
use arachne_protocol::{
    AttachmentIdentifier, CourierAttachmentReference, CourierAttachmentReferenceError,
    MessageIdentifier, MessageIdentifierError,
};
use minicbor::{Decoder, Encoder};

use crate::{
    EncryptedStateStore, MessageExpiry, MessageExpiryError, StateDocument, StateDocumentError,
    StateStoreError,
};

pub const COURIER_ATTACHMENT_JOB_SCHEMA_VERSION: u8 = 1;
pub const MAX_COURIER_ATTACHMENT_JOBS: usize = 256;
const ROOT_FIELDS: u64 = 2;
const JOB_FIELDS: u64 = 7;
const UPLOADING: u8 = 1;
const AWAITING_ACKNOWLEDGEMENT: u8 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CourierAttachmentJobState {
    Uploading,
    AwaitingAcknowledgement,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CourierAttachmentJob {
    attachment_identifier: AttachmentIdentifier,
    message_identifier: MessageIdentifier,
    recipient: IdentityPublicKey,
    reference: Vec<u8>,
    expiry: MessageExpiry,
    state: CourierAttachmentJobState,
}

impl CourierAttachmentJob {
    #[must_use]
    pub const fn attachment_identifier(&self) -> AttachmentIdentifier {
        self.attachment_identifier
    }

    #[must_use]
    pub const fn message_identifier(&self) -> MessageIdentifier {
        self.message_identifier
    }

    #[must_use]
    pub const fn recipient(&self) -> &IdentityPublicKey {
        &self.recipient
    }

    pub fn reference(&self) -> Result<CourierAttachmentReference, CourierAttachmentJobError> {
        CourierAttachmentReference::decode(&self.reference)
            .map_err(CourierAttachmentJobError::Reference)
    }

    #[must_use]
    pub const fn expiry(&self) -> MessageExpiry {
        self.expiry
    }

    #[must_use]
    pub const fn state(&self) -> CourierAttachmentJobState {
        self.state
    }
}

pub struct CourierAttachmentJobStore {
    state: EncryptedStateStore,
    jobs: BTreeMap<[u8; 16], CourierAttachmentJob>,
}

impl CourierAttachmentJobStore {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, CourierAttachmentJobError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let jobs = match state.load()? {
            Some(document) => decode_jobs(document.as_bytes())?,
            None => BTreeMap::new(),
        };
        Ok(Self { state, jobs })
    }

    #[must_use]
    pub fn jobs(&self) -> impl Iterator<Item = &CourierAttachmentJob> {
        self.jobs.values()
    }

    pub fn enqueue(
        &mut self,
        recipient: IdentityPublicKey,
        message_identifier: MessageIdentifier,
        reference: CourierAttachmentReference,
        expiry: MessageExpiry,
    ) -> Result<(), CourierAttachmentJobError> {
        if self.jobs.len() >= MAX_COURIER_ATTACHMENT_JOBS {
            return Err(CourierAttachmentJobError::CapacityExceeded);
        }
        let reference = reference
            .encode()
            .map_err(CourierAttachmentJobError::Reference)?;
        let decoded = CourierAttachmentReference::decode(&reference)
            .map_err(CourierAttachmentJobError::Reference)?;
        let key = *decoded.identifier().as_bytes();
        if self.jobs.contains_key(&key)
            || self
                .jobs
                .values()
                .any(|job| job.message_identifier == message_identifier)
        {
            return Err(CourierAttachmentJobError::DuplicateIdentifier);
        }
        self.jobs.insert(
            key,
            CourierAttachmentJob {
                attachment_identifier: decoded.identifier(),
                message_identifier,
                recipient,
                reference,
                expiry,
                state: CourierAttachmentJobState::Uploading,
            },
        );
        if let Err(error) = self.persist() {
            self.jobs.remove(&key);
            return Err(error);
        }
        Ok(())
    }

    pub fn mark_reference_queued(
        &mut self,
        attachment_identifier: AttachmentIdentifier,
    ) -> Result<(), CourierAttachmentJobError> {
        let job = self
            .jobs
            .get_mut(attachment_identifier.as_bytes())
            .ok_or(CourierAttachmentJobError::UnknownAttachment)?;
        if job.state == CourierAttachmentJobState::AwaitingAcknowledgement {
            return Ok(());
        }
        job.state = CourierAttachmentJobState::AwaitingAcknowledgement;
        if let Err(error) = self.persist() {
            self.jobs
                .get_mut(attachment_identifier.as_bytes())
                .expect("existing job")
                .state = CourierAttachmentJobState::Uploading;
            return Err(error);
        }
        Ok(())
    }

    pub fn remove_for_message(
        &mut self,
        message_identifier: MessageIdentifier,
    ) -> Result<Option<CourierAttachmentJob>, CourierAttachmentJobError> {
        let key = self
            .jobs
            .iter()
            .find(|(_, job)| job.message_identifier == message_identifier)
            .map(|(key, _)| *key);
        let Some(key) = key else {
            return Ok(None);
        };
        let job = self.jobs.remove(&key).expect("job was selected from map");
        if let Err(error) = self.persist() {
            self.jobs.insert(key, job);
            return Err(error);
        }
        Ok(Some(job))
    }

    pub fn expire_due(
        &mut self,
        now: u64,
    ) -> Result<Vec<CourierAttachmentJob>, CourierAttachmentJobError> {
        let prior = self.jobs.clone();
        let expired = self
            .jobs
            .values()
            .filter(|job| job.expiry.is_expired(now))
            .map(|job| job.attachment_identifier)
            .collect::<Vec<_>>();
        let jobs = expired
            .iter()
            .filter_map(|identifier| self.jobs.remove(identifier.as_bytes()))
            .collect::<Vec<_>>();
        if jobs.is_empty() {
            return Ok(jobs);
        }
        if let Err(error) = self.persist() {
            self.jobs = prior;
            return Err(error);
        }
        Ok(jobs)
    }

    fn persist(&mut self) -> Result<(), CourierAttachmentJobError> {
        let document = StateDocument::new(encode_jobs(&self.jobs)?)
            .map_err(CourierAttachmentJobError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierAttachmentJobError {
    #[error("courier attachment-job encrypted state operation failed")]
    State(#[from] StateStoreError),
    #[error("courier attachment-job state document is invalid")]
    Document(#[source] StateDocumentError),
    #[error("courier attachment-job reference is invalid")]
    Reference(#[source] CourierAttachmentReferenceError),
    #[error("courier attachment-job message identifier is invalid")]
    MessageIdentifier(#[source] MessageIdentifierError),
    #[error("courier attachment-job expiry is invalid")]
    Expiry(#[source] MessageExpiryError),
    #[error("courier attachment-job encoding failed")]
    Encode,
    #[error("courier attachment-job decoding failed")]
    Decode,
    #[error("courier attachment-job state has an invalid shape")]
    InvalidShape,
    #[error("courier attachment-job state schema is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier attachment-job state has invalid data")]
    InvalidData,
    #[error("courier attachment-job state has duplicate identifiers")]
    DuplicateIdentifier,
    #[error("courier attachment-job state is not canonical")]
    NonCanonicalEncoding,
    #[error("courier attachment-job capacity is exhausted")]
    CapacityExceeded,
    #[error("courier attachment job is unknown")]
    UnknownAttachment,
}

fn encode_jobs(
    jobs: &BTreeMap<[u8; 16], CourierAttachmentJob>,
) -> Result<Vec<u8>, CourierAttachmentJobError> {
    if jobs.len() > MAX_COURIER_ATTACHMENT_JOBS {
        return Err(CourierAttachmentJobError::CapacityExceeded);
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(ROOT_FIELDS)
        .map_err(|_| CourierAttachmentJobError::Encode)?
        .u8(COURIER_ATTACHMENT_JOB_SCHEMA_VERSION)
        .map_err(|_| CourierAttachmentJobError::Encode)?
        .array(u64::try_from(jobs.len()).map_err(|_| CourierAttachmentJobError::Encode)?)
        .map_err(|_| CourierAttachmentJobError::Encode)?;
    for job in jobs.values() {
        encoder
            .array(JOB_FIELDS)
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .bytes(job.attachment_identifier.as_bytes())
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .bytes(job.message_identifier.as_bytes())
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .bytes(job.recipient.as_bytes())
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .bytes(&job.reference)
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .u64(job.expiry.created_at())
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .u32(job.expiry.ttl_seconds())
            .map_err(|_| CourierAttachmentJobError::Encode)?
            .u8(match job.state {
                CourierAttachmentJobState::Uploading => UPLOADING,
                CourierAttachmentJobState::AwaitingAcknowledgement => AWAITING_ACKNOWLEDGEMENT,
            })
            .map_err(|_| CourierAttachmentJobError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_jobs(
    encoded: &[u8],
) -> Result<BTreeMap<[u8; 16], CourierAttachmentJob>, CourierAttachmentJobError> {
    let mut decoder = Decoder::new(encoded);
    if decoder
        .array()
        .map_err(|_| CourierAttachmentJobError::Decode)?
        != Some(ROOT_FIELDS)
    {
        return Err(CourierAttachmentJobError::InvalidShape);
    }
    if decoder
        .u8()
        .map_err(|_| CourierAttachmentJobError::Decode)?
        != COURIER_ATTACHMENT_JOB_SCHEMA_VERSION
    {
        return Err(CourierAttachmentJobError::UnsupportedSchemaVersion);
    }
    let count = decoder
        .array()
        .map_err(|_| CourierAttachmentJobError::Decode)?
        .ok_or(CourierAttachmentJobError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| CourierAttachmentJobError::CapacityExceeded)?;
    if count > MAX_COURIER_ATTACHMENT_JOBS {
        return Err(CourierAttachmentJobError::CapacityExceeded);
    }
    let mut jobs = BTreeMap::new();
    for _ in 0..count {
        if decoder
            .array()
            .map_err(|_| CourierAttachmentJobError::Decode)?
            != Some(JOB_FIELDS)
        {
            return Err(CourierAttachmentJobError::InvalidShape);
        }
        let attachment_identifier = AttachmentIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierAttachmentJobError::Decode)?
                .try_into()
                .map_err(|_| CourierAttachmentJobError::InvalidData)?,
        )
        .map_err(|_| CourierAttachmentJobError::InvalidData)?;
        let message_identifier = MessageIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierAttachmentJobError::Decode)?
                .try_into()
                .map_err(|_| CourierAttachmentJobError::InvalidData)?,
        )
        .map_err(CourierAttachmentJobError::MessageIdentifier)?;
        let recipient = IdentityPublicKey::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierAttachmentJobError::Decode)?
                .try_into()
                .map_err(|_| CourierAttachmentJobError::InvalidData)?,
        )
        .map_err(|_| CourierAttachmentJobError::InvalidData)?;
        let reference = decoder
            .bytes()
            .map_err(|_| CourierAttachmentJobError::Decode)?
            .to_vec();
        let decoded = CourierAttachmentReference::decode(&reference)
            .map_err(CourierAttachmentJobError::Reference)?;
        if decoded.identifier() != attachment_identifier {
            return Err(CourierAttachmentJobError::InvalidData);
        }
        let expiry = MessageExpiry::new(
            decoder
                .u64()
                .map_err(|_| CourierAttachmentJobError::Decode)?,
            decoder
                .u32()
                .map_err(|_| CourierAttachmentJobError::Decode)?,
        )
        .map_err(CourierAttachmentJobError::Expiry)?;
        let state = match decoder
            .u8()
            .map_err(|_| CourierAttachmentJobError::Decode)?
        {
            UPLOADING => CourierAttachmentJobState::Uploading,
            AWAITING_ACKNOWLEDGEMENT => CourierAttachmentJobState::AwaitingAcknowledgement,
            _ => return Err(CourierAttachmentJobError::InvalidData),
        };
        if jobs
            .insert(
                *attachment_identifier.as_bytes(),
                CourierAttachmentJob {
                    attachment_identifier,
                    message_identifier,
                    recipient,
                    reference,
                    expiry,
                    state,
                },
            )
            .is_some()
        {
            return Err(CourierAttachmentJobError::DuplicateIdentifier);
        }
    }
    if decoder.position() != encoded.len() || encode_jobs(&jobs)? != encoded {
        return Err(CourierAttachmentJobError::NonCanonicalEncoding);
    }
    Ok(jobs)
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
    use arachne_protocol::{
        AttachmentChunkHash, AttachmentIdentifier, AttachmentKey, AttachmentManifest,
        CourierAttachmentReference, MessageIdentifier,
    };

    use super::{CourierAttachmentJobState, CourierAttachmentJobStore};
    use crate::MessageExpiry;

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

    fn path() -> PathBuf {
        std::env::temp_dir().join(format!(
            "arachne-courier-attachment-jobs-{}.sqlite",
            std::process::id()
        ))
    }

    fn reference() -> CourierAttachmentReference {
        let identifier = AttachmentIdentifier::from_bytes([0x11; 16]).unwrap();
        let message_key = [0x22; 32];
        let key = AttachmentKey::derive(&message_key, identifier).unwrap();
        let manifest = AttachmentManifest::new(
            identifier,
            1,
            vec![AttachmentChunkHash::from_bytes([0x33; 32])],
        )
        .unwrap()
        .encrypt(&key)
        .unwrap();
        CourierAttachmentReference::new(identifier, message_key, manifest).unwrap()
    }

    #[test]
    fn persists_attachment_job_until_delivery_acknowledgement() {
        let path = path();
        let _ = fs::remove_file(&path);
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let identifier = MessageIdentifier::from_bytes([0x44; 16]).unwrap();
        let attachment = reference();
        let attachment_identifier = attachment.identifier();
        let mut jobs = CourierAttachmentJobStore::open(&path, &mut keystore).unwrap();
        jobs.enqueue(
            recipient,
            identifier,
            attachment,
            MessageExpiry::new(100, 60).unwrap(),
        )
        .unwrap();
        drop(jobs);

        let mut jobs = CourierAttachmentJobStore::open(&path, &mut keystore).unwrap();
        assert_eq!(jobs.jobs().count(), 1);
        assert_eq!(
            jobs.jobs().next().unwrap().state(),
            CourierAttachmentJobState::Uploading
        );
        jobs.mark_reference_queued(attachment_identifier).unwrap();
        drop(jobs);

        let mut jobs = CourierAttachmentJobStore::open(&path, &mut keystore).unwrap();
        assert_eq!(
            jobs.jobs().next().unwrap().state(),
            CourierAttachmentJobState::AwaitingAcknowledgement
        );
        assert!(jobs.remove_for_message(identifier).unwrap().is_some());
        drop(jobs);
        assert_eq!(
            CourierAttachmentJobStore::open(&path, &mut keystore)
                .unwrap()
                .jobs()
                .count(),
            0
        );
        fs::remove_file(path).unwrap();
    }
}
