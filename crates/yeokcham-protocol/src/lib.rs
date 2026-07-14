#![forbid(unsafe_code)]

mod domain;
mod encrypted_message;
mod version;
mod wire;

pub use domain::CryptoDomain;
pub use encrypted_message::{
    EncryptedMessageEnvelope, EncryptedMessageError, MAX_ENCRYPTED_HEADER_BYTES,
};
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
