use arachne_core::{IdentityPublicKey, OsKeystore};
use arachne_daemon::{ClientIdentity, ClientIdentityError, ClientIdentityInitialization};

use crate::{SdkRecoveryArchive, SdkRecoveryError, SdkRecoveryPassphrase};

pub struct SdkIdentityManager<K> {
    keystore: K,
}

impl<K> SdkIdentityManager<K> {
    #[must_use]
    pub const fn new(keystore: K) -> Self {
        Self { keystore }
    }

    #[must_use]
    pub fn into_inner(self) -> K {
        self.keystore
    }

    pub(crate) fn keystore_mut(&mut self) -> &mut K {
        &mut self.keystore
    }
}

impl<K: OsKeystore> SdkIdentityManager<K> {
    pub fn create(&mut self) -> Result<SdkIdentity, SdkIdentityError> {
        ClientIdentity::create(&mut self.keystore)
            .map(|identity| {
                SdkIdentity::new(identity.public_key(), SdkIdentityInitialization::Created)
            })
            .map_err(SdkIdentityError::from)
    }

    pub fn load(&self) -> Result<SdkIdentity, SdkIdentityError> {
        ClientIdentity::load(&self.keystore)
            .map(|identity| {
                SdkIdentity::new(identity.public_key(), SdkIdentityInitialization::Loaded)
            })
            .map_err(SdkIdentityError::from)
    }

    pub fn create_or_load(&mut self) -> Result<SdkIdentity, SdkIdentityError> {
        ClientIdentity::create_or_load(&mut self.keystore)
            .map(|(identity, initialization)| {
                let initialization = match initialization {
                    ClientIdentityInitialization::Created => SdkIdentityInitialization::Created,
                    ClientIdentityInitialization::Loaded => SdkIdentityInitialization::Loaded,
                };
                SdkIdentity::new(identity.public_key(), initialization)
            })
            .map_err(SdkIdentityError::from)
    }

    pub fn export_recovery(
        &self,
        passphrase: &SdkRecoveryPassphrase,
    ) -> Result<SdkRecoveryArchive, SdkRecoveryError> {
        ClientIdentity::load(&self.keystore)
            .map_err(map_recovery_error)
            .and_then(|identity| {
                identity
                    .export_recovery(passphrase.as_inner())
                    .map(SdkRecoveryArchive::from_trusted_bytes)
                    .map_err(map_recovery_error)
            })
    }

    pub fn import_recovery(
        &mut self,
        archive: &SdkRecoveryArchive,
        passphrase: &SdkRecoveryPassphrase,
    ) -> Result<SdkIdentity, SdkRecoveryError> {
        ClientIdentity::import_recovery(
            &mut self.keystore,
            archive.as_bytes(),
            passphrase.as_inner(),
        )
        .map(|identity| {
            SdkIdentity::new(identity.public_key(), SdkIdentityInitialization::Recovered)
        })
        .map_err(map_recovery_error)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkIdentity {
    public_key: IdentityPublicKey,
    initialization: SdkIdentityInitialization,
}

impl SdkIdentity {
    const fn new(public_key: IdentityPublicKey, initialization: SdkIdentityInitialization) -> Self {
        Self {
            public_key,
            initialization,
        }
    }

    #[must_use]
    pub const fn public_key(self) -> IdentityPublicKey {
        self.public_key
    }

    #[must_use]
    pub const fn initialization(self) -> SdkIdentityInitialization {
        self.initialization
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkIdentityInitialization {
    Created,
    Loaded,
    Recovered,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkIdentityError {
    #[error("SDK identity already exists")]
    AlreadyInitialized,
    #[error("SDK identity does not exist")]
    NotInitialized,
    #[error("SDK identity generation failed")]
    Generation,
    #[error("SDK identity stored material is invalid")]
    InvalidStoredIdentity,
    #[error("SDK identity keystore operation failed")]
    Keystore,
}

impl From<ClientIdentityError> for SdkIdentityError {
    fn from(error: ClientIdentityError) -> Self {
        match error {
            ClientIdentityError::AlreadyInitialized => Self::AlreadyInitialized,
            ClientIdentityError::NotInitialized => Self::NotInitialized,
            ClientIdentityError::Generation(_) => Self::Generation,
            ClientIdentityError::InvalidStoredIdentity(_) => Self::InvalidStoredIdentity,
            ClientIdentityError::InvalidKeyEntry
            | ClientIdentityError::RecoveryExport(_)
            | ClientIdentityError::RecoveryImport(_)
            | ClientIdentityError::KeystoreSecret(_)
            | ClientIdentityError::Keystore => Self::Keystore,
        }
    }
}

fn map_recovery_error(error: ClientIdentityError) -> crate::SdkRecoveryError {
    match error {
        ClientIdentityError::RecoveryImport(_) => crate::SdkRecoveryError::InvalidArchive,
        ClientIdentityError::RecoveryExport(_) => crate::SdkRecoveryError::Operation,
        error => crate::SdkRecoveryError::Identity(error.into()),
    }
}
