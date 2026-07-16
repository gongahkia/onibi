#![forbid(unsafe_code)]

mod capability;
mod client;
mod config;
mod error;
mod event;
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
pub use error::SdkError;
pub use event::{SdkEvent, SdkEventEnvelope, SdkEventError};
pub use version::{SDK_API_VERSION, SDK_API_VERSION_MAJOR, SDK_API_VERSION_MINOR, SdkApiVersion};
