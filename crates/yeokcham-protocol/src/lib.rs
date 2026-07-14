#![forbid(unsafe_code)]

mod version;
mod wire;

pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
