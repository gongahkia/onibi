#![forbid(unsafe_code)]

use std::collections::{HashMap, VecDeque};
use std::fmt;
use std::net::SocketAddr;
use std::path::{Path, PathBuf};

use rusqlite::{Connection, OptionalExtension, Transaction, params};
use sha2::{Digest, Sha256};
use yeokcham_core::{
    ED25519_SIGNATURE_BYTES, Error, KeystoreEntryName, KeystoreSecret, OsKeystore, RelayPublicKey,
    RelaySigningKeypair, Result,
};
use yeokcham_protocol::{
    AttachmentIdentifier, CryptoDomain, EncryptedAttachmentChunk, EncryptedMessageEnvelope,
    MAILBOX_IDENTIFIER_BYTES, MAX_RELAY_INVITATION_TTL_SECONDS, MailboxCapability,
    RelayStorageReceipt,
};

pub const MAX_RELAY_RETENTION_TTL_SECONDS: u32 = MAX_RELAY_INVITATION_TTL_SECONDS;
pub const MAX_MAILBOX_RETRIEVAL_ENVELOPES: u16 = 128;
pub const MAX_RELAY_INGRESS_REQUESTS_PER_WINDOW: u16 = 1024;
pub const MAX_RELAY_INGRESS_WINDOW_SECONDS: u32 = 3600;
pub const RELAY_SCHEMA_VERSION: u32 = 4;
const RELAY_IDENTITY_KEY_ENTRY: &str = "relay_identity_v1";

pub const RELAY_HEALTH_PATH: &str = "/healthz";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayOperationalMetrics {
    registered_mailboxes: u64,
    stored_envelopes: u64,
    stored_bytes: u64,
}

impl RelayOperationalMetrics {
    #[must_use]
    pub const fn registered_mailboxes(self) -> u64 {
        self.registered_mailboxes
    }

    #[must_use]
    pub const fn stored_envelopes(self) -> u64 {
        self.stored_envelopes
    }

    #[must_use]
    pub const fn stored_bytes(self) -> u64 {
        self.stored_bytes
    }
}

pub trait RelayMetricsEmitter {
    fn emit(&mut self, metrics: RelayOperationalMetrics);
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayHealthEndpoint {
    address: SocketAddr,
}

impl RelayHealthEndpoint {
    pub fn new(address: SocketAddr) -> Result<Self, RelayHealthEndpointError> {
        if address.port() == 0 {
            return Err(RelayHealthEndpointError::ZeroPort);
        }
        if !address.ip().is_loopback() {
            return Err(RelayHealthEndpointError::NonLoopbackAddress);
        }
        Ok(Self { address })
    }

    #[must_use]
    pub const fn address(self) -> SocketAddr {
        self.address
    }

    pub fn response(
        &self,
        path: &str,
        database: &RelayDatabase,
    ) -> Result<&'static str, RelayHealthEndpointError> {
        if path != RELAY_HEALTH_PATH {
            return Err(RelayHealthEndpointError::UnknownPath);
        }
        database
            .schema_version()
            .map_err(|_| RelayHealthEndpointError::Unhealthy)?;
        Ok("ok\n")
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayHealthEndpointError {
    #[error("relay health endpoint port must be nonzero")]
    ZeroPort,
    #[error("relay health endpoint must bind to loopback")]
    NonLoopbackAddress,
    #[error("relay health endpoint path is unknown")]
    UnknownPath,
    #[error("relay health check failed")]
    Unhealthy,
}

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct SyntheticRelayTrafficProof([u8; ED25519_SIGNATURE_BYTES]);

impl SyntheticRelayTrafficProof {
    pub fn for_mailbox_registration(
        authority: &RelaySigningKeypair,
        capability: &MailboxCapability,
        quota: MailboxQuota,
        created_at: u64,
    ) -> Result<Self, ProjectTestRelayError> {
        Ok(Self(authority.sign(&mailbox_registration_input(
            capability, quota, created_at,
        )?)))
    }

    pub fn for_envelope_insertion(
        authority: &RelaySigningKeypair,
        capability: &MailboxCapability,
        envelope: &EncryptedMessageEnvelope,
        received_at: u64,
        retention: RelayRetentionPolicy,
    ) -> Result<Self, ProjectTestRelayError> {
        Ok(Self(authority.sign(&envelope_insertion_input(
            capability,
            envelope,
            received_at,
            retention,
        )?)))
    }

    pub fn for_envelope_retrieval(
        authority: &RelaySigningKeypair,
        capability: &MailboxCapability,
        after_sequence: Option<u64>,
        now: u64,
        limit: u16,
    ) -> Result<Self, ProjectTestRelayError> {
        Ok(Self(authority.sign(&envelope_retrieval_input(
            capability,
            after_sequence,
            now,
            limit,
        )?)))
    }

    pub fn for_envelope_acknowledgement(
        authority: &RelaySigningKeypair,
        capability: &MailboxCapability,
        sequence: u64,
    ) -> Result<Self, ProjectTestRelayError> {
        Ok(Self(authority.sign(&envelope_acknowledgement_input(
            capability, sequence,
        )?)))
    }

    fn verify(self, authority: &RelayPublicKey, input: &[u8]) -> Result<(), ProjectTestRelayError> {
        authority
            .verify(input, &self.0)
            .map_err(|_| ProjectTestRelayError::InvalidSyntheticTraffic)
    }
}

impl fmt::Debug for SyntheticRelayTrafficProof {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_tuple("SyntheticRelayTrafficProof")
            .field(&"REDACTED")
            .finish()
    }
}

pub struct ProjectTestRelay {
    database: RelayDatabase,
    synthetic_traffic_authority: RelayPublicKey,
}

impl ProjectTestRelay {
    #[must_use]
    pub const fn new(database: RelayDatabase, synthetic_traffic_authority: RelayPublicKey) -> Self {
        Self {
            database,
            synthetic_traffic_authority,
        }
    }

    pub fn register_mailbox(
        &mut self,
        proof: SyntheticRelayTrafficProof,
        capability: &MailboxCapability,
        quota: MailboxQuota,
        created_at: u64,
    ) -> Result<(), ProjectTestRelayError> {
        proof.verify(
            &self.synthetic_traffic_authority,
            &mailbox_registration_input(capability, quota, created_at)?,
        )?;
        self.database
            .register_mailbox(capability, quota, created_at)
            .map_err(ProjectTestRelayError::Database)
    }

    pub fn insert_envelope(
        &mut self,
        proof: SyntheticRelayTrafficProof,
        capability: &MailboxCapability,
        envelope: &EncryptedMessageEnvelope,
        received_at: u64,
        retention: RelayRetentionPolicy,
    ) -> Result<u64, ProjectTestRelayError> {
        proof.verify(
            &self.synthetic_traffic_authority,
            &envelope_insertion_input(capability, envelope, received_at, retention)?,
        )?;
        self.database
            .insert_envelope(capability, envelope, received_at, retention)
            .map_err(ProjectTestRelayError::Database)
    }

    pub fn retrieve_envelopes(
        &mut self,
        proof: SyntheticRelayTrafficProof,
        capability: &MailboxCapability,
        after_sequence: Option<u64>,
        now: u64,
        limit: u16,
    ) -> Result<Vec<RelayEnvelope>, ProjectTestRelayError> {
        proof.verify(
            &self.synthetic_traffic_authority,
            &envelope_retrieval_input(capability, after_sequence, now, limit)?,
        )?;
        self.database
            .retrieve_envelopes(capability, after_sequence, now, limit)
            .map_err(ProjectTestRelayError::Database)
    }

