#![forbid(unsafe_code)]

mod error;
mod identity;
mod secret;
#[cfg(feature = "test-fixtures")]
pub mod test_fixture;

pub use error::{Error, Result};
pub use identity::{
    ED25519_PUBLIC_KEY_BYTES, IdentityKeyError, IdentityKeypair, IdentityPublicKey,
    IdentityPublicKeyError,
};
pub use secret::Secret;
