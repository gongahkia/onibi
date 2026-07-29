use arachne_core::{IdentityPublicKey, RelaySigningKeypair};
use arachne_protocol::{
    MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability, RelayInvitation,
    RelayInvitationError, TorMaildropProfileConfig,
};
use getrandom::{SysRng, rand_core::TryRng};
use zeroize::Zeroizing;

use crate::{MailboxQuota, RelayDatabase, RelayDatabaseError};

pub const MAX_RELAY_INVITE_PROVISION_ATTEMPTS: u8 = 8;

pub struct RelayInviteProvisioner<'a> {
    database: RelayDatabase,
    relay: &'a RelaySigningKeypair,
    endpoint: TorMaildropProfileConfig,
    quota: MailboxQuota,
}

impl<'a> RelayInviteProvisioner<'a> {
    #[must_use]
    pub const fn new(
        database: RelayDatabase,
        relay: &'a RelaySigningKeypair,
        endpoint: TorMaildropProfileConfig,
        quota: MailboxQuota,
    ) -> Self {
        Self {
            database,
            relay,
            endpoint,
            quota,
        }
    }

    pub fn provision(
        &mut self,
        recipient: IdentityPublicKey,
        issued_at: u64,
        ttl_seconds: u32,
    ) -> Result<RelayInvitation, RelayInviteProvisioningError> {
        for _ in 0..MAX_RELAY_INVITE_PROVISION_ATTEMPTS {
            let capability = random_capability()?;
            let invitation = RelayInvitation::create(
                self.relay,
                recipient,
                self.endpoint,
                capability,
                issued_at,
                ttl_seconds,
            )
            .map_err(RelayInviteProvisioningError::Invitation)?;
            match self.database.register_mailbox(
                invitation.mailbox_capability(),
                self.quota,
                issued_at,
            ) {
                Ok(()) => return Ok(invitation),
                Err(RelayDatabaseError::MailboxAlreadyRegistered) => {}
                Err(error) => return Err(RelayInviteProvisioningError::Database(error)),
            }
        }
        Err(RelayInviteProvisioningError::MailboxIdentifierExhausted)
    }

    #[must_use]
    pub fn into_database(self) -> RelayDatabase {
        self.database
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RelayInviteProvisioningError {
    #[error("relay invite capability could not be generated")]
    Randomness,
    #[error("relay invite could not be signed")]
    Invitation(#[source] RelayInvitationError),
    #[error("relay invite mailbox could not be registered")]
    Database(#[source] RelayDatabaseError),
    #[error("relay invite mailbox identifier allocation was exhausted")]
    MailboxIdentifierExhausted,
}

fn random_capability() -> Result<MailboxCapability, RelayInviteProvisioningError> {
    let mut mailbox_id = [0; MAILBOX_IDENTIFIER_BYTES];
    let mut token = Zeroizing::new([0; MAILBOX_CAPABILITY_TOKEN_BYTES]);
    let mut random_source = SysRng;
    random_source
        .try_fill_bytes(&mut mailbox_id)
        .and_then(|()| random_source.try_fill_bytes(&mut token[..]))
        .map_err(|_| RelayInviteProvisioningError::Randomness)?;
    MailboxCapability::new(mailbox_id, *token).map_err(|_| RelayInviteProvisioningError::Randomness)
}

#[cfg(test)]
mod tests {
    use arachne_core::{IdentityKeypair, RelaySigningKeypair};
    use arachne_protocol::{
        RelayInvitation, RelayInvitationError, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
        TorMaildropProfileConfig,
    };
    use rusqlite::Connection;

    use super::{RelayInviteProvisioner, RelayInviteProvisioningError};
    use crate::{MailboxQuota, RelayDatabase, RelayMetricsEmitter, RelayOperationalMetrics};

    fn endpoint() -> TorMaildropProfileConfig {
        TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 443).unwrap()
    }

    fn database() -> RelayDatabase {
        RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap()
    }

    #[derive(Default)]
    struct Metrics(Option<RelayOperationalMetrics>);

    impl RelayMetricsEmitter for Metrics {
        fn emit(&mut self, metrics: RelayOperationalMetrics) {
            self.0 = Some(metrics);
        }
    }

    #[test]
    fn provisions_a_signed_recipient_bound_invite_and_registers_its_mailbox() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let mut provisioner = RelayInviteProvisioner::new(
            database(),
            &relay,
            endpoint(),
            MailboxQuota::new(1024).unwrap(),
        );
        let invitation = provisioner
            .provision(recipient.public_key(), 1_700_000_000, 3_600)
            .unwrap();
        let encoded = invitation.encode().unwrap();
        let decoded = RelayInvitation::decode(&encoded).unwrap();
        decoded
            .validate(&recipient.public_key(), 1_700_003_600)
            .unwrap();
        assert_eq!(decoded.relay(), &relay.public_key());
        assert_eq!(decoded.endpoint(), endpoint());

        let mut database = provisioner.into_database();
        assert!(
            database
                .retrieve_envelopes(decoded.mailbox_capability(), None, 1_700_000_000, 1)
                .unwrap()
                .is_empty()
        );
    }

    #[test]
    fn rejects_invalid_invites_without_registering_a_mailbox() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let mut provisioner = RelayInviteProvisioner::new(
            database(),
            &relay,
            endpoint(),
            MailboxQuota::new(1024).unwrap(),
        );
        assert!(matches!(
            provisioner.provision(recipient.public_key(), 1_700_000_000, 0),
            Err(RelayInviteProvisioningError::Invitation(
                RelayInvitationError::InvalidTtl
            ))
        ));
        let database = provisioner.into_database();
        let mut metrics = Metrics::default();
        database.emit_operational_metrics(&mut metrics).unwrap();
        assert_eq!(metrics.0.unwrap().registered_mailboxes(), 0);
    }
}
