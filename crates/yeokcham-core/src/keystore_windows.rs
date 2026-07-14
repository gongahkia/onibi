use crate::{KeystoreEntryName, KeystoreSecret, KeystoreSecretError};

#[cfg(target_os = "windows")]
use crate::OsKeystore;

pub const WINDOWS_CREDENTIAL_SERVICE: &str = "com.github.gongahkia.yeokcham";
pub const MAX_WINDOWS_CREDENTIAL_SECRET_BYTES: usize = 5 * 512;

#[cfg(target_os = "windows")]
use std::collections::HashMap;

#[cfg(target_os = "windows")]
use keyring_core::api::CredentialStoreApi;

#[cfg(target_os = "windows")]
use windows_native_keyring_store::Store;

#[cfg(target_os = "windows")]
pub struct WindowsKeystore {
    inner: CredentialKeystore<SystemCredentialManager>,
}

#[cfg(target_os = "windows")]
impl WindowsKeystore {
    pub fn new() -> Result<Self, WindowsKeystoreError> {
        Ok(Self {
            inner: CredentialKeystore::new(SystemCredentialManager::new()?),
        })
    }
}

#[cfg(target_os = "windows")]
impl OsKeystore for WindowsKeystore {
    type Error = WindowsKeystoreError;

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

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WindowsKeystoreError {
    #[error("Windows Credential Manager operation failed: {0}")]
    CredentialManager(String),
    #[error("Windows Credential Manager secret exceeds its byte limit")]
    SecretTooLong,
    #[error("Windows Credential Manager returned an invalid secret: {0}")]
    InvalidSecret(#[from] KeystoreSecretError),
}

struct CredentialKeystore<C> {
    credential_manager: C,
}

impl<C> CredentialKeystore<C>
where
    C: WindowsCredentialManager,
{
    const fn new(credential_manager: C) -> Self {
        Self { credential_manager }
    }

    fn load(
        &self,
        entry: &KeystoreEntryName,
    ) -> Result<Option<KeystoreSecret>, WindowsKeystoreError> {
        match self.credential_manager.load(&credential_target(entry)) {
            Ok(secret) => Ok(Some(KeystoreSecret::new(secret)?)),
            Err(WindowsCredentialFailure::NotFound) => Ok(None),
            Err(failure) => Err(credential_manager_error(failure)),
        }
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), WindowsKeystoreError> {
        if secret.as_bytes().len() > MAX_WINDOWS_CREDENTIAL_SECRET_BYTES {
            return Err(WindowsKeystoreError::SecretTooLong);
        }
        self.credential_manager
            .store(
                &credential_target(entry),
                secret.as_bytes(),
                CredentialPersistence::Local,
            )
            .map_err(credential_manager_error)
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), WindowsKeystoreError> {
        match self.credential_manager.delete(&credential_target(entry)) {
            Ok(()) | Err(WindowsCredentialFailure::NotFound) => Ok(()),
            Err(failure) => Err(credential_manager_error(failure)),
        }
    }
}

fn credential_target(entry: &KeystoreEntryName) -> String {
    format!("{WINDOWS_CREDENTIAL_SERVICE}/{}", entry.as_str())
}

fn credential_manager_error(failure: WindowsCredentialFailure) -> WindowsKeystoreError {
    match failure {
        WindowsCredentialFailure::NotFound => {
            WindowsKeystoreError::CredentialManager("credential was not found".to_owned())
        }
        WindowsCredentialFailure::System(message) => {
            WindowsKeystoreError::CredentialManager(message)
        }
    }
}

enum WindowsCredentialFailure {
    NotFound,
    System(String),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum CredentialPersistence {
    Local,
}

trait WindowsCredentialManager {
    fn load(&self, target: &str) -> Result<Vec<u8>, WindowsCredentialFailure>;
    fn store(
        &mut self,
        target: &str,
        secret: &[u8],
        persistence: CredentialPersistence,
    ) -> Result<(), WindowsCredentialFailure>;
    fn delete(&mut self, target: &str) -> Result<(), WindowsCredentialFailure>;
}

#[cfg(target_os = "windows")]
struct SystemCredentialManager {
    store: std::sync::Arc<Store>,
}

#[cfg(target_os = "windows")]
impl SystemCredentialManager {
    fn new() -> Result<Self, WindowsKeystoreError> {
        Store::new()
            .map(|store| Self { store })
            .map_err(system_failure)
            .map_err(credential_manager_error)
    }

    fn entry(
        &self,
        target: &str,
        persistence: Option<CredentialPersistence>,
    ) -> Result<keyring_core::Entry, WindowsCredentialFailure> {
        let mut modifiers = HashMap::from([("target", target)]);
        if let Some(persistence) = persistence {
            modifiers.insert(
                "persistence",
                match persistence {
                    CredentialPersistence::Local => "Local",
                },
            );
        }
        self.store
            .build(WINDOWS_CREDENTIAL_SERVICE, target, Some(&modifiers))
            .map_err(system_failure)
    }
}

#[cfg(target_os = "windows")]
impl WindowsCredentialManager for SystemCredentialManager {
    fn load(&self, target: &str) -> Result<Vec<u8>, WindowsCredentialFailure> {
        self.entry(target, None)?
            .get_secret()
            .map_err(system_failure)
    }

    fn store(
        &mut self,
        target: &str,
        secret: &[u8],
        persistence: CredentialPersistence,
    ) -> Result<(), WindowsCredentialFailure> {
        self.entry(target, Some(persistence))?
            .set_secret(secret)
            .map_err(system_failure)
    }

    fn delete(&mut self, target: &str) -> Result<(), WindowsCredentialFailure> {
        self.entry(target, None)?
            .delete_credential()
            .map_err(system_failure)
    }
}

#[cfg(target_os = "windows")]
fn system_failure(error: keyring_core::Error) -> WindowsCredentialFailure {
    match error {
        keyring_core::Error::NoEntry => WindowsCredentialFailure::NotFound,
        error => WindowsCredentialFailure::System(error.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::{
        CredentialKeystore, CredentialPersistence, MAX_WINDOWS_CREDENTIAL_SECRET_BYTES,
        WINDOWS_CREDENTIAL_SERVICE, WindowsCredentialFailure, WindowsCredentialManager,
        WindowsKeystoreError,
    };
    use crate::{KeystoreEntryName, KeystoreSecret, KeystoreSecretError};

    #[derive(Default)]
    struct InMemoryCredentialManager {
        stored: Option<(String, Vec<u8>, CredentialPersistence)>,
    }

    impl WindowsCredentialManager for InMemoryCredentialManager {
        fn load(&self, target: &str) -> Result<Vec<u8>, WindowsCredentialFailure> {
            self.stored
                .as_ref()
                .filter(|(stored_target, _, _)| stored_target == target)
                .map(|(_, secret, _)| secret.clone())
                .ok_or(WindowsCredentialFailure::NotFound)
        }

        fn store(
            &mut self,
            target: &str,
            secret: &[u8],
            persistence: CredentialPersistence,
        ) -> Result<(), WindowsCredentialFailure> {
            self.stored = Some((target.to_owned(), secret.to_vec(), persistence));
            Ok(())
        }

        fn delete(&mut self, target: &str) -> Result<(), WindowsCredentialFailure> {
            if self
                .stored
                .as_ref()
                .is_some_and(|(stored_target, _, _)| stored_target == target)
            {
                self.stored = None;
                Ok(())
            } else {
                Err(WindowsCredentialFailure::NotFound)
            }
        }
    }

    struct FailingCredentialManager;

    impl WindowsCredentialManager for FailingCredentialManager {
        fn load(&self, _: &str) -> Result<Vec<u8>, WindowsCredentialFailure> {
            Ok(Vec::new())
        }

        fn store(
            &mut self,
            _: &str,
            _: &[u8],
            _: CredentialPersistence,
        ) -> Result<(), WindowsCredentialFailure> {
            Err(WindowsCredentialFailure::System("access denied".to_owned()))
        }

        fn delete(&mut self, _: &str) -> Result<(), WindowsCredentialFailure> {
            Err(WindowsCredentialFailure::System("access denied".to_owned()))
        }
    }

    #[test]
    fn stores_loads_and_deletes_namespaced_local_credentials() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1, 2, 3]).unwrap();
        let mut keystore = CredentialKeystore::new(InMemoryCredentialManager::default());

        keystore.store(&entry, &secret).unwrap();
        assert_eq!(
            keystore
                .credential_manager
                .stored
                .as_ref()
                .map(|(target, _, _)| target.as_str()),
            Some("com.github.gongahkia.yeokcham/identity_primary")
        );
        assert_eq!(
            keystore
                .credential_manager
                .stored
                .as_ref()
                .map(|(_, _, persistence)| *persistence),
            Some(CredentialPersistence::Local)
        );
        assert_eq!(WINDOWS_CREDENTIAL_SERVICE, "com.github.gongahkia.yeokcham");
        assert_eq!(
            keystore.load(&entry).unwrap().unwrap().as_bytes(),
            [1, 2, 3]
        );
        keystore.delete(&entry).unwrap();
        keystore.delete(&entry).unwrap();
        assert!(keystore.load(&entry).unwrap().is_none());
    }

    #[test]
    fn fails_closed_for_invalid_secrets_credential_errors_and_platform_limits() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1]).unwrap();
        let oversized =
            KeystoreSecret::new(vec![0; MAX_WINDOWS_CREDENTIAL_SECRET_BYTES + 1]).unwrap();
        let mut failing_keystore = CredentialKeystore::new(FailingCredentialManager);

        assert!(matches!(
            failing_keystore.load(&entry),
            Err(WindowsKeystoreError::InvalidSecret(
                KeystoreSecretError::Empty
            ))
        ));
        assert_eq!(
            failing_keystore.store(&entry, &oversized).unwrap_err(),
            WindowsKeystoreError::SecretTooLong
        );
        assert_eq!(
            failing_keystore.store(&entry, &secret).unwrap_err(),
            WindowsKeystoreError::CredentialManager("access denied".to_owned())
        );
        assert_eq!(
            failing_keystore.delete(&entry).unwrap_err(),
            WindowsKeystoreError::CredentialManager("access denied".to_owned())
        );
    }
}
