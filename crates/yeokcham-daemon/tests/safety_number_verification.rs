use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    ContactStatus, ContactStore, ContactStoreError, ContactVerificationMethod,
    PendingContactImportService, SafetyNumberVerificationError, SafetyNumberVerificationService,
};
use yeokcham_protocol::{
    ContactInvitation, SAFETY_NUMBER_FINGERPRINT_BYTES, SafetyNumberFingerprint,
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
            "yeokcham-safety-number-verification-{}-{number}",
            std::process::id()
        ))
        .join("contacts.sqlite")
}

#[test]
fn verifies_a_pending_contact_with_the_exact_safety_number() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let remote_identity = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote_identity).unwrap();
    let safety_number =
        SafetyNumberFingerprint::derive(&local_public_key, &remote_identity.public_key()).unwrap();
    {
        let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
        PendingContactImportService::new(&local_public_key, &mut contacts)
            .import_encoded(&invitation.encode().unwrap())
            .unwrap();
        let verified = SafetyNumberVerificationService::new(&local_public_key, &mut contacts)
            .verify(&remote_identity.public_key(), safety_number.as_bytes())
            .unwrap();
        assert_eq!(verified.status(), ContactStatus::Verified);
        assert_eq!(
            verified.verification_method(),
            Some(ContactVerificationMethod::SafetyNumber)
        );
    }
    let contacts = ContactStore::open(&path, &mut keystore).unwrap();
    assert_eq!(contacts.contacts().len(), 1);
    assert_eq!(
        contacts.contacts()[0].verification_method(),
        Some(ContactVerificationMethod::SafetyNumber)
    );
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_invalid_mismatched_and_unknown_safety_numbers_without_verification() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let remote_identity = IdentityKeypair::generate().unwrap();
    let unknown_identity = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote_identity).unwrap();
    let safety_number =
        SafetyNumberFingerprint::derive(&local_public_key, &remote_identity.public_key()).unwrap();
    let unknown_safety_number =
        SafetyNumberFingerprint::derive(&local_public_key, &unknown_identity.public_key()).unwrap();
    let mut mismatched = *safety_number.as_bytes();
    mismatched[0] ^= 1;
    let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
    PendingContactImportService::new(&local_public_key, &mut contacts)
        .import_encoded(&invitation.encode().unwrap())
        .unwrap();
    {
        let mut verifier = SafetyNumberVerificationService::new(&local_public_key, &mut contacts);
        assert!(matches!(
            verifier.verify(
                &remote_identity.public_key(),
                &[0; SAFETY_NUMBER_FINGERPRINT_BYTES - 1]
            ),
            Err(SafetyNumberVerificationError::InvalidFingerprintLength)
        ));
        assert!(matches!(
            verifier.verify(&remote_identity.public_key(), &mismatched),
            Err(SafetyNumberVerificationError::ContactStore(
                ContactStoreError::SafetyNumberMismatch
            ))
        ));
        assert!(matches!(
            verifier.verify(
                &unknown_identity.public_key(),
                unknown_safety_number.as_bytes()
            ),
            Err(SafetyNumberVerificationError::ContactStore(
                ContactStoreError::UnknownContact
            ))
        ));
    }
    assert_eq!(
        contacts
            .contact(&remote_identity.public_key())
            .unwrap()
            .status(),
        ContactStatus::Pending
    );
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
