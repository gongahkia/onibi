#![forbid(unsafe_code)]

mod delivery_profile;
mod direct_profile;
mod domain;
mod encrypted_header;
mod encrypted_message;
mod error_code;
mod extension_frame;
mod identity_export;
mod identity_identifier;
mod local_mesh_profile;
mod mailbox_capability;
mod message_payload;
mod negotiation;
mod qr_verification;
mod recipient_capability;
mod safety_number;
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
pub use encrypted_header::{
    ENCRYPTED_HEADER_SCHEMA_VERSION, EncryptedHeader, EncryptedHeaderError,
};
pub use encrypted_message::{
    EncryptedMessageEnvelope, EncryptedMessageError, MAX_ENCRYPTED_HEADER_BYTES,
};
pub use error_code::{ProtocolErrorCode, ProtocolErrorCodeError};
pub use extension_frame::{ExtensionFrame, ExtensionFrameError, MAX_EXTENSION_DATA_BYTES};
pub use identity_export::{
    IDENTITY_EXPORT_BYTES, IDENTITY_EXPORT_FORMAT_VERSION, IDENTITY_EXPORT_SALT_BYTES,
    IdentityExportError, IdentityExportPassphrase, IdentityExportPassphraseError,
    MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES, export_identity, import_identity,
};
pub use identity_identifier::{IDENTITY_IDENTIFIER_BYTES, IdentityIdentifier};
pub use local_mesh_profile::{
    LOCAL_MESH_PROFILE_CONFIG_SCHEMA_VERSION, LocalMeshProfileConfig, LocalMeshProfileConfigError,
    LocalMeshTransportKind,
};
pub use mailbox_capability::{
    MAILBOX_CAPABILITY_SCHEMA_VERSION, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
    MailboxCapability, MailboxCapabilityError,
};
pub use message_payload::{
    MAX_MESSAGE_PAYLOAD_BYTES, MessageContentType, MessagePayload, MessagePayloadError,
};
pub use negotiation::{VersionNegotiation, VersionNegotiationError};
pub use qr_verification::{
    QR_VERIFICATION_PAYLOAD_BYTES, QR_VERIFICATION_SCHEMA_VERSION, QrVerificationError,
    QrVerificationPayload,
};
pub use recipient_capability::{
    RECIPIENT_CAPABILITY_SCHEMA_VERSION, RecipientCapability, RecipientCapabilityError,
};
pub use safety_number::{
    SAFETY_NUMBER_FINGERPRINT_BYTES, SafetyNumberError, SafetyNumberFingerprint,
};
pub use signing::SigningInputError;
pub use tor_maildrop_profile::{
    TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
    TorMaildropProfileConfig, TorMaildropProfileConfigError,
};
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
