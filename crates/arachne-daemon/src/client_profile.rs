use std::{
    fs::{self, OpenOptions},
    io::Write,
};

use arachne_core::{KeystoreEntryName, OsKeystore};
use arachne_protocol::IdentityExportPassphrase;
use getrandom::{SysRng, rand_core::TryRng};

use crate::{ClientIdentity, ClientIdentityError, ClientStateDirectory};

pub const CLIENT_PROFILE_FILE: &str = "arachne-profile-v1.bin";
const PROFILE_VERSION: u8 = 1;
const PROFILE_ID_BYTES: usize = 16;

/// An opaque, immutable namespace for one complete local client profile.
///
/// The identifier is intentionally persisted under the state directory instead
/// of being derived from its path. Moving a profile retains its Keychain names.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ClientProfileId([u8; PROFILE_ID_BYTES]);

impl ClientProfileId {
    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; PROFILE_ID_BYTES] {
        &self.0
    }

    pub fn keystore_entry(&self, purpose: &str) -> Result<KeystoreEntryName, ClientProfileError> {
        if purpose.is_empty()
            || !purpose
                .bytes()
                .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'_')
        {
            return Err(ClientProfileError::InvalidPurpose);
        }
        KeystoreEntryName::new(format!(
            "arachne_profile_v1_{}_{}",
            hexadecimal(&self.0),
            purpose
        ))
        .map_err(|_| ClientProfileError::InvalidKeyEntry)
    }
}

pub struct ClientProfile {
    id: ClientProfileId,
}

