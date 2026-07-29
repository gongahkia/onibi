use arachne_core::IdentityPublicKey;
use arachne_protocol::{ContactInvitation, ContactInvitationError};

use crate::{Contact, ContactStore, ContactStoreError};

pub struct PendingContactImportService<'a> {
    local_identity: &'a IdentityPublicKey,
    contacts: &'a mut ContactStore,
}

impl<'a> PendingContactImportService<'a> {
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

    pub fn import_encoded(&mut self, encoded: &[u8]) -> Result<Contact, PendingContactImportError> {
        let invitation =
            ContactInvitation::decode(encoded).map_err(PendingContactImportError::Invitation)?;
        self.contacts
            .import_invitation(self.local_identity, &invitation)
            .map_err(PendingContactImportError::ContactStore)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum PendingContactImportError {
    #[error("contact invitation is invalid")]
    Invitation(#[source] ContactInvitationError),
    #[error("pending-contact import failed")]
    ContactStore(#[source] ContactStoreError),
}