    pub fn acknowledge_envelope(
        &mut self,
        proof: SyntheticRelayTrafficProof,
        capability: &MailboxCapability,
        sequence: u64,
    ) -> Result<(), ProjectTestRelayError> {
        proof.verify(
            &self.synthetic_traffic_authority,
            &envelope_acknowledgement_input(capability, sequence)?,
        )?;
        self.database
            .acknowledge_envelope(capability, sequence)
            .map_err(ProjectTestRelayError::Database)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ProjectTestRelayError {
    #[error("synthetic relay traffic proof is invalid")]
    InvalidSyntheticTraffic,
    #[error("synthetic relay traffic has an invalid mailbox capability")]
    InvalidCapability,
    #[error("synthetic relay traffic has an invalid encrypted envelope")]
    InvalidEnvelope,
    #[error("project test relay database operation failed")]
    Database(#[source] RelayDatabaseError),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayIngressRateLimit {
    max_requests: u16,
    window_seconds: u32,
}

impl RelayIngressRateLimit {
    pub fn new(max_requests: u16, window_seconds: u32) -> Result<Self, RelayIngressRateLimitError> {
        if max_requests == 0 || max_requests > MAX_RELAY_INGRESS_REQUESTS_PER_WINDOW {
            return Err(RelayIngressRateLimitError::InvalidMaxRequests);
        }
        if window_seconds == 0 || window_seconds > MAX_RELAY_INGRESS_WINDOW_SECONDS {
            return Err(RelayIngressRateLimitError::InvalidWindow);
        }
        Ok(Self {
            max_requests,
            window_seconds,
        })
    }

    #[must_use]
    pub const fn max_requests(self) -> u16 {
        self.max_requests
    }

    #[must_use]
    pub const fn window_seconds(self) -> u32 {
        self.window_seconds
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayIngressRateLimitError {
    #[error("relay ingress maximum request count is invalid")]
    InvalidMaxRequests,
    #[error("relay ingress rate-limit window is invalid")]
    InvalidWindow,
}

struct CapabilityIngressWindow {
    requests: VecDeque<u64>,
    last_seen: u64,
}

pub struct RelayIngressRateLimiter {
    limit: RelayIngressRateLimit,
    windows: HashMap<[u8; 32], CapabilityIngressWindow>,
    latest_timestamp: Option<u64>,
}

impl RelayIngressRateLimiter {
    #[must_use]
    pub fn new(limit: RelayIngressRateLimit) -> Self {
        Self {
            limit,
            windows: HashMap::new(),
            latest_timestamp: None,
        }
    }

    fn admit(&mut self, capability_digest: [u8; 32], now: u64) -> Result<(), RelayIngressError> {
        if self.latest_timestamp.is_some_and(|latest| now < latest) {
            return Err(RelayIngressError::TimestampRegression);
        }
        let window_seconds = u64::from(self.limit.window_seconds);
        self.windows
            .retain(|_, window| now.saturating_sub(window.last_seen) < window_seconds);
        let window =
            self.windows
                .entry(capability_digest)
                .or_insert_with(|| CapabilityIngressWindow {
                    requests: VecDeque::new(),
                    last_seen: now,
                });
        while window
            .requests
            .front()
            .is_some_and(|request| now.saturating_sub(*request) >= window_seconds)
        {
            window.requests.pop_front();
        }
        if window.requests.len() >= usize::from(self.limit.max_requests) {
            return Err(RelayIngressError::RateLimited);
        }
        window.requests.push_back(now);
        window.last_seen = now;
        self.latest_timestamp = Some(now);
        Ok(())
    }
}

pub struct RateLimitedRelay {
    database: RelayDatabase,
    limiter: RelayIngressRateLimiter,
}

impl RateLimitedRelay {
    #[must_use]
    pub fn new(database: RelayDatabase, limit: RelayIngressRateLimit) -> Self {
        Self {
            database,
            limiter: RelayIngressRateLimiter::new(limit),
        }
    }

    pub fn insert_envelope(
        &mut self,
        capability: &MailboxCapability,
        envelope: &EncryptedMessageEnvelope,
        received_at: u64,
        retention: RelayRetentionPolicy,
    ) -> Result<u64, RelayIngressError> {
        database_timestamp(received_at).map_err(RelayIngressError::Database)?;
        let capability_digest =
            capability_digest(capability).map_err(RelayIngressError::Database)?;
        if !self
            .database
            .is_registered_capability(capability, &capability_digest)
            .map_err(RelayIngressError::Database)?
        {
            return Err(RelayIngressError::InvalidCapability);
        }
        self.limiter.admit(capability_digest, received_at)?;
        self.database
            .insert_envelope(capability, envelope, received_at, retention)
            .map_err(RelayIngressError::Database)
    }

    #[must_use]
    pub fn into_database(self) -> RelayDatabase {
        self.database
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RelayIngressError {
    #[error("relay ingress capability is invalid")]
    InvalidCapability,
    #[error("relay ingress rate limit is exceeded")]
    RateLimited,
    #[error("relay ingress timestamp moved backwards")]
    TimestampRegression,
    #[error("relay ingress database operation failed")]
    Database(#[source] RelayDatabaseError),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SelfHostedRelayConfig {
    listen_address: std::net::SocketAddr,
    database_path: PathBuf,
    mailbox_quota: MailboxQuota,
    retention: RelayRetentionPolicy,
}

impl SelfHostedRelayConfig {
    pub fn new(
        listen_address: std::net::SocketAddr,
        database_path: PathBuf,
        mailbox_quota: MailboxQuota,
        retention: RelayRetentionPolicy,
    ) -> Result<Self, SelfHostedRelayConfigError> {
        if listen_address.port() == 0 {
            return Err(SelfHostedRelayConfigError::ZeroListenPort);
        }
        if !database_path.is_absolute() || database_path.parent().is_none() {
            return Err(SelfHostedRelayConfigError::InvalidDatabasePath);
        }
        Ok(Self {
            listen_address,
            database_path,
            mailbox_quota,
            retention,
        })
    }

    #[must_use]
    pub const fn listen_address(&self) -> std::net::SocketAddr {
        self.listen_address
    }
    #[must_use]
    pub fn database_path(&self) -> &Path {
        &self.database_path
    }
    #[must_use]
    pub const fn mailbox_quota(&self) -> MailboxQuota {
        self.mailbox_quota
    }
    #[must_use]
    pub const fn retention(&self) -> RelayRetentionPolicy {
        self.retention
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SelfHostedRelayConfigError {
    #[error("self-hosted relay listen port must be nonzero")]
    ZeroListenPort,
    #[error("self-hosted relay database path must be absolute")]
    InvalidDatabasePath,
}

pub struct RelayIdentity {
    signing_keypair: RelaySigningKeypair,
}

impl RelayIdentity {
    pub fn load_or_generate<K: OsKeystore>(keystore: &mut K) -> Result<Self, RelayIdentityError> {
        let entry = KeystoreEntryName::new(RELAY_IDENTITY_KEY_ENTRY.to_owned())
            .map_err(|_| RelayIdentityError::InvalidKeyEntry)?;
        let signing_keypair = match keystore
            .load(&entry)
            .map_err(|_| RelayIdentityError::Keystore)?
        {
            Some(secret) => RelaySigningKeypair::deserialize(secret.as_bytes())
                .map_err(|_| RelayIdentityError::InvalidStoredKey)?,
            None => {
                let signing_keypair =
                    RelaySigningKeypair::generate().map_err(|_| RelayIdentityError::Randomness)?;
                let secret = KeystoreSecret::new(signing_keypair.serialize().to_vec())
                    .map_err(|_| RelayIdentityError::InvalidStoredKey)?;
                keystore
                    .store(&entry, &secret)
                    .map_err(|_| RelayIdentityError::Keystore)?;
                signing_keypair
            }
        };
        Ok(Self { signing_keypair })
    }

    #[must_use]
    pub const fn signing_keypair(&self) -> &RelaySigningKeypair {
        &self.signing_keypair
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayIdentityError {
    #[error("relay identity key entry is invalid")]
    InvalidKeyEntry,
    #[error("relay identity keystore operation failed")]
    Keystore,
    #[error("relay identity could not be generated")]
    Randomness,
    #[error("stored relay identity is invalid")]
    InvalidStoredKey,
}

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

    fn is_registered_capability(
        &self,
        capability: &MailboxCapability,
        capability_digest: &[u8; 32],
    ) -> Result<bool, RelayDatabaseError> {
        self.connection
            .query_row(
                "SELECT EXISTS(
                 SELECT 1 FROM relay_mailboxes
                 WHERE mailbox_id = ?1 AND capability_digest = ?2
             )",
                params![
                    capability.mailbox_id().as_slice(),
                    capability_digest.as_slice(),
                ],
                |row| row.get(0),
            )
            .map_err(Into::into)
    }

    pub fn emit_operational_metrics<E: RelayMetricsEmitter>(
        &self,
        emitter: &mut E,
    ) -> Result<(), RelayDatabaseError> {
        let registered_mailboxes = count_rows(&self.connection, "relay_mailboxes")?;
        let stored_envelopes = count_rows(&self.connection, "relay_envelopes")?;
        let stored_bytes = self.connection.query_row(
            "SELECT COALESCE((SELECT SUM(length(ciphertext)) FROM relay_envelopes), 0)
             + COALESCE((SELECT SUM(length(encoded_chunk)) FROM relay_attachment_chunks), 0)",
            [],
            |row| row.get::<_, i64>(0),
        )?;
        emitter.emit(RelayOperationalMetrics {
            registered_mailboxes,
            stored_envelopes,
            stored_bytes: u64::try_from(stored_bytes)
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
        });
        Ok(())
    }

    pub fn garbage_collect_expired_mailboxes(
        &mut self,
        now: u64,
        retention: RelayRetentionPolicy,
    ) -> Result<RelayGarbageCollection, RelayDatabaseError> {
        let now = database_timestamp(now)?;
        let transaction = self.connection.transaction()?;
        let reclaimed_bytes = transaction.query_row(
            "SELECT COALESCE(SUM(length(ciphertext)), 0) FROM relay_envelopes
             WHERE expires_at <= ?1",
            [now],
            |row| row.get::<_, i64>(0),
        )?;
        let removed_envelopes =
            transaction.execute("DELETE FROM relay_envelopes WHERE expires_at <= ?1", [now])?;
        let usage = {
            let mut statement = transaction.prepare(
                "SELECT mailbox_id,
                     COALESCE((SELECT SUM(length(ciphertext)) FROM relay_envelopes
                               WHERE relay_envelopes.mailbox_id = relay_mailboxes.mailbox_id), 0)
                     + COALESCE((SELECT SUM(length(encoded_chunk)) FROM relay_attachment_chunks
                                 WHERE relay_attachment_chunks.mailbox_id = relay_mailboxes.mailbox_id), 0)
                 FROM relay_mailboxes",
            )?;
            statement
                .query_map([], |row| {
                    Ok((row.get::<_, Vec<u8>>(0)?, row.get::<_, i64>(1)?))
                })?
                .collect::<std::result::Result<Vec<_>, _>>()?
        };
        for (mailbox_id, used_bytes) in usage {
            let mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES] = mailbox_id
                .try_into()
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
            let used_bytes =
                u64::try_from(used_bytes).map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
            transaction.execute(
                "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
                params![used_bytes.to_be_bytes().as_slice(), mailbox_id.as_slice()],
            )?;
        }
        let removed_mailboxes = now
            .checked_sub(i64::from(retention.ttl_seconds()))
            .map(|created_at_cutoff| {
                transaction.execute(
                    "DELETE FROM relay_mailboxes
                     WHERE created_at <= ?1
                     AND NOT EXISTS(
                         SELECT 1 FROM relay_envelopes
                         WHERE relay_envelopes.mailbox_id = relay_mailboxes.mailbox_id
                     )
                     AND NOT EXISTS(
                         SELECT 1 FROM relay_attachment_chunks
                         WHERE relay_attachment_chunks.mailbox_id = relay_mailboxes.mailbox_id
                     )",
                    [created_at_cutoff],
                )
            })
            .transpose()?
            .unwrap_or_default();
        transaction.commit()?;
        Ok(RelayGarbageCollection {
            removed_mailboxes: u64::try_from(removed_mailboxes)
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
            removed_envelopes: u64::try_from(removed_envelopes)
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
            reclaimed_bytes: u64::try_from(reclaimed_bytes)
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
        })
    }

    pub fn garbage_collect_expired_attachment_chunks(
        &mut self,
        now: u64,
    ) -> Result<RelayAttachmentGarbageCollection, RelayDatabaseError> {
        let now = database_timestamp(now)?;
        let transaction = self.connection.transaction()?;
        let reclaimed_bytes = transaction.query_row(
            "SELECT COALESCE(SUM(length(encoded_chunk)), 0) FROM relay_attachment_chunks
             WHERE expires_at <= ?1",
            [now],
            |row| row.get::<_, i64>(0),
        )?;
        let removed_chunks = transaction.execute(
            "DELETE FROM relay_attachment_chunks WHERE expires_at <= ?1",
            [now],
        )?;
        let usage = {
            let mut statement = transaction.prepare(
                "SELECT mailbox_id,
                     COALESCE((SELECT SUM(length(ciphertext)) FROM relay_envelopes
                               WHERE relay_envelopes.mailbox_id = relay_mailboxes.mailbox_id), 0)
                     + COALESCE((SELECT SUM(length(encoded_chunk)) FROM relay_attachment_chunks
                                 WHERE relay_attachment_chunks.mailbox_id = relay_mailboxes.mailbox_id), 0)
                 FROM relay_mailboxes",
            )?;
            statement
                .query_map([], |row| {
                    Ok((row.get::<_, Vec<u8>>(0)?, row.get::<_, i64>(1)?))
                })?
                .collect::<std::result::Result<Vec<_>, _>>()?
        };
        for (mailbox_id, used_bytes) in usage {
            let mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES] = mailbox_id
                .try_into()
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
            let used_bytes =
                u64::try_from(used_bytes).map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
            transaction.execute(
                "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
                params![used_bytes.to_be_bytes().as_slice(), mailbox_id.as_slice()],
            )?;
        }
        transaction.commit()?;
        Ok(RelayAttachmentGarbageCollection {
            removed_chunks: u64::try_from(removed_chunks)
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
            reclaimed_bytes: u64::try_from(reclaimed_bytes)
                .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
        })
    }

    pub fn register_mailbox(
        &mut self,
        capability: &MailboxCapability,
        quota: MailboxQuota,
        created_at: u64,
    ) -> Result<(), RelayDatabaseError> {
        let created_at = database_timestamp(created_at)?;
        let capability_digest = capability_digest(capability)?;
        let inserted = self.connection.execute(
            "INSERT INTO relay_mailboxes(
                 mailbox_id, capability_digest, quota_bytes, used_bytes, created_at
             ) VALUES (?1, ?2, ?3, ?4, ?5)
             ON CONFLICT(mailbox_id) DO NOTHING",
            params![
                capability.mailbox_id().as_slice(),
                capability_digest.as_slice(),
                quota.bytes().to_be_bytes().as_slice(),
                [0_u8; 8].as_slice(),
                created_at,
            ],
        )?;
        if inserted == 0 {
            return Err(RelayDatabaseError::MailboxAlreadyRegistered);
        }
        Ok(())
    }

    pub fn insert_envelope(
        &mut self,
        capability: &MailboxCapability,
        envelope: &EncryptedMessageEnvelope,
        received_at: u64,
        retention: RelayRetentionPolicy,
    ) -> Result<u64, RelayDatabaseError> {
        let ciphertext = envelope
            .encode()
            .map_err(|_| RelayDatabaseError::InvalidEnvelope)?;
        let bytes =
            u64::try_from(ciphertext.len()).map_err(|_| RelayDatabaseError::QuotaExceeded)?;
        let expires_at = retention
            .expires_at(received_at)
            .map_err(|_| RelayDatabaseError::TimestampOutOfRange)?;
        let received_at = database_timestamp(received_at)?;
        let expires_at = database_timestamp(expires_at)?;
        let capability_digest = capability_digest(capability)?;
        let transaction = self.connection.transaction()?;
        let (quota_bytes, used_bytes): (Vec<u8>, Vec<u8>) = transaction
            .query_row(
                "SELECT quota_bytes, used_bytes FROM relay_mailboxes
                 WHERE mailbox_id = ?1 AND capability_digest = ?2",
                params![
                    capability.mailbox_id().as_slice(),
                    capability_digest.as_slice(),
                ],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?
            .ok_or(RelayDatabaseError::InvalidCapability)?;
        let quota = MailboxQuota::new(parse_u64(&quota_bytes)?)
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        let mut tracker = MailboxQuotaTracker::new(quota);
        tracker
            .reserve(parse_u64(&used_bytes)?)
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        tracker
            .reserve(bytes)
            .map_err(|_| RelayDatabaseError::QuotaExceeded)?;
        let sequence = transaction
            .query_row(
                "SELECT MAX(sequence) FROM relay_envelopes WHERE mailbox_id = ?1",
                [capability.mailbox_id().as_slice()],
                |row| row.get::<_, Option<i64>>(0),
            )?
            .map(|sequence| {
                sequence
                    .checked_add(1)
                    .ok_or(RelayDatabaseError::SequenceExhausted)
            })
            .transpose()?
            .unwrap_or(0);
        transaction.execute(
            "INSERT INTO relay_envelopes(mailbox_id, sequence, ciphertext, received_at, expires_at)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![
                capability.mailbox_id().as_slice(),
                sequence,
                ciphertext,
                received_at,
                expires_at,
            ],
        )?;
        transaction.execute(
            "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
            params![
                tracker.used_bytes().to_be_bytes().as_slice(),
                capability.mailbox_id().as_slice(),
            ],
        )?;
        transaction.commit()?;
        u64::try_from(sequence).map_err(|_| RelayDatabaseError::SequenceExhausted)
    }

    pub fn insert_envelope_with_receipt(
        &mut self,
        capability: &MailboxCapability,
        envelope: &EncryptedMessageEnvelope,
        received_at: u64,
        retention: RelayRetentionPolicy,
        relay: &RelaySigningKeypair,
    ) -> Result<RelayStorageReceipt, RelayDatabaseError> {
        let expires_at = retention
            .expires_at(received_at)
            .map_err(|_| RelayDatabaseError::TimestampOutOfRange)?;
        let sequence = self.insert_envelope(capability, envelope, received_at, retention)?;
        RelayStorageReceipt::issue(
            relay,
            *capability.mailbox_id(),
            sequence,
            received_at,
            expires_at,
        )
        .map_err(|_| RelayDatabaseError::Receipt)
    }

    pub fn store_attachment_chunk(
        &mut self,
        capability: &MailboxCapability,
        chunk: &EncryptedAttachmentChunk,
        received_at: u64,
        retention: RelayRetentionPolicy,
    ) -> Result<bool, RelayDatabaseError> {
        let encoded_chunk = chunk
            .encode()
            .map_err(|_| RelayDatabaseError::InvalidAttachmentChunk)?;
        let bytes =
            u64::try_from(encoded_chunk.len()).map_err(|_| RelayDatabaseError::QuotaExceeded)?;
        let expires_at = retention
            .expires_at(received_at)
            .map_err(|_| RelayDatabaseError::TimestampOutOfRange)?;
        let received_at = database_timestamp(received_at)?;
        let expires_at = database_timestamp(expires_at)?;
        let capability_digest = capability_digest(capability)?;
        let transaction = self.connection.transaction()?;
        let (quota_bytes, used_bytes): (Vec<u8>, Vec<u8>) = transaction
            .query_row(
                "SELECT quota_bytes, used_bytes FROM relay_mailboxes
                 WHERE mailbox_id = ?1 AND capability_digest = ?2",
                params![
                    capability.mailbox_id().as_slice(),
                    capability_digest.as_slice(),
                ],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?
            .ok_or(RelayDatabaseError::InvalidCapability)?;
        let existing: Option<Vec<u8>> = transaction
            .query_row(
                "SELECT encoded_chunk FROM relay_attachment_chunks
                 WHERE mailbox_id = ?1 AND attachment_id = ?2 AND chunk_index = ?3",
                params![
                    capability.mailbox_id().as_slice(),
                    chunk.identifier().as_bytes().as_slice(),
                    i64::from(chunk.index()),
                ],
                |row| row.get(0),
            )
            .optional()?;
        if let Some(existing) = existing {
            if existing == encoded_chunk {
                transaction.commit()?;
                return Ok(false);
            }
            return Err(RelayDatabaseError::AttachmentChunkConflict);
        }
        let quota = MailboxQuota::new(parse_u64(&quota_bytes)?)
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        let mut tracker = MailboxQuotaTracker::new(quota);
        tracker
            .reserve(parse_u64(&used_bytes)?)
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        tracker
            .reserve(bytes)
            .map_err(|_| RelayDatabaseError::QuotaExceeded)?;
        transaction.execute(
            "INSERT INTO relay_attachment_chunks(
                 mailbox_id, attachment_id, chunk_index, encoded_chunk, received_at, expires_at
             ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                capability.mailbox_id().as_slice(),
                chunk.identifier().as_bytes().as_slice(),
                i64::from(chunk.index()),
                encoded_chunk,
                received_at,
                expires_at,
            ],
        )?;
        transaction.execute(
            "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
            params![
                tracker.used_bytes().to_be_bytes().as_slice(),
                capability.mailbox_id().as_slice(),
            ],
        )?;
        transaction.commit()?;
        Ok(true)
    }

    pub fn retrieve_attachment_chunk(
        &mut self,
        capability: &MailboxCapability,
        identifier: AttachmentIdentifier,
        index: u32,
        now: u64,
    ) -> Result<EncryptedAttachmentChunk, RelayDatabaseError> {
        let capability_digest = capability_digest(capability)?;
        let now = database_timestamp(now)?;
        if !self.is_registered_capability(capability, &capability_digest)? {
            return Err(RelayDatabaseError::InvalidCapability);
        }
        let (encoded_chunk, received_at, expires_at): (Vec<u8>, i64, i64) = self
            .connection
            .query_row(
                "SELECT relay_attachment_chunks.encoded_chunk,
                        relay_attachment_chunks.received_at,
                        relay_attachment_chunks.expires_at
                 FROM relay_attachment_chunks
                 JOIN relay_mailboxes USING(mailbox_id)
                 WHERE relay_attachment_chunks.mailbox_id = ?1
                   AND relay_mailboxes.capability_digest = ?2
                   AND attachment_id = ?3 AND chunk_index = ?4 AND expires_at > ?5",
                params![
                    capability.mailbox_id().as_slice(),
                    capability_digest.as_slice(),
                    identifier.as_bytes().as_slice(),
                    i64::from(index),
                    now,
                ],
                |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
            )
            .optional()?
            .ok_or(RelayDatabaseError::UnknownAttachmentChunk)?;
        match EncryptedAttachmentChunk::decode(&encoded_chunk) {
            Ok(chunk) if chunk.identifier() == identifier && chunk.index() == index => Ok(chunk),
            _ => {
                self.quarantine_attachment_chunk(
                    *capability.mailbox_id(),
                    identifier,
                    index,
                    &encoded_chunk,
                    received_at,
                    expires_at,
                    now,
                )?;
                Err(RelayDatabaseError::CorruptBlobQuarantined)
            }
        }
    }

    pub fn retrieve_envelopes(
        &mut self,
        capability: &MailboxCapability,
        after_sequence: Option<u64>,
        now: u64,
        limit: u16,
    ) -> Result<Vec<RelayEnvelope>, RelayDatabaseError> {
        if limit == 0 || limit > MAX_MAILBOX_RETRIEVAL_ENVELOPES {
            return Err(RelayDatabaseError::InvalidRetrievalLimit);
        }
        let capability_digest = capability_digest(capability)?;
        let now = database_timestamp(now)?;
        let after_sequence = after_sequence
            .map(database_timestamp)
            .transpose()?
            .unwrap_or(-1);
        let registered = self.connection.query_row(
            "SELECT EXISTS(
                 SELECT 1 FROM relay_mailboxes
                 WHERE mailbox_id = ?1 AND capability_digest = ?2
             )",
            params![
                capability.mailbox_id().as_slice(),
                capability_digest.as_slice(),
            ],
            |row| row.get::<_, bool>(0),
        )?;
        if !registered {
            return Err(RelayDatabaseError::InvalidCapability);
        }
        let rows = {
            let mut statement = self.connection.prepare(
                "SELECT sequence, ciphertext, received_at, expires_at FROM relay_envelopes
                 WHERE mailbox_id = ?1 AND sequence > ?2 AND expires_at > ?3
                 ORDER BY sequence ASC LIMIT ?4",
            )?;
            statement
                .query_map(
                    params![
                        capability.mailbox_id().as_slice(),
                        after_sequence,
                        now,
                        i64::from(limit),
                    ],
                    |row| {
                        Ok((
                            row.get::<_, i64>(0)?,
                            row.get::<_, Vec<u8>>(1)?,
                            row.get::<_, i64>(2)?,
                            row.get::<_, i64>(3)?,
                        ))
                    },
                )?
                .collect::<std::result::Result<Vec<_>, _>>()?
        };
        let mut envelopes = Vec::with_capacity(rows.len());
        for (sequence, ciphertext, received_at, expires_at) in rows {
            let envelope = EncryptedMessageEnvelope::decode(&ciphertext);
            let invalid =
                sequence < 0 || received_at < 0 || expires_at <= received_at || envelope.is_err();
            if invalid {
                self.quarantine_envelope(
                    *capability.mailbox_id(),
                    sequence,
                    &ciphertext,
                    received_at,
                    expires_at,
                    now,
                )?;
                return Err(RelayDatabaseError::CorruptBlobQuarantined);
            }
            envelopes.push(RelayEnvelope {
                sequence: sequence as u64,
                envelope: match envelope {
                    Ok(envelope) => envelope,
                    Err(_) => return Err(RelayDatabaseError::CorruptBlobQuarantined),
                },
                received_at: received_at as u64,
                expires_at: expires_at as u64,
            });
        }
        Ok(envelopes)
    }

    fn quarantine_envelope(
        &mut self,
        mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
        sequence: i64,
        ciphertext: &[u8],
        received_at: i64,
        expires_at: i64,
        quarantined_at: i64,
    ) -> Result<(), RelayDatabaseError> {
        let transaction = self.connection.transaction()?;
        transaction.execute(
            "INSERT INTO relay_quarantined_envelopes(
                 mailbox_id, sequence, ciphertext, received_at, expires_at, quarantined_at
             ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)
             ON CONFLICT(mailbox_id, sequence) DO NOTHING",
            params![
                mailbox_id.as_slice(),
                sequence,
                ciphertext,
                received_at,
                expires_at,
                quarantined_at,
            ],
        )?;
        if transaction.execute(
            "DELETE FROM relay_envelopes WHERE mailbox_id = ?1 AND sequence = ?2",
            params![mailbox_id.as_slice(), sequence],
        )? != 1
        {
            return Err(RelayDatabaseError::InvalidMailboxRecord);
        }
        let used_bytes = active_mailbox_usage(&transaction, mailbox_id)?;
        transaction.execute(
            "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
            params![used_bytes.to_be_bytes().as_slice(), mailbox_id.as_slice()],
        )?;
        transaction.commit()?;
        Ok(())
    }

    fn quarantine_attachment_chunk(
        &mut self,
        mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
        identifier: AttachmentIdentifier,
        index: u32,
        encoded_chunk: &[u8],
        received_at: i64,
        expires_at: i64,
        quarantined_at: i64,
    ) -> Result<(), RelayDatabaseError> {
        let transaction = self.connection.transaction()?;
        transaction.execute(
            "INSERT INTO relay_quarantined_attachment_chunks(
                 mailbox_id, attachment_id, chunk_index, encoded_chunk,
                 received_at, expires_at, quarantined_at
             ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
             ON CONFLICT(mailbox_id, attachment_id, chunk_index) DO NOTHING",
            params![
                mailbox_id.as_slice(),
                identifier.as_bytes().as_slice(),
                i64::from(index),
                encoded_chunk,
                received_at,
                expires_at,
                quarantined_at,
            ],
        )?;
        if transaction.execute(
            "DELETE FROM relay_attachment_chunks
             WHERE mailbox_id = ?1 AND attachment_id = ?2 AND chunk_index = ?3",
            params![
                mailbox_id.as_slice(),
                identifier.as_bytes().as_slice(),
                i64::from(index),
            ],
        )? != 1
        {
            return Err(RelayDatabaseError::InvalidMailboxRecord);
        }
        let used_bytes = active_mailbox_usage(&transaction, mailbox_id)?;
        transaction.execute(
            "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
            params![used_bytes.to_be_bytes().as_slice(), mailbox_id.as_slice()],
        )?;
        transaction.commit()?;
        Ok(())
    }

    pub fn acknowledge_envelope(
        &mut self,
        capability: &MailboxCapability,
        sequence: u64,
    ) -> Result<(), RelayDatabaseError> {
        let capability_digest = capability_digest(capability)?;
        let sequence = database_timestamp(sequence)?;
        let transaction = self.connection.transaction()?;
        let (quota_bytes, used_bytes): (Vec<u8>, Vec<u8>) = transaction
            .query_row(
                "SELECT quota_bytes, used_bytes FROM relay_mailboxes
                 WHERE mailbox_id = ?1 AND capability_digest = ?2",
                params![
                    capability.mailbox_id().as_slice(),
                    capability_digest.as_slice(),
                ],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?
            .ok_or(RelayDatabaseError::InvalidCapability)?;
        let ciphertext: Vec<u8> = transaction
            .query_row(
                "SELECT ciphertext FROM relay_envelopes WHERE mailbox_id = ?1 AND sequence = ?2",
                params![capability.mailbox_id().as_slice(), sequence],
                |row| row.get(0),
            )
            .optional()?
            .ok_or(RelayDatabaseError::UnknownEnvelope)?;
        let quota = MailboxQuota::new(parse_u64(&quota_bytes)?)
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        let mut tracker = MailboxQuotaTracker::new(quota);
        tracker
            .reserve(parse_u64(&used_bytes)?)
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        tracker
            .release(
                u64::try_from(ciphertext.len())
                    .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?,
            )
            .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)?;
        transaction.execute(
            "DELETE FROM relay_envelopes WHERE mailbox_id = ?1 AND sequence = ?2",
            params![capability.mailbox_id().as_slice(), sequence],
        )?;
        transaction.execute(
            "UPDATE relay_mailboxes SET used_bytes = ?1 WHERE mailbox_id = ?2",
            params![
                tracker.used_bytes().to_be_bytes().as_slice(),
                capability.mailbox_id().as_slice(),
            ],
        )?;
        transaction.commit()?;
        Ok(())
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
    #[error("mailbox is already registered")]
    MailboxAlreadyRegistered,
    #[error("mailbox capability is invalid")]
    InvalidCapability,
    #[error("mailbox record is invalid")]
    InvalidMailboxRecord,
    #[error("encrypted relay envelope is invalid")]
    InvalidEnvelope,
    #[error("encrypted relay attachment chunk is invalid")]
    InvalidAttachmentChunk,
    #[error("corrupted relay blob was quarantined")]
    CorruptBlobQuarantined,
    #[error("relay attachment chunk conflicts with an existing chunk")]
    AttachmentChunkConflict,
    #[error("relay attachment chunk is unknown")]
    UnknownAttachmentChunk,
    #[error("mailbox quota exceeded")]
    QuotaExceeded,
    #[error("relay timestamp is out of range")]
    TimestampOutOfRange,
    #[error("mailbox envelope sequence is exhausted")]
    SequenceExhausted,
    #[error("mailbox retrieval limit is invalid")]
    InvalidRetrievalLimit,
    #[error("mailbox envelope is unknown")]
    UnknownEnvelope,
    #[error("relay storage receipt could not be issued")]
    Receipt,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayGarbageCollection {
    removed_mailboxes: u64,
    removed_envelopes: u64,
    reclaimed_bytes: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayAttachmentGarbageCollection {
    removed_chunks: u64,
    reclaimed_bytes: u64,
}

impl RelayAttachmentGarbageCollection {
    #[must_use]
    pub const fn removed_chunks(self) -> u64 {
        self.removed_chunks
    }

    #[must_use]
    pub const fn reclaimed_bytes(self) -> u64 {
        self.reclaimed_bytes
    }
}

impl RelayGarbageCollection {
    #[must_use]
    pub const fn removed_mailboxes(self) -> u64 {
        self.removed_mailboxes
    }

    #[must_use]
    pub const fn removed_envelopes(self) -> u64 {
        self.removed_envelopes
    }

    #[must_use]
    pub const fn reclaimed_bytes(self) -> u64 {
        self.reclaimed_bytes
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RelayEnvelope {
    sequence: u64,
    envelope: EncryptedMessageEnvelope,
    received_at: u64,
    expires_at: u64,
}

impl RelayEnvelope {
    #[must_use]
    pub const fn sequence(&self) -> u64 {
        self.sequence
    }

    #[must_use]
    pub const fn envelope(&self) -> &EncryptedMessageEnvelope {
        &self.envelope
    }

    #[must_use]
    pub const fn received_at(&self) -> u64 {
        self.received_at
    }

    #[must_use]
    pub const fn expires_at(&self) -> u64 {
        self.expires_at
    }
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
    if current_version < 1 {
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
            "INSERT INTO relay_schema_migrations(version) VALUES (1)",
            [],
        )?;
        transaction.commit()?;
    }
    if current_version < 2 {
        let transaction = connection.transaction()?;
        transaction.execute_batch(
            "ALTER TABLE relay_mailboxes RENAME COLUMN capability_token TO capability_digest;",
        )?;
        transaction.execute(
            "INSERT INTO relay_schema_migrations(version) VALUES (2)",
            [],
        )?;
        transaction.commit()?;
    }
    if current_version < 3 {
        let transaction = connection.transaction()?;
        transaction.execute_batch(
            "CREATE TABLE relay_attachment_chunks(
             mailbox_id BLOB NOT NULL CHECK(length(mailbox_id) = 16),
             attachment_id BLOB NOT NULL CHECK(length(attachment_id) = 16),
             chunk_index INTEGER NOT NULL CHECK(chunk_index >= 0),
             encoded_chunk BLOB NOT NULL CHECK(length(encoded_chunk) > 0),
             received_at INTEGER NOT NULL CHECK(received_at >= 0),
             expires_at INTEGER NOT NULL CHECK(expires_at > received_at),
             PRIMARY KEY(mailbox_id, attachment_id, chunk_index),
             FOREIGN KEY(mailbox_id) REFERENCES relay_mailboxes(mailbox_id) ON DELETE CASCADE
         ) STRICT;
         CREATE INDEX relay_attachment_chunks_expiration ON relay_attachment_chunks(expires_at);
         INSERT INTO relay_schema_migrations(version) VALUES (3);",
        )?;
        transaction.commit()?;
    }
    if current_version < 4 {
        let transaction = connection.transaction()?;
        transaction.execute_batch(
            "CREATE TABLE relay_quarantined_envelopes(
             mailbox_id BLOB NOT NULL CHECK(length(mailbox_id) = 16),
             sequence INTEGER NOT NULL,
             ciphertext BLOB NOT NULL CHECK(length(ciphertext) > 0),
             received_at INTEGER NOT NULL,
             expires_at INTEGER NOT NULL,
             quarantined_at INTEGER NOT NULL CHECK(quarantined_at >= 0),
             PRIMARY KEY(mailbox_id, sequence)
         ) STRICT;
         CREATE TABLE relay_quarantined_attachment_chunks(
             mailbox_id BLOB NOT NULL CHECK(length(mailbox_id) = 16),
             attachment_id BLOB NOT NULL CHECK(length(attachment_id) = 16),
             chunk_index INTEGER NOT NULL CHECK(chunk_index >= 0),
             encoded_chunk BLOB NOT NULL CHECK(length(encoded_chunk) > 0),
             received_at INTEGER NOT NULL,
             expires_at INTEGER NOT NULL,
             quarantined_at INTEGER NOT NULL CHECK(quarantined_at >= 0),
             PRIMARY KEY(mailbox_id, attachment_id, chunk_index)
         ) STRICT;
         INSERT INTO relay_schema_migrations(version) VALUES (4);",
        )?;
        transaction.commit()?;
    }
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

fn count_rows(connection: &Connection, table: &str) -> Result<u64, RelayDatabaseError> {
    let query = match table {
        "relay_mailboxes" => "SELECT COUNT(*) FROM relay_mailboxes",
        "relay_envelopes" => "SELECT COUNT(*) FROM relay_envelopes",
        _ => return Err(RelayDatabaseError::InvalidMailboxRecord),
    };
    let count = connection.query_row(query, [], |row| row.get::<_, i64>(0))?;
    u64::try_from(count).map_err(|_| RelayDatabaseError::InvalidMailboxRecord)
}

fn active_mailbox_usage(
    transaction: &Transaction<'_>,
    mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
) -> Result<u64, RelayDatabaseError> {
    let used_bytes = transaction.query_row(
        "SELECT COALESCE((SELECT SUM(length(ciphertext)) FROM relay_envelopes
                           WHERE mailbox_id = ?1), 0)
         + COALESCE((SELECT SUM(length(encoded_chunk)) FROM relay_attachment_chunks
                     WHERE mailbox_id = ?1), 0)",
        [mailbox_id.as_slice()],
        |row| row.get::<_, i64>(0),
    )?;
    u64::try_from(used_bytes).map_err(|_| RelayDatabaseError::InvalidMailboxRecord)
}

fn mailbox_registration_input(
    capability: &MailboxCapability,
    quota: MailboxQuota,
    created_at: u64,
) -> Result<[u8; 32], ProjectTestRelayError> {
    let capability = capability
        .encode()
        .map_err(|_| ProjectTestRelayError::InvalidCapability)?;
    synthetic_traffic_input(
        1,
        &[
            &capability,
            &quota.bytes().to_be_bytes(),
            &created_at.to_be_bytes(),
        ],
    )
}

fn envelope_insertion_input(
    capability: &MailboxCapability,
    envelope: &EncryptedMessageEnvelope,
    received_at: u64,
    retention: RelayRetentionPolicy,
) -> Result<[u8; 32], ProjectTestRelayError> {
    let capability = capability
        .encode()
        .map_err(|_| ProjectTestRelayError::InvalidCapability)?;
    let envelope = envelope
        .encode()
        .map_err(|_| ProjectTestRelayError::InvalidEnvelope)?;
    synthetic_traffic_input(
        2,
        &[
            &capability,
            &envelope,
            &received_at.to_be_bytes(),
            &retention.ttl_seconds().to_be_bytes(),
        ],
    )
}

fn envelope_retrieval_input(
    capability: &MailboxCapability,
    after_sequence: Option<u64>,
    now: u64,
    limit: u16,
) -> Result<[u8; 32], ProjectTestRelayError> {
    let capability = capability
        .encode()
        .map_err(|_| ProjectTestRelayError::InvalidCapability)?;
    let mut after_sequence_field = [0; 9];
    if let Some(after_sequence) = after_sequence {
        after_sequence_field[0] = 1;
        after_sequence_field[1..].copy_from_slice(&after_sequence.to_be_bytes());
    }
    synthetic_traffic_input(
        3,
        &[
            &capability,
            &after_sequence_field,
            &now.to_be_bytes(),
            &limit.to_be_bytes(),
        ],
    )
}

fn envelope_acknowledgement_input(
    capability: &MailboxCapability,
    sequence: u64,
) -> Result<[u8; 32], ProjectTestRelayError> {
    let capability = capability
        .encode()
        .map_err(|_| ProjectTestRelayError::InvalidCapability)?;
    synthetic_traffic_input(4, &[&capability, &sequence.to_be_bytes()])
}

fn synthetic_traffic_input(
    operation: u8,
    fields: &[&[u8]],
) -> Result<[u8; 32], ProjectTestRelayError> {
    let mut hasher = Sha256::new();
    hasher.update(CryptoDomain::ProjectTestRelaySyntheticTraffic.context());
    hasher.update([operation]);
    for field in fields {
        let length = u64::try_from(field.len())
            .map_err(|_| ProjectTestRelayError::InvalidSyntheticTraffic)?;
        hasher.update(length.to_be_bytes());
        hasher.update(field);
    }
    Ok(hasher.finalize().into())
}

fn capability_digest(capability: &MailboxCapability) -> Result<[u8; 32], RelayDatabaseError> {
    let encoded = capability
        .encode()
        .map_err(|_| RelayDatabaseError::InvalidCapability)?;
    Ok(Sha256::digest(encoded).into())
}

fn parse_u64(value: &[u8]) -> Result<u64, RelayDatabaseError> {
    value
        .try_into()
        .map(u64::from_be_bytes)
        .map_err(|_| RelayDatabaseError::InvalidMailboxRecord)
}

fn database_timestamp(value: u64) -> Result<i64, RelayDatabaseError> {
    i64::try_from(value).map_err(|_| RelayDatabaseError::TimestampOutOfRange)
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
    use std::convert::Infallible;

    use super::{
        MAX_MAILBOX_RETRIEVAL_ENVELOPES, MAX_RELAY_INGRESS_REQUESTS_PER_WINDOW,
        MAX_RELAY_INGRESS_WINDOW_SECONDS, MAX_RELAY_RETENTION_TTL_SECONDS, MailboxIngress,
        MailboxIngressError, MailboxQuota, MailboxQuotaError, MailboxQuotaTracker,
        ProjectTestRelay, ProjectTestRelayError, RELAY_HEALTH_PATH, RELAY_SCHEMA_VERSION,
        RateLimitedRelay, RelayAttachmentGarbageCollection, RelayDatabase, RelayDatabaseError,
        RelayGarbageCollection, RelayHealthEndpoint, RelayHealthEndpointError, RelayIdentity,
        RelayIngressError, RelayIngressRateLimit, RelayIngressRateLimitError, RelayMetricsEmitter,
        RelayOperationalMetrics, RelayRetentionPolicy, RelayRetentionPolicyError,
        SelfHostedRelayConfig, SelfHostedRelayConfigError, SyntheticRelayTrafficProof,
    };
    use rusqlite::Connection;
    use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore, RelaySigningKeypair};
    use yeokcham_protocol::{
        ATTACHMENT_CHUNK_BYTES, AttachmentDownloadJournal, AttachmentIdentifier, AttachmentKey,
        AttachmentManifest, AttachmentUploadJournal, EncryptedAttachmentChunk,
        EncryptedAttachmentManifest, EncryptedMessageEnvelope, MAILBOX_CAPABILITY_TOKEN_BYTES,
        MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
    };

    fn capability() -> MailboxCapability {
        capability_with(0x11, 0x22)
    }

    fn capability_with(mailbox_byte: u8, token_byte: u8) -> MailboxCapability {
        MailboxCapability::new(
            [mailbox_byte; MAILBOX_IDENTIFIER_BYTES],
            [token_byte; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
    }

    fn envelope() -> EncryptedMessageEnvelope {
        EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc3]).unwrap()
    }

    fn attachment_chunk(identifier: AttachmentIdentifier, index: u32) -> EncryptedAttachmentChunk {
        let key = AttachmentKey::derive(&[0x44; 32], identifier)
            .unwrap()
            .derive_chunk_key(index)
            .unwrap();
        EncryptedAttachmentChunk::encrypt(
            identifier,
            index,
            &key,
            &vec![u8::try_from(index).unwrap(); ATTACHMENT_CHUNK_BYTES],
        )
        .unwrap()
    }

    #[derive(Default)]
    struct MetricsEmitter(Vec<RelayOperationalMetrics>);

    impl RelayMetricsEmitter for MetricsEmitter {
        fn emit(&mut self, metrics: RelayOperationalMetrics) {
            self.0.push(metrics);
        }
    }

    #[derive(Default)]
    struct MemoryKeystore(Option<KeystoreSecret>);

    impl OsKeystore for MemoryKeystore {
        type Error = Infallible;

        fn load(&self, _: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .0
                .as_ref()
                .map(|secret| KeystoreSecret::new(secret.as_bytes().to_vec()).unwrap()))
        }

        fn store(
            &mut self,
            _: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.0 = Some(KeystoreSecret::new(secret.as_bytes().to_vec()).unwrap());
            Ok(())
        }

        fn delete(&mut self, _: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.0 = None;
            Ok(())
        }
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
    fn inserts_envelopes_and_usage_in_one_durable_transaction() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let envelope = envelope();
        let envelope_bytes = u64::try_from(envelope.encode().unwrap().len()).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(envelope_bytes).unwrap(), 100)
            .unwrap();

        assert_eq!(
            database
                .insert_envelope(
                    &capability,
                    &envelope,
                    100,
                    RelayRetentionPolicy::new(10).unwrap(),
                )
                .unwrap(),
            0
        );
        let stored = database
            .connection
            .query_row(
                "SELECT ciphertext, used_bytes, received_at, expires_at FROM relay_envelopes
                 JOIN relay_mailboxes USING(mailbox_id)",
                [],
                |row| {
                    Ok((
                        row.get::<_, Vec<u8>>(0)?,
                        row.get::<_, Vec<u8>>(1)?,
                        row.get::<_, i64>(2)?,
                        row.get::<_, i64>(3)?,
                    ))
                },
            )
            .unwrap();
        assert_eq!(stored.0, envelope.encode().unwrap());
        assert_eq!(
            u64::from_be_bytes(stored.1.try_into().unwrap()),
            envelope_bytes
        );
        assert_eq!((stored.2, stored.3), (100, 110));
    }

    #[test]
    fn rejects_unauthorized_or_over_quota_insertions_without_writing() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let envelope = envelope();
        let envelope_bytes = u64::try_from(envelope.encode().unwrap().len()).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(envelope_bytes).unwrap(), 100)
            .unwrap();
        let unauthorized = MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap();

        assert!(matches!(
            database.insert_envelope(
                &unauthorized,
                &envelope,
                100,
                RelayRetentionPolicy::new(10).unwrap(),
            ),
            Err(RelayDatabaseError::InvalidCapability)
        ));
        assert_eq!(
            database
                .insert_envelope(
                    &capability,
                    &envelope,
                    100,
                    RelayRetentionPolicy::new(10).unwrap(),
                )
                .unwrap(),
            0
        );
        assert!(matches!(
            database.insert_envelope(
                &capability,
                &envelope,
                100,
                RelayRetentionPolicy::new(10).unwrap(),
            ),
            Err(RelayDatabaseError::QuotaExceeded)
        ));
        let envelope_count = database
            .connection
            .query_row("SELECT COUNT(*) FROM relay_envelopes", [], |row| {
                row.get::<_, u8>(0)
            })
            .unwrap();
        assert_eq!(envelope_count, 1);
    }

    #[test]
    fn accounts_attachment_chunks_against_mailbox_quota_without_mutating_on_rejection() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let first = attachment_chunk(identifier, 0);
        let first_bytes = u64::try_from(first.encode().unwrap().len()).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(first_bytes).unwrap(), 100)
            .unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();

