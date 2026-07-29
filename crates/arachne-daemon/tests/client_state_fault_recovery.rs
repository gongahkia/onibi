use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{EncryptedStateStore, StateDocument, StateStoreError};
use rusqlite::Connection;

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
            "arachne-client-state-fault-recovery-{}-{number}",
            std::process::id()
        ))
        .join("state.sqlite")
}

#[test]
fn rolls_back_an_injected_client_state_fault_and_recovers_after_reopen() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let original = StateDocument::new(b"original client state".to_vec()).unwrap();
    let replacement = StateDocument::new(b"replacement client state".to_vec()).unwrap();
    let mut keystore = MemoryKeystore::default();
    {
        let mut store = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        store.replace(&original).unwrap();
    }
    let connection = Connection::open(&path).unwrap();
    connection
        .execute_batch(
            "CREATE TRIGGER reject_client_state_update
             BEFORE UPDATE ON sealed_state
             BEGIN SELECT RAISE(ABORT, 'injected client-state fault'); END;",
        )
        .unwrap();
    drop(connection);

    {
        let mut store = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        assert!(matches!(
            store.replace(&replacement),
            Err(StateStoreError::Sqlite(_))
        ));
    }
    let recovered = EncryptedStateStore::open(&path, &mut keystore)
        .unwrap()
        .load()
        .unwrap()
        .unwrap();
    assert_eq!(recovered.as_bytes(), original.as_bytes());

    let connection = Connection::open(&path).unwrap();
    connection
        .execute_batch("DROP TRIGGER reject_client_state_update;")
        .unwrap();
    drop(connection);
    {
        let mut store = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        store.replace(&replacement).unwrap();
    }
    let recovered = EncryptedStateStore::open(&path, &mut keystore)
        .unwrap()
        .load()
        .unwrap()
        .unwrap();
    assert_eq!(recovered.as_bytes(), replacement.as_bytes());
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
