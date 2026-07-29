use arachne_core::IdentityPublicKey;
use arachne_protocol::SAFETY_NUMBER_FINGERPRINT_BYTES;

use crate::{Contact, ContactStore, ContactStoreError};

pub struct SafetyNumberVerificationService<'a> {
    local_identity: &'a IdentityPublicKey,
    contacts: &'a mut ContactStore,
}

impl<'a> SafetyNumberVerificationService<'a> {
    #[must_use]
    pub const fn new(
        local_identity: &'a IdentityPublicKey,
        contacts: &'a mut ContactStore,
    ) -> Self {
        Self {
            local_identity,
            contacts,
        }
    }

    pub fn verify(
        &mut self,
        remote_identity: &IdentityPublicKey,
        supplied: &[u8],
    ) -> Result<Contact, SafetyNumberVerificationError> {
        let supplied: &[u8; SAFETY_NUMBER_FINGERPRINT_BYTES] = supplied
            .try_into()
            .map_err(|_| SafetyNumberVerificationError::InvalidFingerprintLength)?;
        self.contacts
            .verify_safety_number(self.local_identity, remote_identity, supplied)
            .map_err(SafetyNumberVerificationError::ContactStore)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SafetyNumberVerificationError {
    #[error("safety number has an invalid length")]
    InvalidFingerprintLength,
    #[error("safety-number verification failed")]
    ContactStore(#[source] ContactStoreError),
}
