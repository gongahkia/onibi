#![forbid(unsafe_code)]

mod error;
mod identity;
mod secret;
#[cfg(feature = "test-fixtures")]
pub mod test_fixture;

pub use error::{Error, Result};
pub use identity::{
    ED25519_PUBLIC_KEY_BYTES, IDENTITY_SERIALIZATION_VERSION, IDENTITY_SERIALIZED_BYTES,
    IdentityKeyError, IdentityKeypair, IdentityPublicKey, IdentityPublicKeyError,
    IdentitySerializationError,
};
pub use secret::Secret;
