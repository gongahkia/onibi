use std::{fmt, path::Path};

use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use rusqlite::{Connection, OptionalExtension, params};
use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_protocol::CryptoDomain;
use zeroize::Zeroizing;

pub const MAX_STATE_DOCUMENT_BYTES: usize = 4 * 1024 * 1024;
const STATE_FORMAT_VERSION: u8 = 1;
const DATABASE_ID_BYTES: usize = 16;
const DATABASE_KEY_BYTES: usize = 32;
const NONCE_BYTES: usize = 24;
const KEY_ENTRY_PREFIX: &str = "state_db_";

#[derive(Eq, PartialEq)]
pub struct StateDocument(Zeroizing<Vec<u8>>);

impl StateDocument {
    pub fn new(value: Vec<u8>) -> Result<Self, StateDocumentError> {
        if value.is_empty() {
            return Err(StateDocumentError::Empty);
        }
        if value.len() > MAX_STATE_DOCUMENT_BYTES {
            return Err(StateDocumentError::TooLarge);
        }
        Ok(Self(Zeroizing::new(value)))
    }

    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }
}

impl fmt::Debug for StateDocument {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("StateDocument(REDACTED)")
    }
}

pub struct EncryptedStateStore {
    connection: Connection,
    database_id: [u8; DATABASE_ID_BYTES],
    key: Zeroizing<[u8; DATABASE_KEY_BYTES]>,
}

impl EncryptedStateStore {
    pub fn open<K: OsKeystore>(path: &Path, keystore: &mut K) -> Result<Self, StateStoreError> {
        let mut connection = Connection::open(path)?;
        connection.pragma_update(None, "foreign_keys", "ON")?;
        connection.pragma_update(None, "journal_mode", "WAL")?;
        connection.pragma_update(None, "synchronous", "FULL")?;
        initialize_schema(&connection)?;

        let metadata = read_metadata(&connection)?;
        let (database_id, key) = match metadata {
            Some(metadata) => (
                metadata.database_id,
                load_key(keystore, &metadata.key_entry)?,
            ),
            None => create_metadata(&mut connection, keystore)?,
        };
        Ok(Self {
            connection,
            database_id,
            key,
        })
    }

    pub fn load(&self) -> Result<Option<StateDocument>, StateStoreError> {
        let row = self
            .connection
            .query_row(
                "SELECT nonce, ciphertext FROM sealed_state WHERE id = 1",
                [],
                |row| Ok((row.get::<_, Vec<u8>>(0)?, row.get::<_, Vec<u8>>(1)?)),
            )
            .optional()?;
        let Some((nonce, ciphertext)) = row else {
            return Ok(None);
        };
        if nonce.len() != NONCE_BYTES || ciphertext.len() < 16 {
            return Err(StateStoreError::InvalidSealedState);
        }
        let cipher = XChaCha20Poly1305::new_from_slice(self.key.as_ref())
            .map_err(|_| StateStoreError::Encryption)?;
        let nonce = XNonce::try_from(nonce.as_slice()).map_err(|_| StateStoreError::Encryption)?;
        let plaintext = cipher
            .decrypt(
                &nonce,
                Payload {
                    msg: &ciphertext,
                    aad: &associated_data(&self.database_id),
                },
            )
            .map_err(|_| StateStoreError::Authentication)?;
        StateDocument::new(plaintext)
            .map(Some)
            .map_err(StateStoreError::InvalidDocument)
    }

