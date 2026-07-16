use yeokcham_core::{IdentityPublicKey, OsKeystore};
use yeokcham_daemon::{ClientIdentity, ClientIdentityError, ClientIdentityInitialization};

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
            | ClientIdentityError::KeystoreSecret(_)
            | ClientIdentityError::Keystore => Self::Keystore,
        }
    }
}
