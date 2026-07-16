use yeokcham_core::{IdentityPublicKey, OsKeystore};
use yeokcham_daemon::{
    Contact, ContactLifecycleError, ContactLifecycleService, ContactStatus, ContactStore,
    ContactStoreError, ContactVerificationMethod, PendingContactImportError,
    PendingContactImportService, QrContactVerificationError, QrContactVerificationService,
    SafetyNumberVerificationError, SafetyNumberVerificationService,
};

use crate::{SdkClient, SdkIdentityError, SdkIdentityManager};

pub struct SdkContactManager<'a> {
    _client: &'a SdkClient,
    contacts: ContactStore,
    local_identity: IdentityPublicKey,
}

impl<'a> SdkContactManager<'a> {
    pub(crate) fn open<K: OsKeystore>(
        client: &'a SdkClient,
        identity: &mut SdkIdentityManager<K>,
    ) -> Result<Self, SdkContactError> {
        let local_identity = identity.load()?.public_key();
        let contacts = ContactStore::open(&client.contacts_path(), identity.keystore_mut())
            .map_err(|error| map_contact_store_error(&error))?;
        Ok(Self {
            _client: client,
            contacts,
            local_identity,
        })
    }

    pub fn contacts(&self) -> impl Iterator<Item = SdkContact> + '_ {
        self.contacts
            .contacts()
            .iter()
            .copied()
            .map(SdkContact::from)
    }

    #[must_use]
    pub fn contact(&self, identity: &IdentityPublicKey) -> Option<SdkContact> {
        self.contacts.contact(identity).map(SdkContact::from)
    }

    pub fn import_invitation(&mut self, encoded: &[u8]) -> Result<SdkContact, SdkContactError> {
        PendingContactImportService::new(&self.local_identity, &mut self.contacts)
            .import_encoded(encoded)
            .map(SdkContact::from)
            .map_err(|error| map_pending_contact_import_error(&error))
    }

    pub fn apply_rotation(&mut self, encoded: &[u8]) -> Result<SdkContact, SdkContactError> {
        ContactLifecycleService::new(&self.local_identity, &mut self.contacts)
            .apply_rotation_encoded(encoded)
            .map(SdkContact::from)
            .map_err(|error| map_contact_lifecycle_error(&error))
    }

    pub fn verify_qr(&mut self, encoded: &[u8]) -> Result<SdkContact, SdkContactError> {
        QrContactVerificationService::new(&self.local_identity, &mut self.contacts)
            .verify_encoded(encoded)
            .map(SdkContact::from)
            .map_err(|error| map_qr_contact_verification_error(&error))
    }

    pub fn verify_safety_number(
        &mut self,
        remote_identity: &IdentityPublicKey,
        supplied: &[u8],
    ) -> Result<SdkContact, SdkContactError> {
        SafetyNumberVerificationService::new(&self.local_identity, &mut self.contacts)
            .verify(remote_identity, supplied)
            .map(SdkContact::from)
            .map_err(|error| map_safety_number_verification_error(&error))
    }

    pub fn revoke(&mut self, identity: &IdentityPublicKey) -> Result<SdkContact, SdkContactError> {
        ContactLifecycleService::new(&self.local_identity, &mut self.contacts)
            .revoke(identity)
            .map(SdkContact::from)
            .map_err(|error| map_contact_lifecycle_error(&error))
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkContact {
    identity: IdentityPublicKey,
    status: SdkContactStatus,
    verification: Option<SdkContactVerificationMethod>,
}

impl SdkContact {
    #[must_use]
    pub const fn identity(self) -> IdentityPublicKey {
        self.identity
    }

    #[must_use]
    pub const fn status(self) -> SdkContactStatus {
        self.status
    }

    #[must_use]
    pub const fn verification_method(self) -> Option<SdkContactVerificationMethod> {
        self.verification
    }
}

impl From<Contact> for SdkContact {
    fn from(contact: Contact) -> Self {
        Self {
            identity: *contact.identity(),
            status: contact.status().into(),
            verification: contact.verification_method().map(Into::into),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkContactStatus {
    Pending,
    Verified,
    Revoked,
}

impl From<ContactStatus> for SdkContactStatus {
    fn from(status: ContactStatus) -> Self {
        match status {
            ContactStatus::Pending => Self::Pending,
            ContactStatus::Verified => Self::Verified,
            ContactStatus::Revoked => Self::Revoked,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkContactVerificationMethod {
    Qr,
    SafetyNumber,
}

impl From<ContactVerificationMethod> for SdkContactVerificationMethod {
    fn from(method: ContactVerificationMethod) -> Self {
        match method {
            ContactVerificationMethod::Qr => Self::Qr,
            ContactVerificationMethod::SafetyNumber => Self::SafetyNumber,
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkContactError {
    #[error("SDK identity operation failed")]
    Identity(#[from] SdkIdentityError),
    #[error("contact invitation is invalid")]
    InvalidInvitation,
    #[error("identity rotation is invalid")]
    InvalidRotation,
    #[error("local identity cannot be imported as a contact")]
    SelfContact,
    #[error("contact is not known")]
    UnknownContact,
    #[error("contact is not pending verification")]
    NotPending,
    #[error("contact is not verified")]
    NotVerified,
    #[error("replacement identity is already known")]
    ReplacementAlreadyKnown,
    #[error("contact is already revoked")]
    AlreadyRevoked,
    #[error("contact verification data is invalid")]
    InvalidVerification,
    #[error("contact state is unavailable")]
    State,
}

fn map_pending_contact_import_error(error: &PendingContactImportError) -> SdkContactError {
    match error {
        PendingContactImportError::Invitation(_) => SdkContactError::InvalidInvitation,
        PendingContactImportError::ContactStore(error) => map_contact_store_error(error),
    }
}

fn map_contact_lifecycle_error(error: &ContactLifecycleError) -> SdkContactError {
    match error {
        ContactLifecycleError::IdentityRotation(_) => SdkContactError::InvalidRotation,
        ContactLifecycleError::ContactStore(error) => map_contact_store_error(error),
    }
}

fn map_qr_contact_verification_error(error: &QrContactVerificationError) -> SdkContactError {
    match error {
        QrContactVerificationError::Payload(_) => SdkContactError::InvalidVerification,
        QrContactVerificationError::ContactStore(error) => map_contact_store_error(error),
    }
}

fn map_safety_number_verification_error(error: &SafetyNumberVerificationError) -> SdkContactError {
    match error {
        SafetyNumberVerificationError::InvalidFingerprintLength => {
            SdkContactError::InvalidVerification
        }
        SafetyNumberVerificationError::ContactStore(error) => map_contact_store_error(error),
    }
}

fn map_contact_store_error(error: &ContactStoreError) -> SdkContactError {
    match error {
        ContactStoreError::SelfContact => SdkContactError::SelfContact,
        ContactStoreError::QrDoesNotContainLocalIdentity
        | ContactStoreError::SafetyNumberMismatch => SdkContactError::InvalidVerification,
        ContactStoreError::UnknownContact => SdkContactError::UnknownContact,
        ContactStoreError::NotPending => SdkContactError::NotPending,
        ContactStoreError::NotVerified => SdkContactError::NotVerified,
        ContactStoreError::ReplacementAlreadyKnown => SdkContactError::ReplacementAlreadyKnown,
        ContactStoreError::AlreadyRevoked => SdkContactError::AlreadyRevoked,
        ContactStoreError::StateStore(_)
        | ContactStoreError::InvalidState(_)
        | ContactStoreError::InvalidDocument(_)
        | ContactStoreError::Encode
        | ContactStoreError::UnsupportedSchemaVersion(_)
        | ContactStoreError::InvalidShape
        | ContactStoreError::InvalidIdentity
        | ContactStoreError::InvalidStatus
        | ContactStoreError::DuplicateIdentity
        | ContactStoreError::TrailingBytes
        | ContactStoreError::NonCanonicalEncoding => SdkContactError::State,
    }
}
