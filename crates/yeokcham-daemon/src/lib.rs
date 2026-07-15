#![forbid(unsafe_code)]

mod bluetooth_transport;
mod contact_store;
mod daemon_config;
mod delivery_scheduler;
mod direct_transport;
mod inbox_deduplication;
mod lan_transport;
mod lifecycle;
mod local_transport;
mod local_transport_availability;
mod mdns;
mod nat_pmp;
mod outbox_store;
mod ratchet_store;
mod relay_tls;
mod replication_scheduler;
mod state_store;
mod stun;
mod tor_socks;
mod upnp;
mod wifi_direct_transport;
mod wifi_hotspot_transport;

use yeokcham_core::{Error, Result};
use yeokcham_protocol::ProtocolVersion;

pub use bluetooth_transport::{BluetoothTransport, BluetoothTransportError};
pub use contact_store::{
    CONTACT_STATE_SCHEMA_VERSION, Contact, ContactStatus, ContactStore, ContactStoreError,
    ContactVerificationMethod,
};
pub use daemon_config::{
    DAEMON_CONFIG_SCHEMA_VERSION, DaemonConfig, DaemonConfigError, MAX_DAEMON_CONFIG_BYTES,
};
pub use delivery_scheduler::{
    BackgroundDeliveryScheduler, DeliverySchedulerCycle, DeliverySchedulerError,
    DeliverySchedulerOutcome, DeliveryTransport, MAX_DELIVERY_SCHEDULE_INTERVAL,
    MIN_DELIVERY_SCHEDULE_INTERVAL,
};
pub use direct_transport::{
    DirectConnection, DirectConnectionAttempts, DirectConnectionLimits, DirectTransport,
    DirectTransportError, MAX_DIRECT_CONCURRENT_STREAMS, MAX_DIRECT_CONNECTION_ATTEMPT_TIMEOUT,
    MAX_DIRECT_CONNECTION_ATTEMPTS, MAX_DIRECT_CONNECTION_WINDOW_BYTES, MAX_DIRECT_RETRY_BACKOFF,
};
pub use inbox_deduplication::{
    INBOX_DEDUPLICATION_SCHEMA_VERSION, InboxDeduplicationResult, MAX_INBOX_DEDUPLICATION_ENTRIES,
    RecipientInboxDeduplication, RecipientInboxDeduplicationError,
};
pub use lan_transport::LanDirectTransport;
pub use lifecycle::{DAEMON_LOCK_FILE, DaemonLifecycleError, DaemonRuntime};
pub use local_transport::LocalTransport;
pub use local_transport_availability::{
    LocalTransportAvailability, LocalTransportAvailabilityError,
};
pub use mdns::{LAN_MDNS_SERVICE_TYPE, LanPeer, LanPeerDiscovery, LanPeerDiscoveryError};
pub use nat_pmp::{
    MAX_NAT_PMP_LEASE_SECONDS, NatPmpMapping, NatPmpMappingError, NatPmpMappingRequest,
};
pub use outbox_store::{
    DeliveryState, DeliveryStatus, MAX_DELIVERY_STATE_HISTORY, MAX_MESSAGE_EXPIRY_SECONDS,
    MAX_OUTBOX_MESSAGES, MIN_MESSAGE_EXPIRY_SECONDS, MessageExpiry, MessageExpiryError,
    OUTBOX_STATE_SCHEMA_VERSION, OutboxMessage, SenderOutbox, SenderOutboxError,
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
pub use wifi_direct_transport::WifiDirectTransport;
pub use wifi_hotspot_transport::WifiHotspotTransport;

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
