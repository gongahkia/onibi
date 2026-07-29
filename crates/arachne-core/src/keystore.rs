use std::error::Error;
use std::fmt;

use zeroize::Zeroizing;

pub const MAX_KEYSTORE_ENTRY_NAME_BYTES: usize = 64;
pub const MAX_KEYSTORE_SECRET_BYTES: usize = 4_096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct KeystoreEntryName(String);

impl KeystoreEntryName {
    pub fn new(value: String) -> Result<Self, KeystoreEntryNameError> {
        if value.is_empty() {
            return Err(KeystoreEntryNameError::Empty);
        }
        if value.len() > MAX_KEYSTORE_ENTRY_NAME_BYTES {
            return Err(KeystoreEntryNameError::TooLong);
        }
        if !value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
        {
            return Err(KeystoreEntryNameError::InvalidCharacter);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

pub struct KeystoreSecret(Zeroizing<Vec<u8>>);

impl KeystoreSecret {
    pub fn new(value: Vec<u8>) -> Result<Self, KeystoreSecretError> {
        let value = Zeroizing::new(value);
        if value.is_empty() {
            return Err(KeystoreSecretError::Empty);
        }
        if value.len() > MAX_KEYSTORE_SECRET_BYTES {
            return Err(KeystoreSecretError::TooLong);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }
}

impl fmt::Debug for KeystoreSecret {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("KeystoreSecret(REDACTED)")
    }
}

pub trait OsKeystore {
    type Error: Error + Send + Sync + 'static;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error>;
    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error>;
    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error>;
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum KeystoreEntryNameError {
    #[error("keystore entry name is required")]
    Empty,
    #[error("keystore entry name exceeds the configured limit")]
    TooLong,
    #[error("keystore entry name contains unsupported characters")]
    InvalidCharacter,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum KeystoreSecretError {
    #[error("keystore secret is required")]
    Empty,
    #[error("keystore secret exceeds the configured limit")]
    TooLong,
}

#[cfg(test)]
mod tests {
    use std::convert::Infallible;

    use super::{
        KeystoreEntryName, KeystoreEntryNameError, KeystoreSecret, KeystoreSecretError,
        MAX_KEYSTORE_ENTRY_NAME_BYTES, MAX_KEYSTORE_SECRET_BYTES, OsKeystore,
    };

    struct InMemoryKeystore {
        entry: Option<(KeystoreEntryName, KeystoreSecret)>,
    }

    impl OsKeystore for InMemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self.entry.as_ref().and_then(|(stored_entry, secret)| {
                (stored_entry == entry)
                    .then(|| KeystoreSecret::new(secret.as_bytes().to_vec()).unwrap())
            }))
        }

        fn store(
            &mut self,
            entry: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.entry = Some((
                entry.clone(),
                KeystoreSecret::new(secret.as_bytes().to_vec()).unwrap(),
            ));
            Ok(())
        }

        fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            if self
                .entry
                .as_ref()
                .is_some_and(|(stored_entry, _)| stored_entry == entry)
            {
                self.entry = None;
            }
            Ok(())
        }
    }

    #[test]
    fn validates_entry_names_and_secret_bounds() {
        assert_eq!(
            KeystoreEntryName::new(String::new()).unwrap_err(),
            KeystoreEntryNameError::Empty
        );
        assert_eq!(
            KeystoreEntryName::new("x".repeat(MAX_KEYSTORE_ENTRY_NAME_BYTES + 1)).unwrap_err(),
            KeystoreEntryNameError::TooLong
        );
        assert_eq!(
            KeystoreEntryName::new("identity/key".to_owned()).unwrap_err(),
            KeystoreEntryNameError::InvalidCharacter
        );
        assert_eq!(
            KeystoreSecret::new(Vec::new()).unwrap_err(),
            KeystoreSecretError::Empty
        );
        assert_eq!(
            KeystoreSecret::new(vec![0; MAX_KEYSTORE_SECRET_BYTES + 1]).unwrap_err(),
            KeystoreSecretError::TooLong
        );
    }

    #[test]
    fn keystore_contract_round_trips_and_deletes_redacted_secrets() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1, 2, 3]).unwrap();
        let mut keystore = InMemoryKeystore { entry: None };

        keystore.store(&entry, &secret).unwrap();
        let loaded = keystore.load(&entry).unwrap().unwrap();
        assert_eq!(loaded.as_bytes(), [1, 2, 3]);
        assert_eq!(format!("{loaded:?}"), "KeystoreSecret(REDACTED)");
        keystore.delete(&entry).unwrap();
        assert!(keystore.load(&entry).unwrap().is_none());
    }
}
