use security_framework::base::Error as SecurityFrameworkError;
use security_framework::passwords::{
    PasswordOptions, delete_generic_password_options, generic_password,
    set_generic_password_options,
};
use security_framework_sys::base::errSecItemNotFound;

use crate::{KeystoreEntryName, KeystoreSecret, KeystoreSecretError, OsKeystore};

pub const MACOS_KEYCHAIN_SERVICE: &str = "com.github.gongahkia.yeokcham";

pub struct MacOsKeystore {
    inner: KeychainKeystore<SystemKeychain>,
}

impl MacOsKeystore {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            inner: KeychainKeystore::new(SystemKeychain),
        }
    }
}

impl Default for MacOsKeystore {
    fn default() -> Self {
        Self::new()
    }
}

impl OsKeystore for MacOsKeystore {
    type Error = MacOsKeystoreError;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        self.inner.load(entry)
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error> {
        self.inner.store(entry, secret)
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
        self.inner.delete(entry)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum MacOsKeystoreError {
    #[error("macOS Keychain operation failed: {0}")]
    Keychain(#[source] SecurityFrameworkError),
    #[error("macOS Keychain returned an invalid secret: {0}")]
    InvalidSecret(#[from] KeystoreSecretError),
}

struct KeychainKeystore<K> {
    keychain: K,
}

impl<K> KeychainKeystore<K>
where
    K: MacOsKeychain,
{
    const fn new(keychain: K) -> Self {
        Self { keychain }
    }

    fn load(
        &self,
        entry: &KeystoreEntryName,
    ) -> Result<Option<KeystoreSecret>, MacOsKeystoreError> {
        match self.keychain.load(MACOS_KEYCHAIN_SERVICE, entry.as_str()) {
            Ok(secret) => Ok(Some(KeystoreSecret::new(secret)?)),
            Err(MacOsKeychainFailure::NotFound) => Ok(None),
            Err(MacOsKeychainFailure::System(error)) => Err(MacOsKeystoreError::Keychain(error)),
        }
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), MacOsKeystoreError> {
        self.keychain
            .store(MACOS_KEYCHAIN_SERVICE, entry.as_str(), secret.as_bytes())
            .map_err(MacOsKeystoreError::from)
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), MacOsKeystoreError> {
        match self.keychain.delete(MACOS_KEYCHAIN_SERVICE, entry.as_str()) {
            Ok(()) | Err(MacOsKeychainFailure::NotFound) => Ok(()),
            Err(MacOsKeychainFailure::System(error)) => Err(MacOsKeystoreError::Keychain(error)),
        }
    }
}

impl From<MacOsKeychainFailure> for MacOsKeystoreError {
    fn from(value: MacOsKeychainFailure) -> Self {
        match value {
            MacOsKeychainFailure::NotFound => {
                Self::Keychain(SecurityFrameworkError::from_code(errSecItemNotFound))
            }
            MacOsKeychainFailure::System(error) => Self::Keychain(error),
        }
    }
}

enum MacOsKeychainFailure {
    NotFound,
    System(SecurityFrameworkError),
}

trait MacOsKeychain {
    fn load(&self, service: &str, account: &str) -> Result<Vec<u8>, MacOsKeychainFailure>;
    fn store(
        &mut self,
        service: &str,
        account: &str,
        secret: &[u8],
    ) -> Result<(), MacOsKeychainFailure>;
    fn delete(&mut self, service: &str, account: &str) -> Result<(), MacOsKeychainFailure>;
}

struct SystemKeychain;

impl MacOsKeychain for SystemKeychain {
    fn load(&self, service: &str, account: &str) -> Result<Vec<u8>, MacOsKeychainFailure> {
        let mut options = PasswordOptions::new_generic_password(service, account);
        options.set_access_synchronized(Some(false));
        generic_password(options).map_err(system_failure)
    }

    fn store(
        &mut self,
        service: &str,
        account: &str,
        secret: &[u8],
    ) -> Result<(), MacOsKeychainFailure> {
        let mut options = PasswordOptions::new_generic_password(service, account);
        options.set_access_synchronized(Some(false));
        set_generic_password_options(secret, options).map_err(system_failure)
    }

    fn delete(&mut self, service: &str, account: &str) -> Result<(), MacOsKeychainFailure> {
        let mut options = PasswordOptions::new_generic_password(service, account);
        options.set_access_synchronized(Some(false));
        delete_generic_password_options(options).map_err(system_failure)
    }
}

fn system_failure(error: SecurityFrameworkError) -> MacOsKeychainFailure {
    if error.code() == errSecItemNotFound {
        MacOsKeychainFailure::NotFound
    } else {
        MacOsKeychainFailure::System(error)
    }
}

#[cfg(test)]
mod tests {
    use super::{
        KeychainKeystore, MACOS_KEYCHAIN_SERVICE, MacOsKeychain, MacOsKeychainFailure,
        MacOsKeystoreError,
    };
    use crate::{KeystoreEntryName, KeystoreSecret, KeystoreSecretError};

    #[derive(Default)]
    struct InMemoryKeychain {
        stored: Option<(String, String, Vec<u8>)>,
    }

    impl MacOsKeychain for InMemoryKeychain {
        fn load(&self, service: &str, account: &str) -> Result<Vec<u8>, MacOsKeychainFailure> {
            self.stored
                .as_ref()
                .filter(|(stored_service, stored_account, _)| {
                    stored_service == service && stored_account == account
                })
                .map(|(_, _, secret)| secret.clone())
                .ok_or(MacOsKeychainFailure::NotFound)
        }

        fn store(
            &mut self,
            service: &str,
            account: &str,
            secret: &[u8],
        ) -> Result<(), MacOsKeychainFailure> {
            self.stored = Some((service.to_owned(), account.to_owned(), secret.to_vec()));
            Ok(())
        }

        fn delete(&mut self, service: &str, account: &str) -> Result<(), MacOsKeychainFailure> {
            if self
                .stored
                .as_ref()
                .is_some_and(|(stored_service, stored_account, _)| {
                    stored_service == service && stored_account == account
                })
            {
                self.stored = None;
                Ok(())
            } else {
                Err(MacOsKeychainFailure::NotFound)
            }
        }
    }

    struct FailingKeychain;

    impl MacOsKeychain for FailingKeychain {
        fn load(&self, _: &str, _: &str) -> Result<Vec<u8>, MacOsKeychainFailure> {
            Ok(Vec::new())
        }

        fn store(&mut self, _: &str, _: &str, _: &[u8]) -> Result<(), MacOsKeychainFailure> {
            Err(MacOsKeychainFailure::System(
                security_framework::base::Error::from_code(-1),
            ))
        }

        fn delete(&mut self, _: &str, _: &str) -> Result<(), MacOsKeychainFailure> {
            Err(MacOsKeychainFailure::System(
                security_framework::base::Error::from_code(-1),
            ))
        }
    }

    #[test]
    fn stores_loads_and_deletes_local_keychain_entries() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1, 2, 3]).unwrap();
        let mut keystore = KeychainKeystore::new(InMemoryKeychain::default());

        keystore.store(&entry, &secret).unwrap();
        assert_eq!(
            keystore
                .keychain
                .stored
                .as_ref()
                .map(|(service, _, _)| service.as_str()),
            Some(MACOS_KEYCHAIN_SERVICE)
        );
        assert_eq!(
            keystore.load(&entry).unwrap().unwrap().as_bytes(),
            [1, 2, 3]
        );
        keystore.delete(&entry).unwrap();
        assert!(keystore.load(&entry).unwrap().is_none());
    }

    #[test]
    fn fails_closed_for_invalid_keychain_secrets_and_system_errors() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1]).unwrap();
        let mut keystore = KeychainKeystore::new(FailingKeychain);

        assert!(matches!(
            keystore.load(&entry),
            Err(MacOsKeystoreError::InvalidSecret(
                KeystoreSecretError::Empty
            ))
        ));
        assert!(matches!(
            keystore.store(&entry, &secret),
            Err(MacOsKeystoreError::Keychain(error)) if error.code() == -1
        ));
        assert!(matches!(
            keystore.delete(&entry),
            Err(MacOsKeystoreError::Keychain(error)) if error.code() == -1
        ));
    }
}
