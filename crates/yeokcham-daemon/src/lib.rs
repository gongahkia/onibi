#![forbid(unsafe_code)]

mod attachment_delivery;
mod attachment_journal_store;
mod attachment_submission_store;
mod bluetooth_transport;
mod client_identity;
mod client_state;
mod contact_lifecycle;
mod contact_store;
mod daemon_config;
mod daemon_endpoint;
#[cfg(unix)]
mod daemon_server;
mod delivery_scheduler;
mod direct_transport;
mod grpc_service;
mod inbox_deduplication;
mod lan_transport;
mod lifecycle;
mod linux_wifi_hotspot_capability;
mod local_auth;
mod local_transport;
mod local_transport_availability;
mod local_transport_reconnect;
mod mdns;
mod nat_pmp;
mod native_transport_capability;
mod one_time_prekey_replenisher;
mod outbox_store;
mod pending_contact_import;
mod proximity_contact_invitation;
mod qr_contact_verification;
mod ratchet_store;
mod relay_tls;
#[cfg(feature = "experimental-relay-replication")]
mod replication_scheduler;
mod safety_number_verification;
mod signed_prekey_lifecycle;
mod state_store;
mod stun;
mod tor_runtime;
mod tor_socks;
mod tor_tonic;
#[cfg(unix)]
mod unix_listener;
mod upnp;
mod wifi_direct_transport;
mod wifi_hotspot_transport;
mod x3dh_session_service;

use yeokcham_core::{Error, Result};
use yeokcham_protocol::ProtocolVersion;

