use std::{error::Error, fmt};

use crate::{
    KeystoreEntryName, KeystoreEntryNameError, KeystoreSecret, KeystoreSecretError, OsKeystore,
    X25519Prekey, X25519PrekeyPublicKey, X25519PrekeySerializationError,
};

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct OneTimePrekeyId(u64);

impl OneTimePrekeyId {
    pub const fn new(value: u64) -> Result<Self, OneTimePrekeyIdError> {
        if value == 0 {
            return Err(OneTimePrekeyIdError::Zero);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }

    fn entry_name(self) -> Result<KeystoreEntryName, KeystoreEntryNameError> {
        KeystoreEntryName::new(format!("otp_{:016x}", self.0))
    }
}

pub struct OneTimePrekeyStore<K> {
    keystore: K,
}

impl<K> OneTimePrekeyStore<K>
where
    K: OsKeystore,
{
    #[must_use]
    pub const fn new(keystore: K) -> Self {
        Self { keystore }
    }

    #[must_use]
    pub fn into_inner(self) -> K {
        self.keystore
    }

    pub fn store(
        &mut self,
        identifier: OneTimePrekeyId,
        prekey: X25519Prekey,
    ) -> Result<(), OneTimePrekeyStoreError<K::Error>> {
        let entry = identifier.entry_name()?;
        if self
            .keystore
            .load(&entry)
            .map_err(OneTimePrekeyStoreError::Keystore)?
            .is_some()
        {
            return Err(OneTimePrekeyStoreError::AlreadyExists);
        }
        let serialized = prekey.serialize();
        drop(prekey);
        let secret = KeystoreSecret::new(serialized.to_vec())?;
        self.keystore
            .store(&entry, &secret)
            .map_err(OneTimePrekeyStoreError::Keystore)
    }

    pub fn take(
        &mut self,
        identifier: OneTimePrekeyId,
    ) -> Result<X25519Prekey, OneTimePrekeyStoreError<K::Error>> {
        let prekey = self.load(identifier)?;
        let entry = identifier.entry_name()?;
        self.keystore
            .delete(&entry)
            .map_err(OneTimePrekeyStoreError::Keystore)?;
        Ok(prekey)
    }

    pub fn load(
        &self,
        identifier: OneTimePrekeyId,
    ) -> Result<X25519Prekey, OneTimePrekeyStoreError<K::Error>> {
        let entry = identifier.entry_name()?;
        let secret = self
            .keystore
            .load(&entry)
            .map_err(OneTimePrekeyStoreError::Keystore)?
            .ok_or(OneTimePrekeyStoreError::NotFound)?;
        Ok(X25519Prekey::deserialize(secret.as_bytes())?)
    }

    pub fn public(
        &self,
        identifier: OneTimePrekeyId,
    ) -> Result<X25519PrekeyPublicKey, OneTimePrekeyStoreError<K::Error>> {
        let entry = identifier.entry_name()?;
        let secret = self
            .keystore
            .load(&entry)
            .map_err(OneTimePrekeyStoreError::Keystore)?
            .ok_or(OneTimePrekeyStoreError::NotFound)?;
        Ok(X25519Prekey::deserialize(secret.as_bytes())?.public_key())
    }
}

impl<K> fmt::Debug for OneTimePrekeyStore<K> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("OneTimePrekeyStore(REDACTED)")
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum OneTimePrekeyIdError {
    #[error("one-time prekey identifier must not be zero")]
    Zero,
}

#[derive(Debug, thiserror::Error)]
pub enum OneTimePrekeyStoreError<E>
where
    E: Error + 'static,
{
    #[error("operating-system keystore operation failed: {0}")]
    Keystore(#[source] E),
    #[error("one-time prekey already exists")]
    AlreadyExists,
    #[error("one-time prekey was not found")]
    NotFound,
    #[error(transparent)]
    EntryName(#[from] KeystoreEntryNameError),
    #[error(transparent)]
    Secret(#[from] KeystoreSecretError),
    #[error(transparent)]
    Prekey(#[from] X25519PrekeySerializationError),
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible};

    use super::{
        OneTimePrekeyId, OneTimePrekeyIdError, OneTimePrekeyStore, OneTimePrekeyStoreError,
    };
    use crate::{KeystoreEntryName, KeystoreSecret, OsKeystore, X25519Prekey};

    #[derive(Default)]
    struct InMemoryKeystore {
        entries: BTreeMap<String, KeystoreSecret>,
    }

    impl OsKeystore for InMemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self.entries.get(entry.as_str()).map(|secret| {
                KeystoreSecret::new(secret.as_bytes().to_vec()).expect("stored secret is valid")
            }))
        }

        fn store(
            &mut self,
            entry: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.entries.insert(
                entry.as_str().to_owned(),
                KeystoreSecret::new(secret.as_bytes().to_vec()).expect("stored secret is valid"),
            );
            Ok(())
        }

        fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.entries.remove(entry.as_str());
            Ok(())
        }
    }

