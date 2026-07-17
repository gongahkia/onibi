#![forbid(unsafe_code)]

mod async_policy;
mod capability;
mod client;
mod config;
mod contact;
mod delivery_profile;
mod error;
mod event;
mod identity;
mod message;
mod version;

pub use async_policy::{MAX_SDK_ASYNC_DEADLINE, SdkAsyncPolicy, SdkAsyncPolicyError};
pub use capability::{
    TRANSPORT_KIND_COUNT, TransportAvailability, TransportCapability, TransportCapabilityError,
    TransportCapabilityMatrix, TransportKind,
};
pub use client::{SdkClient, SdkClientError};
pub use config::{
    EventBufferConfigured, EventBufferNotConfigured, LocalDaemonEndpoint, LocalDaemonEndpointError,
    MAX_LOCAL_DAEMON_ENDPOINT_BYTES, MAX_SDK_EVENT_BUFFER_CAPACITY, RuntimeMode,
    RuntimeModeConfigured, RuntimeModeNotConfigured, SdkClientBuilder, SdkConfig, SdkConfigError,
};
pub use contact::{
    SdkContact, SdkContactError, SdkContactManager, SdkContactStatus, SdkContactVerificationMethod,
};
pub use delivery_profile::{
    SdkDeliveryProfile, SdkDeliveryProfileKind, SdkDeliveryProfilePolicy,
    SdkDeliveryProfilePolicyError, SdkDirectIpDisclosureAcknowledgement, SdkLocalMeshPolicy,
    SdkLocalMeshTransportKind,
};
pub use error::SdkError;
pub use event::{
    SDK_EVENT_ENVELOPE_VERSION, SdkEvent, SdkEventEnvelope, SdkEventError, SdkEventStream,
    SdkEventStreamError,
};
pub use identity::{SdkIdentity, SdkIdentityError, SdkIdentityInitialization, SdkIdentityManager};
pub use message::{
    SdkDeliveryStatus, SdkMessageEnvelope, SdkMessageEnvelopeError, SdkMessageError,
    SdkMessageExpiry, SdkMessageExpiryError, SdkMessageIdentifier, SdkMessageIdentifierError,
    SdkMessageSendRequest, SdkQueuedMessage,
};
pub use tokio_util::sync::CancellationToken;
pub use version::{SDK_API_VERSION, SDK_API_VERSION_MAJOR, SDK_API_VERSION_MINOR, SdkApiVersion};
pub use yeokcham_core::IdentityPublicKey;
