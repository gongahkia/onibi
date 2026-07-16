use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    ContactStatus, ContactStore, ContactStoreError, PendingContactImportService,
    QrContactVerificationError, QrContactVerificationService,
};
use yeokcham_protocol::{
    ContactInvitation, QR_VERIFICATION_PAYLOAD_BYTES, QrVerificationError, QrVerificationPayload,
};

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

fn contacts_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "yeokcham-qr-contact-verification-{}-{number}",
            std::process::id()
        ))
        .join("contacts.sqlite")
}

#[test]
fn verifies_a_pending_contact_and_persists_the_qr_verification() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let remote_identity = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote_identity).unwrap();
    let payload = QrVerificationPayload::new(local_public_key, remote_identity.public_key())
        .unwrap()
        .encode()
        .unwrap();
    {
        let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
        PendingContactImportService::new(&local_public_key, &mut contacts)
            .import_encoded(&invitation.encode().unwrap())
            .unwrap();
        let verified = QrContactVerificationService::new(&local_public_key, &mut contacts)
            .verify_encoded(&payload)
            .unwrap();
        assert_eq!(verified.identity(), &remote_identity.public_key());
        assert_eq!(verified.status(), ContactStatus::Verified);
    }
    let contacts = ContactStore::open(&path, &mut keystore).unwrap();
    assert_eq!(contacts.contacts().len(), 1);
    assert_eq!(contacts.contacts()[0].status(), ContactStatus::Verified);
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_oversized_and_unknown_qr_payloads_without_changing_contacts() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let unknown_identity = IdentityKeypair::generate().unwrap();
    let unknown_payload =
        QrVerificationPayload::new(local_public_key, unknown_identity.public_key())
            .unwrap()
            .encode()
            .unwrap();
    let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
    {
        let mut verifier = QrContactVerificationService::new(&local_public_key, &mut contacts);
        assert!(matches!(
            verifier.verify_encoded(&[0; QR_VERIFICATION_PAYLOAD_BYTES + 1]),
            Err(QrContactVerificationError::Payload(
                QrVerificationError::PayloadTooLarge
            ))
        ));
        assert!(matches!(
            verifier.verify_encoded(&unknown_payload),
            Err(QrContactVerificationError::ContactStore(
                ContactStoreError::UnknownContact
            ))
        ));
    }
    assert!(contacts.contacts().is_empty());
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
