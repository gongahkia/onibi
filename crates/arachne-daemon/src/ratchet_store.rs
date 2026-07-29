use std::path::Path;

use arachne_core::OsKeystore;
use arachne_protocol::DoubleRatchetState;

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub struct RatchetStore {
    state: EncryptedStateStore,
}

impl RatchetStore {
    pub fn open<K: OsKeystore>(path: &Path, keystore: &mut K) -> Result<Self, RatchetStoreError> {
        Ok(Self {
            state: EncryptedStateStore::open(path, keystore)?,
        })
    }

    pub fn load(&self) -> Result<Option<DoubleRatchetState>, RatchetStoreError> {
        self.state
            .load()?
            .map(|document| DoubleRatchetState::decode(document.as_bytes()))
            .transpose()
            .map_err(RatchetStoreError::Ratchet)
    }

    pub fn replace(&mut self, ratchet: &DoubleRatchetState) -> Result<(), RatchetStoreError> {
        let encoded = ratchet.encode().map_err(RatchetStoreError::Ratchet)?;
        let document = StateDocument::new(encoded.to_vec()).map_err(RatchetStoreError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RatchetStoreError {
    #[error("encrypted state-store operation failed: {0}")]
    StateStore(#[from] StateStoreError),
    #[error("ratchet-state document is invalid: {0}")]
    Ratchet(#[source] arachne_protocol::DoubleRatchetError),
    #[error("ratchet-state document violates storage bounds: {0}")]
    Document(#[source] StateDocumentError),
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore, X25519Prekey};
    use arachne_protocol::DoubleRatchetState;

    use super::RatchetStore;

    #[derive(Default)]
    struct MemoryKeystore(BTreeMap<String, Vec<u8>>);
    impl OsKeystore for MemoryKeystore {
        type Error = Infallible;
        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .0
                .get(entry.as_str())
                .map(|value| KeystoreSecret::new(value.clone()).unwrap()))
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
    fn path() -> PathBuf {
        std::env::temp_dir().join(format!(
            "arachne-ratchet-store-{}.sqlite",
            std::process::id()
        ))
    }

    #[test]
    fn persists_encrypted_ratchet_state() {
        let path = path();
        let mut keystore = MemoryKeystore::default();
        let peer = X25519Prekey::generate().unwrap();
        let mut ratchet = DoubleRatchetState::initialize([7; 32], peer.public_key()).unwrap();
        ratchet.next_sending_key().unwrap();
        RatchetStore::open(&path, &mut keystore)
            .unwrap()
            .replace(&ratchet)
            .unwrap();
        let mut restored = RatchetStore::open(&path, &mut keystore)
            .unwrap()
            .load()
            .unwrap()
            .unwrap();
        assert_ne!(&*restored.next_sending_key().unwrap(), &[0; 32]);
        fs::remove_file(path).unwrap();
    }
}
