#![allow(clippy::similar_names)]

mod support;

use std::{sync::Arc, time::Duration};

use quinn::{
    ClientConfig, ServerConfig,
    rustls::{RootCertStore, pki_types::PrivatePkcs8KeyDer},
};
use rcgen::generate_simple_self_signed;
use support::assert_transport_conformance;
use yeokcham_core::IdentityKeypair;
use yeokcham_daemon::{
    DirectConnectionAttempts, DirectTransport, LanDirectTransport, LocalTransport,
};
use yeokcham_protocol::{
    DirectProfileConfig, EnvelopeKind, ProtocolVersion, WireEnvelope, WireLimits,
};

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
async fn authenticated_lan_adapters_conform_over_direct_quic() {
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
    let (node_b_authentication, node_a_authentication) = tokio::join!(
        node_b_connection.authenticate_responder(&node_b_identity, &node_a_public),
        node_a_connection.authenticate_initiator(&node_a_identity, &node_b_public)
    );
    node_a_authentication.unwrap();
    node_b_authentication.unwrap();
    let node_a_adapter = LanDirectTransport::new(node_a_connection);
    let node_b_adapter = LanDirectTransport::new(node_b_connection);
    assert!(
        node_a_adapter
            .send_frame(
                &WireEnvelope {
                    version: ProtocolVersion::INITIAL,
                    kind: EnvelopeKind::EncryptedMessage,
                    payload: vec![0xa1, 0xb2],
                },
                WireLimits::new(64, 1).unwrap(),
            )
            .await
            .is_err()
    );
    assert_transport_conformance(
        &node_a_adapter,
        &node_b_adapter,
        yeokcham_protocol::LocalMeshTransportKind::Lan,
    )
    .await;
    node_a_adapter.connection().close();
    node_b_adapter.connection().close();
    node_a.shutdown();
    node_b.shutdown();
    node_a.wait_idle().await;
    node_b.wait_idle().await;
}
