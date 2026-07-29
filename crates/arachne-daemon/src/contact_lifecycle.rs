use arachne_core::IdentityPublicKey;
use arachne_protocol::{IdentityRotation, IdentityRotationError};

use crate::{Contact, ContactStore, ContactStoreError};

pub struct ContactLifecycleService<'a> {
    local_identity: &'a IdentityPublicKey,
    contacts: &'a mut ContactStore,
}

impl<'a> ContactLifecycleService<'a> {
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

    pub fn apply_rotation_encoded(
        &mut self,
        encoded: &[u8],
    ) -> Result<Contact, ContactLifecycleError> {
        let rotation =
            IdentityRotation::decode(encoded).map_err(ContactLifecycleError::IdentityRotation)?;
        self.contacts
            .apply_identity_rotation(self.local_identity, &rotation)
            .map_err(ContactLifecycleError::ContactStore)
    }

    pub fn revoke(
        &mut self,
        identity: &IdentityPublicKey,
    ) -> Result<Contact, ContactLifecycleError> {
        self.contacts
            .revoke(identity)
            .map_err(ContactLifecycleError::ContactStore)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ContactLifecycleError {
    #[error("identity rotation is invalid")]
    IdentityRotation(#[source] IdentityRotationError),
    #[error("contact lifecycle update failed")]
    ContactStore(#[source] ContactStoreError),
}
