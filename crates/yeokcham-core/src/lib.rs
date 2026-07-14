#![forbid(unsafe_code)]

mod error;
mod identity;
mod keystore;
#[cfg(any(target_os = "linux", test))]
mod keystore_linux;
#[cfg(target_os = "macos")]
mod keystore_macos;
#[cfg(any(target_os = "windows", test))]
mod keystore_windows;
mod one_time_prekey;
mod prekey;
mod secret;
#[cfg(feature = "test-fixtures")]
pub mod test_fixture;

pub use error::{Error, Result};
pub use identity::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IDENTITY_SERIALIZATION_VERSION,
    IDENTITY_SERIALIZED_BYTES, IdentityKeyError, IdentityKeypair, IdentityPublicKey,
    IdentityPublicKeyError, IdentitySerializationError, IdentitySignatureError,
};
pub use keystore::{
    KeystoreEntryName, KeystoreEntryNameError, KeystoreSecret, KeystoreSecretError,
    MAX_KEYSTORE_ENTRY_NAME_BYTES, MAX_KEYSTORE_SECRET_BYTES, OsKeystore,
};
#[cfg(target_os = "linux")]
pub use keystore_linux::{LINUX_SECRET_SERVICE, LinuxKeystore, LinuxKeystoreError};
#[cfg(target_os = "macos")]
pub use keystore_macos::{MACOS_KEYCHAIN_SERVICE, MacOsKeystore, MacOsKeystoreError};
#[cfg(target_os = "windows")]
pub use keystore_windows::{
    MAX_WINDOWS_CREDENTIAL_SECRET_BYTES, WINDOWS_CREDENTIAL_SERVICE, WindowsKeystore,
    WindowsKeystoreError,
};
pub use one_time_prekey::{
    OneTimePrekeyId, OneTimePrekeyIdError, OneTimePrekeyStore, OneTimePrekeyStoreError,
};
pub use prekey::{
    X25519_KEY_BYTES, X25519_PREKEY_SERIALIZATION_VERSION, X25519_PREKEY_SERIALIZED_BYTES,
    X25519Prekey, X25519PrekeyError, X25519PrekeyPublicKey, X25519PrekeyPublicKeyError,
    X25519PrekeySerializationError,
};
pub use secret::Secret;
