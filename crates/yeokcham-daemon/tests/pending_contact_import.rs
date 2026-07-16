use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    ContactStatus, ContactStore, ContactStoreError, PendingContactImportError,
    PendingContactImportService,
};
use yeokcham_protocol::{ContactInvitation, ContactInvitationError};

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
            "yeokcham-pending-contact-import-{}-{number}",
            std::process::id()
        ))
        .join("contacts.sqlite")
}

#[test]
fn imports_a_pending_contact_once_and_persists_it() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let remote_identity = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote_identity)
        .unwrap()
        .encode()
        .unwrap();
    {
        let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
        let mut importer = PendingContactImportService::new(&local_public_key, &mut contacts);
        let contact = importer.import_encoded(&invitation).unwrap();
        assert_eq!(contact.identity(), &remote_identity.public_key());
        assert_eq!(contact.status(), ContactStatus::Pending);
        assert_eq!(importer.import_encoded(&invitation).unwrap(), contact);
    }
    let contacts = ContactStore::open(&path, &mut keystore).unwrap();
    assert_eq!(contacts.contacts().len(), 1);
    assert_eq!(
        contacts.contacts()[0].identity(),
        &remote_identity.public_key()
    );
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_invalid_and_self_invitations_without_persisting_a_contact() {
    let path = contacts_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let local_identity = IdentityKeypair::generate().unwrap();
    let local_public_key = local_identity.public_key();
    let self_invitation = ContactInvitation::create(&local_identity)
        .unwrap()
        .encode()
        .unwrap();
    let mut contacts = ContactStore::open(&path, &mut keystore).unwrap();
    {
        let mut importer = PendingContactImportService::new(&local_public_key, &mut contacts);
        assert!(matches!(
            importer.import_encoded(&[0xa1]),
            Err(PendingContactImportError::Invitation(
                ContactInvitationError::InvalidLength
            ))
        ));
        assert!(matches!(
            importer.import_encoded(&self_invitation),
            Err(PendingContactImportError::ContactStore(
                ContactStoreError::SelfContact
            ))
        ));
    }
    assert!(contacts.contacts().is_empty());
    drop(contacts);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
