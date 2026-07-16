use yeokcham_core::IdentityPublicKey;
use yeokcham_protocol::{QrVerificationError, QrVerificationPayload};

use crate::{Contact, ContactStore, ContactStoreError};

pub struct QrContactVerificationService<'a> {
    local_identity: &'a IdentityPublicKey,
    contacts: &'a mut ContactStore,
}

impl<'a> QrContactVerificationService<'a> {
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

    pub fn verify_encoded(
        &mut self,
        encoded: &[u8],
    ) -> Result<Contact, QrContactVerificationError> {
        let payload =
            QrVerificationPayload::decode(encoded).map_err(QrContactVerificationError::Payload)?;
        self.contacts
            .verify_qr(self.local_identity, &payload)
            .map_err(QrContactVerificationError::ContactStore)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum QrContactVerificationError {
    #[error("QR verification payload is invalid")]
    Payload(#[source] QrVerificationError),
    #[error("QR contact verification failed")]
    ContactStore(#[source] ContactStoreError),
}
