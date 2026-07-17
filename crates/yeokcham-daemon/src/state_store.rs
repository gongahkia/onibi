use std::{fmt, path::Path};

use chacha20poly1305::{
    XChaCha20Poly1305, XNonce,
    aead::{Aead, KeyInit, Payload},
};
use getrandom::{SysRng, rand_core::TryRng};
use rusqlite::{Connection, OptionalExtension, params};
use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_protocol::{
    CryptoDomain, IdentityExportPassphrase, StateExportError, export_state, import_state,
};
use zeroize::Zeroizing;

pub const MAX_STATE_DOCUMENT_BYTES: usize = 4 * 1024 * 1024;
pub const CURRENT_STATE_FORMAT_VERSION: u8 = 2;
const STATE_FORMAT_VERSION_V1: u8 = 1;
const DATABASE_ID_BYTES: usize = 16;
const DATABASE_KEY_BYTES: usize = 32;
const NONCE_BYTES: usize = 24;
const KEY_ENTRY_PREFIX: &str = "state_db_";

struct StateMigration {
    from: u8,
    to: u8,
    transform: fn(&StateDocument) -> Result<StateDocument, StateDocumentError>,
}

const STATE_MIGRATIONS: &[StateMigration] = &[StateMigration {
    from: STATE_FORMAT_VERSION_V1,
    to: CURRENT_STATE_FORMAT_VERSION,
    transform: migrate_v1_to_v2,
}];

#[derive(Eq, PartialEq)]
pub struct StateDocument(Zeroizing<Vec<u8>>);

impl StateDocument {
    pub fn new(value: Vec<u8>) -> Result<Self, StateDocumentError> {
        Self::from_zeroizing(Zeroizing::new(value))
    }

