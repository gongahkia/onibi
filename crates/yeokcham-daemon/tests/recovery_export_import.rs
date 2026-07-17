use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    ClientIdentity, ClientIdentityError, EncryptedStateStore, StateDocument, StateStoreError,
};
use yeokcham_protocol::{IdentityExportError, IdentityExportPassphrase, StateExportError};

static NEXT_TEST_DIRECTORY: AtomicU64 = AtomicU64::new(0);

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

fn test_directory() -> PathBuf {
    let number = NEXT_TEST_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-recovery-export-import-{}-{number}",
        std::process::id()
    ))
}

#[test]
fn transfers_recovery_material_to_empty_destinations_only() {
    let directory = test_directory();
    fs::create_dir(&directory).unwrap();
    let passphrase =
        IdentityExportPassphrase::new(b"recovery transfer passphrase".to_vec()).unwrap();
    let document = StateDocument::new(b"durable recovery state".to_vec()).unwrap();

    let (identity_export, state_export, public_key) = {
        let mut identity_keystore = MemoryKeystore::default();
        let identity = ClientIdentity::create(&mut identity_keystore).unwrap();
        let mut state_keystore = MemoryKeystore::default();
        let mut state =
            EncryptedStateStore::open(&directory.join("source.sqlite"), &mut state_keystore)
                .unwrap();
        state.replace(&document).unwrap();
        (
            identity.export_recovery(&passphrase).unwrap(),
            state.export_recovery(&passphrase).unwrap(),
            identity.public_key(),
        )
    };

    let mut restored_identity_keystore = MemoryKeystore::default();
    let restored_identity = ClientIdentity::import_recovery(
        &mut restored_identity_keystore,
        &identity_export,
        &passphrase,
    )
    .unwrap();
    assert_eq!(restored_identity.public_key(), public_key);
    assert!(matches!(
        ClientIdentity::import_recovery(
            &mut restored_identity_keystore,
            &identity_export,
            &passphrase
        ),
        Err(ClientIdentityError::AlreadyInitialized)
    ));

    let mut restored_state_keystore = MemoryKeystore::default();
    let mut restored_state = EncryptedStateStore::open(
        &directory.join("restored.sqlite"),
        &mut restored_state_keystore,
    )
    .unwrap();
    restored_state
        .import_recovery(&state_export, &passphrase)
        .unwrap();
    assert_eq!(
        restored_state.load().unwrap().unwrap().as_bytes(),
        document.as_bytes()
    );
    assert!(matches!(
        restored_state.import_recovery(&state_export, &passphrase),
        Err(StateStoreError::StateImportDestinationNotEmpty)
    ));

    let mut empty_identity_keystore = MemoryKeystore::default();
    let mut tampered_identity_export = identity_export;
    *tampered_identity_export.last_mut().unwrap() ^= 0x01;
    assert!(matches!(
        ClientIdentity::import_recovery(
            &mut empty_identity_keystore,
            &tampered_identity_export,
            &passphrase
        ),
        Err(ClientIdentityError::RecoveryImport(
            IdentityExportError::Authentication
        ))
    ));
    assert!(matches!(
        ClientIdentity::load(&empty_identity_keystore),
        Err(ClientIdentityError::NotInitialized)
    ));

    let mut empty_state_keystore = MemoryKeystore::default();
    let mut empty_state =
        EncryptedStateStore::open(&directory.join("empty.sqlite"), &mut empty_state_keystore)
            .unwrap();
    let mut tampered_state_export = state_export;
    *tampered_state_export.last_mut().unwrap() ^= 0x01;
    assert!(matches!(
        empty_state.import_recovery(&tampered_state_export, &passphrase),
        Err(StateStoreError::RecoveryImport(
            StateExportError::Authentication
        ))
    ));
    assert!(empty_state.load().unwrap().is_none());

    drop(empty_state);
    drop(restored_state);
    fs::remove_dir_all(directory).unwrap();
}