pub use attachment_delivery::{
    AttachmentChunkSource, AttachmentDeliveryCycle, AttachmentDeliveryError,
    AttachmentDeliveryOrchestrator, AttachmentDeliveryOutcome, AttachmentDeliveryTransport,
    MAX_ATTACHMENT_DELIVERY_CHUNKS_PER_CYCLE,
};
pub use attachment_journal_store::{AttachmentJournalStoreError, AttachmentTransferJournalStore};
pub use attachment_submission_store::{
    AttachmentSubmissionStatus, AttachmentSubmissionStore, AttachmentSubmissionStoreError,
    AttachmentUploadSubmission, MAX_DAEMON_ATTACHMENT_CHUNKS, MAX_DAEMON_ATTACHMENT_MANIFEST_BYTES,
};
pub use bluetooth_transport::{BluetoothTransport, BluetoothTransportError};
pub use client_identity::{
    CLIENT_IDENTITY_KEY_ENTRY, ClientIdentity, ClientIdentityError, ClientIdentityInitialization,
};
pub use client_state::{
    ATTACHMENT_UPLOAD_DIRECTORY, CLIENT_STATE_DIRECTORY_LAYOUT_VERSION, CONTACTS_DATABASE_FILE,
    ClientStateDirectory, ClientStateDirectoryError, INBOX_DATABASE_FILE,
    ONE_TIME_PREKEY_INVENTORY_DATABASE_FILE, OUTBOX_DATABASE_FILE, RATCHETS_DATABASE_FILE,
};
pub use contact_lifecycle::{ContactLifecycleError, ContactLifecycleService};
pub use contact_store::{
    CONTACT_STATE_SCHEMA_VERSION, Contact, ContactStatus, ContactStore, ContactStoreError,
    ContactVerificationMethod,
};
pub use daemon_config::{
    DAEMON_CONFIG_SCHEMA_VERSION, DaemonConfig, DaemonConfigError, MAX_DAEMON_CONFIG_BYTES,
};
pub use daemon_endpoint::{
    DAEMON_ENDPOINT_CONFIG_VERSION, DEFAULT_DAEMON_UNIX_SOCKET_NAME, DaemonEndpoint,
    DaemonEndpointConfig, DaemonEndpointConfigError, MAX_DAEMON_ENDPOINT_CONFIG_BYTES,
    MAX_DAEMON_ENDPOINT_NAME_BYTES,
};
#[cfg(unix)]
pub use daemon_server::{DaemonServer, DaemonServerError};
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
pub use grpc_service::{
    DaemonGrpcService, DaemonGrpcServiceConfigurationError, MAX_DAEMON_CONTACTS_RESPONSE,
};
pub use inbox_deduplication::{
    INBOX_DEDUPLICATION_SCHEMA_VERSION, InboxDeduplicationResult, InboxMessage,
    MAX_INBOX_DEDUPLICATION_ENTRIES, RecipientInboxDeduplication, RecipientInboxDeduplicationError,
};
pub use lan_transport::LanDirectTransport;
pub use lifecycle::{DAEMON_LOCK_FILE, DaemonLifecycleError, DaemonRuntime};
pub use linux_wifi_hotspot_capability::{
    LinuxWifiHotspotCapabilityProbe, MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES,
};
pub use local_auth::{
    DaemonLocalAuth, DaemonLocalAuthError, DaemonLocalAuthInterceptor, DaemonLocalAuthToken,
    LOCAL_AUTH_TOKEN_BYTES, LOCAL_AUTH_TOKEN_METADATA_KEY, LOCAL_AUTH_TOKEN_VERSION,
    MAX_LOCAL_AUTH_METADATA_BYTES,
};
pub use local_transport::LocalTransport;
pub use local_transport_availability::{
    LocalTransportAvailability, LocalTransportAvailabilityError,
};
pub use local_transport_reconnect::{
    LocalTransportConnector, LocalTransportReconnectError, LocalTransportReconnectPolicy,
    LocalTransportReconnectPolicyError, MAX_LOCAL_TRANSPORT_RECONNECT_ATTEMPTS,
};
pub use mdns::{LAN_MDNS_SERVICE_TYPE, LanPeer, LanPeerDiscovery, LanPeerDiscoveryError};
pub use nat_pmp::{
    MAX_NAT_PMP_LEASE_SECONDS, NatPmpMapping, NatPmpMappingError, NatPmpMappingRequest,
};
pub use native_transport_capability::{
    NativeTransportCapabilityProbe, NativeTransportCapabilityProbeError,
};
pub use one_time_prekey_replenisher::{
    InventoryError, ONE_TIME_PREKEY_INVENTORY_SCHEMA_VERSION, OneTimePrekeyReplenisher,
    OneTimePrekeyReplenisherError,
};
pub use outbox_store::{
    DeliveryState, DeliveryStatus, MAX_DELIVERY_ATTEMPTS, MAX_DELIVERY_STATE_HISTORY,
    MAX_MESSAGE_EXPIRY_SECONDS, MAX_OUTBOX_MESSAGES, MIN_MESSAGE_EXPIRY_SECONDS, MessageExpiry,
    MessageExpiryError, OUTBOX_STATE_SCHEMA_VERSION, OutboxMessage, SenderOutbox,
    SenderOutboxError,
};
pub use pending_contact_import::{PendingContactImportError, PendingContactImportService};
pub use proximity_contact_invitation::{
    ProximityContactInvitationExchange, ProximityContactInvitationExchangeError,
};
pub use qr_contact_verification::{QrContactVerificationError, QrContactVerificationService};
pub use ratchet_store::{RatchetStore, RatchetStoreError};
pub use relay_tls::{RELAY_TLS_PIN_BYTES, RelayTlsEndpoint, RelayTlsEndpointError, RelayTlsPin};
#[cfg(feature = "experimental-relay-replication")]
pub use replication_scheduler::{MaildropReplicationError, MaildropReplicationScheduler};
pub use safety_number_verification::{
    SafetyNumberVerificationError, SafetyNumberVerificationService,
};
pub use signed_prekey_lifecycle::{
    SIGNED_PREKEY_KEY_ENTRY, SignedPrekeyInitialization, SignedPrekeyLifecycle,
    SignedPrekeyLifecycleError,
};
pub use state_store::{
    CURRENT_STATE_FORMAT_VERSION, EncryptedStateStore, MAX_STATE_DOCUMENT_BYTES, StateDocument,
    StateDocumentError, StateStoreError,
};
pub use stun::{MAX_STUN_SERVERS, StunServer, StunServerError, StunServers};
pub use tor_runtime::{
    DEFAULT_EXTERNAL_TOR_CONNECT_TIMEOUT, DEFAULT_EXTERNAL_TOR_SOCKS_PORT, ExternalTorRuntime,
    ExternalTorRuntimeConfig, ExternalTorRuntimeConfigError, ExternalTorRuntimeError,
    MAX_EXTERNAL_TOR_CONNECT_TIMEOUT,
};
pub use tor_socks::{TorSocksConnector, TorSocksError, TorSocksTarget};
pub use tor_tonic::{TorSocksTonicConnector, TorSocksTonicConnectorError};
#[cfg(unix)]
pub use unix_listener::{DAEMON_UNIX_SOCKET_FILE, DaemonUnixListener, DaemonUnixListenerError};
pub use upnp::{MAX_UPNP_LEASE_SECONDS, UpnpMapping, UpnpMappingError, UpnpMappingRequest};
pub use wifi_direct_transport::WifiDirectTransport;
pub use wifi_hotspot_transport::WifiHotspotTransport;
pub use x3dh_session_service::{
    X3dhSessionEstablishmentError, X3dhSessionEstablishmentService, initiate_x3dh_session,
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
