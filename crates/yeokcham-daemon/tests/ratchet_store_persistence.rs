use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore, X25519Prekey};
use yeokcham_daemon::{EncryptedStateStore, RatchetStore, RatchetStoreError, StateDocument};
use yeokcham_protocol::DoubleRatchetState;

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
    std::env::temp_dir()
        .join(format!(
            "yeokcham-ratchet-store-persistence-{}-{number}",
            std::process::id()
        ))
        .join("ratchets.sqlite")
}

#[test]
fn persists_an_advanced_double_ratchet_session_across_reopen() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let peer = X25519Prekey::generate().unwrap();
    let mut ratchet = DoubleRatchetState::initialize([7; 32], peer.public_key()).unwrap();
    let expected_sending_key = ratchet.next_sending_key().unwrap();
    {
        let mut store = RatchetStore::open(&path, &mut keystore).unwrap();
        store.replace(&ratchet).unwrap();
    }
    let mut restored = RatchetStore::open(&path, &mut keystore)
        .unwrap()
        .load()
        .unwrap()
        .unwrap();
    assert_ne!(
        expected_sending_key.as_ref(),
        restored.next_sending_key().unwrap().as_ref()
    );
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn fails_closed_when_the_encrypted_document_is_not_a_ratchet() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    EncryptedStateStore::open(&path, &mut keystore)
        .unwrap()
        .replace(&StateDocument::new(vec![0xa1]).unwrap())
        .unwrap();
    assert!(matches!(
        RatchetStore::open(&path, &mut keystore).unwrap().load(),
        Err(RatchetStoreError::Ratchet(_))
    ));
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
