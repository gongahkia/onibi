use std::{collections::BTreeMap, convert::Infallible};

use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    SIGNED_PREKEY_KEY_ENTRY, SignedPrekeyInitialization, SignedPrekeyLifecycle,
    SignedPrekeyLifecycleError,
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

#[derive(Default)]
struct StoreFailingKeystore {
    entries: BTreeMap<String, Vec<u8>>,
    fail_store: bool,
}

#[derive(Debug, thiserror::Error)]
#[error("keystore write failed")]
struct StoreFailure;

impl OsKeystore for StoreFailingKeystore {
    type Error = StoreFailure;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .entries
            .get(entry.as_str())
            .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error> {
        if self.fail_store {
            return Err(StoreFailure);
        }
        self.entries
            .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
        Ok(())
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
        self.entries.remove(entry.as_str());
        Ok(())
    }
}

#[test]
fn creates_loads_and_rotates_one_signed_prekey_for_the_client_identity() {
    let identity = IdentityKeypair::generate().unwrap();
    let mut keystore = MemoryKeystore::default();
    let (mut lifecycle, initialization) =
        SignedPrekeyLifecycle::create_or_load(&identity, &mut keystore).unwrap();
    let initial = lifecycle.public();
    assert_eq!(initialization, SignedPrekeyInitialization::Created);
    assert_eq!(initial.verify(&identity.public_key()), Ok(()));
    assert!(keystore.0.contains_key(SIGNED_PREKEY_KEY_ENTRY));

    let (loaded, initialization) =
        SignedPrekeyLifecycle::create_or_load(&identity, &mut keystore).unwrap();
    assert_eq!(initialization, SignedPrekeyInitialization::Loaded);
    assert_eq!(loaded.public(), initial);

    lifecycle.rotate(&identity, &mut keystore).unwrap();
    let rotated = lifecycle.public();
    assert_eq!(rotated.generation(), initial.generation() + 1);
    assert_ne!(rotated.prekey(), initial.prekey());
    assert_eq!(rotated.verify(&identity.public_key()), Ok(()));
    assert_eq!(
        SignedPrekeyLifecycle::load(&identity, &keystore)
            .unwrap()
            .public(),
        rotated
    );
}

#[test]
fn rejects_missing_duplicate_corrupt_and_wrong_identity_signed_prekeys() {
    let identity = IdentityKeypair::generate().unwrap();
    let wrong_identity = IdentityKeypair::generate().unwrap();
    let mut keystore = MemoryKeystore::default();
    assert!(matches!(
        SignedPrekeyLifecycle::load(&identity, &keystore),
        Err(SignedPrekeyLifecycleError::NotInitialized)
    ));
    SignedPrekeyLifecycle::create(&identity, &mut keystore).unwrap();
    assert!(matches!(
        SignedPrekeyLifecycle::create(&identity, &mut keystore),
        Err(SignedPrekeyLifecycleError::AlreadyInitialized)
    ));
    assert!(matches!(
        SignedPrekeyLifecycle::load(&wrong_identity, &keystore),
        Err(SignedPrekeyLifecycleError::InvalidStoredPrekey(_))
    ));
    keystore
        .0
        .insert(SIGNED_PREKEY_KEY_ENTRY.to_owned(), vec![0; 1]);
    assert!(matches!(
        SignedPrekeyLifecycle::load(&identity, &keystore),
        Err(SignedPrekeyLifecycleError::InvalidSerialization)
    ));
}

#[test]
fn preserves_the_loaded_signed_prekey_when_rotation_persistence_fails() {
    let identity = IdentityKeypair::generate().unwrap();
    let mut keystore = StoreFailingKeystore::default();
    let mut lifecycle = SignedPrekeyLifecycle::create(&identity, &mut keystore).unwrap();
    let initial = lifecycle.public();
    keystore.fail_store = true;
    assert!(matches!(
        lifecycle.rotate(&identity, &mut keystore),
        Err(SignedPrekeyLifecycleError::Keystore)
    ));
    assert_eq!(lifecycle.public(), initial);
}
