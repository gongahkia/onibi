#![forbid(unsafe_code)]

use std::path::Path;

use rusqlite::Connection;
use yeokcham_core::{Error, Result};
use yeokcham_protocol::{
    MAILBOX_IDENTIFIER_BYTES, MAX_RELAY_INVITATION_TTL_SECONDS, MailboxCapability,
};

pub const MAX_RELAY_RETENTION_TTL_SECONDS: u32 = MAX_RELAY_INVITATION_TTL_SECONDS;
pub const RELAY_SCHEMA_VERSION: u32 = 1;

pub struct RelayDatabase {
    connection: Connection,
}

impl RelayDatabase {
    pub fn open(path: &Path) -> Result<Self, RelayDatabaseError> {
        Self::from_connection(Connection::open(path)?)
    }

    pub fn schema_version(&self) -> Result<u32, RelayDatabaseError> {
        read_schema_version(&self.connection)
    }

    fn from_connection(mut connection: Connection) -> Result<Self, RelayDatabaseError> {
        connection.pragma_update(None, "foreign_keys", "ON")?;
        connection.pragma_update(None, "journal_mode", "WAL")?;
        connection.pragma_update(None, "synchronous", "FULL")?;
        apply_migrations(&mut connection)?;
        Ok(Self { connection })
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RelayDatabaseError {
    #[error("SQLite relay-database operation failed")]
    Sqlite(#[from] rusqlite::Error),
    #[error("relay database schema version {0} is unsupported")]
    UnsupportedSchemaVersion(u32),
}

fn apply_migrations(connection: &mut Connection) -> Result<(), RelayDatabaseError> {
    connection.execute_batch(
        "CREATE TABLE IF NOT EXISTS relay_schema_migrations(
             version INTEGER PRIMARY KEY CHECK(version > 0)
         ) STRICT;",
    )?;
    let current_version = read_schema_version(connection)?;
    if current_version > RELAY_SCHEMA_VERSION {
        return Err(RelayDatabaseError::UnsupportedSchemaVersion(
            current_version,
        ));
    }
    if current_version == RELAY_SCHEMA_VERSION {
        return Ok(());
    }

    let transaction = connection.transaction()?;
    transaction.execute_batch(
        "CREATE TABLE relay_mailboxes(
             mailbox_id BLOB PRIMARY KEY NOT NULL CHECK(length(mailbox_id) = 16),
             capability_token BLOB NOT NULL CHECK(length(capability_token) = 32),
             quota_bytes BLOB NOT NULL CHECK(length(quota_bytes) = 8),
             used_bytes BLOB NOT NULL CHECK(length(used_bytes) = 8),
             created_at INTEGER NOT NULL CHECK(created_at >= 0)
         ) STRICT;
         CREATE TABLE relay_envelopes(
             mailbox_id BLOB NOT NULL CHECK(length(mailbox_id) = 16),
             sequence INTEGER NOT NULL CHECK(sequence >= 0),
             ciphertext BLOB NOT NULL CHECK(length(ciphertext) > 0),
             received_at INTEGER NOT NULL CHECK(received_at >= 0),
             expires_at INTEGER NOT NULL CHECK(expires_at > received_at),
             PRIMARY KEY(mailbox_id, sequence),
             FOREIGN KEY(mailbox_id) REFERENCES relay_mailboxes(mailbox_id) ON DELETE CASCADE
         ) STRICT;
         CREATE INDEX relay_envelopes_expiration ON relay_envelopes(expires_at);
         CREATE INDEX relay_envelopes_retrieval ON relay_envelopes(mailbox_id, sequence);",
    )?;
    transaction.execute(
        "INSERT INTO relay_schema_migrations(version) VALUES (?1)",
        [RELAY_SCHEMA_VERSION],
    )?;
    transaction.commit()?;
    Ok(())
}

fn read_schema_version(connection: &Connection) -> Result<u32, RelayDatabaseError> {
    Ok(connection
        .query_row(
            "SELECT MAX(version) FROM relay_schema_migrations",
            [],
            |row| row.get::<_, Option<u32>>(0),
        )?
        .unwrap_or_default())
}

pub struct MailboxIngress {
    capability: MailboxCapability,
}

impl MailboxIngress {
    #[must_use]
    pub const fn new(capability: MailboxCapability) -> Self {
        Self { capability }
    }

