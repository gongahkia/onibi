#![allow(clippy::similar_names, clippy::struct_field_names)]

use std::{sync::Arc, time::Duration};

use quinn::{
    ClientConfig, ServerConfig,
    rustls::{RootCertStore, pki_types::PrivatePkcs8KeyDer},
};
use rcgen::generate_simple_self_signed;
use yeokcham_core::IdentityKeypair;
use yeokcham_daemon::{
    AuthenticatedLocalMeshSession, DirectConnectionAttempts, DirectTransport,
    LocalTransportAvailability,
};
use yeokcham_protocol::{DirectProfileConfig, LocalMeshPeer, LocalMeshTransportKind};

pub struct AuthenticatedSessionPair {
    pub node_a_session: AuthenticatedLocalMeshSession,
    pub node_b_session: AuthenticatedLocalMeshSession,
    node_a: DirectTransport,
    node_b: DirectTransport,
}

impl AuthenticatedSessionPair {
    pub async fn shutdown(self) {
        self.node_a.shutdown();
        self.node_b.shutdown();
        self.node_a.wait_idle().await;
        self.node_b.wait_idle().await;
    }
}

pub async fn establish_authenticated_sessions(
    transport: LocalMeshTransportKind,
) -> AuthenticatedSessionPair {
    let (node_b_tls, certificate) = server_config();
    let node_b = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), Some(node_b_tls)).unwrap();
    let node_a = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), None).unwrap();
    let node_a_identity = IdentityKeypair::generate().unwrap();
    let node_b_identity = IdentityKeypair::generate().unwrap();
    let node_a_peer = LocalMeshPeer::new(
        transport,
        node_a_identity.public_key(),
        Some(DirectProfileConfig::new(node_a.local_address().unwrap()).unwrap()),
    )
    .unwrap();
    let node_b_peer = LocalMeshPeer::new(
        transport,
        node_b_identity.public_key(),
        Some(DirectProfileConfig::new(node_b.local_address().unwrap()).unwrap()),
    )
    .unwrap();
    let attempts = DirectConnectionAttempts::new(1, Duration::from_secs(1)).unwrap();
    let (node_b_session, node_a_session) = tokio::join!(
        AuthenticatedLocalMeshSession::respond(
            &node_b,
            LocalTransportAvailability::Available,
            node_a_peer,
            &node_b_identity,
        ),
        AuthenticatedLocalMeshSession::initiate(
            &node_a,
            LocalTransportAvailability::Available,
            node_b_peer,
            &node_a_identity,
            client_config(certificate),
            "node-b",
            attempts,
        )
    );
    AuthenticatedSessionPair {
        node_a_session: node_a_session.unwrap(),
        node_b_session: node_b_session.unwrap(),
        node_a,
        node_b,
    }
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
