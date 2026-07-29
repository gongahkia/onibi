use crate::{KeystoreEntryName, KeystoreSecret, KeystoreSecretError};

#[cfg(target_os = "linux")]
use crate::OsKeystore;

pub const LINUX_SECRET_SERVICE: &str = "com.github.gongahkia.arachne";

#[cfg(target_os = "linux")]
use keyring_core::api::CredentialStoreApi;

#[cfg(target_os = "linux")]
use zbus_secret_service_keyring_store::Store;

#[cfg(target_os = "linux")]
pub struct LinuxKeystore {
    inner: SecretServiceKeystore<SystemSecretService>,
}

#[cfg(target_os = "linux")]
impl LinuxKeystore {
    pub fn new() -> Result<Self, LinuxKeystoreError> {
        Ok(Self {
            inner: SecretServiceKeystore::new(SystemSecretService::new()?),
        })
    }
}

#[cfg(target_os = "linux")]
impl OsKeystore for LinuxKeystore {
    type Error = LinuxKeystoreError;

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
pub enum LinuxKeystoreError {
    #[error("Linux Secret Service operation failed: {0}")]
    SecretService(String),
    #[error("Linux Secret Service returned an invalid secret: {0}")]
    InvalidSecret(#[from] KeystoreSecretError),
}

struct SecretServiceKeystore<S> {
    secret_service: S,
}

impl<S> SecretServiceKeystore<S>
where
    S: LinuxSecretService,
{
    const fn new(secret_service: S) -> Self {
        Self { secret_service }
    }

    fn load(
        &self,
        entry: &KeystoreEntryName,
    ) -> Result<Option<KeystoreSecret>, LinuxKeystoreError> {
        match self
            .secret_service
            .load(LINUX_SECRET_SERVICE, entry.as_str())
        {
            Ok(secret) => Ok(Some(KeystoreSecret::new(secret)?)),
            Err(LinuxSecretServiceFailure::NotFound) => Ok(None),
            Err(failure) => Err(secret_service_error(failure)),
        }
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), LinuxKeystoreError> {
        self.secret_service
            .store(LINUX_SECRET_SERVICE, entry.as_str(), secret.as_bytes())
            .map_err(secret_service_error)
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), LinuxKeystoreError> {
        match self
            .secret_service
            .delete(LINUX_SECRET_SERVICE, entry.as_str())
        {
            Ok(()) | Err(LinuxSecretServiceFailure::NotFound) => Ok(()),
            Err(failure) => Err(secret_service_error(failure)),
        }
    }
}

fn secret_service_error(failure: LinuxSecretServiceFailure) -> LinuxKeystoreError {
    match failure {
        LinuxSecretServiceFailure::NotFound => {
            LinuxKeystoreError::SecretService("credential was not found".to_owned())
        }
        LinuxSecretServiceFailure::System(message) => LinuxKeystoreError::SecretService(message),
    }
}

enum LinuxSecretServiceFailure {
    NotFound,
    System(String),
}

trait LinuxSecretService {
    fn load(&self, service: &str, account: &str) -> Result<Vec<u8>, LinuxSecretServiceFailure>;
    fn store(
        &mut self,
        service: &str,
        account: &str,
        secret: &[u8],
    ) -> Result<(), LinuxSecretServiceFailure>;
    fn delete(&mut self, service: &str, account: &str) -> Result<(), LinuxSecretServiceFailure>;
}

#[cfg(target_os = "linux")]
struct SystemSecretService {
    store: std::sync::Arc<Store>,
}

#[cfg(target_os = "linux")]
impl SystemSecretService {
    fn new() -> Result<Self, LinuxKeystoreError> {
        Store::new()
            .map(|store| Self { store })
            .map_err(system_failure)
            .map_err(secret_service_error)
    }

    fn entry(
        &self,
        service: &str,
        account: &str,
    ) -> Result<keyring_core::Entry, LinuxSecretServiceFailure> {
        self.store
            .build(service, account, None)
            .map_err(system_failure)
    }
}

#[cfg(target_os = "linux")]
impl LinuxSecretService for SystemSecretService {
    fn load(&self, service: &str, account: &str) -> Result<Vec<u8>, LinuxSecretServiceFailure> {
        self.entry(service, account)?
            .get_secret()
            .map_err(system_failure)
    }

