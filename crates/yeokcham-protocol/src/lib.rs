#![forbid(unsafe_code)]

mod delivery_profile;
mod direct_profile;
mod domain;
mod encrypted_message;
mod error_code;
mod extension_frame;
mod mailbox_capability;
mod message_payload;
mod negotiation;
mod signing;
mod tor_maildrop_profile;
mod version;
mod wire;

pub use delivery_profile::{
    DELIVERY_PROFILE_SCHEMA_VERSION, DeliveryProfile, DeliveryProfileConstraintError,
    DeliveryProfileConstraints, DeliveryProfileError, DeliveryProfileKind,
    DeliveryProfilePolicyDecision,
};
pub use direct_profile::{
    DIRECT_PROFILE_CONFIG_SCHEMA_VERSION, DirectProfileConfig, DirectProfileConfigError,
};
pub use domain::CryptoDomain;
pub use encrypted_message::{
    EncryptedMessageEnvelope, EncryptedMessageError, MAX_ENCRYPTED_HEADER_BYTES,
};
pub use error_code::{ProtocolErrorCode, ProtocolErrorCodeError};
pub use extension_frame::{ExtensionFrame, ExtensionFrameError, MAX_EXTENSION_DATA_BYTES};
pub use mailbox_capability::{
    MAILBOX_CAPABILITY_SCHEMA_VERSION, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
    MailboxCapability, MailboxCapabilityError,
};
pub use message_payload::{
    MAX_MESSAGE_PAYLOAD_BYTES, MessageContentType, MessagePayload, MessagePayloadError,
};
pub use negotiation::{VersionNegotiation, VersionNegotiationError};
pub use signing::SigningInputError;
pub use tor_maildrop_profile::{
    TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
    TorMaildropProfileConfig, TorMaildropProfileConfigError,
};
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
