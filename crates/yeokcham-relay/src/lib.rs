#![forbid(unsafe_code)]

use yeokcham_core::{Error, Result};
use yeokcham_protocol::{
    MAILBOX_IDENTIFIER_BYTES, MAX_RELAY_INVITATION_TTL_SECONDS, MailboxCapability,
};

pub const MAX_RELAY_RETENTION_TTL_SECONDS: u32 = MAX_RELAY_INVITATION_TTL_SECONDS;

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
        MailboxQuotaError, MailboxQuotaTracker, RelayRetentionPolicy, RelayRetentionPolicyError,
    };
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
}