        assert!(
            database
                .store_attachment_chunk(&capability, &first, 100, retention)
                .unwrap()
        );
        assert_eq!(
            database
                .retrieve_attachment_chunk(&capability, identifier, 0, 109)
                .unwrap(),
            first
        );
        let unauthorized = capability_with(0x11, 0x33);
        assert!(matches!(
            database.retrieve_attachment_chunk(&unauthorized, identifier, 0, 109),
            Err(RelayDatabaseError::InvalidCapability)
        ));
        assert!(matches!(
            database.store_attachment_chunk(
                &unauthorized,
                &attachment_chunk(identifier, 1),
                100,
                retention,
            ),
            Err(RelayDatabaseError::InvalidCapability)
        ));
        assert!(
            !database
                .store_attachment_chunk(&capability, &first, 100, retention)
                .unwrap()
        );
        assert!(matches!(
            database.store_attachment_chunk(
                &capability,
                &attachment_chunk(identifier, 0),
                100,
                retention,
            ),
            Err(RelayDatabaseError::AttachmentChunkConflict)
        ));
        assert!(matches!(
            database.store_attachment_chunk(
                &capability,
                &attachment_chunk(identifier, 1),
                100,
                retention,
            ),
            Err(RelayDatabaseError::QuotaExceeded)
        ));
        let (stored_chunks, used_bytes): (u64, Vec<u8>) = database
            .connection
            .query_row(
                "SELECT COUNT(*), used_bytes FROM relay_attachment_chunks
                 JOIN relay_mailboxes USING(mailbox_id)",
                [],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .unwrap();
        assert_eq!(stored_chunks, 1);
        assert_eq!(
            u64::from_be_bytes(used_bytes.try_into().unwrap()),
            first_bytes
        );
    }

