#![forbid(unsafe_code)]

mod error;
mod identity;
mod keystore;
#[cfg(target_os = "macos")]
mod keystore_macos;
mod secret;
#[cfg(feature = "test-fixtures")]
pub mod test_fixture;

pub use error::{Error, Result};
pub use identity::{
    ED25519_PUBLIC_KEY_BYTES, IDENTITY_SERIALIZATION_VERSION, IDENTITY_SERIALIZED_BYTES,
    IdentityKeyError, IdentityKeypair, IdentityPublicKey, IdentityPublicKeyError,
    IdentitySerializationError,
};
pub use keystore::{
    KeystoreEntryName, KeystoreEntryNameError, KeystoreSecret, KeystoreSecretError,
    MAX_KEYSTORE_ENTRY_NAME_BYTES, MAX_KEYSTORE_SECRET_BYTES, OsKeystore,
};
#[cfg(target_os = "macos")]
pub use keystore_macos::{MACOS_KEYCHAIN_SERVICE, MacOsKeystore, MacOsKeystoreError};
pub use secret::Secret;
