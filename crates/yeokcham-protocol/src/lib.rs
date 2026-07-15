#![forbid(unsafe_code)]

mod attachment_chunk;
mod attachment_download;
mod attachment_key;
mod attachment_manifest;
mod attachment_upload;
mod contact_invitation;
mod delivery_acknowledgement;
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
mod message_identifier;
mod message_payload;
mod negotiation;
mod prekey_bundle;
mod qr_verification;
mod recipient_capability;
mod relay_invitation;
mod relay_replica;
mod relay_storage_receipt;
mod safety_number;
mod signed_prekey;
mod signing;
mod tor_maildrop_profile;
mod version;
mod wire;
mod x3dh;

pub use attachment_chunk::{
    ATTACHMENT_CHUNK_BYTES, ATTACHMENT_CHUNK_HASH_BYTES, ATTACHMENT_CHUNK_NONCE_BYTES,
    ATTACHMENT_CHUNK_TAG_BYTES, AttachmentChunkError, AttachmentChunkHash,
    ENCRYPTED_ATTACHMENT_CHUNK_BYTES, ENCRYPTED_ATTACHMENT_CHUNK_SCHEMA_VERSION,
    EncryptedAttachmentChunk, MAX_ENCODED_ATTACHMENT_CHUNK_BYTES,
};
pub use attachment_download::{
    ATTACHMENT_DOWNLOAD_JOURNAL_SCHEMA_VERSION, AttachmentDownloadError, AttachmentDownloadJournal,
    MAX_ATTACHMENT_DOWNLOAD_JOURNAL_BYTES,
};
pub use attachment_key::{
    ATTACHMENT_IDENTIFIER_BYTES, ATTACHMENT_KEY_BYTES, AttachmentChunkKey, AttachmentIdentifier,
    AttachmentKey, AttachmentKeyError,
};
pub use attachment_manifest::{
    ATTACHMENT_MANIFEST_NONCE_BYTES, ATTACHMENT_MANIFEST_SCHEMA_VERSION,
    ATTACHMENT_MANIFEST_TAG_BYTES, AttachmentManifest, AttachmentManifestError,
    ENCRYPTED_ATTACHMENT_MANIFEST_SCHEMA_VERSION, EncryptedAttachmentManifest,
    MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES,
};
pub use attachment_upload::{
    ATTACHMENT_UPLOAD_JOURNAL_SCHEMA_VERSION, AttachmentUploadError, AttachmentUploadJournal,
    MAX_ATTACHMENT_UPLOAD_JOURNAL_BYTES,
};
pub use contact_invitation::{
    CONTACT_INVITATION_BYTES, CONTACT_INVITATION_NONCE_BYTES, CONTACT_INVITATION_SCHEMA_VERSION,
    ContactInvitation, ContactInvitationError,
};
pub use delivery_acknowledgement::{
    DELIVERY_ACKNOWLEDGEMENT_SCHEMA_VERSION, DeliveryAcknowledgement, DeliveryAcknowledgementError,
};
pub use delivery_profile::{
    DELIVERY_PROFILE_SCHEMA_VERSION, DeliveryProfile, DeliveryProfileConstraintError,
    DeliveryProfileConstraints, DeliveryProfileError, DeliveryProfileKind,
    DeliveryProfilePolicyDecision, DeliveryProfilePrivacyWarning, DeliveryProfileTransitionError,
    DirectProfileSelection,
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
pub use message_identifier::{MESSAGE_IDENTIFIER_BYTES, MessageIdentifier, MessageIdentifierError};
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
pub use relay_replica::{MAX_RELAY_REPLICAS, RelayReplicaSelection, RelayReplicaSelectionError};
pub use relay_storage_receipt::{
    RELAY_STORAGE_RECEIPT_SCHEMA_VERSION, RelayStorageReceipt, RelayStorageReceiptError,
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
