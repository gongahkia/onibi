use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OneTimePrekeyId, OsKeystore};
use yeokcham_daemon::{OneTimePrekeyReplenisher, OneTimePrekeyReplenisherError};
use yeokcham_protocol::MAX_ONE_TIME_PREKEYS;

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

#[derive(Default)]
struct StoreFailingKeystore {
    entries: BTreeMap<String, Vec<u8>>,
    fail_prekey_store: bool,
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
        if self.fail_prekey_store && entry.as_str().starts_with("otp_") {
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

fn inventory_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "yeokcham-one-time-prekey-replenisher-{}-{number}",
            std::process::id()
        ))
        .join("inventory.sqlite")
}

#[test]
fn replenishes_to_the_requested_target_and_restores_the_durable_inventory() {
    let path = inventory_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut replenisher = OneTimePrekeyReplenisher::open(&path, MemoryKeystore::default()).unwrap();
    assert_eq!(
        replenisher
            .replenish(3)
            .unwrap()
            .into_iter()
            .map(OneTimePrekeyId::get)
            .collect::<Vec<_>>(),
        vec![1, 2, 3]
    );
    assert_eq!(
        replenisher
            .available_public()
            .unwrap()
            .iter()
            .map(|prekey| prekey.identifier().get())
            .collect::<Vec<_>>(),
        vec![1, 2, 3]
    );
    assert_eq!(
        replenisher
            .available()
            .map(OneTimePrekeyId::get)
            .collect::<Vec<_>>(),
        vec![1, 2, 3]
    );
    let keystore = replenisher.into_keystore();
    let mut restored = OneTimePrekeyReplenisher::open(&path, keystore).unwrap();
    assert!(restored.replenish(3).unwrap().is_empty());
    assert_eq!(
        restored
            .replenish(5)
            .unwrap()
            .into_iter()
            .map(OneTimePrekeyId::get)
            .collect::<Vec<_>>(),
        vec![4, 5]
    );
    drop(restored);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_oversized_targets_and_never_reuses_reserved_identifiers_after_a_write_failure() {
    let path = inventory_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut replenisher = OneTimePrekeyReplenisher::open(
        &path,
        StoreFailingKeystore {
            entries: BTreeMap::new(),
            fail_prekey_store: true,
        },
    )
    .unwrap();
    assert!(matches!(
        replenisher.replenish(MAX_ONE_TIME_PREKEYS + 1),
        Err(OneTimePrekeyReplenisherError::TargetTooLarge)
    ));
    assert!(matches!(
        replenisher.replenish(2),
        Err(OneTimePrekeyReplenisherError::Keystore(_))
    ));
    let mut keystore = replenisher.into_keystore();
    keystore.fail_prekey_store = false;
    let mut restored = OneTimePrekeyReplenisher::open(&path, keystore).unwrap();
    assert_eq!(
        restored
            .replenish(2)
            .unwrap()
            .into_iter()
            .map(OneTimePrekeyId::get)
            .collect::<Vec<_>>(),
        vec![3, 4]
    );
    drop(restored);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