    pub fn replace(&mut self, document: &StateDocument) -> Result<(), StateStoreError> {
        let mut nonce = [0; NONCE_BYTES];
        fill_random(&mut nonce)?;
        let cipher = XChaCha20Poly1305::new_from_slice(self.key.as_ref())
            .map_err(|_| StateStoreError::Encryption)?;
        let nonce_value =
            XNonce::try_from(nonce.as_slice()).map_err(|_| StateStoreError::Encryption)?;
        let ciphertext = cipher
            .encrypt(
                &nonce_value,
                Payload {
                    msg: document.as_bytes(),
                    aad: &associated_data(&self.database_id),
                },
            )
            .map_err(|_| StateStoreError::Encryption)?;
        let transaction = self.connection.transaction()?;
        transaction.execute(
            "INSERT INTO sealed_state(id, nonce, ciphertext) VALUES (1, ?1, ?2)
             ON CONFLICT(id) DO UPDATE SET nonce = excluded.nonce, ciphertext = excluded.ciphertext",
            params![nonce.as_slice(), ciphertext],
        )?;
        transaction.commit()?;
        Ok(())
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum StateDocumentError {
    #[error("state document must not be empty")]
    Empty,
    #[error("state document exceeds the configured limit")]
    TooLarge,
}

#[derive(Debug, thiserror::Error)]
pub enum StateStoreError {
    #[error("SQLite state-store operation failed")]
    Sqlite(#[from] rusqlite::Error),
    #[error("operating-system random source failed")]
    Randomness,
    #[error("keystore operation failed")]
    Keystore,
    #[error("state-store metadata is invalid")]
    InvalidMetadata,
    #[error("state-store key is unavailable")]
    KeyUnavailable,
    #[error("state-store key has an invalid length")]
    InvalidKey,
    #[error("state-store sealed state is invalid")]
    InvalidSealedState,
    #[error("state-store encryption failed")]
    Encryption,
    #[error("state-store authentication failed")]
    Authentication,
    #[error("state-store document is invalid: {0}")]
    InvalidDocument(StateDocumentError),
}

struct Metadata {
    database_id: [u8; DATABASE_ID_BYTES],
    key_entry: KeystoreEntryName,
}

fn initialize_schema(connection: &Connection) -> Result<(), StateStoreError> {
    connection.execute_batch(
        "CREATE TABLE IF NOT EXISTS state_metadata(
             id INTEGER PRIMARY KEY CHECK(id = 1),
             format_version INTEGER NOT NULL,
             database_id BLOB NOT NULL,
             key_entry TEXT NOT NULL
         ) STRICT;
         CREATE TABLE IF NOT EXISTS sealed_state(
             id INTEGER PRIMARY KEY CHECK(id = 1),
             nonce BLOB NOT NULL,
             ciphertext BLOB NOT NULL
         ) STRICT;",
    )?;
    Ok(())
}

fn read_metadata(connection: &Connection) -> Result<Option<Metadata>, StateStoreError> {
    let row = connection
        .query_row(
            "SELECT format_version, database_id, key_entry FROM state_metadata WHERE id = 1",
            [],
            |row| {
                Ok((
                    row.get::<_, u8>(0)?,
                    row.get::<_, Vec<u8>>(1)?,
                    row.get::<_, String>(2)?,
                ))
            },
        )
        .optional()?;
    let Some((format_version, database_id, key_entry)) = row else {
        return Ok(None);
    };
    if format_version != STATE_FORMAT_VERSION {
        return Err(StateStoreError::InvalidMetadata);
    }
    let database_id: [u8; DATABASE_ID_BYTES] = database_id
        .try_into()
        .map_err(|_| StateStoreError::InvalidMetadata)?;
    let key_entry =
        KeystoreEntryName::new(key_entry).map_err(|_| StateStoreError::InvalidMetadata)?;
    Ok(Some(Metadata {
        database_id,
        key_entry,
    }))
}

fn create_metadata<K: OsKeystore>(
    connection: &mut Connection,
    keystore: &mut K,
) -> Result<([u8; DATABASE_ID_BYTES], Zeroizing<[u8; DATABASE_KEY_BYTES]>), StateStoreError> {
    let mut database_id = [0; DATABASE_ID_BYTES];
    let mut key = Zeroizing::new([0; DATABASE_KEY_BYTES]);
    fill_random(&mut database_id)?;
    fill_random(key.as_mut())?;
    let key_entry =
        KeystoreEntryName::new(format!("{KEY_ENTRY_PREFIX}{}", encode_hex(&database_id)))
            .map_err(|_| StateStoreError::InvalidMetadata)?;
    let secret = KeystoreSecret::new(key.to_vec()).map_err(|_| StateStoreError::InvalidKey)?;
    keystore
        .store(&key_entry, &secret)
        .map_err(|_| StateStoreError::Keystore)?;
    let transaction = connection.transaction()?;
    transaction.execute(
        "INSERT INTO state_metadata(id, format_version, database_id, key_entry) VALUES (1, ?1, ?2, ?3)",
        params![STATE_FORMAT_VERSION, database_id.as_slice(), key_entry.as_str()],
    )?;
    transaction.commit()?;
    Ok((database_id, key))
}

fn load_key<K: OsKeystore>(
    keystore: &K,
    key_entry: &KeystoreEntryName,
) -> Result<Zeroizing<[u8; DATABASE_KEY_BYTES]>, StateStoreError> {
    let secret = keystore
        .load(key_entry)
        .map_err(|_| StateStoreError::Keystore)?
        .ok_or(StateStoreError::KeyUnavailable)?;
    let key: [u8; DATABASE_KEY_BYTES] = secret
        .as_bytes()
        .try_into()
        .map_err(|_| StateStoreError::InvalidKey)?;
    Ok(Zeroizing::new(key))
}

fn associated_data(database_id: &[u8; DATABASE_ID_BYTES]) -> Vec<u8> {
    let context = CryptoDomain::DurableStateEncryption.context();
    let mut data = Vec::with_capacity(context.len() + 1 + database_id.len());
    data.extend_from_slice(context);
    data.push(STATE_FORMAT_VERSION);
    data.extend_from_slice(database_id);
    data
}

fn fill_random(bytes: &mut [u8]) -> Result<(), StateStoreError> {
    let mut random_source = SysRng;
    random_source
        .try_fill_bytes(bytes)
        .map_err(|_| StateStoreError::Randomness)
}

fn encode_hex(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(char::from(HEX[usize::from(byte >> 4)]));
        output.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    output
}

#[cfg(test)]
mod tests {
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};

    use super::{
        EncryptedStateStore, MAX_STATE_DOCUMENT_BYTES, StateDocument, StateDocumentError,
        StateStoreError,
    };

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    #[derive(Default)]
    struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

    impl OsKeystore for MemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .0
                .get(entry.as_str())
                .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
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

    fn database_path() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir().join(format!(
            "yeokcham-state-store-{}-{number}.sqlite",
            std::process::id()
        ))
    }

