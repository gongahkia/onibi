#![forbid(unsafe_code)]

mod contact_store;
mod direct_transport;
mod nat_pmp;
mod ratchet_store;
mod state_store;
mod stun;
mod upnp;

use yeokcham_core::{Error, Result};
use yeokcham_protocol::ProtocolVersion;

pub use contact_store::{
    CONTACT_STATE_SCHEMA_VERSION, Contact, ContactStatus, ContactStore, ContactStoreError,
    ContactVerificationMethod,
};
pub use direct_transport::{DirectConnection, DirectTransport, DirectTransportError};
pub use nat_pmp::{
    MAX_NAT_PMP_LEASE_SECONDS, NatPmpMapping, NatPmpMappingError, NatPmpMappingRequest,
};
pub use ratchet_store::{RatchetStore, RatchetStoreError};
pub use state_store::{
    EncryptedStateStore, MAX_STATE_DOCUMENT_BYTES, StateDocument, StateDocumentError,
    StateStoreError,
};
pub use stun::{MAX_STUN_SERVERS, StunServer, StunServerError, StunServers};
pub use upnp::{MAX_UPNP_LEASE_SECONDS, UpnpMapping, UpnpMappingError, UpnpMappingRequest};

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
