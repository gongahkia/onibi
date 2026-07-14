#![forbid(unsafe_code)]

mod delivery_profile;
mod domain;
mod encrypted_message;
mod message_payload;
mod version;
mod wire;

pub use delivery_profile::{
    DELIVERY_PROFILE_SCHEMA_VERSION, DeliveryProfile, DeliveryProfileError, DeliveryProfileKind,
};
pub use domain::CryptoDomain;
pub use encrypted_message::{
    EncryptedMessageEnvelope, EncryptedMessageError, MAX_ENCRYPTED_HEADER_BYTES,
};
pub use message_payload::{
    MAX_MESSAGE_PAYLOAD_BYTES, MessageContentType, MessagePayload, MessagePayloadError,
};
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
