#![forbid(unsafe_code)]

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

pub use capability::{
    TransportAvailability, TransportCapability, TransportCapabilityError, TransportKind,
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
pub use event::{SdkEvent, SdkEventEnvelope, SdkEventError, SdkEventStream, SdkEventStreamError};
pub use identity::{SdkIdentity, SdkIdentityError, SdkIdentityInitialization, SdkIdentityManager};
pub use message::{
    SdkDeliveryStatus, SdkMessageEnvelope, SdkMessageEnvelopeError, SdkMessageError,
    SdkMessageExpiry, SdkMessageExpiryError, SdkMessageIdentifier, SdkMessageIdentifierError,
    SdkMessageSendRequest, SdkQueuedMessage,
};
pub use version::{SDK_API_VERSION, SDK_API_VERSION_MAJOR, SDK_API_VERSION_MINOR, SdkApiVersion};
pub use yeokcham_core::IdentityPublicKey;