impl ClientProfile {
    pub fn open_or_create(layout: &ClientStateDirectory) -> Result<Self, ClientProfileError> {
        fs::create_dir_all(layout.root()).map_err(|_| ClientProfileError::Io)?;
        let path = layout.root().join(CLIENT_PROFILE_FILE);
        match fs::read(&path) {
            Ok(encoded) => Ok(Self {
                id: decode(&encoded)?,
            }),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                let mut id = [0; PROFILE_ID_BYTES];
                SysRng
                    .try_fill_bytes(&mut id)
                    .map_err(|_| ClientProfileError::Randomness)?;
                let encoded = encode(ClientProfileId(id));
                let mut file = match OpenOptions::new().write(true).create_new(true).open(&path) {
                    Ok(file) => file,
                    Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                        return fs::read(&path)
                            .map_err(|_| ClientProfileError::Io)
                            .and_then(|existing| {
                                Ok(Self {
                                    id: decode(&existing)?,
                                })
                            });
                    }
                    Err(_) => return Err(ClientProfileError::Io),
                };
                file.write_all(&encoded)
                    .map_err(|_| ClientProfileError::Io)?;
                file.sync_all().map_err(|_| ClientProfileError::Io)?;
                Ok(Self {
                    id: ClientProfileId(id),
                })
            }
            Err(_) => Err(ClientProfileError::Io),
        }
    }

    #[must_use]
    pub const fn id(&self) -> ClientProfileId {
        self.id
    }

    pub fn create_identity<K: OsKeystore>(
        &self,
        keystore: &mut K,
    ) -> Result<ClientIdentity, ClientIdentityError> {
        ClientIdentity::create_with_entry(keystore, self.identity_entry()?)
    }

    pub fn load_identity<K: OsKeystore>(
        &self,
        keystore: &K,
    ) -> Result<ClientIdentity, ClientIdentityError> {
        ClientIdentity::load_with_entry(keystore, self.identity_entry()?)
    }

    pub fn create_or_load_identity<K: OsKeystore>(
        &self,
        keystore: &mut K,
    ) -> Result<(ClientIdentity, crate::ClientIdentityInitialization), ClientIdentityError> {
        ClientIdentity::create_or_load_with_entry(keystore, self.identity_entry()?)
    }

    pub fn import_identity_recovery<K: OsKeystore>(
        &self,
        keystore: &mut K,
        encoded: &[u8],
        passphrase: &IdentityExportPassphrase,
    ) -> Result<ClientIdentity, ClientIdentityError> {
        ClientIdentity::import_recovery_with_entry(
            keystore,
            self.identity_entry()?,
            encoded,
            passphrase,
        )
    }

    fn identity_entry(&self) -> Result<KeystoreEntryName, ClientIdentityError> {
        self.id
            .keystore_entry("identity")
            .map_err(|_| ClientIdentityError::InvalidKeyEntry)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ClientProfileError {
    #[error("client profile I/O failed")]
    Io,
    #[error("client profile randomness failed")]
    Randomness,
    #[error("client profile file is invalid")]
    InvalidProfile,
    #[error("client profile Keychain purpose is invalid")]
    InvalidPurpose,
    #[error("client profile Keychain entry is invalid")]
    InvalidKeyEntry,
}

fn encode(id: ClientProfileId) -> [u8; PROFILE_ID_BYTES + 1] {
    let mut encoded = [0; PROFILE_ID_BYTES + 1];
    encoded[0] = PROFILE_VERSION;
    encoded[1..].copy_from_slice(id.as_bytes());
    encoded
}

fn decode(encoded: &[u8]) -> Result<ClientProfileId, ClientProfileError> {
    if encoded.len() != PROFILE_ID_BYTES + 1 || encoded.first() != Some(&PROFILE_VERSION) {
        return Err(ClientProfileError::InvalidProfile);
    }
    let id: [u8; PROFILE_ID_BYTES] = encoded[1..]
        .try_into()
        .map_err(|_| ClientProfileError::InvalidProfile)?;
    if id.iter().all(|byte| *byte == 0) {
        return Err(ClientProfileError::InvalidProfile);
    }
    Ok(ClientProfileId(id))
}

fn hexadecimal(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(char::from(HEX[usize::from(*byte >> 4)]));
        encoded.push(char::from(HEX[usize::from(*byte & 0x0f)]));
    }
    encoded
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs};

    use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};

    use super::{ClientProfile, ClientProfileError};
    use crate::ClientStateDirectory;

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

    #[test]
    fn independent_state_directories_use_independent_profile_namespaces() {
        let root =
            std::env::temp_dir().join(format!("arachne-profile-test-{}", std::process::id()));
        let first = ClientStateDirectory::new(root.join("first")).unwrap();
        let second = ClientStateDirectory::new(root.join("second")).unwrap();
        let first = ClientProfile::open_or_create(&first).unwrap();
        let second = ClientProfile::open_or_create(&second).unwrap();
        assert_ne!(first.id(), second.id());
        assert_ne!(
            first.id().keystore_entry("identity").unwrap(),
            second.id().keystore_entry("identity").unwrap()
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn profiles_isolate_identities_in_one_keystore() {
        let root = std::env::temp_dir().join(format!(
            "arachne-profile-identities-test-{}",
            std::process::id()
        ));
        let first =
            ClientProfile::open_or_create(&ClientStateDirectory::new(root.join("first")).unwrap())
                .unwrap();
        let second =
            ClientProfile::open_or_create(&ClientStateDirectory::new(root.join("second")).unwrap())
                .unwrap();
        let mut keystore = MemoryKeystore::default();
        let first_identity = first.create_or_load_identity(&mut keystore).unwrap().0;
        let second_identity = second.create_or_load_identity(&mut keystore).unwrap().0;
        assert_ne!(first_identity.public_key(), second_identity.public_key());
        assert_eq!(
            first.load_identity(&keystore).unwrap().public_key(),
            first_identity.public_key()
        );
        assert_eq!(
            second.load_identity(&keystore).unwrap().public_key(),
            second_identity.public_key()
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn rejects_invalid_profile_file() {
        let root = std::env::temp_dir().join(format!(
            "arachne-profile-invalid-test-{}",
            std::process::id()
        ));
        let layout = ClientStateDirectory::new(&root).unwrap();
        fs::create_dir_all(&root).unwrap();
        fs::write(root.join(super::CLIENT_PROFILE_FILE), [1, 0]).unwrap();
        assert!(matches!(
            ClientProfile::open_or_create(&layout),
            Err(ClientProfileError::InvalidProfile)
        ));
        let _ = fs::remove_dir_all(root);
    }
}