    #[must_use]
    pub const fn mailbox_id(&self) -> &[u8; MAILBOX_IDENTIFIER_BYTES] {
        self.capability.mailbox_id()
    }

    pub fn validate(&self, presented: &[u8]) -> Result<(), MailboxIngressError> {
        let presented = MailboxCapability::decode(presented)
            .map_err(|_| MailboxIngressError::InvalidCapability)?;
        if self.capability.authorizes(&presented) {
            Ok(())
        } else {
            Err(MailboxIngressError::InvalidCapability)
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MailboxIngressError {
    #[error("invalid mailbox capability")]
    InvalidCapability,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MailboxQuota {
    bytes: u64,
}

impl MailboxQuota {
    pub fn new(bytes: u64) -> Result<Self> {
        if bytes == 0 {
            return Err(Error::InvalidInput("mailbox quota must be nonzero"));
        }
        Ok(Self { bytes })
    }

    #[must_use]
    pub const fn bytes(self) -> u64 {
        self.bytes
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MailboxQuotaTracker {
    quota: MailboxQuota,
    used_bytes: u64,
}

impl MailboxQuotaTracker {
    #[must_use]
    pub const fn new(quota: MailboxQuota) -> Self {
        Self {
            quota,
            used_bytes: 0,
        }
    }

    #[must_use]
    pub const fn quota(self) -> MailboxQuota {
        self.quota
    }

    #[must_use]
    pub const fn used_bytes(self) -> u64 {
        self.used_bytes
    }

    #[must_use]
    pub const fn remaining_bytes(self) -> u64 {
        self.quota.bytes - self.used_bytes
    }

    pub fn reserve(&mut self, bytes: u64) -> Result<(), MailboxQuotaError> {
        let used_bytes = self
            .used_bytes
            .checked_add(bytes)
            .ok_or(MailboxQuotaError::Exceeded)?;
        if used_bytes > self.quota.bytes {
            return Err(MailboxQuotaError::Exceeded);
        }
        self.used_bytes = used_bytes;
        Ok(())
    }

    pub fn release(&mut self, bytes: u64) -> Result<(), MailboxQuotaError> {
        self.used_bytes = self
            .used_bytes
            .checked_sub(bytes)
            .ok_or(MailboxQuotaError::ReleaseExceedsUsage)?;
        Ok(())
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MailboxQuotaError {
    #[error("mailbox quota exceeded")]
    Exceeded,
    #[error("mailbox quota release exceeds recorded usage")]
    ReleaseExceedsUsage,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayRetentionPolicy {
    ttl_seconds: u32,
}

impl RelayRetentionPolicy {
    pub fn new(ttl_seconds: u32) -> Result<Self, RelayRetentionPolicyError> {
        if ttl_seconds == 0 || ttl_seconds > MAX_RELAY_RETENTION_TTL_SECONDS {
            return Err(RelayRetentionPolicyError::InvalidTtl);
        }
        Ok(Self { ttl_seconds })
    }

    #[must_use]
    pub const fn ttl_seconds(self) -> u32 {
        self.ttl_seconds
    }

    pub fn expires_at(self, received_at: u64) -> Result<u64, RelayRetentionPolicyError> {
        received_at
            .checked_add(u64::from(self.ttl_seconds))
            .ok_or(RelayRetentionPolicyError::TimestampOverflow)
    }

    pub fn is_expired(self, received_at: u64, now: u64) -> Result<bool, RelayRetentionPolicyError> {
        Ok(now >= self.expires_at(received_at)?)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayRetentionPolicyError {
    #[error("relay retention TTL is invalid")]
    InvalidTtl,
    #[error("relay retention timestamp overflowed")]
    TimestampOverflow,
}

#[cfg(test)]
mod tests {
    use super::{
        MAX_RELAY_RETENTION_TTL_SECONDS, MailboxIngress, MailboxIngressError, MailboxQuota,
        MailboxQuotaError, MailboxQuotaTracker, RELAY_SCHEMA_VERSION, RelayDatabase,
        RelayDatabaseError, RelayRetentionPolicy, RelayRetentionPolicyError,
    };
    use rusqlite::Connection;
    use yeokcham_protocol::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
    };

    fn capability() -> MailboxCapability {
        MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x22; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
    }

    #[test]
    fn admits_only_the_registered_canonical_capability() {
        let capability = capability();
        let encoded = capability.encode().unwrap();
        let ingress = MailboxIngress::new(capability);

        assert_eq!(ingress.mailbox_id(), &[0x11; MAILBOX_IDENTIFIER_BYTES]);
        assert_eq!(ingress.validate(&encoded), Ok(()));
    }

    #[test]
    fn rejects_malformed_or_unregistered_capabilities() {
        let ingress = MailboxIngress::new(capability());
        let other = MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
        .encode()
        .unwrap();

        assert_eq!(
            ingress.validate(&[0x80]),
            Err(MailboxIngressError::InvalidCapability)
        );
        assert_eq!(
            ingress.validate(&other),
            Err(MailboxIngressError::InvalidCapability)
        );
    }

    #[test]
    fn enforces_mailbox_quota_without_mutating_on_rejection() {
        let quota = MailboxQuota::new(10).unwrap();
        let mut tracker = MailboxQuotaTracker::new(quota);

        assert_eq!(tracker.reserve(6), Ok(()));
        assert_eq!(tracker.reserve(4), Ok(()));
        assert_eq!(tracker.reserve(1), Err(MailboxQuotaError::Exceeded));
        assert_eq!(tracker.quota(), quota);
        assert_eq!(tracker.used_bytes(), 10);
        assert_eq!(tracker.remaining_bytes(), 0);

        assert_eq!(tracker.release(4), Ok(()));
        assert_eq!(tracker.reserve(1), Ok(()));
        assert_eq!(tracker.used_bytes(), 7);
    }

    #[test]
    fn rejects_overflowing_reservations_and_invalid_releases() {
        let mut tracker = MailboxQuotaTracker::new(MailboxQuota::new(u64::MAX).unwrap());

        assert_eq!(tracker.reserve(u64::MAX), Ok(()));
        assert_eq!(tracker.reserve(1), Err(MailboxQuotaError::Exceeded));
        assert_eq!(tracker.used_bytes(), u64::MAX);
        assert_eq!(tracker.release(u64::MAX), Ok(()));
        assert_eq!(
            tracker.release(1),
            Err(MailboxQuotaError::ReleaseExceedsUsage)
        );
    }

    #[test]
    fn enforces_retention_ttl_at_the_expiration_boundary() {
        let policy = RelayRetentionPolicy::new(10).unwrap();

        assert_eq!(policy.expires_at(100), Ok(110));
        assert_eq!(policy.is_expired(100, 109), Ok(false));
        assert_eq!(policy.is_expired(100, 110), Ok(true));
    }

    #[test]
    fn rejects_invalid_or_overflowing_retention_policy_values() {
        assert_eq!(
            RelayRetentionPolicy::new(0),
            Err(RelayRetentionPolicyError::InvalidTtl)
        );
        assert_eq!(
            RelayRetentionPolicy::new(MAX_RELAY_RETENTION_TTL_SECONDS + 1),
            Err(RelayRetentionPolicyError::InvalidTtl)
        );
        assert_eq!(
            RelayRetentionPolicy::new(1).unwrap().expires_at(u64::MAX),
            Err(RelayRetentionPolicyError::TimestampOverflow)
        );
    }

    #[test]
    fn migrates_an_empty_relay_database_to_the_current_schema() {
        let database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();

        assert_eq!(database.schema_version().unwrap(), RELAY_SCHEMA_VERSION);
        let schema_version = database
            .connection
            .query_row("SELECT version FROM relay_schema_migrations", [], |row| {
                row.get::<_, u32>(0)
            })
            .unwrap();
        assert_eq!(schema_version, RELAY_SCHEMA_VERSION);
        let table_count = database
            .connection
            .query_row(
                "SELECT COUNT(*) FROM sqlite_master
                 WHERE type = 'table' AND name IN ('relay_mailboxes', 'relay_envelopes')",
                [],
                |row| row.get::<_, u8>(0),
            )
            .unwrap();
        assert_eq!(table_count, 2);
    }

    #[test]
    fn rejects_databases_from_newer_schema_versions() {
        let connection = Connection::open_in_memory().unwrap();
        connection
            .execute_batch(
                "CREATE TABLE relay_schema_migrations(
                     version INTEGER PRIMARY KEY CHECK(version > 0)
                 ) STRICT;
                 INSERT INTO relay_schema_migrations(version) VALUES (2);",
            )
            .unwrap();

        assert!(matches!(
            RelayDatabase::from_connection(connection),
            Err(RelayDatabaseError::UnsupportedSchemaVersion(2))
        ));
    }
}
