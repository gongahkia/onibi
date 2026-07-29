use std::path::Path;

use arachne_core::{IdentityPublicKey, OsKeystore};
use arachne_protocol::{
    ContactInvitation, IdentityRotation, QrVerificationPayload, SAFETY_NUMBER_FINGERPRINT_BYTES,
    SafetyNumberFingerprint,
};
use minicbor::{Decoder, Encoder};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub const CONTACT_STATE_SCHEMA_VERSION: u8 = 3;
const CONTACT_STATE_SCHEMA_VERSION_V1: u8 = 1;
const CONTACT_STATE_SCHEMA_VERSION_V2: u8 = 2;
const CONTACT_STATE_FIELDS: u64 = 2;
const CONTACT_FIELDS: u64 = 3;
const IDENTITY_PUBLIC_KEY_BYTES: usize = 32;
const PENDING_STATUS: u8 = 1;
const VERIFIED_STATUS: u8 = 2;
const REVOKED_STATUS: u8 = 3;
const NO_VERIFICATION: u8 = 0;
const QR_VERIFICATION: u8 = 1;
const SAFETY_NUMBER_VERIFICATION: u8 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ContactStatus {
    Pending,
    Verified,
    Revoked,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ContactVerificationMethod {
    Qr,
    SafetyNumber,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Contact {
    identity: IdentityPublicKey,
    status: ContactStatus,
    verification: Option<ContactVerificationMethod>,
}

impl Contact {
    #[must_use]
    pub const fn identity(&self) -> &IdentityPublicKey {
        &self.identity
    }

    #[must_use]
    pub const fn status(self) -> ContactStatus {
        self.status
    }

    #[must_use]
    pub const fn verification_method(self) -> Option<ContactVerificationMethod> {
        self.verification
    }
}

pub struct ContactStore {
    state: EncryptedStateStore,
    contacts: Vec<Contact>,
}

impl ContactStore {
    pub fn open<K: OsKeystore>(path: &Path, keystore: &mut K) -> Result<Self, ContactStoreError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let contacts = match state.load()? {
            Some(document) => decode_contacts(document.as_bytes())?,
            None => Vec::new(),
        };
        Ok(Self { state, contacts })
    }

    #[must_use]
    pub fn contact(&self, identity: &IdentityPublicKey) -> Option<Contact> {
        self.contacts
            .iter()
            .copied()
            .find(|contact| contact.identity == *identity)
    }

    #[must_use]
    pub fn contacts(&self) -> &[Contact] {
        &self.contacts
    }

    pub fn import_invitation(
        &mut self,
        local_identity: &IdentityPublicKey,
        invitation: &ContactInvitation,
    ) -> Result<Contact, ContactStoreError> {
        let identity = *invitation.inviter();
        if identity == *local_identity {
            return Err(ContactStoreError::SelfContact);
        }
        if let Some(contact) = self.contact(&identity) {
            return Ok(contact);
        }
        let contact = Contact {
            identity,
            status: ContactStatus::Pending,
            verification: None,
        };
        self.contacts.push(contact);
        if let Err(error) = self.persist() {
            self.contacts.retain(|stored| stored.identity != identity);
            return Err(error);
        }
        Ok(contact)
    }

    pub fn verify_qr(
        &mut self,
        local_identity: &IdentityPublicKey,
        payload: &QrVerificationPayload,
    ) -> Result<Contact, ContactStoreError> {
        let (first, second) = payload.identities();
        let remote_identity = if first == local_identity {
            second
        } else if second == local_identity {
            first
        } else {
            return Err(ContactStoreError::QrDoesNotContainLocalIdentity);
        };
        self.verify_pending_contact(*remote_identity, ContactVerificationMethod::Qr)
    }

    pub fn verify_safety_number(
        &mut self,
        local_identity: &IdentityPublicKey,
        remote_identity: &IdentityPublicKey,
        supplied: &[u8; SAFETY_NUMBER_FINGERPRINT_BYTES],
    ) -> Result<Contact, ContactStoreError> {
        if remote_identity == local_identity {
            return Err(ContactStoreError::SelfContact);
        }
        let expected = SafetyNumberFingerprint::derive(local_identity, remote_identity)
            .map_err(|_| ContactStoreError::SelfContact)?;
        if expected.as_bytes() != supplied {
            return Err(ContactStoreError::SafetyNumberMismatch);
        }
        self.verify_pending_contact(*remote_identity, ContactVerificationMethod::SafetyNumber)
    }

    fn verify_pending_contact(
        &mut self,
        remote_identity: IdentityPublicKey,
        verification: ContactVerificationMethod,
    ) -> Result<Contact, ContactStoreError> {
        let contact_index = self
            .contacts
            .iter()
            .position(|contact| contact.identity == remote_identity)
            .ok_or(ContactStoreError::UnknownContact)?;
        if self.contacts[contact_index].status != ContactStatus::Pending {
            return Err(ContactStoreError::NotPending);
        }
        let pending = self.contacts[contact_index];
        self.contacts[contact_index].status = ContactStatus::Verified;
        self.contacts[contact_index].verification = Some(verification);
        let verified = self.contacts[contact_index];
        if let Err(error) = self.persist() {
            let stored = self
                .contacts
                .iter_mut()
                .find(|contact| contact.identity == pending.identity)
                .expect("verified contact must remain in memory after a failed write");
            *stored = pending;
            return Err(error);
        }
        Ok(verified)
    }

    pub fn apply_identity_rotation(
        &mut self,
        local_identity: &IdentityPublicKey,
        rotation: &IdentityRotation,
    ) -> Result<Contact, ContactStoreError> {
        let previous = *rotation.previous();
        let replacement = *rotation.replacement();
        if previous == *local_identity || replacement == *local_identity {
            return Err(ContactStoreError::SelfContact);
        }
        let previous_index = self
            .contacts
            .iter()
            .position(|contact| contact.identity == previous)
            .ok_or(ContactStoreError::UnknownContact)?;
        if self.contacts[previous_index].status != ContactStatus::Verified {
            return Err(ContactStoreError::NotVerified);
        }
        if self.contact(&replacement).is_some() {
            return Err(ContactStoreError::ReplacementAlreadyKnown);
        }
        let prior_contacts = self.contacts.clone();
        self.contacts[previous_index].status = ContactStatus::Revoked;
        self.contacts[previous_index].verification = None;
        let replacement_contact = Contact {
            identity: replacement,
            status: ContactStatus::Pending,
            verification: None,
        };
        self.contacts.push(replacement_contact);
        if let Err(error) = self.persist() {
            self.contacts = prior_contacts;
            return Err(error);
        }
        Ok(replacement_contact)
    }

    pub fn revoke(&mut self, identity: &IdentityPublicKey) -> Result<Contact, ContactStoreError> {
        let index = self
            .contacts
            .iter()
            .position(|contact| contact.identity == *identity)
            .ok_or(ContactStoreError::UnknownContact)?;
        if self.contacts[index].status == ContactStatus::Revoked {
            return Err(ContactStoreError::AlreadyRevoked);
        }
        let previous = self.contacts[index];
        self.contacts[index].status = ContactStatus::Revoked;
        self.contacts[index].verification = None;
        let revoked = self.contacts[index];
        if let Err(error) = self.persist() {
            self.contacts[index] = previous;
            return Err(error);
        }
        Ok(revoked)
    }

    fn persist(&mut self) -> Result<(), ContactStoreError> {
        self.contacts
            .sort_unstable_by_key(|contact| *contact.identity.as_bytes());
        let document = StateDocument::new(encode_contacts(&self.contacts)?)
            .map_err(ContactStoreError::InvalidDocument)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ContactStoreError {
    #[error("encrypted state-store operation failed: {0}")]
    StateStore(#[from] StateStoreError),
    #[error("local identity cannot be imported as a contact")]
    SelfContact,
    #[error("QR verification payload does not contain the local identity")]
    QrDoesNotContainLocalIdentity,
    #[error("contact is not known")]
    UnknownContact,
    #[error("contact is not pending verification")]
    NotPending,
    #[error("contact is not verified")]
    NotVerified,
    #[error("replacement identity is already known")]
    ReplacementAlreadyKnown,
    #[error("contact is already revoked")]
    AlreadyRevoked,
    #[error("safety number does not match this contact")]
    SafetyNumberMismatch,
    #[error("contact-state document is invalid")]
    InvalidState(#[source] minicbor::decode::Error),
    #[error("contact-state document violates storage bounds: {0}")]
    InvalidDocument(#[source] StateDocumentError),
    #[error("contact-state encoding failed")]
    Encode,
    #[error("contact-state schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("contact-state document has an invalid shape")]
    InvalidShape,
    #[error("contact-state document contains an invalid identity")]
    InvalidIdentity,
    #[error("contact-state document contains an invalid status")]
    InvalidStatus,
    #[error("contact-state document contains a duplicate identity")]
    DuplicateIdentity,
    #[error("contact-state document has trailing bytes")]
    TrailingBytes,
    #[error("contact-state document is not canonical")]
    NonCanonicalEncoding,
}

fn encode_contacts(contacts: &[Contact]) -> Result<Vec<u8>, ContactStoreError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(CONTACT_STATE_FIELDS)
        .map_err(|_| ContactStoreError::Encode)?
        .u8(CONTACT_STATE_SCHEMA_VERSION)
        .map_err(|_| ContactStoreError::Encode)?
        .array(u64::try_from(contacts.len()).map_err(|_| ContactStoreError::Encode)?)
        .map_err(|_| ContactStoreError::Encode)?;
    for contact in contacts {
        encoder
            .array(CONTACT_FIELDS)
            .map_err(|_| ContactStoreError::Encode)?
            .bytes(contact.identity.as_bytes())
            .map_err(|_| ContactStoreError::Encode)?
            .u8(status_value(contact.status))
            .map_err(|_| ContactStoreError::Encode)?
            .u8(verification_value(contact.verification))
            .map_err(|_| ContactStoreError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_contacts(encoded: &[u8]) -> Result<Vec<Contact>, ContactStoreError> {
    let mut decoder = Decoder::new(encoded);
    if decoder.array().map_err(ContactStoreError::InvalidState)? != Some(CONTACT_STATE_FIELDS) {
        return Err(ContactStoreError::InvalidShape);
    }
    let version = decoder.u8().map_err(ContactStoreError::InvalidState)?;
    if version != CONTACT_STATE_SCHEMA_VERSION
        && version != CONTACT_STATE_SCHEMA_VERSION_V2
        && version != CONTACT_STATE_SCHEMA_VERSION_V1
    {
        return Err(ContactStoreError::UnsupportedSchemaVersion(version));
    }
    let count = decoder
        .array()
        .map_err(ContactStoreError::InvalidState)?
        .ok_or(ContactStoreError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| ContactStoreError::InvalidShape)?;
    let mut contacts = Vec::with_capacity(count);
    for _ in 0..count {
        if decoder.array().map_err(ContactStoreError::InvalidState)? != Some(CONTACT_FIELDS) {
            return Err(ContactStoreError::InvalidShape);
        }
        let identity = decode_identity(decoder.bytes().map_err(ContactStoreError::InvalidState)?)?;
        let status = decode_status(decoder.u8().map_err(ContactStoreError::InvalidState)?)?;
        if version == CONTACT_STATE_SCHEMA_VERSION_V1 && status == ContactStatus::Revoked {
            return Err(ContactStoreError::InvalidStatus);
        }
        let verification = decode_verification(
            version,
            decoder.u8().map_err(ContactStoreError::InvalidState)?,
        )?;
        if matches!(status, ContactStatus::Verified) != verification.is_some() {
            return Err(ContactStoreError::InvalidStatus);
        }
        contacts.push(Contact {
            identity,
            status,
            verification,
        });
    }
    if decoder.position() != encoded.len() {
        return Err(ContactStoreError::TrailingBytes);
    }
    let mut canonical = contacts.clone();
    canonical.sort_unstable_by_key(|contact| *contact.identity.as_bytes());
    if canonical
        .windows(2)
        .any(|pair| pair[0].identity == pair[1].identity)
    {
        return Err(ContactStoreError::DuplicateIdentity);
    }
    if canonical != contacts {
        return Err(ContactStoreError::NonCanonicalEncoding);
    }
    if version == CONTACT_STATE_SCHEMA_VERSION && encode_contacts(&contacts)? != encoded {
        return Err(ContactStoreError::NonCanonicalEncoding);
    }
    Ok(contacts)
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, ContactStoreError> {
    let bytes: [u8; IDENTITY_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| ContactStoreError::InvalidIdentity)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| ContactStoreError::InvalidIdentity)
}

const fn status_value(status: ContactStatus) -> u8 {
    match status {
        ContactStatus::Pending => PENDING_STATUS,
        ContactStatus::Verified => VERIFIED_STATUS,
        ContactStatus::Revoked => REVOKED_STATUS,
    }
}

const fn verification_value(verification: Option<ContactVerificationMethod>) -> u8 {
    match verification {
        None => NO_VERIFICATION,
        Some(ContactVerificationMethod::Qr) => QR_VERIFICATION,
        Some(ContactVerificationMethod::SafetyNumber) => SAFETY_NUMBER_VERIFICATION,
    }
}

fn decode_status(value: u8) -> Result<ContactStatus, ContactStoreError> {
    match value {
        PENDING_STATUS => Ok(ContactStatus::Pending),
        VERIFIED_STATUS => Ok(ContactStatus::Verified),
        REVOKED_STATUS => Ok(ContactStatus::Revoked),
        _ => Err(ContactStoreError::InvalidStatus),
    }
}

fn decode_verification(
    schema_version: u8,
    value: u8,
) -> Result<Option<ContactVerificationMethod>, ContactStoreError> {
    match value {
        NO_VERIFICATION => Ok(None),
        QR_VERIFICATION => Ok(Some(ContactVerificationMethod::Qr)),
        SAFETY_NUMBER_VERIFICATION if schema_version == CONTACT_STATE_SCHEMA_VERSION => {
            Ok(Some(ContactVerificationMethod::SafetyNumber))
        }
        _ => Err(ContactStoreError::InvalidStatus),
    }
}

#[cfg(test)]
mod tests {
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
    use arachne_protocol::{ContactInvitation, IdentityRotation, QrVerificationPayload};

    use super::{
        ContactStatus, ContactStore, ContactStoreError, ContactVerificationMethod, decode_contacts,
    };
    use crate::{EncryptedStateStore, StateDocument};

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
        std::env::temp_dir().join(format!(
            "arachne-contact-store-{}-{number}.sqlite",
            std::process::id()
        ))
    }

    #[test]
    fn imports_pending_contacts_and_persists_qr_verification() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let local = IdentityKeypair::generate().unwrap();
        let remote = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote).unwrap();
        let payload = QrVerificationPayload::new(local.public_key(), remote.public_key()).unwrap();
        {
            let mut store = ContactStore::open(&path, &mut keystore).unwrap();
            let contact = store
                .import_invitation(&local.public_key(), &invitation)
                .unwrap();
            assert_eq!(contact.status(), ContactStatus::Pending);
            assert_eq!(
                store
                    .import_invitation(&local.public_key(), &invitation)
                    .unwrap(),
                contact
            );
            assert_eq!(
                store
                    .verify_qr(&local.public_key(), &payload)
                    .unwrap()
                    .status(),
                ContactStatus::Verified
            );
            assert_eq!(
                store
                    .contact(&remote.public_key())
                    .unwrap()
                    .verification_method(),
                Some(ContactVerificationMethod::Qr)
            );
        }
        let store = ContactStore::open(&path, &mut keystore).unwrap();
        assert_eq!(store.contacts().len(), 1);
        assert_eq!(
            store.contact(&remote.public_key()).unwrap().status(),
            ContactStatus::Verified
        );
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_invalid_or_unconfirmed_contact_transitions() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let local = IdentityKeypair::generate().unwrap();
        let remote = IdentityKeypair::generate().unwrap();
        let other = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote).unwrap();
        let mut store = ContactStore::open(&path, &mut keystore).unwrap();
        assert!(matches!(
            store.import_invitation(
                &local.public_key(),
                &ContactInvitation::create(&local).unwrap()
            ),
            Err(ContactStoreError::SelfContact)
        ));
        assert!(matches!(
            store.verify_qr(
                &local.public_key(),
                &QrVerificationPayload::new(local.public_key(), remote.public_key()).unwrap()
            ),
            Err(ContactStoreError::UnknownContact)
        ));
        store
            .import_invitation(&local.public_key(), &invitation)
            .unwrap();
        assert!(matches!(
            store.verify_qr(
                &other.public_key(),
                &QrVerificationPayload::new(local.public_key(), remote.public_key()).unwrap()
            ),
            Err(ContactStoreError::QrDoesNotContainLocalIdentity)
        ));
        let payload = QrVerificationPayload::new(local.public_key(), remote.public_key()).unwrap();
        store.verify_qr(&local.public_key(), &payload).unwrap();
        assert!(matches!(
            store.verify_qr(&local.public_key(), &payload),
            Err(ContactStoreError::NotPending)
        ));
        drop(store);
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rotates_verified_contacts_to_a_revoked_key_and_pending_replacement() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let local = IdentityKeypair::generate().unwrap();
        let remote = IdentityKeypair::generate().unwrap();
        let replacement = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote).unwrap();
        let payload = QrVerificationPayload::new(local.public_key(), remote.public_key()).unwrap();
        let rotation = IdentityRotation::create(&remote, replacement.public_key()).unwrap();
        {
            let mut store = ContactStore::open(&path, &mut keystore).unwrap();
            store
                .import_invitation(&local.public_key(), &invitation)
                .unwrap();
            store.verify_qr(&local.public_key(), &payload).unwrap();

            let pending = store
                .apply_identity_rotation(&local.public_key(), &rotation)
                .unwrap();
            assert_eq!(pending.status(), ContactStatus::Pending);
            assert_eq!(
                store.contact(&remote.public_key()).unwrap().status(),
                ContactStatus::Revoked
            );
            assert_eq!(
                store
                    .contact(&replacement.public_key())
                    .unwrap()
                    .verification_method(),
                None
            );
            assert!(matches!(
                store.apply_identity_rotation(&local.public_key(), &rotation),
                Err(ContactStoreError::NotVerified)
            ));
        }
        let store = ContactStore::open(&path, &mut keystore).unwrap();
        assert_eq!(
            store.contact(&remote.public_key()).unwrap().status(),
            ContactStatus::Revoked
        );
        assert_eq!(
            store.contact(&replacement.public_key()).unwrap().status(),
            ContactStatus::Pending
        );
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn revocation_rejects_unknown_or_already_revoked_contacts() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let local = IdentityKeypair::generate().unwrap();
        let remote = IdentityKeypair::generate().unwrap();
        let unknown = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote).unwrap();
        let mut store = ContactStore::open(&path, &mut keystore).unwrap();
        store
            .import_invitation(&local.public_key(), &invitation)
            .unwrap();

        assert_eq!(
            store.revoke(&remote.public_key()).unwrap().status(),
            ContactStatus::Revoked
        );
        assert!(matches!(
            store.revoke(&remote.public_key()),
            Err(ContactStoreError::AlreadyRevoked)
        ));
        assert!(matches!(
            store.revoke(&unknown.public_key()),
            Err(ContactStoreError::UnknownContact)
        ));
        drop(store);
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_malformed_or_noncanonical_sealed_contact_documents() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let mut state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        state
            .replace(&StateDocument::new(vec![0x82, 0x01, 0x80, 0x00]).unwrap())
            .unwrap();
        drop(state);
        assert!(matches!(
            ContactStore::open(&path, &mut keystore),
            Err(ContactStoreError::TrailingBytes)
        ));
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn loads_empty_v1_contact_state_for_migration() {
        assert!(decode_contacts(&[0x82, 0x01, 0x80]).unwrap().is_empty());
    }

    #[test]
    fn loads_legacy_qr_state_and_rejects_safety_number_state_with_legacy_schema() {
        let identity = IdentityKeypair::generate().unwrap().public_key();
        let qr_contact = super::Contact {
            identity,
            status: ContactStatus::Verified,
            verification: Some(ContactVerificationMethod::Qr),
        };
        let mut v2_qr = super::encode_contacts(&[qr_contact]).unwrap();
        assert_eq!(v2_qr[1], super::CONTACT_STATE_SCHEMA_VERSION);
        v2_qr[1] = super::CONTACT_STATE_SCHEMA_VERSION_V2;
        assert_eq!(decode_contacts(&v2_qr).unwrap(), vec![qr_contact]);

        let safety_number_contact = super::Contact {
            identity,
            status: ContactStatus::Verified,
            verification: Some(ContactVerificationMethod::SafetyNumber),
        };
        let mut v2_safety_number = super::encode_contacts(&[safety_number_contact]).unwrap();
        v2_safety_number[1] = super::CONTACT_STATE_SCHEMA_VERSION_V2;
        assert!(matches!(
            decode_contacts(&v2_safety_number),
            Err(ContactStoreError::InvalidStatus)
        ));
    }
}
