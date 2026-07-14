#![forbid(unsafe_code)]

mod contact_store;
mod ratchet_store;
mod state_store;

use yeokcham_core::{Error, Result};
use yeokcham_protocol::ProtocolVersion;

pub use contact_store::{
    CONTACT_STATE_SCHEMA_VERSION, Contact, ContactStatus, ContactStore, ContactStoreError,
    ContactVerificationMethod,
};
pub use ratchet_store::{RatchetStore, RatchetStoreError};
pub use state_store::{
    EncryptedStateStore, MAX_STATE_DOCUMENT_BYTES, StateDocument, StateDocumentError,
    StateStoreError,
};

#[derive(Debug)]
pub struct Daemon {
    version: ProtocolVersion,
}

impl Daemon {
    pub fn new(version: ProtocolVersion) -> Result<Self> {
        if version != ProtocolVersion::INITIAL {
            return Err(Error::UnsupportedVersion(version.get()));
        }
        Ok(Self { version })
    }

    #[must_use]
    pub const fn protocol_version(&self) -> ProtocolVersion {
        self.version
    }
}
