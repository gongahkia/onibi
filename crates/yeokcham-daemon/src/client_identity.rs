use yeokcham_core::{
    IdentityKeyError, IdentityKeypair, IdentityPublicKey, IdentitySerializationError,
    KeystoreEntryName, KeystoreSecret, KeystoreSecretError, OsKeystore,
};
use yeokcham_protocol::{
    IdentityExportError, IdentityExportPassphrase, export_identity, import_identity,
};

pub const CLIENT_IDENTITY_KEY_ENTRY: &str = "yeokcham_client_identity_v1";

pub struct ClientIdentity {
    keypair: IdentityKeypair,
}

impl ClientIdentity {
    pub fn create<K: OsKeystore>(keystore: &mut K) -> Result<Self, ClientIdentityError> {
        let entry = identity_entry()?;
        if keystore
            .load(&entry)
            .map_err(|_| ClientIdentityError::Keystore)?
            .is_some()
        {
            return Err(ClientIdentityError::AlreadyInitialized);
        }
        let keypair = IdentityKeypair::generate().map_err(ClientIdentityError::Generation)?;
        let secret = KeystoreSecret::new(keypair.serialize().to_vec())
            .map_err(ClientIdentityError::KeystoreSecret)?;
        keystore
            .store(&entry, &secret)
            .map_err(|_| ClientIdentityError::Keystore)?;
        Ok(Self { keypair })
    }

    pub fn load<K: OsKeystore>(keystore: &K) -> Result<Self, ClientIdentityError> {
        let entry = identity_entry()?;
        let secret = keystore
            .load(&entry)
            .map_err(|_| ClientIdentityError::Keystore)?
            .ok_or(ClientIdentityError::NotInitialized)?;
        let keypair = IdentityKeypair::deserialize(secret.as_bytes())
            .map_err(ClientIdentityError::InvalidStoredIdentity)?;
        Ok(Self { keypair })
    }

    pub fn create_or_load<K: OsKeystore>(
        keystore: &mut K,
    ) -> Result<(Self, ClientIdentityInitialization), ClientIdentityError> {
        match Self::load(keystore) {
            Ok(identity) => Ok((identity, ClientIdentityInitialization::Loaded)),
            Err(ClientIdentityError::NotInitialized) => {
                let identity = Self::create(keystore)?;
                Ok((identity, ClientIdentityInitialization::Created))
            }
            Err(error) => Err(error),
        }
    }

    pub fn export_recovery(
        &self,
        passphrase: &IdentityExportPassphrase,
    ) -> Result<Vec<u8>, ClientIdentityError> {
        export_identity(&self.keypair, passphrase).map_err(ClientIdentityError::RecoveryExport)
    }

    pub fn import_recovery<K: OsKeystore>(
        keystore: &mut K,
        encoded: &[u8],
        passphrase: &IdentityExportPassphrase,
    ) -> Result<Self, ClientIdentityError> {
        let entry = identity_entry()?;
        if keystore
            .load(&entry)
            .map_err(|_| ClientIdentityError::Keystore)?
            .is_some()
        {
            return Err(ClientIdentityError::AlreadyInitialized);
        }
        let keypair =
            import_identity(encoded, passphrase).map_err(ClientIdentityError::RecoveryImport)?;
        let secret = KeystoreSecret::new(keypair.serialize().to_vec())
            .map_err(ClientIdentityError::KeystoreSecret)?;
        keystore
            .store(&entry, &secret)
            .map_err(|_| ClientIdentityError::Keystore)?;
        Ok(Self { keypair })
    }

    #[must_use]
    pub fn public_key(&self) -> IdentityPublicKey {
        self.keypair.public_key()
    }

    #[must_use]
    pub const fn keypair(&self) -> &IdentityKeypair {
        &self.keypair
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ClientIdentityInitialization {
    Created,
    Loaded,
}

#[derive(Debug, thiserror::Error)]
pub enum ClientIdentityError {
    #[error("client identity key entry is invalid")]
    InvalidKeyEntry,
    #[error("client identity already exists")]
    AlreadyInitialized,
    #[error("client identity does not exist")]
    NotInitialized,
    #[error("client identity generation failed")]
    Generation(#[source] IdentityKeyError),
    #[error("client identity stored material is invalid")]
    InvalidStoredIdentity(#[source] IdentitySerializationError),
    #[error("client identity recovery export failed")]
    RecoveryExport(#[source] IdentityExportError),
    #[error("client identity recovery import failed")]
    RecoveryImport(#[source] IdentityExportError),
    #[error("client identity secret cannot be stored")]
    KeystoreSecret(#[source] KeystoreSecretError),
    #[error("client identity keystore operation failed")]
    Keystore,
}

fn identity_entry() -> Result<KeystoreEntryName, ClientIdentityError> {
    KeystoreEntryName::new(CLIENT_IDENTITY_KEY_ENTRY.to_owned())
        .map_err(|_| ClientIdentityError::InvalidKeyEntry)
}
