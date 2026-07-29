use std::{collections::BTreeMap, convert::Infallible};

use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    CLIENT_IDENTITY_KEY_ENTRY, ClientIdentity, ClientIdentityError, ClientIdentityInitialization,
};

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

#[test]
fn initializes_one_identity_and_reloads_the_same_public_key() {
    let mut keystore = MemoryKeystore::default();
    let (created, state) = ClientIdentity::create_or_load(&mut keystore).unwrap();
    assert_eq!(state, ClientIdentityInitialization::Created);
    assert!(keystore.0.contains_key(CLIENT_IDENTITY_KEY_ENTRY));
    let (loaded, state) = ClientIdentity::create_or_load(&mut keystore).unwrap();
    assert_eq!(state, ClientIdentityInitialization::Loaded);
    assert_eq!(created.public_key(), loaded.public_key());
}

#[test]
fn rejects_duplicate_missing_and_corrupt_identity_lifecycle_state() {
    let mut keystore = MemoryKeystore::default();
    assert!(matches!(
        ClientIdentity::load(&keystore),
        Err(ClientIdentityError::NotInitialized)
    ));
    ClientIdentity::create(&mut keystore).unwrap();
    assert!(matches!(
        ClientIdentity::create(&mut keystore),
        Err(ClientIdentityError::AlreadyInitialized)
    ));
    keystore
        .0
        .insert(CLIENT_IDENTITY_KEY_ENTRY.to_owned(), vec![0; 1]);
    assert!(matches!(
        ClientIdentity::load(&keystore),
        Err(ClientIdentityError::InvalidStoredIdentity(_))
    ));
}