    #[test]
    fn encrypts_and_restores_state_without_plaintext_persistence() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let document = StateDocument::new(b"canonical state document".to_vec()).unwrap();
        {
            let mut store = EncryptedStateStore::open(&path, &mut keystore).unwrap();
            assert_eq!(store.load().unwrap(), None);
            store.replace(&document).unwrap();
        }
        let bytes = fs::read(&path).unwrap();
        assert!(
            !bytes
                .windows(document.as_bytes().len())
                .any(|window| window == document.as_bytes())
        );
        let restored = EncryptedStateStore::open(&path, &mut keystore)
            .unwrap()
            .load()
            .unwrap()
            .unwrap();
        assert_eq!(restored.as_bytes(), document.as_bytes());
        assert_eq!(format!("{restored:?}"), "StateDocument(REDACTED)");
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn fails_closed_for_tampering_or_missing_keystore_key() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let document = StateDocument::new(b"sealed state".to_vec()).unwrap();
        {
            let mut store = EncryptedStateStore::open(&path, &mut keystore).unwrap();
            store.replace(&document).unwrap();
        }
        let connection = rusqlite::Connection::open(&path).unwrap();
        connection
            .execute(
                "UPDATE sealed_state SET ciphertext = zeroblob(16) WHERE id = 1",
                [],
            )
            .unwrap();
        assert!(matches!(
            EncryptedStateStore::open(&path, &mut keystore)
                .unwrap()
                .load(),
            Err(StateStoreError::Authentication)
        ));
        keystore.0.clear();
        assert!(matches!(
            EncryptedStateStore::open(&path, &mut keystore),
            Err(StateStoreError::KeyUnavailable)
        ));
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn recovers_after_an_injected_sqlite_state_write_fault() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let original = StateDocument::new(b"original state".to_vec()).unwrap();
        let replacement = StateDocument::new(b"replacement state".to_vec()).unwrap();
        let mut store = EncryptedStateStore::open(&path, &mut keystore).unwrap();
        store.replace(&original).unwrap();
        store
            .connection
            .execute_batch(
                "CREATE TRIGGER reject_sealed_state_update
                 BEFORE UPDATE ON sealed_state
                 BEGIN SELECT RAISE(ABORT, 'injected state write fault'); END;",
            )
            .unwrap();

        assert!(matches!(
            store.replace(&replacement),
            Err(StateStoreError::Sqlite(_))
        ));
        assert_eq!(
            store.load().unwrap().unwrap().as_bytes(),
            original.as_bytes()
        );

        store
            .connection
            .execute_batch("DROP TRIGGER reject_sealed_state_update;")
            .unwrap();
        store.replace(&replacement).unwrap();
        assert_eq!(
            store.load().unwrap().unwrap().as_bytes(),
            replacement.as_bytes()
        );
        drop(store);
        fs::remove_file(path).unwrap();
    }

    #[test]
    fn rejects_invalid_document_sizes() {
        assert_eq!(
            StateDocument::new(Vec::new()).unwrap_err(),
            StateDocumentError::Empty
        );
        assert_eq!(
            StateDocument::new(vec![0; MAX_STATE_DOCUMENT_BYTES + 1]).unwrap_err(),
            StateDocumentError::TooLarge
        );
    }
}
