#![forbid(unsafe_code)]

mod contact_store;
mod direct_transport;
mod mdns;
mod nat_pmp;
mod ratchet_store;
mod relay_tls;
mod replication_scheduler;
mod state_store;
mod stun;
mod tor_socks;
mod upnp;

use yeokcham_core::{Error, Result};
use yeokcham_protocol::ProtocolVersion;

pub use contact_store::{
    CONTACT_STATE_SCHEMA_VERSION, Contact, ContactStatus, ContactStore, ContactStoreError,
    ContactVerificationMethod,
};
pub use direct_transport::{
    DirectConnection, DirectConnectionAttempts, DirectConnectionLimits, DirectTransport,
    DirectTransportError, MAX_DIRECT_CONCURRENT_STREAMS, MAX_DIRECT_CONNECTION_ATTEMPT_TIMEOUT,
    MAX_DIRECT_CONNECTION_ATTEMPTS, MAX_DIRECT_CONNECTION_WINDOW_BYTES, MAX_DIRECT_RETRY_BACKOFF,
};
pub use mdns::{LAN_MDNS_SERVICE_TYPE, LanPeer, LanPeerDiscovery, LanPeerDiscoveryError};
pub use nat_pmp::{
    MAX_NAT_PMP_LEASE_SECONDS, NatPmpMapping, NatPmpMappingError, NatPmpMappingRequest,
};
pub use ratchet_store::{RatchetStore, RatchetStoreError};
pub use relay_tls::{RELAY_TLS_PIN_BYTES, RelayTlsEndpoint, RelayTlsEndpointError, RelayTlsPin};
pub use replication_scheduler::{MaildropReplicationError, MaildropReplicationScheduler};
pub use state_store::{
    EncryptedStateStore, MAX_STATE_DOCUMENT_BYTES, StateDocument, StateDocumentError,
    StateStoreError,
};
pub use stun::{MAX_STUN_SERVERS, StunServer, StunServerError, StunServers};
pub use tor_socks::{TorSocksConnector, TorSocksError, TorSocksTarget};
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
