#![forbid(unsafe_code)]

mod contact_invitation;
mod delivery_profile;
mod direct_peer_auth;
mod direct_profile;
mod domain;
mod double_ratchet;
mod encrypted_header;
mod encrypted_message;
mod error_code;
mod extension_frame;
mod identity_export;
mod identity_identifier;
mod identity_rotation;
mod local_mesh_profile;
mod mailbox_capability;
mod message_payload;
mod negotiation;
mod prekey_bundle;
mod qr_verification;
mod recipient_capability;
mod relay_invitation;
mod safety_number;
mod signed_prekey;
mod signing;
mod tor_maildrop_profile;
mod version;
mod wire;
mod x3dh;

pub use contact_invitation::{
    CONTACT_INVITATION_BYTES, CONTACT_INVITATION_NONCE_BYTES, CONTACT_INVITATION_SCHEMA_VERSION,
    ContactInvitation, ContactInvitationError,
};
pub use delivery_profile::{
    DELIVERY_PROFILE_SCHEMA_VERSION, DeliveryProfile, DeliveryProfileConstraintError,
    DeliveryProfileConstraints, DeliveryProfileError, DeliveryProfileKind,
    DeliveryProfilePolicyDecision, DeliveryProfilePrivacyWarning, DirectProfileSelection,
};
pub use direct_peer_auth::{
    DIRECT_PEER_PROOF_BYTES, DIRECT_PEER_PROOF_SCHEMA_VERSION, DirectPeerProof,
    DirectPeerProofError,
};
pub use direct_profile::{
    DIRECT_PROFILE_CONFIG_SCHEMA_VERSION, DirectProfileConfig, DirectProfileConfigError,
};
pub use domain::CryptoDomain;
pub use double_ratchet::{
    DOUBLE_RATCHET_STATE_SCHEMA_VERSION, DoubleRatchetError, DoubleRatchetState,
    MAX_RETIRED_RATCHET_KEYS, MAX_SKIPPED_MESSAGE_KEYS, RATCHET_KEY_BYTES,
};
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
pub use identity_rotation::{
    IDENTITY_ROTATION_SCHEMA_VERSION, IdentityRotation, IdentityRotationError,
};
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
pub use prekey_bundle::{
    MAX_ONE_TIME_PREKEYS, MAX_PREKEY_BUNDLE_BYTES, OneTimePrekeyPublic,
    PREKEY_BUNDLE_SCHEMA_VERSION, PrekeyBundle, PrekeyBundleError,
};
pub use qr_verification::{
    QR_VERIFICATION_PAYLOAD_BYTES, QR_VERIFICATION_SCHEMA_VERSION, QrVerificationError,
    QrVerificationPayload,
};
pub use recipient_capability::{
    RECIPIENT_CAPABILITY_SCHEMA_VERSION, RecipientCapability, RecipientCapabilityError,
};
pub use relay_invitation::{
    MAX_RELAY_INVITATION_BYTES, MAX_RELAY_INVITATION_TTL_SECONDS, RELAY_INVITATION_GRANT_ID_BYTES,
    RELAY_INVITATION_SCHEMA_VERSION, RelayInvitation, RelayInvitationError,
};
pub use safety_number::{
    SAFETY_NUMBER_FINGERPRINT_BYTES, SafetyNumberError, SafetyNumberFingerprint,
};
pub use signed_prekey::{
    SIGNED_PREKEY_INITIAL_GENERATION, SIGNED_PREKEY_SCHEMA_VERSION, SignedPrekey,
    SignedPrekeyError, SignedPrekeyPublic, SignedPrekeyValidationError,
};
pub use signing::SigningInputError;
pub use tor_maildrop_profile::{
    TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
    TorMaildropProfileConfig, TorMaildropProfileConfigError,
};
pub use version::{ProtocolVersion, VersionRange};
pub use wire::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
pub use x3dh::{
    MAX_X3DH_INITIAL_MESSAGE_BYTES, MAX_X3DH_PREKEY_BUNDLE_BYTES,
    X3DH_INITIAL_MESSAGE_SCHEMA_VERSION, X3DH_PREKEY_BUNDLE_SCHEMA_VERSION, X3DH_ROOT_KEY_BYTES,
    X3dhError, X3dhInitialMessage, X3dhPrekeyBundle, X3dhSession,
    X25519_IDENTITY_BINDING_SCHEMA_VERSION, X25519IdentityBinding, initiate_x3dh, respond_x3dh,
};
