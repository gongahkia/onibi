const PREFIX: &[u8] = b"yeokcham/v1/";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CryptoDomain {
    IdentityIdentifier,
    IdentityExportKey,
    ContactInvitationSignature,
    RelayInvitationSignature,
    SafetyNumber,
    PrekeyBundleSignature,
    X3dhRootKey,
    RatchetRootKey,
    RatchetChainKey,
    RatchetMessageKey,
    EnvelopeEncryptionKey,
    EnvelopeSignature,
    DeliveryAcknowledgementSignature,
    MailboxCapability,
    RelayStorageReceiptSignature,
    AttachmentKey,
    AttachmentChunkKey,
    ProfileBinding,
}

impl CryptoDomain {
    pub const ALL: &[Self] = &[
        Self::IdentityIdentifier,
        Self::IdentityExportKey,
        Self::ContactInvitationSignature,
        Self::RelayInvitationSignature,
        Self::SafetyNumber,
        Self::PrekeyBundleSignature,
        Self::X3dhRootKey,
        Self::RatchetRootKey,
        Self::RatchetChainKey,
        Self::RatchetMessageKey,
        Self::EnvelopeEncryptionKey,
        Self::EnvelopeSignature,
        Self::DeliveryAcknowledgementSignature,
        Self::MailboxCapability,
        Self::RelayStorageReceiptSignature,
        Self::AttachmentKey,
        Self::AttachmentChunkKey,
        Self::ProfileBinding,
    ];

    #[must_use]
    pub fn context(self) -> &'static [u8] {
        let context: &[u8] = match self {
            Self::IdentityIdentifier => b"yeokcham/v1/identity-identifier",
            Self::IdentityExportKey => b"yeokcham/v1/identity-export-key",
            Self::ContactInvitationSignature => b"yeokcham/v1/contact-invitation-signature",
            Self::RelayInvitationSignature => b"yeokcham/v1/relay-invitation-signature",
            Self::SafetyNumber => b"yeokcham/v1/safety-number",
            Self::PrekeyBundleSignature => b"yeokcham/v1/prekey-bundle-signature",
            Self::X3dhRootKey => b"yeokcham/v1/x3dh-root-key",
            Self::RatchetRootKey => b"yeokcham/v1/ratchet-root-key",
            Self::RatchetChainKey => b"yeokcham/v1/ratchet-chain-key",
            Self::RatchetMessageKey => b"yeokcham/v1/ratchet-message-key",
            Self::EnvelopeEncryptionKey => b"yeokcham/v1/envelope-encryption-key",
            Self::EnvelopeSignature => b"yeokcham/v1/envelope-signature",
            Self::DeliveryAcknowledgementSignature => {
                b"yeokcham/v1/delivery-acknowledgement-signature"
            }
            Self::MailboxCapability => b"yeokcham/v1/mailbox-capability",
            Self::RelayStorageReceiptSignature => b"yeokcham/v1/relay-storage-receipt-signature",
            Self::AttachmentKey => b"yeokcham/v1/attachment-key",
            Self::AttachmentChunkKey => b"yeokcham/v1/attachment-chunk-key",
            Self::ProfileBinding => b"yeokcham/v1/profile-binding",
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
            b"yeokcham/v1/envelope-signature"
        );
    }
}
