use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    ContactLifecycleError, ContactLifecycleService, ContactStatus, ContactStore, ContactStoreError,
    PendingContactImportService, QrContactVerificationService,
};
use arachne_protocol::{ContactInvitation, IdentityRotation, QrVerificationPayload};

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
            "arachne-contact-lifecycle-{}-{number}",
            std::process::id()
        ))
        .join("contacts.sqlite")
}

#[test]
fn applies_a_verified_contact_rotation_then_revokes_the_replacement() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let remote_identity = IdentityKeypair::generate().unwrap();
    let replacement_identity = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote_identity).unwrap();
    let verification = QrVerificationPayload::new(local_public_key, remote_identity.public_key())
        .unwrap()
        .encode()
        .unwrap();
    let rotation = IdentityRotation::create(&remote_identity, replacement_identity.public_key())
        .unwrap()
        .encode()
        .unwrap();
    {
        let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
        PendingContactImportService::new(&local_public_key, &mut contacts)
            .import_encoded(&invitation.encode().unwrap())
            .unwrap();
        QrContactVerificationService::new(&local_public_key, &mut contacts)
            .verify_encoded(&verification)
            .unwrap();
        let pending = ContactLifecycleService::new(&local_public_key, &mut contacts)
            .apply_rotation_encoded(&rotation)
            .unwrap();
        assert_eq!(pending.identity(), &replacement_identity.public_key());
        assert_eq!(pending.status(), ContactStatus::Pending);
        assert_eq!(
            contacts
                .contact(&remote_identity.public_key())
                .unwrap()
                .status(),
            ContactStatus::Revoked
        );
        let revoked = ContactLifecycleService::new(&local_public_key, &mut contacts)
            .revoke(&replacement_identity.public_key())
            .unwrap();
        assert_eq!(revoked.status(), ContactStatus::Revoked);
    }
    let contacts = ContactStore::open(&path, &mut keystore).unwrap();
    assert_eq!(contacts.contacts().len(), 2);
    assert!(
        contacts
            .contacts()
            .iter()
            .all(|contact| contact.status() == ContactStatus::Revoked)
    );
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_invalid_or_unknown_lifecycle_updates_without_persisting_state() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let unknown_identity = IdentityKeypair::generate().unwrap();
    let replacement_identity = IdentityKeypair::generate().unwrap();
    let unknown_rotation =
        IdentityRotation::create(&unknown_identity, replacement_identity.public_key())
            .unwrap()
            .encode()
            .unwrap();
    let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
    {
        let mut lifecycle = ContactLifecycleService::new(&local_public_key, &mut contacts);
        assert!(matches!(
            lifecycle.apply_rotation_encoded(&[0xa1]),
            Err(ContactLifecycleError::IdentityRotation(_))
        ));
        assert!(matches!(
            lifecycle.apply_rotation_encoded(&unknown_rotation),
            Err(ContactLifecycleError::ContactStore(
                ContactStoreError::UnknownContact
            ))
        ));
        assert!(matches!(
            lifecycle.revoke(&unknown_identity.public_key()),
            Err(ContactLifecycleError::ContactStore(
                ContactStoreError::UnknownContact
            ))
        ));
    }
    assert!(contacts.contacts().is_empty());
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