    #[test]
    fn retries_identical_attachment_chunks_without_charging_or_extending_retention() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let chunk = attachment_chunk(identifier, 0);
        let chunk_bytes = u64::try_from(chunk.encode().unwrap().len()).unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(chunk_bytes).unwrap(), 100)
            .unwrap();
        assert!(
            database
                .store_attachment_chunk(&capability, &chunk, 100, retention)
                .unwrap()
        );
        assert!(
            !database
                .store_attachment_chunk(&capability, &chunk, 109, retention)
                .unwrap()
        );
        assert!(matches!(
            database.retrieve_attachment_chunk(&capability, identifier, 0, 110),
            Err(RelayDatabaseError::UnknownAttachmentChunk)
        ));
        let used_bytes: Vec<u8> = database
            .connection
            .query_row(
                "SELECT used_bytes FROM relay_mailboxes WHERE mailbox_id = ?1",
                [capability.mailbox_id().as_slice()],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(
            u64::from_be_bytes(used_bytes.try_into().unwrap()),
            chunk_bytes
        );
    }

    #[test]
    fn quarantines_corrupted_envelopes_and_attachment_chunks() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let envelope = envelope();
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let chunk = attachment_chunk(identifier, 0);
        let envelope_bytes = u64::try_from(envelope.encode().unwrap().len()).unwrap();
        let chunk_bytes = u64::try_from(chunk.encode().unwrap().len()).unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database
            .register_mailbox(
                &capability,
                MailboxQuota::new(envelope_bytes + chunk_bytes).unwrap(),
                100,
            )
            .unwrap();
        database
            .insert_envelope(&capability, &envelope, 100, retention)
            .unwrap();
        database
            .store_attachment_chunk(&capability, &chunk, 100, retention)
            .unwrap();
        database
            .connection
            .execute("UPDATE relay_envelopes SET ciphertext = x'80'", [])
            .unwrap();