    #[derive(Debug, thiserror::Error)]
    #[error("deletion failed")]
    struct DeleteFailure;

    #[derive(Default)]
    struct DeleteFailingKeystore {
        secret: Option<KeystoreSecret>,
    }

    impl OsKeystore for DeleteFailingKeystore {
        type Error = DeleteFailure;

        fn load(&self, _: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self.secret.as_ref().map(|secret| {
                KeystoreSecret::new(secret.as_bytes().to_vec()).expect("stored secret is valid")
            }))
        }

        fn store(
            &mut self,
            _: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.secret = Some(
                KeystoreSecret::new(secret.as_bytes().to_vec()).expect("stored secret is valid"),
            );
            Ok(())
        }

        fn delete(&mut self, _: &KeystoreEntryName) -> Result<(), Self::Error> {
            Err(DeleteFailure)
        }
    }

    #[test]
    fn stores_and_consumes_prekeys_once() {
        let identifier = OneTimePrekeyId::new(7).unwrap();
        let prekey = X25519Prekey::generate().unwrap();
        let public_key = prekey.public_key();
        let mut store = OneTimePrekeyStore::new(InMemoryKeystore::default());

        store.store(identifier, prekey).unwrap();
        assert_eq!(store.public(identifier).unwrap(), public_key);
        assert_eq!(store.load(identifier).unwrap().public_key(), public_key);
        assert_eq!(store.take(identifier).unwrap().public_key(), public_key);
        assert!(matches!(
            store.take(identifier),
            Err(OneTimePrekeyStoreError::NotFound)
        ));
    }

    #[test]
    fn rejects_invalid_identifiers_and_duplicate_entries() {
        assert_eq!(
            OneTimePrekeyId::new(0).unwrap_err(),
            OneTimePrekeyIdError::Zero
        );
        let identifier = OneTimePrekeyId::new(1).unwrap();
        let mut store = OneTimePrekeyStore::new(InMemoryKeystore::default());

        store
            .store(identifier, X25519Prekey::generate().unwrap())
            .unwrap();
        assert!(matches!(
            store.store(identifier, X25519Prekey::generate().unwrap()),
            Err(OneTimePrekeyStoreError::AlreadyExists)
        ));
    }

    #[test]
    fn rejects_corrupt_keystore_prekeys() {
        let identifier = OneTimePrekeyId::new(9).unwrap();
        let entry = identifier.entry_name().unwrap();
        let mut keystore = InMemoryKeystore::default();
        let mut corrupt = vec![1; 33];
        corrupt[0] = 2;
        keystore
            .store(&entry, &KeystoreSecret::new(corrupt).unwrap())
            .unwrap();
        let mut store = OneTimePrekeyStore::new(keystore);

        assert!(matches!(
            store.take(identifier),
            Err(OneTimePrekeyStoreError::Prekey(_))
        ));
    }

    #[test]
    fn does_not_return_a_prekey_when_deletion_fails() {
        let identifier = OneTimePrekeyId::new(11).unwrap();
        let mut store = OneTimePrekeyStore::new(DeleteFailingKeystore::default());

        store
            .store(identifier, X25519Prekey::generate().unwrap())
            .unwrap();
        assert!(matches!(
            store.take(identifier),
            Err(OneTimePrekeyStoreError::Keystore(DeleteFailure))
        ));
    }
}