    fn store(
        &mut self,
        service: &str,
        account: &str,
        secret: &[u8],
    ) -> Result<(), LinuxSecretServiceFailure> {
        self.entry(service, account)?
            .set_secret(secret)
            .map_err(system_failure)
    }

    fn delete(&mut self, service: &str, account: &str) -> Result<(), LinuxSecretServiceFailure> {
        self.entry(service, account)?
            .delete_credential()
            .map_err(system_failure)
    }
}

#[cfg(target_os = "linux")]
fn system_failure(error: keyring_core::Error) -> LinuxSecretServiceFailure {
    match error {
        keyring_core::Error::NoEntry => LinuxSecretServiceFailure::NotFound,
        error => LinuxSecretServiceFailure::System(error.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::{
        LINUX_SECRET_SERVICE, LinuxKeystoreError, LinuxSecretService, LinuxSecretServiceFailure,
        SecretServiceKeystore,
    };
    use crate::{KeystoreEntryName, KeystoreSecret, KeystoreSecretError};

    #[derive(Default)]
    struct InMemorySecretService {
        stored: Option<(String, String, Vec<u8>)>,
    }

    impl LinuxSecretService for InMemorySecretService {
        fn load(&self, service: &str, account: &str) -> Result<Vec<u8>, LinuxSecretServiceFailure> {
            self.stored
                .as_ref()
                .filter(|(stored_service, stored_account, _)| {
                    stored_service == service && stored_account == account
                })
                .map(|(_, _, secret)| secret.clone())
                .ok_or(LinuxSecretServiceFailure::NotFound)
        }

        fn store(
            &mut self,
            service: &str,
            account: &str,
            secret: &[u8],
        ) -> Result<(), LinuxSecretServiceFailure> {
            self.stored = Some((service.to_owned(), account.to_owned(), secret.to_vec()));
            Ok(())
        }

        fn delete(
            &mut self,
            service: &str,
            account: &str,
        ) -> Result<(), LinuxSecretServiceFailure> {
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
                Err(LinuxSecretServiceFailure::NotFound)
            }
        }
    }

    struct FailingSecretService;

    impl LinuxSecretService for FailingSecretService {
        fn load(&self, _: &str, _: &str) -> Result<Vec<u8>, LinuxSecretServiceFailure> {
            Ok(Vec::new())
        }

        fn store(&mut self, _: &str, _: &str, _: &[u8]) -> Result<(), LinuxSecretServiceFailure> {
            Err(LinuxSecretServiceFailure::System(
                "access denied".to_owned(),
            ))
        }

        fn delete(&mut self, _: &str, _: &str) -> Result<(), LinuxSecretServiceFailure> {
            Err(LinuxSecretServiceFailure::System(
                "access denied".to_owned(),
            ))
        }
    }

    #[test]
    fn stores_loads_and_deletes_namespaced_secret_service_entries() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1, 2, 3]).unwrap();
        let mut keystore = SecretServiceKeystore::new(InMemorySecretService::default());

        keystore.store(&entry, &secret).unwrap();
        assert_eq!(
            keystore
                .secret_service
                .stored
                .as_ref()
                .map(|(service, _, _)| service.as_str()),
            Some(LINUX_SECRET_SERVICE)
        );
        assert_eq!(
            keystore
                .secret_service
                .stored
                .as_ref()
                .map(|(_, account, _)| account.as_str()),
            Some("identity_primary")
        );
        assert_eq!(
            keystore.load(&entry).unwrap().unwrap().as_bytes(),
            [1, 2, 3]
        );
        keystore.delete(&entry).unwrap();
        keystore.delete(&entry).unwrap();
        assert!(keystore.load(&entry).unwrap().is_none());
    }

    #[test]
    fn fails_closed_for_invalid_secrets_and_secret_service_errors() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let secret = KeystoreSecret::new(vec![1]).unwrap();
        let mut keystore = SecretServiceKeystore::new(FailingSecretService);

        assert!(matches!(
            keystore.load(&entry),
            Err(LinuxKeystoreError::InvalidSecret(
                KeystoreSecretError::Empty
            ))
        ));
        assert_eq!(
            keystore.store(&entry, &secret).unwrap_err(),
            LinuxKeystoreError::SecretService("access denied".to_owned())
        );
        assert_eq!(
            keystore.delete(&entry).unwrap_err(),
            LinuxKeystoreError::SecretService("access denied".to_owned())
        );
    }
}
