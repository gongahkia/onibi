#![allow(clippy::similar_names)]

mod support;

use std::{sync::Arc, time::Duration};

use arachne_core::IdentityKeypair;
use arachne_daemon::{
    AuthenticatedLocalMeshSession, DirectConnectionAttempts, DirectTransport,
    LocalMeshSessionError, LocalTransportAvailability, WifiDirectTransport, WifiHotspotTransport,
};
use arachne_protocol::{DirectProfileConfig, LocalMeshPeer, LocalMeshTransportKind};
use quinn::{
    ClientConfig, ServerConfig,
    rustls::{RootCertStore, pki_types::PrivatePkcs8KeyDer},
};
use rcgen::generate_simple_self_signed;
use support::assert_transport_conformance;

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

async fn establish_sessions(
    transport: LocalMeshTransportKind,
    node_a_identity: &IdentityKeypair,
    node_b_identity: &IdentityKeypair,
) -> (
    AuthenticatedLocalMeshSession,
    AuthenticatedLocalMeshSession,
    DirectTransport,
    DirectTransport,
    quinn::rustls::pki_types::CertificateDer<'static>,
) {
    let (node_b_tls, certificate) = server_config();
    let node_b = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), Some(node_b_tls)).unwrap();
    let node_a = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), None).unwrap();
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
            node_b_identity,
        ),
        AuthenticatedLocalMeshSession::initiate(
            &node_a,
            LocalTransportAvailability::Available,
            node_b_peer,
            node_a_identity,
            client_config(certificate.clone()),
            "node-b",
            attempts,
        )
    );
    (
        node_a_session.unwrap(),
        node_b_session.unwrap(),
        node_a,
        node_b,
        certificate,
    )
}

#[tokio::test]
async fn authenticated_wifi_direct_sessions_reject_route_substitution_then_handoff() {
    let node_a_identity = IdentityKeypair::generate().unwrap();
    let node_b_identity = IdentityKeypair::generate().unwrap();
    let (mut node_a_session, mut node_b_session, node_a, node_b, _) = establish_sessions(
        LocalMeshTransportKind::WifiDirect,
        &node_a_identity,
        &node_b_identity,
    )
    .await;
    assert!(matches!(
        node_a_session.handoff_for(LocalMeshTransportKind::WifiHotspot),
        Err(LocalMeshSessionError::TransportMismatch {
            actual: LocalMeshTransportKind::WifiDirect,
            expected: LocalMeshTransportKind::WifiHotspot,
        })
    ));
    assert!(node_a_session.is_active());
    let node_a_adapter = WifiDirectTransport::from_authenticated_handoff(
        node_a_session
            .handoff_for(LocalMeshTransportKind::WifiDirect)
            .unwrap(),
    )
    .unwrap();
    let node_b_adapter = WifiDirectTransport::from_authenticated_handoff(
        node_b_session
            .handoff_for(LocalMeshTransportKind::WifiDirect)
            .unwrap(),
    )
    .unwrap();
    assert_transport_conformance(
        &node_a_adapter,
        &node_b_adapter,
        LocalMeshTransportKind::WifiDirect,
    )
    .await;
    node_a_adapter.connection().close();
    node_b_adapter.connection().close();
    node_a.shutdown();
    node_b.shutdown();
    node_a.wait_idle().await;
    node_b.wait_idle().await;
}