        assert!(matches!(
            database.retrieve_envelopes(&capability, None, 101, 1),
            Err(RelayDatabaseError::CorruptBlobQuarantined)
        ));
        let quarantined_envelope: Vec<u8> = database
            .connection
            .query_row(
                "SELECT ciphertext FROM relay_quarantined_envelopes",
                [],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(quarantined_envelope, vec![0x80]);
        database
            .connection
            .execute(
                "UPDATE relay_attachment_chunks SET encoded_chunk = x'80'",
                [],
            )
            .unwrap();
        assert!(matches!(
            database.retrieve_attachment_chunk(&capability, identifier, 0, 101),
            Err(RelayDatabaseError::CorruptBlobQuarantined)
        ));
        let (active_envelopes, active_chunks, quarantined_envelopes, quarantined_chunks): (
            u64,
            u64,
            u64,
            u64,
        ) = database
            .connection
            .query_row(
                "SELECT
                     (SELECT COUNT(*) FROM relay_envelopes),
                     (SELECT COUNT(*) FROM relay_attachment_chunks),
                     (SELECT COUNT(*) FROM relay_quarantined_envelopes),
                     (SELECT COUNT(*) FROM relay_quarantined_attachment_chunks)",
                [],
                |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
            )
            .unwrap();
        assert_eq!(
            (
                active_envelopes,
                active_chunks,
                quarantined_envelopes,
                quarantined_chunks
            ),
            (0, 0, 1, 1)
        );
        let used_bytes: Vec<u8> = database
            .connection
            .query_row(
                "SELECT used_bytes FROM relay_mailboxes WHERE mailbox_id = ?1",
                [capability.mailbox_id().as_slice()],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(u64::from_be_bytes(used_bytes.try_into().unwrap()), 0);
    }

    #[test]
    fn delivers_an_encrypted_message_and_attachment_end_to_end() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let attachment_key = AttachmentKey::derive(&[0x44; 32], identifier).unwrap();
        let plaintext = vec![0x5a; ATTACHMENT_CHUNK_BYTES];
        let chunk = EncryptedAttachmentChunk::encrypt(
            identifier,
            0,
            &attachment_key.derive_chunk_key(0).unwrap(),
            &plaintext,
        )
        .unwrap();
        let manifest = AttachmentManifest::new(
            identifier,
            u64::try_from(plaintext.len()).unwrap(),
            vec![chunk.hash().unwrap()],
        )
        .unwrap();
        let encrypted_manifest = manifest.encrypt(&attachment_key).unwrap();
        let envelope =
            EncryptedMessageEnvelope::new(vec![0xa1], encrypted_manifest.encode().unwrap())
                .unwrap();
        let envelope_bytes = u64::try_from(envelope.encode().unwrap().len()).unwrap();
        let chunk_bytes = u64::try_from(chunk.encode().unwrap().len()).unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database
            .register_mailbox(
                &capability,
                MailboxQuota::new(envelope_bytes + chunk_bytes).unwrap(),
                100,
            )
            .unwrap();
        let mut upload = AttachmentUploadJournal::new(std::slice::from_ref(&chunk)).unwrap();
        let sequence = database
            .insert_envelope(&capability, &envelope, 100, retention)
            .unwrap();
        assert!(
            database
                .store_attachment_chunk(&capability, &chunk, 100, retention)
                .unwrap()
        );
        assert!(upload.mark_uploaded(&chunk).unwrap());
        assert!(upload.is_complete());

        let delivered = database
            .retrieve_envelopes(&capability, None, 101, 1)
            .unwrap();
        assert_eq!(delivered.len(), 1);
        assert_eq!(delivered[0].sequence(), sequence);
        let received_manifest =
            EncryptedAttachmentManifest::decode(delivered[0].envelope().ciphertext())
                .unwrap()
                .decrypt(&attachment_key)
                .unwrap();
        assert_eq!(received_manifest, manifest);
        let mut download = AttachmentDownloadJournal::new(
            received_manifest.identifier(),
            received_manifest.chunk_hashes(),
        )
        .unwrap();
        let received_chunk = database
            .retrieve_attachment_chunk(&capability, identifier, 0, 101)
            .unwrap();
        assert!(download.mark_downloaded(&received_chunk).unwrap());
        assert!(download.is_complete());
        assert_eq!(
            received_chunk
                .decrypt(&attachment_key.derive_chunk_key(0).unwrap())
                .unwrap()
                .as_slice(),
            plaintext
        );
        database
            .acknowledge_envelope(&capability, sequence)
            .unwrap();
        assert!(
            database
                .retrieve_envelopes(&capability, None, 101, 1)
                .unwrap()
                .is_empty()
        );
    }

