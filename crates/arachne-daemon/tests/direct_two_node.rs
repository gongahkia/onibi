#![allow(clippy::similar_names)]

use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
    time::Duration,
};

use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    DeliveryState, DirectConnectionAttempts, DirectTransport, InboxDeduplicationResult,
    MessageExpiry, RecipientInboxDeduplication, SenderOutbox,
};
use arachne_protocol::{
    DeliveryAcknowledgement, DirectProfileConfig, EncryptedMessageEnvelope, EnvelopeKind,
    ProtocolVersion, WireEnvelope, WireLimits,
};
use quinn::{
    ClientConfig, ServerConfig, rustls::RootCertStore, rustls::pki_types::PrivatePkcs8KeyDer,
};
use rcgen::generate_simple_self_signed;

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

fn database_path(name: &str) -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "arachne-direct-two-node-{name}-{}-{number}.sqlite",
        std::process::id()
    ))
}

fn server_config() -> (
    ServerConfig,
    quinn::rustls::pki_types::CertificateDer<'static>,
) {
    let certificate = generate_simple_self_signed(vec!["node-b".to_owned()]).unwrap();
    let certificate_der = certificate.cert.der().clone();
    let private_key = PrivatePkcs8KeyDer::from(certificate.signing_key.serialize_der());
    let server_config =
        ServerConfig::with_single_cert(vec![certificate_der.clone()], private_key.into()).unwrap();
    (server_config, certificate_der)
}

fn client_config(certificate: quinn::rustls::pki_types::CertificateDer<'static>) -> ClientConfig {
    let mut roots = RootCertStore::empty();
    roots.add(certificate).unwrap();
    ClientConfig::with_root_certificates(Arc::new(roots)).unwrap()
}

#[tokio::test]
async fn two_nodes_authenticate_and_exchange_direct_frames() {
    let (node_b_tls, certificate) = server_config();
    let node_b = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), Some(node_b_tls)).unwrap();
    let node_a = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), None).unwrap();
    let node_b_profile = DirectProfileConfig::new(node_b.local_address().unwrap()).unwrap();
    let attempts = DirectConnectionAttempts::new(1, Duration::from_secs(1)).unwrap();

    let (node_b_connection, node_a_connection) = tokio::join!(
        node_b.accept(),
        node_a.connect_with_attempts(
            node_b_profile,
            client_config(certificate),
            "node-b",
            attempts
        )
    );
    let node_b_connection = node_b_connection.unwrap();
    let node_a_connection = node_a_connection.unwrap();
    let node_a_identity = IdentityKeypair::generate().unwrap();
    let node_b_identity = IdentityKeypair::generate().unwrap();
    let node_a_public = node_a_identity.public_key();
    let node_b_public = node_b_identity.public_key();
    let (node_b_auth, node_a_auth) = tokio::join!(
        node_b_connection.authenticate_responder(&node_b_identity, &node_a_public),
        node_a_connection.authenticate_initiator(&node_a_identity, &node_b_public)
    );
    assert!(node_a_auth.is_ok());
    assert!(node_b_auth.is_ok());

    let node_a_frame = WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload: b"node-a-to-node-b".to_vec(),
    };
    let node_b_frame = WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload: b"node-b-to-node-a".to_vec(),
    };
    let ((a_sent, b_sent), (a_received, b_received)) = tokio::join!(
        async {
            tokio::join!(
                node_a_connection.send_frame(&node_a_frame, WireLimits::REFERENCE),
                node_b_connection.send_frame(&node_b_frame, WireLimits::REFERENCE)
            )
        },
        async {
            tokio::join!(
                node_a_connection.receive_frame(WireLimits::REFERENCE),
                node_b_connection.receive_frame(WireLimits::REFERENCE)
            )
        }
    );
    assert!(a_sent.is_ok());
    assert!(b_sent.is_ok());
    assert_eq!(a_received.unwrap(), node_b_frame);
    assert_eq!(b_received.unwrap(), node_a_frame);

    node_a_connection.close();
    node_b_connection.close();
    node_a.shutdown();
    node_b.shutdown();
    node_a.wait_idle().await;
    node_b.wait_idle().await;
}

