#![forbid(unsafe_code)]

mod delivery_profile;
mod domain;
mod encrypted_message;
mod extension_frame;
mod mailbox_capability;
mod message_payload;
mod signing;
mod version;
mod wire;

pub use delivery_profile::{
    DELIVERY_PROFILE_SCHEMA_VERSION, DeliveryProfile, DeliveryProfileError, DeliveryProfileKind,
};
pub use domain::CryptoDomain;
pub use encrypted_message::{
    EncryptedMessageEnvelope, EncryptedMessageError, MAX_ENCRYPTED_HEADER_BYTES,
};
pub use extension_frame::{ExtensionFrame, ExtensionFrameError, MAX_EXTENSION_DATA_BYTES};
pub use mailbox_capability::{
    MAILBOX_CAPABILITY_SCHEMA_VERSION, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
    MailboxCapability, MailboxCapabilityError,
};
pub use message_payload::{
    MAX_MESSAGE_PAYLOAD_BYTES, MessageContentType, MessagePayload, MessagePayloadError,
};
pub use signing::SigningInputError;
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