    #[test]
    fn retrieves_unexpired_envelopes_in_sequence_order() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let first = envelope();
        let second = EncryptedMessageEnvelope::new(vec![0xd4], vec![0xe5]).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(100).unwrap(), 100)
            .unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database
            .insert_envelope(&capability, &first, 100, retention)
            .unwrap();
        database
            .insert_envelope(&capability, &second, 100, retention)
            .unwrap();

        let retrieved = database
            .retrieve_envelopes(&capability, None, 109, 2)
            .unwrap();
        assert_eq!(retrieved.len(), 2);
        assert_eq!(retrieved[0].sequence(), 0);
        assert_eq!(retrieved[0].envelope(), &first);
        assert_eq!(retrieved[0].received_at(), 100);
        assert_eq!(retrieved[0].expires_at(), 110);
        assert_eq!(retrieved[1].sequence(), 1);
        assert_eq!(retrieved[1].envelope(), &second);
        assert_eq!(
            database
                .retrieve_envelopes(&capability, Some(0), 109, 1)
                .unwrap(),
            vec![retrieved[1].clone()]
        );
        assert!(
            database
                .retrieve_envelopes(&capability, None, 110, 2)
                .unwrap()
                .is_empty()
        );
    }

    #[test]
    fn rejects_unauthorized_or_unbounded_envelope_retrieval() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let unauthorized = MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap();

        assert!(matches!(
            database.retrieve_envelopes(&unauthorized, None, 100, 1),
            Err(RelayDatabaseError::InvalidCapability)
        ));
        assert!(matches!(
            database.retrieve_envelopes(&unauthorized, None, 100, 0),
            Err(RelayDatabaseError::InvalidRetrievalLimit)
        ));
        assert!(matches!(
            database.retrieve_envelopes(
                &unauthorized,
                None,
                100,
                MAX_MAILBOX_RETRIEVAL_ENVELOPES + 1,
            ),
            Err(RelayDatabaseError::InvalidRetrievalLimit)
        ));
    }

    #[test]
    fn acknowledges_one_envelope_and_releases_its_quota() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let envelope = envelope();
        let envelope_bytes = u64::try_from(envelope.encode().unwrap().len()).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(envelope_bytes).unwrap(), 100)
            .unwrap();
        database
            .insert_envelope(
                &capability,
                &envelope,
                100,
                RelayRetentionPolicy::new(10).unwrap(),
            )
            .unwrap();
        let unauthorized = MailboxCapability::new(
            [0x11; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap();

        assert!(matches!(
            database.acknowledge_envelope(&unauthorized, 0),
            Err(RelayDatabaseError::InvalidCapability)
        ));
        database.acknowledge_envelope(&capability, 0).unwrap();
        assert!(
            database
                .retrieve_envelopes(&capability, None, 100, 1)
                .unwrap()
                .is_empty()
        );
        assert!(matches!(
            database.acknowledge_envelope(&capability, 0),
            Err(RelayDatabaseError::UnknownEnvelope)
        ));
        assert_eq!(
            database
                .insert_envelope(
                    &capability,
                    &envelope,
                    100,
                    RelayRetentionPolicy::new(10).unwrap(),
                )
                .unwrap(),
            0
        );
    }

    #[test]
    fn issues_a_receipt_for_the_durably_stored_envelope() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let envelope = envelope();
        database
            .register_mailbox(&capability, MailboxQuota::new(100).unwrap(), 100)
            .unwrap();
        let relay = RelaySigningKeypair::generate().unwrap();

        let receipt = database
            .insert_envelope_with_receipt(
                &capability,
                &envelope,
                100,
                RelayRetentionPolicy::new(10).unwrap(),
                &relay,
            )
            .unwrap();

        assert_eq!(receipt.relay(), &relay.public_key());
        assert_eq!(receipt.mailbox_id(), capability.mailbox_id());
        assert_eq!(receipt.sequence(), 0);
        assert_eq!((receipt.received_at(), receipt.expires_at()), (100, 110));
        receipt.verify().unwrap();
    }

    #[test]
    fn persists_and_restores_the_relay_identity() {
        let mut keystore = MemoryKeystore::default();
        let first = RelayIdentity::load_or_generate(&mut keystore).unwrap();
        let second = RelayIdentity::load_or_generate(&mut keystore).unwrap();

        assert_eq!(
            first.signing_keypair().public_key(),
            second.signing_keypair().public_key()
        );
    }

    #[test]
    fn validates_self_hosted_relay_configuration() {
        let quota = MailboxQuota::new(1024).unwrap();
        let retention = RelayRetentionPolicy::new(60).unwrap();
        let config = SelfHostedRelayConfig::new(
            "127.0.0.1:9443".parse().unwrap(),
            "/var/lib/yeokcham/relay.sqlite".into(),
            quota,
            retention,
        )
        .unwrap();

        assert_eq!(config.listen_address(), "127.0.0.1:9443".parse().unwrap());
        assert_eq!(
            config.database_path(),
            std::path::Path::new("/var/lib/yeokcham/relay.sqlite")
        );
        assert_eq!(config.mailbox_quota(), quota);
        assert_eq!(config.retention(), retention);
        assert_eq!(
            SelfHostedRelayConfig::new(
                "127.0.0.1:0".parse().unwrap(),
                "/var/lib/yeokcham/relay.sqlite".into(),
                quota,
                retention,
            )
            .unwrap_err(),
            SelfHostedRelayConfigError::ZeroListenPort
        );
        assert_eq!(
            SelfHostedRelayConfig::new(
                "127.0.0.1:9443".parse().unwrap(),
                "relay.sqlite".into(),
                quota,
                retention,
            )
            .unwrap_err(),
            SelfHostedRelayConfigError::InvalidDatabasePath
        );
    }

    #[test]
    fn exposes_loopback_only_redacted_health_response() {
        let database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let endpoint = RelayHealthEndpoint::new("127.0.0.1:8080".parse().unwrap()).unwrap();

        assert_eq!(endpoint.response(RELAY_HEALTH_PATH, &database), Ok("ok\n"));
        assert_eq!(
            endpoint.response("/metrics", &database),
            Err(RelayHealthEndpointError::UnknownPath)
        );
        assert_eq!(
            RelayHealthEndpoint::new("192.0.2.1:8080".parse().unwrap()),
            Err(RelayHealthEndpointError::NonLoopbackAddress)
        );
    }

    #[test]
    fn emits_only_aggregate_relay_operational_metrics() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        database
            .register_mailbox(&capability, MailboxQuota::new(1024).unwrap(), 1)
            .unwrap();
        database
            .insert_envelope(
                &capability,
                &envelope(),
                2,
                RelayRetentionPolicy::new(60).unwrap(),
            )
            .unwrap();
        let mut emitter = MetricsEmitter::default();

        database.emit_operational_metrics(&mut emitter).unwrap();

        assert_eq!(
            emitter.0,
            vec![RelayOperationalMetrics {
                registered_mailboxes: 1,
                stored_envelopes: 1,
                stored_bytes: 6,
            }]
        );
    }

    #[test]
    fn project_test_relay_admits_only_authorized_synthetic_traffic() {
        let authority = RelaySigningKeypair::generate().unwrap();
        let unrelated_authority = RelaySigningKeypair::generate().unwrap();
        let database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let mut relay = ProjectTestRelay::new(database, authority.public_key());
        let capability = capability();
        let quota = MailboxQuota::new(1024).unwrap();
        let registration =
            SyntheticRelayTrafficProof::for_mailbox_registration(&authority, &capability, quota, 1)
                .unwrap();

        relay
            .register_mailbox(registration, &capability, quota, 1)
            .unwrap();
        let invalid_registration = SyntheticRelayTrafficProof::for_mailbox_registration(
            &unrelated_authority,
            &capability,
            quota,
            1,
        )
        .unwrap();
        assert!(matches!(
            relay.register_mailbox(invalid_registration, &capability, quota, 1),
            Err(ProjectTestRelayError::InvalidSyntheticTraffic)
        ));

        let envelope = envelope();
        let insertion = SyntheticRelayTrafficProof::for_envelope_insertion(
            &authority,
            &capability,
            &envelope,
            2,
            RelayRetentionPolicy::new(60).unwrap(),
        )
        .unwrap();
        assert_eq!(
            relay
                .insert_envelope(
                    insertion,
                    &capability,
                    &envelope,
                    2,
                    RelayRetentionPolicy::new(60).unwrap(),
                )
                .unwrap(),
            0
        );
        let tampered = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc4]).unwrap();
        assert!(matches!(
            relay.insert_envelope(
                insertion,
                &capability,
                &tampered,
                2,
                RelayRetentionPolicy::new(60).unwrap(),
            ),
            Err(ProjectTestRelayError::InvalidSyntheticTraffic)
        ));

        let retrieval =
            SyntheticRelayTrafficProof::for_envelope_retrieval(&authority, &capability, None, 3, 1)
                .unwrap();
        assert_eq!(
            relay
                .retrieve_envelopes(retrieval, &capability, None, 3, 1)
                .unwrap()
                .len(),
            1
        );
        let acknowledgement =
            SyntheticRelayTrafficProof::for_envelope_acknowledgement(&authority, &capability, 0)
                .unwrap();
        relay
            .acknowledge_envelope(acknowledgement, &capability, 0)
            .unwrap();
        assert!(format!("{insertion:?}").contains("REDACTED"));
    }

    #[test]
    fn garbage_collects_expired_mailboxes_and_repairs_retained_usage() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let expired = capability_with(0x33, 0x44);
        let active = capability_with(0x55, 0x66);
        let recently_registered = capability_with(0x77, 0x88);
        let quota = MailboxQuota::new(1024).unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database.register_mailbox(&expired, quota, 100).unwrap();
        database.register_mailbox(&active, quota, 100).unwrap();
        database
            .register_mailbox(&recently_registered, quota, 101)
            .unwrap();
        database
            .insert_envelope(&expired, &envelope(), 100, retention)
            .unwrap();
        database
            .insert_envelope(&active, &envelope(), 105, retention)
            .unwrap();

        assert_eq!(
            database
                .garbage_collect_expired_mailboxes(110, retention)
                .unwrap(),
            RelayGarbageCollection {
                removed_mailboxes: 1,
                removed_envelopes: 1,
                reclaimed_bytes: 6,
            }
        );
        assert!(matches!(
            database.retrieve_envelopes(&expired, None, 110, 1),
            Err(RelayDatabaseError::InvalidCapability)
        ));
        assert_eq!(
            database
                .retrieve_envelopes(&active, None, 110, 1)
                .unwrap()
                .len(),
            1
        );
        assert!(
            database
                .retrieve_envelopes(&recently_registered, None, 110, 1)
                .unwrap()
                .is_empty()
        );
        let active_usage = database
            .connection
            .query_row(
                "SELECT used_bytes FROM relay_mailboxes WHERE mailbox_id = ?1",
                [active.mailbox_id().as_slice()],
                |row| row.get::<_, Vec<u8>>(0),
            )
            .unwrap();
        assert_eq!(u64::from_be_bytes(active_usage.try_into().unwrap()), 6);
    }

    #[test]
    fn retains_mailboxes_with_unexpired_attachment_chunks() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let chunk = attachment_chunk(identifier, 0);
        let chunk_bytes = u64::try_from(chunk.encode().unwrap().len()).unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(chunk_bytes).unwrap(), 100)
            .unwrap();
        database
            .store_attachment_chunk(&capability, &chunk, 105, retention)
            .unwrap();

        assert_eq!(
            database
                .garbage_collect_expired_mailboxes(110, retention)
                .unwrap(),
            RelayGarbageCollection {
                removed_mailboxes: 0,
                removed_envelopes: 0,
                reclaimed_bytes: 0,
            }
        );
        assert_eq!(
            database
                .retrieve_attachment_chunk(&capability, identifier, 0, 110)
                .unwrap(),
            chunk
        );
    }

    #[test]
    fn garbage_collects_expired_attachment_chunks_and_releases_quota() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability();
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let expired = attachment_chunk(identifier, 0);
        let active = attachment_chunk(identifier, 1);
        let expired_bytes = u64::try_from(expired.encode().unwrap().len()).unwrap();
        let active_bytes = u64::try_from(active.encode().unwrap().len()).unwrap();
        let retention = RelayRetentionPolicy::new(10).unwrap();
        database
            .register_mailbox(
                &capability,
                MailboxQuota::new(expired_bytes + active_bytes).unwrap(),
                100,
            )
            .unwrap();
        database
            .store_attachment_chunk(&capability, &expired, 100, retention)
            .unwrap();
        database
            .store_attachment_chunk(&capability, &active, 105, retention)
            .unwrap();

        assert_eq!(
            database
                .garbage_collect_expired_attachment_chunks(110)
                .unwrap(),
            RelayAttachmentGarbageCollection {
                removed_chunks: 1,
                reclaimed_bytes: expired_bytes,
            }
        );
        assert!(matches!(
            database.retrieve_attachment_chunk(&capability, identifier, 0, 110),
            Err(RelayDatabaseError::UnknownAttachmentChunk)
        ));
        assert_eq!(
            database
                .retrieve_attachment_chunk(&capability, identifier, 1, 110)
                .unwrap(),
            active
        );
        let used_bytes: Vec<u8> = database
            .connection
            .query_row(
                "SELECT used_bytes FROM relay_mailboxes WHERE mailbox_id = ?1",
                [capability.mailbox_id().as_slice()],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(
            u64::from_be_bytes(used_bytes.try_into().unwrap()),
            active_bytes
        );
        assert_eq!(
            database
                .garbage_collect_expired_attachment_chunks(110)
                .unwrap(),
            RelayAttachmentGarbageCollection {
                removed_chunks: 0,
                reclaimed_bytes: 0,
            }
        );
    }

    #[test]
    fn rate_limits_relay_ingress_per_registered_capability() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let first_capability = capability_with(0x99, 0xaa);
        let second_capability = capability_with(0xbb, 0xcc);
        let quota = MailboxQuota::new(1024).unwrap();
        database
            .register_mailbox(&first_capability, quota, 0)
            .unwrap();
        database
            .register_mailbox(&second_capability, quota, 0)
            .unwrap();
        let mut relay = RateLimitedRelay::new(database, RelayIngressRateLimit::new(2, 10).unwrap());
        let retention = RelayRetentionPolicy::new(60).unwrap();
        let unregistered_capability = capability_with(0xdd, 0xee);

        assert!(matches!(
            relay.insert_envelope(&unregistered_capability, &envelope(), 100, retention),
            Err(RelayIngressError::InvalidCapability)
        ));

        relay
            .insert_envelope(&first_capability, &envelope(), 100, retention)
            .unwrap();
        relay
            .insert_envelope(&first_capability, &envelope(), 101, retention)
            .unwrap();
        assert!(matches!(
            relay.insert_envelope(&first_capability, &envelope(), 109, retention),
            Err(RelayIngressError::RateLimited)
        ));
        relay
            .insert_envelope(&second_capability, &envelope(), 109, retention)
            .unwrap();
        relay
            .insert_envelope(&first_capability, &envelope(), 110, retention)
            .unwrap();
        assert!(matches!(
            relay.insert_envelope(&second_capability, &envelope(), 108, retention),
            Err(RelayIngressError::TimestampRegression)
        ));
        let mut database = relay.into_database();
        assert_eq!(
            database
                .retrieve_envelopes(&first_capability, None, 110, 10)
                .unwrap()
                .len(),
            3
        );
        assert_eq!(
            database
                .retrieve_envelopes(&second_capability, None, 110, 10)
                .unwrap()
                .len(),
            1
        );
        assert_eq!(
            RelayIngressRateLimit::new(0, 10),
            Err(RelayIngressRateLimitError::InvalidMaxRequests)
        );
        assert_eq!(
            RelayIngressRateLimit::new(MAX_RELAY_INGRESS_REQUESTS_PER_WINDOW + 1, 10),
            Err(RelayIngressRateLimitError::InvalidMaxRequests)
        );
        assert_eq!(
            RelayIngressRateLimit::new(1, 0),
            Err(RelayIngressRateLimitError::InvalidWindow)
        );
        assert_eq!(
            RelayIngressRateLimit::new(1, MAX_RELAY_INGRESS_WINDOW_SECONDS + 1),
            Err(RelayIngressRateLimitError::InvalidWindow)
        );
    }

    #[test]
    fn abuse_rejections_leave_victim_mailbox_quota_intact() {
        let mut database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();
        let capability = capability_with(0x23, 0x45);
        let unauthorized = capability_with(0x23, 0x67);
        let envelope = envelope();
        let envelope_bytes = u64::try_from(envelope.encode().unwrap().len()).unwrap();
        database
            .register_mailbox(&capability, MailboxQuota::new(envelope_bytes).unwrap(), 0)
            .unwrap();
        let mut relay = RateLimitedRelay::new(database, RelayIngressRateLimit::new(1, 60).unwrap());
        let retention = RelayRetentionPolicy::new(120).unwrap();

        relay
            .insert_envelope(&capability, &envelope, 100, retention)
            .unwrap();
        for received_at in 101..105 {
            assert!(matches!(
                relay.insert_envelope(&capability, &envelope, received_at, retention),
                Err(RelayIngressError::RateLimited)
            ));
        }
        assert!(matches!(
            relay.insert_envelope(&unauthorized, &envelope, 105, retention),
            Err(RelayIngressError::InvalidCapability)
        ));
        assert!(matches!(
            relay.insert_envelope(&capability, &envelope, 160, retention),
            Err(RelayIngressError::Database(
                RelayDatabaseError::QuotaExceeded
            ))
        ));
        let database = relay.into_database();
        let (envelope_count, used_bytes): (u64, Vec<u8>) = database
            .connection
            .query_row(
                "SELECT COUNT(*), used_bytes FROM relay_envelopes
                 JOIN relay_mailboxes USING(mailbox_id)
                 WHERE mailbox_id = ?1",
                [capability.mailbox_id().as_slice()],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .unwrap();
        assert_eq!(envelope_count, 1);
        assert_eq!(
            u64::from_be_bytes(used_bytes.try_into().unwrap()),
            envelope_bytes
        );
    }

    #[test]
    fn migrates_an_empty_relay_database_to_the_current_schema() {
        let database =
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap();

        assert_eq!(database.schema_version().unwrap(), RELAY_SCHEMA_VERSION);
        let schema_version = database
            .connection
            .query_row(
                "SELECT MAX(version) FROM relay_schema_migrations",
                [],
                |row| row.get::<_, u32>(0),
            )
            .unwrap();
        assert_eq!(schema_version, RELAY_SCHEMA_VERSION);
        let table_count = database
            .connection
            .query_row(
                "SELECT COUNT(*) FROM sqlite_master
                 WHERE type = 'table' AND name IN (
                     'relay_mailboxes', 'relay_envelopes', 'relay_attachment_chunks',
                     'relay_quarantined_envelopes', 'relay_quarantined_attachment_chunks'
                 )",
                [],
                |row| row.get::<_, u8>(0),
            )
            .unwrap();
        assert_eq!(table_count, 5);
    }

    #[test]
    fn migrates_v1_capability_tokens_to_digests() {
        let connection = Connection::open_in_memory().unwrap();
        connection
            .execute_batch(
                "CREATE TABLE relay_schema_migrations(
                     version INTEGER PRIMARY KEY CHECK(version > 0)
                 ) STRICT;
                 INSERT INTO relay_schema_migrations(version) VALUES (1);
                 CREATE TABLE relay_mailboxes(
                     mailbox_id BLOB PRIMARY KEY NOT NULL CHECK(length(mailbox_id) = 16),
                     capability_token BLOB NOT NULL CHECK(length(capability_token) = 32),
                     quota_bytes BLOB NOT NULL CHECK(length(quota_bytes) = 8),
                     used_bytes BLOB NOT NULL CHECK(length(used_bytes) = 8),
                     created_at INTEGER NOT NULL CHECK(created_at >= 0)
                 ) STRICT;",
            )
            .unwrap();

        let database = RelayDatabase::from_connection(connection).unwrap();
        assert_eq!(database.schema_version().unwrap(), RELAY_SCHEMA_VERSION);
        let digest_column = database
            .connection
            .query_row(
                "SELECT COUNT(*) FROM pragma_table_info('relay_mailboxes')
                 WHERE name = 'capability_digest'",
                [],
                |row| row.get::<_, u8>(0),
            )
            .unwrap();
        assert_eq!(digest_column, 1);
    }

    #[test]
    fn rejects_databases_from_newer_schema_versions() {
        let connection = Connection::open_in_memory().unwrap();
        connection
            .execute_batch(
                "CREATE TABLE relay_schema_migrations(
                     version INTEGER PRIMARY KEY CHECK(version > 0)
                 ) STRICT;
                 INSERT INTO relay_schema_migrations(version) VALUES (5);",
            )
            .unwrap();

        assert!(matches!(
            RelayDatabase::from_connection(connection),
            Err(RelayDatabaseError::UnsupportedSchemaVersion(5))
        ));
    }
}