    pub fn from_zeroizing(value: Zeroizing<Vec<u8>>) -> Result<Self, StateDocumentError> {
        if value.is_empty() {
            return Err(StateDocumentError::Empty);
        }
        if value.len() > MAX_STATE_DOCUMENT_BYTES {
            return Err(StateDocumentError::TooLarge);
        }
        Ok(Self(value))
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
            Some(metadata) => {
                let key = load_key(keystore, &metadata.key_entry)?;
                let metadata = migrate_state(&mut connection, metadata, &key)?;
                (metadata.database_id, key)
            }
            None => create_metadata(&mut connection, keystore)?,
        };
        Ok(Self {
            connection,
            database_id,
            key,
        })
    }

    pub fn load(&self) -> Result<Option<StateDocument>, StateStoreError> {
        read_sealed_state(
            &self.connection,
            &self.key,
            &self.database_id,
            CURRENT_STATE_FORMAT_VERSION,
        )
    }

    pub fn replace(&mut self, document: &StateDocument) -> Result<(), StateStoreError> {
        let (nonce, ciphertext) = encrypt_state(
            document,
            &self.key,
            &self.database_id,
            CURRENT_STATE_FORMAT_VERSION,
        )?;
        let transaction = self.connection.transaction()?;
        transaction.execute(
            "INSERT INTO sealed_state(id, nonce, ciphertext) VALUES (1, ?1, ?2)
             ON CONFLICT(id) DO UPDATE SET nonce = excluded.nonce, ciphertext = excluded.ciphertext",
            params![nonce.as_slice(), ciphertext],
        )?;
        transaction.commit()?;
        Ok(())
    }

    pub fn export_recovery(
        &self,
        passphrase: &IdentityExportPassphrase,
    ) -> Result<Vec<u8>, StateStoreError> {
        let document = self.load()?.ok_or(StateStoreError::NoStateToExport)?;
        export_state(document.as_bytes(), passphrase).map_err(StateStoreError::RecoveryExport)
    }

    pub fn import_recovery(
        &mut self,
        encoded: &[u8],
        passphrase: &IdentityExportPassphrase,
    ) -> Result<(), StateStoreError> {
        if self.load()?.is_some() {
            return Err(StateStoreError::StateImportDestinationNotEmpty);
        }
        let document = StateDocument::from_zeroizing(
            import_state(encoded, passphrase).map_err(StateStoreError::RecoveryImport)?,
        )
        .map_err(StateStoreError::InvalidDocument)?;
        self.replace(&document)
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
    #[error("state-store format version {0} is newer than this client")]
    UnsupportedFormatVersion(u8),
    #[error("state-store format version {0} has no registered migration")]
    MissingMigration(u8),
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
    #[error("state-store has no state to export")]
    NoStateToExport,
    #[error("state-store recovery export failed")]
    RecoveryExport(#[source] StateExportError),
    #[error("state-store recovery import failed")]
    RecoveryImport(#[source] StateExportError),
    #[error("state-store recovery import would overwrite existing state")]
    StateImportDestinationNotEmpty,
}

struct Metadata {
    format_version: u8,
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
    let database_id: [u8; DATABASE_ID_BYTES] = database_id
        .try_into()
        .map_err(|_| StateStoreError::InvalidMetadata)?;
    let key_entry =
        KeystoreEntryName::new(key_entry).map_err(|_| StateStoreError::InvalidMetadata)?;
    Ok(Some(Metadata {
        format_version,
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
        params![CURRENT_STATE_FORMAT_VERSION, database_id.as_slice(), key_entry.as_str()],
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

fn migrate_state(
    connection: &mut Connection,
    mut metadata: Metadata,
    key: &Zeroizing<[u8; DATABASE_KEY_BYTES]>,
) -> Result<Metadata, StateStoreError> {
    if metadata.format_version > CURRENT_STATE_FORMAT_VERSION {
        return Err(StateStoreError::UnsupportedFormatVersion(
            metadata.format_version,
        ));
    }
    while metadata.format_version != CURRENT_STATE_FORMAT_VERSION {
        let migration = STATE_MIGRATIONS
            .iter()
            .find(|migration| migration.from == metadata.format_version)
            .ok_or(StateStoreError::MissingMigration(metadata.format_version))?;
        let migrated = read_sealed_state(
            connection,
            key,
            &metadata.database_id,
            metadata.format_version,
        )?
        .map(|document| (migration.transform)(&document))
        .transpose()
        .map_err(StateStoreError::InvalidDocument)?;
        let encrypted = migrated
            .as_ref()
            .map(|document| encrypt_state(document, key, &metadata.database_id, migration.to))
            .transpose()?;
        let transaction = connection.transaction()?;
        if let Some((nonce, ciphertext)) = encrypted {
            transaction.execute(
                "UPDATE sealed_state SET nonce = ?1, ciphertext = ?2 WHERE id = 1",
                params![nonce.as_slice(), ciphertext],
            )?;
        }
        transaction.execute(
            "UPDATE state_metadata SET format_version = ?1 WHERE id = 1",
            params![migration.to],
        )?;
        transaction.commit()?;
        metadata.format_version = migration.to;
    }
    Ok(metadata)
}

fn migrate_v1_to_v2(document: &StateDocument) -> Result<StateDocument, StateDocumentError> {
    StateDocument::new(document.as_bytes().to_vec())
}

fn read_sealed_state(
    connection: &Connection,
    key: &[u8; DATABASE_KEY_BYTES],
    database_id: &[u8; DATABASE_ID_BYTES],
    format_version: u8,
) -> Result<Option<StateDocument>, StateStoreError> {
    let row = connection
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
    let cipher = XChaCha20Poly1305::new_from_slice(key).map_err(|_| StateStoreError::Encryption)?;
    let nonce = XNonce::try_from(nonce.as_slice()).map_err(|_| StateStoreError::Encryption)?;
    let plaintext = cipher
        .decrypt(
            &nonce,
            Payload {
                msg: &ciphertext,
                aad: &associated_data(database_id, format_version),
            },
        )
        .map_err(|_| StateStoreError::Authentication)?;
    StateDocument::new(plaintext)
        .map(Some)
        .map_err(StateStoreError::InvalidDocument)
}

fn encrypt_state(
    document: &StateDocument,
    key: &[u8; DATABASE_KEY_BYTES],
    database_id: &[u8; DATABASE_ID_BYTES],
    format_version: u8,
) -> Result<([u8; NONCE_BYTES], Vec<u8>), StateStoreError> {
    let mut nonce = [0; NONCE_BYTES];
    fill_random(&mut nonce)?;
    let cipher = XChaCha20Poly1305::new_from_slice(key).map_err(|_| StateStoreError::Encryption)?;
    let nonce_value =
        XNonce::try_from(nonce.as_slice()).map_err(|_| StateStoreError::Encryption)?;
    let ciphertext = cipher
        .encrypt(
            &nonce_value,
            Payload {
                msg: document.as_bytes(),
                aad: &associated_data(database_id, format_version),
            },
        )
        .map_err(|_| StateStoreError::Encryption)?;
    Ok((nonce, ciphertext))
}

fn associated_data(database_id: &[u8; DATABASE_ID_BYTES], format_version: u8) -> Vec<u8> {
    let context = CryptoDomain::DurableStateEncryption.context();
    let mut data = Vec::with_capacity(context.len() + 1 + database_id.len());
    data.extend_from_slice(context);
    data.push(format_version);
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
    use zeroize::Zeroizing;

    use super::{
        CURRENT_STATE_FORMAT_VERSION, DATABASE_ID_BYTES, DATABASE_KEY_BYTES, EncryptedStateStore,
        MAX_STATE_DOCUMENT_BYTES, STATE_FORMAT_VERSION_V1, StateDocument, StateDocumentError,
        StateStoreError, encrypt_state, initialize_schema, read_sealed_state,
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

    fn seed_state(
        path: &PathBuf,
        keystore: &mut MemoryKeystore,
        format_version: u8,
        document: Option<&StateDocument>,
    ) -> ([u8; DATABASE_ID_BYTES], Zeroizing<[u8; DATABASE_KEY_BYTES]>) {
        let connection = rusqlite::Connection::open(path).unwrap();
        initialize_schema(&connection).unwrap();
        let database_id = [0x42; DATABASE_ID_BYTES];
        let key = Zeroizing::new([0xA5; DATABASE_KEY_BYTES]);
        let key_entry =
            KeystoreEntryName::new(format!("state_db_{}", super::encode_hex(&database_id)))
                .unwrap();
        keystore
            .store(&key_entry, &KeystoreSecret::new(key.to_vec()).unwrap())
            .unwrap();
        connection
            .execute(
                "INSERT INTO state_metadata(id, format_version, database_id, key_entry) VALUES (1, ?1, ?2, ?3)",
                rusqlite::params![format_version, database_id.as_slice(), key_entry.as_str()],
            )
            .unwrap();
        if let Some(document) = document {
            let (nonce, ciphertext) =
                encrypt_state(document, &key, &database_id, format_version).unwrap();
            connection
                .execute(
                    "INSERT INTO sealed_state(id, nonce, ciphertext) VALUES (1, ?1, ?2)",
                    rusqlite::params![nonce.as_slice(), ciphertext],
                )
                .unwrap();
        }
        (database_id, key)
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
    fn migrates_legacy_encrypted_state_and_rejects_unknown_format_versions() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let document = StateDocument::new(b"legacy encrypted state".to_vec()).unwrap();
        let (database_id, key) = seed_state(
            &path,
            &mut keystore,
            STATE_FORMAT_VERSION_V1,
            Some(&document),
        );

        let migrated = EncryptedStateStore::open(&path, &mut keystore)
            .unwrap()
            .load()
            .unwrap()
            .unwrap();
        assert_eq!(migrated.as_bytes(), document.as_bytes());
        let connection = rusqlite::Connection::open(&path).unwrap();
        assert_eq!(
            connection
                .query_row(
                    "SELECT format_version FROM state_metadata WHERE id = 1",
                    [],
                    |row| { row.get::<_, u8>(0) }
                )
                .unwrap(),
            CURRENT_STATE_FORMAT_VERSION
        );
        assert!(matches!(
            read_sealed_state(&connection, &key, &database_id, STATE_FORMAT_VERSION_V1),
            Err(StateStoreError::Authentication)
        ));
        fs::remove_file(&path).unwrap();

        let unsupported_path = database_path();
        let (_, _) = seed_state(
            &unsupported_path,
            &mut keystore,
            CURRENT_STATE_FORMAT_VERSION + 1,
            None,
        );
        assert!(matches!(
            EncryptedStateStore::open(&unsupported_path, &mut keystore),
            Err(StateStoreError::UnsupportedFormatVersion(version))
                if version == CURRENT_STATE_FORMAT_VERSION + 1
        ));
        fs::remove_file(unsupported_path).unwrap();
    }

    #[test]
    fn migration_is_atomic_when_metadata_update_fails() {
        let path = database_path();
        let mut keystore = MemoryKeystore::default();
        let document = StateDocument::new(b"legacy state remains intact".to_vec()).unwrap();
        let (database_id, key) = seed_state(
            &path,
            &mut keystore,
            STATE_FORMAT_VERSION_V1,
            Some(&document),
        );
        let connection = rusqlite::Connection::open(&path).unwrap();
        connection
            .execute_batch(
                "CREATE TRIGGER reject_state_metadata_migration
                 BEFORE UPDATE ON state_metadata
                 BEGIN SELECT RAISE(ABORT, 'injected migration fault'); END;",
            )
            .unwrap();
        drop(connection);

        assert!(matches!(
            EncryptedStateStore::open(&path, &mut keystore),
            Err(StateStoreError::Sqlite(_))
        ));
        let connection = rusqlite::Connection::open(&path).unwrap();
        assert_eq!(
            connection
                .query_row(
                    "SELECT format_version FROM state_metadata WHERE id = 1",
                    [],
                    |row| { row.get::<_, u8>(0) }
                )
                .unwrap(),
            STATE_FORMAT_VERSION_V1
        );
        assert_eq!(
            read_sealed_state(&connection, &key, &database_id, STATE_FORMAT_VERSION_V1)
                .unwrap()
                .unwrap()
                .as_bytes(),
            document.as_bytes()
        );
        connection
            .execute_batch("DROP TRIGGER reject_state_metadata_migration;")
            .unwrap();
        drop(connection);
        assert_eq!(
            EncryptedStateStore::open(&path, &mut keystore)
                .unwrap()
                .load()
                .unwrap()
                .unwrap()
                .as_bytes(),
            document.as_bytes()
        );
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
