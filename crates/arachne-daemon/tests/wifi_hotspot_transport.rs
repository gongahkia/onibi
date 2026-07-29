#![allow(clippy::similar_names)]

mod support;

use arachne_daemon::{LocalTransport, WifiHotspotTransport};
use arachne_protocol::{
    EnvelopeKind, LocalMeshTransportKind, ProtocolVersion, WireEnvelope, WireLimits,
};
use support::{assert_transport_conformance, establish_authenticated_sessions};

#[tokio::test]
async fn authenticated_wifi_hotspot_adapters_conform_over_direct_quic() {
    let mut sessions = establish_authenticated_sessions(LocalMeshTransportKind::WifiHotspot).await;
    let node_a_adapter = WifiHotspotTransport::from_authenticated_handoff(
        sessions
            .node_a_session
            .handoff_for(LocalMeshTransportKind::WifiHotspot)
            .unwrap(),
    )
    .unwrap();
    let node_b_adapter = WifiHotspotTransport::from_authenticated_handoff(
        sessions
            .node_b_session
            .handoff_for(LocalMeshTransportKind::WifiHotspot)
            .unwrap(),
    )
    .unwrap();
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
        LocalMeshTransportKind::WifiHotspot,
    )
    .await;
    node_a_adapter.connection().close();
    node_b_adapter.connection().close();
    sessions.shutdown().await;
}
