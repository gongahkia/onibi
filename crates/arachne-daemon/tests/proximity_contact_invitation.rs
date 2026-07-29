mod support;

use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    ContactStatus, ContactStore, ContactStoreError, LocalTransport,
    ProximityContactInvitationExchange, ProximityContactInvitationExchangeError,
};
use arachne_protocol::{
    ContactInvitation, ContactInvitationError, EnvelopeKind, LocalMeshTransportKind,
    ProtocolVersion, WireEnvelope, WireLimits,
};
use support::InMemoryTransport;

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

#[derive(Default)]
struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

impl OsKeystore for MemoryKeystore {
    type Error = Infallible;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .0
            .get(entry.as_str())
            .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error> {
        self.0
            .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
        Ok(())
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
        self.0.remove(entry.as_str());
        Ok(())
    }
}

fn database_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "arachne-proximity-contact-invitation-{}-{number}.sqlite",
        std::process::id()
    ))
}

#[tokio::test]
async fn proximity_exchange_imports_a_signed_invitation_as_pending() {
    let path = database_path();
    let mut keystore = MemoryKeystore::default();
    let mut recipient_contacts = ContactStore::open(&path, &mut keystore).unwrap();
    let inviter_identity = IdentityKeypair::generate().unwrap();
    let recipient_identity = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&inviter_identity).unwrap();
    let (inviter_transport, recipient_transport) =
        InMemoryTransport::pair(LocalMeshTransportKind::Lan);

    ProximityContactInvitationExchange::send(
        &inviter_transport,
        &invitation,
        WireLimits::REFERENCE,
    )
    .await
    .unwrap();
    let contact = ProximityContactInvitationExchange::receive(
        &recipient_transport,
        &recipient_identity.public_key(),
        &mut recipient_contacts,
        WireLimits::REFERENCE,
    )
    .await
    .unwrap();

    assert_eq!(contact.identity(), &inviter_identity.public_key());
    assert_eq!(contact.status(), ContactStatus::Pending);
    assert_eq!(recipient_contacts.contacts(), &[contact]);
    drop(recipient_contacts);
    fs::remove_file(path).unwrap();
}

#[tokio::test]
async fn proximity_exchange_rejects_unexpected_malformed_and_self_invitations() {
    let path = database_path();
    let mut keystore = MemoryKeystore::default();
    let mut recipient_contacts = ContactStore::open(&path, &mut keystore).unwrap();
    let recipient_identity = IdentityKeypair::generate().unwrap();
    let (sender_transport, recipient_transport) =
        InMemoryTransport::pair(LocalMeshTransportKind::WifiDirect);

    sender_transport
        .send_frame(
            &WireEnvelope {
                version: ProtocolVersion::INITIAL,
                kind: EnvelopeKind::EncryptedMessage,
                payload: vec![0xa1],
            },
            WireLimits::REFERENCE,
        )
        .await
        .unwrap();
    assert!(matches!(
        ProximityContactInvitationExchange::receive(
            &recipient_transport,
            &recipient_identity.public_key(),
            &mut recipient_contacts,
            WireLimits::REFERENCE,
        )
        .await,
        Err(
            ProximityContactInvitationExchangeError::UnexpectedEnvelopeKind(
                EnvelopeKind::EncryptedMessage
            )
        )
    ));
    sender_transport
        .send_frame(
            &WireEnvelope {
                version: ProtocolVersion::INITIAL,
                kind: EnvelopeKind::ContactInvitation,
                payload: vec![0xa1],
            },
            WireLimits::REFERENCE,
        )
        .await
        .unwrap();
    assert!(matches!(
        ProximityContactInvitationExchange::receive(
            &recipient_transport,
            &recipient_identity.public_key(),
            &mut recipient_contacts,
            WireLimits::REFERENCE,
        )
        .await,
        Err(ProximityContactInvitationExchangeError::Invitation(
            ContactInvitationError::InvalidLength
        ))
    ));
    let self_invitation = ContactInvitation::create(&recipient_identity).unwrap();
    ProximityContactInvitationExchange::send(
        &sender_transport,
        &self_invitation,
        WireLimits::REFERENCE,
    )
    .await
    .unwrap();
    assert!(matches!(
        ProximityContactInvitationExchange::receive(
            &recipient_transport,
            &recipient_identity.public_key(),
            &mut recipient_contacts,
            WireLimits::REFERENCE,
        )
        .await,
        Err(ProximityContactInvitationExchangeError::ContactStore(
            ContactStoreError::SelfContact
        ))
    ));
    assert!(recipient_contacts.contacts().is_empty());
    drop(recipient_contacts);
    fs::remove_file(path).unwrap();
}