#[tokio::test]
#[allow(clippy::too_many_lines)]
async fn two_node_demo_delivers_a_deduplicated_message_and_signed_acknowledgement() {
    let outbox_path = database_path("outbox");
    let inbox_path = database_path("inbox");
    let mut outbox_keystore = MemoryKeystore::default();
    let mut inbox_keystore = MemoryKeystore::default();
    let node_a_identity = IdentityKeypair::generate().unwrap();
    let node_b_identity = IdentityKeypair::generate().unwrap();
    let envelope = EncryptedMessageEnvelope::new(vec![0xa1], b"two-node demo".to_vec()).unwrap();
    let mut outbox = SenderOutbox::open(&outbox_path, &mut outbox_keystore).unwrap();
    outbox
        .enqueue(
            node_b_identity.public_key(),
            envelope.clone(),
            MessageExpiry::new(100, 3_600).unwrap(),
        )
        .unwrap();
    let identifier = outbox.next().unwrap().identifier();
    let delivery = WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload: envelope.encode().unwrap(),
    };

    let (node_b_tls, certificate) = server_config();
    let node_b = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), Some(node_b_tls)).unwrap();
    let node_a = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), None).unwrap();
    let node_b_profile = DirectProfileConfig::new(node_b.local_address().unwrap()).unwrap();
    let attempts = DirectConnectionAttempts::new(1, Duration::from_secs(1)).unwrap();
    let (node_b_connection, node_a_connection) = tokio::join!(
        node_b.accept(),
        node_a.connect_with_attempts(
            node_b_profile,
            client_config(certificate),
            "node-b",
            attempts
        )
    );
    let node_b_connection = node_b_connection.unwrap();
    let node_a_connection = node_a_connection.unwrap();
    let node_a_public = node_a_identity.public_key();
    let node_b_public = node_b_identity.public_key();
    let (node_b_auth, node_a_auth) = tokio::join!(
        node_b_connection.authenticate_responder(&node_b_identity, &node_a_public),
        node_a_connection.authenticate_initiator(&node_a_identity, &node_b_public)
    );
    assert!(node_a_auth.is_ok());
    assert!(node_b_auth.is_ok());

    let (delivery_sent, delivered_frame) = tokio::join!(
        node_a_connection.send_frame(&delivery, WireLimits::REFERENCE),
        node_b_connection.receive_frame(WireLimits::REFERENCE)
    );
    assert!(delivery_sent.is_ok());
    let delivered_frame = delivered_frame.unwrap();
    assert_eq!(delivered_frame.kind, EnvelopeKind::EncryptedMessage);
    let delivered = EncryptedMessageEnvelope::decode(&delivered_frame.payload).unwrap();
    assert_eq!(delivered, envelope);
    let mut inbox = RecipientInboxDeduplication::open(&inbox_path, &mut inbox_keystore).unwrap();
    assert_eq!(
        inbox.record(&delivered).unwrap(),
        InboxDeduplicationResult::Accepted
    );

    let (retry_sent, retry_frame) = tokio::join!(
        node_a_connection.send_frame(&delivery, WireLimits::REFERENCE),
        node_b_connection.receive_frame(WireLimits::REFERENCE)
    );
    assert!(retry_sent.is_ok());
    let retried = EncryptedMessageEnvelope::decode(&retry_frame.unwrap().payload).unwrap();
    assert_eq!(
        inbox.record(&retried).unwrap(),
        InboxDeduplicationResult::Duplicate
    );

    let acknowledgement =
        DeliveryAcknowledgement::create(&node_b_identity, identifier, 101).unwrap();
    let acknowledgement_frame = WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::DeliveryAcknowledgement,
        payload: acknowledgement.encode().unwrap(),
    };
    let (acknowledgement_sent, acknowledgement_frame) = tokio::join!(
        node_b_connection.send_frame(&acknowledgement_frame, WireLimits::REFERENCE),
        node_a_connection.receive_frame(WireLimits::REFERENCE)
    );
    assert!(acknowledgement_sent.is_ok());
    let acknowledgement_frame = acknowledgement_frame.unwrap();
    assert_eq!(
        acknowledgement_frame.kind,
        EnvelopeKind::DeliveryAcknowledgement
    );
    let acknowledgement = DeliveryAcknowledgement::decode(&acknowledgement_frame.payload).unwrap();
    assert_eq!(
        outbox
            .acknowledge_delivery(&acknowledgement)
            .unwrap()
            .identifier(),
        identifier
    );
    assert!(outbox.messages().is_empty());
    assert_eq!(
        outbox.delivery_state(identifier),
        Some(DeliveryState::Delivered)
    );

    node_a_connection.close();
    node_b_connection.close();
    node_a.shutdown();
    node_b.shutdown();
    node_a.wait_idle().await;
    node_b.wait_idle().await;
    drop(inbox);
    drop(outbox);
    fs::remove_file(inbox_path).unwrap();
    fs::remove_file(outbox_path).unwrap();
}