#[tokio::test]
async fn hotspot_sessions_cancel_and_reconnect_with_the_same_authenticated_peer() {
    let node_a_identity = IdentityKeypair::generate().unwrap();
    let node_b_identity = IdentityKeypair::generate().unwrap();
    let (mut node_a_session, mut node_b_session, node_a, node_b, certificate) = establish_sessions(
        LocalMeshTransportKind::WifiHotspot,
        &node_a_identity,
        &node_b_identity,
    )
    .await;
    node_a_session.cancel();
    assert!(!node_a_session.is_active());
    assert!(matches!(
        node_a_session.handoff_for(LocalMeshTransportKind::WifiHotspot),
        Err(LocalMeshSessionError::Inactive)
    ));
    node_b_session.cancel();
    let attempts = DirectConnectionAttempts::new(1, Duration::from_secs(1)).unwrap();
    let (node_b_reconnect, node_a_reconnect) = tokio::join!(
        node_b_session.reconnect_responder(
            &node_b,
            LocalTransportAvailability::Available,
            &node_b_identity,
        ),
        node_a_session.reconnect_initiator(
            &node_a,
            LocalTransportAvailability::Available,
            &node_a_identity,
            client_config(certificate),
            "node-b",
            attempts,
        )
    );
    node_a_reconnect.unwrap();
    node_b_reconnect.unwrap();
    let node_a_adapter = WifiHotspotTransport::from_authenticated_handoff(
        node_a_session
            .handoff_for(LocalMeshTransportKind::WifiHotspot)
            .unwrap(),
    )
    .unwrap();
    let node_b_adapter = WifiHotspotTransport::from_authenticated_handoff(
        node_b_session
            .handoff_for(LocalMeshTransportKind::WifiHotspot)
            .unwrap(),
    )
    .unwrap();
    assert_transport_conformance(
        &node_a_adapter,
        &node_b_adapter,
        LocalMeshTransportKind::WifiHotspot,
    )
    .await;
    node_a_adapter.connection().close();
    node_b_adapter.connection().close();
    node_a.shutdown();
    node_b.shutdown();
    node_a.wait_idle().await;
    node_b.wait_idle().await;
}

#[tokio::test]
async fn direct_handoff_fails_closed_for_denied_permission_and_wrong_identity() {
    let (node_b_tls, certificate) = server_config();
    let node_b = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), Some(node_b_tls)).unwrap();
    let node_a = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), None).unwrap();
    let node_a_identity = IdentityKeypair::generate().unwrap();
    let node_b_identity = IdentityKeypair::generate().unwrap();
    let wrong_identity = IdentityKeypair::generate().unwrap();
    let peer = LocalMeshPeer::new(
        LocalMeshTransportKind::WifiDirect,
        wrong_identity.public_key(),
        Some(DirectProfileConfig::new(node_b.local_address().unwrap()).unwrap()),
    )
    .unwrap();
    assert!(matches!(
        AuthenticatedLocalMeshSession::initiate(
            &node_a,
            LocalTransportAvailability::PermissionDenied,
            peer,
            &node_a_identity,
            client_config(certificate.clone()),
            "node-b",
            DirectConnectionAttempts::new(1, Duration::from_secs(1)).unwrap(),
        )
        .await,
        Err(LocalMeshSessionError::Unavailable(_))
    ));
    let node_a_peer = LocalMeshPeer::new(
        LocalMeshTransportKind::WifiDirect,
        node_a_identity.public_key(),
        Some(DirectProfileConfig::new(node_a.local_address().unwrap()).unwrap()),
    )
    .unwrap();
    let attempts = DirectConnectionAttempts::new(1, Duration::from_secs(1)).unwrap();
    let (node_b_result, node_a_result) = tokio::join!(
        AuthenticatedLocalMeshSession::respond(
            &node_b,
            LocalTransportAvailability::Available,
            node_a_peer,
            &node_b_identity,
        ),
        AuthenticatedLocalMeshSession::initiate(
            &node_a,
            LocalTransportAvailability::Available,
            peer,
            &node_a_identity,
            client_config(certificate),
            "node-b",
            attempts,
        )
    );
    assert!(node_b_result.is_ok());
    assert!(matches!(
        node_a_result,
        Err(LocalMeshSessionError::Direct(_))
    ));
    node_a.shutdown();
    node_b.shutdown();
    node_a.wait_idle().await;
    node_b.wait_idle().await;
}
