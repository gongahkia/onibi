const PREFIX: &[u8] = b"arachne/v1/";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CryptoDomain {
    IdentityIdentifier,
    IdentityExportKey,
    StateExportKey,
    ContactInvitationSignature,
    DirectPeerAuthentication,
    LocalLinkTranscript,
    LocalLinkProofBinding,
    IdentityRotationSignature,
    RelayInvitationSignature,
    DurableStateEncryption,
    SafetyNumber,
    PrekeyBundleSignature,
    X3dhIdentityBindingSignature,
    X3dhRootKey,
    X3dhAssociatedData,
    RatchetRootKey,
    RatchetChainKey,
    RatchetMessageKey,
    RatchetMessageAead,
    EnvelopeEncryptionKey,
    EnvelopeSignature,
    DeliveryAcknowledgementSignature,
    MailboxCapability,
    RelayStorageReceiptSignature,
    AttachmentKey,
    AttachmentChunkKey,
    AttachmentChunkEncryption,
    AttachmentChunkHash,
    AttachmentManifestEncryption,
    ProfileBinding,
    ProjectTestRelaySyntheticTraffic,
    RecipientInboxDeduplication,
}

impl CryptoDomain {
    pub const ALL: &[Self] = &[
        Self::IdentityIdentifier,
        Self::IdentityExportKey,
        Self::StateExportKey,
        Self::ContactInvitationSignature,
        Self::DirectPeerAuthentication,
        Self::LocalLinkTranscript,
        Self::LocalLinkProofBinding,
        Self::IdentityRotationSignature,
        Self::RelayInvitationSignature,
        Self::DurableStateEncryption,
        Self::SafetyNumber,
        Self::PrekeyBundleSignature,
        Self::X3dhIdentityBindingSignature,
        Self::X3dhRootKey,
        Self::X3dhAssociatedData,
        Self::RatchetRootKey,
        Self::RatchetChainKey,
        Self::RatchetMessageKey,
        Self::RatchetMessageAead,
        Self::EnvelopeEncryptionKey,
        Self::EnvelopeSignature,
        Self::DeliveryAcknowledgementSignature,
        Self::MailboxCapability,
        Self::RelayStorageReceiptSignature,
        Self::AttachmentKey,
        Self::AttachmentChunkKey,
        Self::AttachmentChunkEncryption,
        Self::AttachmentChunkHash,
        Self::AttachmentManifestEncryption,
        Self::ProfileBinding,
        Self::ProjectTestRelaySyntheticTraffic,
        Self::RecipientInboxDeduplication,
    ];

    #[must_use]
    pub fn context(self) -> &'static [u8] {
        let context: &[u8] = match self {
            Self::IdentityIdentifier => b"arachne/v1/identity-identifier",
            Self::IdentityExportKey => b"arachne/v1/identity-export-key",
            Self::StateExportKey => b"arachne/v1/state-export-key",
            Self::ContactInvitationSignature => b"arachne/v1/contact-invitation-signature",
            Self::DirectPeerAuthentication => b"arachne/v1/direct-peer-authentication",
            Self::LocalLinkTranscript => b"arachne/v1/local-link-transcript",
            Self::LocalLinkProofBinding => b"arachne/v1/local-link-proof-binding",
            Self::IdentityRotationSignature => b"arachne/v1/identity-rotation-signature",
            Self::RelayInvitationSignature => b"arachne/v1/relay-invitation-signature",
            Self::DurableStateEncryption => b"arachne/v1/durable-state-encryption",
            Self::SafetyNumber => b"arachne/v1/safety-number",
            Self::PrekeyBundleSignature => b"arachne/v1/prekey-bundle-signature",
            Self::X3dhIdentityBindingSignature => b"arachne/v1/x3dh-identity-binding-signature",
            Self::X3dhRootKey => b"arachne/v1/x3dh-root-key",
            Self::X3dhAssociatedData => b"arachne/v1/x3dh-associated-data",
            Self::RatchetRootKey => b"arachne/v1/ratchet-root-key",
            Self::RatchetChainKey => b"arachne/v1/ratchet-chain-key",
            Self::RatchetMessageKey => b"arachne/v1/ratchet-message-key",
            Self::RatchetMessageAead => b"arachne/v1/ratchet-message-aead",
            Self::EnvelopeEncryptionKey => b"arachne/v1/envelope-encryption-key",
            Self::EnvelopeSignature => b"arachne/v1/envelope-signature",
            Self::DeliveryAcknowledgementSignature => {
                b"arachne/v1/delivery-acknowledgement-signature"
            }
            Self::MailboxCapability => b"arachne/v1/mailbox-capability",
            Self::RelayStorageReceiptSignature => b"arachne/v1/relay-storage-receipt-signature",
            Self::AttachmentKey => b"arachne/v1/attachment-key",
            Self::AttachmentChunkKey => b"arachne/v1/attachment-chunk-key",
            Self::AttachmentChunkEncryption => b"arachne/v1/attachment-chunk-encryption",
            Self::AttachmentChunkHash => b"arachne/v1/attachment-chunk-hash",
            Self::AttachmentManifestEncryption => b"arachne/v1/attachment-manifest-encryption",
            Self::ProfileBinding => b"arachne/v1/profile-binding",
            Self::ProjectTestRelaySyntheticTraffic => {
                b"arachne/v1/project-test-relay-synthetic-traffic"
            }
            Self::RecipientInboxDeduplication => b"arachne/v1/recipient-inbox-deduplication",
        };
        debug_assert!(context.starts_with(PREFIX));
        context
    }
}

#[cfg(test)]
mod tests {
    use super::{CryptoDomain, PREFIX};

    #[test]
    fn contexts_are_namespaced_and_unique() {
        for (index, domain) in CryptoDomain::ALL.iter().copied().enumerate() {
            let context = domain.context();
            assert!(context.starts_with(PREFIX));
            assert!(
                CryptoDomain::ALL[..index]
                    .iter()
                    .all(|previous| previous.context() != context),
                "duplicate context: {domain:?}"
            );
        }
    }

    #[test]
    fn envelope_signature_context_is_stable() {
        assert_eq!(
            CryptoDomain::EnvelopeSignature.context(),
            b"arachne/v1/envelope-signature"
        );
    }
}
