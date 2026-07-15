mod support;

use support::{InMemoryBluetoothTransport, InMemoryTransportError, assert_transport_conformance};
use yeokcham_daemon::{BluetoothTransport, LocalTransport};
use yeokcham_protocol::{
    EnvelopeKind, LocalMeshTransportKind, ProtocolVersion, WireEnvelope, WireLimits,
};

fn frame(payload: Vec<u8>) -> WireEnvelope {
    WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload,
    }
}

#[tokio::test]
async fn bluetooth_harness_exchanges_frames_over_the_bluetooth_profile() {
    let (node_a, node_b) = InMemoryBluetoothTransport::pair();
    assert!(node_a.verify_bluetooth_kind().is_ok());
    assert!(node_b.verify_bluetooth_kind().is_ok());
    assert_transport_conformance(&node_a, &node_b, LocalMeshTransportKind::Bluetooth).await;
}

#[tokio::test]
async fn bluetooth_harness_fails_closed_for_invalid_frames_and_disconnects() {
    let (node_a, node_b) = InMemoryBluetoothTransport::pair();
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
