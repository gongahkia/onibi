mod support;

use arachne_daemon::LocalTransport;
use arachne_protocol::{
    EnvelopeKind, LocalMeshTransportKind, ProtocolVersion, WireEnvelope, WireLimits,
};
use support::{InMemoryTransport, InMemoryTransportError, assert_transport_conformance};

fn frame(payload: Vec<u8>) -> WireEnvelope {
    WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload,
    }
}

#[tokio::test]
async fn in_memory_transport_conforms_for_every_local_mesh_kind() {
    for kind in [
        LocalMeshTransportKind::Lan,
        LocalMeshTransportKind::WifiHotspot,
        LocalMeshTransportKind::WifiDirect,
        LocalMeshTransportKind::Bluetooth,
    ] {
        let (node_a, node_b) = InMemoryTransport::pair(kind);
        assert_transport_conformance(&node_a, &node_b, kind).await;
    }
}

#[tokio::test]
async fn in_memory_transport_fails_closed_for_invalid_frames_and_disconnects() {
    let (node_a, node_b) = InMemoryTransport::pair(LocalMeshTransportKind::Lan);
    let limits = WireLimits::new(64, 1).unwrap();
    assert_eq!(
        node_a.send_frame(&frame(vec![0xa1, 0xb2]), limits).await,
        Err(InMemoryTransportError::InvalidFrame)
    );
    assert_eq!(node_b.pending_frames().await, 0);
    node_a.inject(vec![0x9f]).await.unwrap();
    assert_eq!(
        node_b.receive_frame(WireLimits::REFERENCE).await,
        Err(InMemoryTransportError::InvalidFrame)
    );
    drop(node_a);
    assert_eq!(
        node_b.receive_frame(WireLimits::REFERENCE).await,
        Err(InMemoryTransportError::Disconnected)
    );
}
