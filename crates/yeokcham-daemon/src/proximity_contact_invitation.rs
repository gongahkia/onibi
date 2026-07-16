use yeokcham_core::IdentityPublicKey;
use yeokcham_protocol::{
    ContactInvitation, ContactInvitationError, EnvelopeKind, ProtocolVersion, WireEnvelope,
    WireLimits,
};

use crate::{
    Contact, ContactStore, ContactStoreError, LocalTransport, PendingContactImportError,
    PendingContactImportService,
};

pub struct ProximityContactInvitationExchange;

impl ProximityContactInvitationExchange {
    pub async fn send<T: LocalTransport>(
        transport: &T,
        invitation: &ContactInvitation,
        limits: WireLimits,
    ) -> Result<(), ProximityContactInvitationExchangeError<T::Error>> {
        let payload = invitation
            .encode()
            .map_err(ProximityContactInvitationExchangeError::Invitation)?;
        transport
            .send_frame(
                &WireEnvelope {
                    version: ProtocolVersion::INITIAL,
                    kind: EnvelopeKind::ContactInvitation,
                    payload,
                },
                limits,
            )
            .await
            .map_err(ProximityContactInvitationExchangeError::Transport)
    }

    pub async fn receive<T: LocalTransport>(
        transport: &T,
        local_identity: &IdentityPublicKey,
        contacts: &mut ContactStore,
        limits: WireLimits,
    ) -> Result<Contact, ProximityContactInvitationExchangeError<T::Error>> {
        let frame = transport
            .receive_frame(limits)
            .await
            .map_err(ProximityContactInvitationExchangeError::Transport)?;
        if frame.kind != EnvelopeKind::ContactInvitation {
            return Err(
                ProximityContactInvitationExchangeError::UnexpectedEnvelopeKind(frame.kind),
            );
        }
        PendingContactImportService::new(local_identity, contacts)
            .import_encoded(&frame.payload)
            .map_err(|error| match error {
                PendingContactImportError::Invitation(error) => {
                    ProximityContactInvitationExchangeError::Invitation(error)
                }
                PendingContactImportError::ContactStore(error) => {
                    ProximityContactInvitationExchangeError::ContactStore(error)
                }
            })
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ProximityContactInvitationExchangeError<E> {
    #[error("local transport exchange failed")]
    Transport(E),
    #[error("contact invitation is invalid")]
    Invitation(#[source] ContactInvitationError),
    #[error("contact-store import failed")]
    ContactStore(#[source] ContactStoreError),
    #[error("unexpected proximity envelope kind: {0:?}")]
    UnexpectedEnvelopeKind(EnvelopeKind),
}
