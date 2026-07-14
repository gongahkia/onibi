#![forbid(unsafe_code)]

mod domain;
mod version;
mod wire;

pub use domain::CryptoDomain;
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
